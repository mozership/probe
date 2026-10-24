package actionrpc

import (
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strings"

	"github.com/mozership/probe/pb"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Guard is what a run allows its actions to do. It is set by whoever runs
// probe, on the command line, rather than by the workflow, so that a
// workflow cannot loosen it. The zero Guard allows everything.
//
// A guard keeps a workflow from writing to, or reaching, what it was not
// meant to; it is not a sandbox. An action keeps to it only as far as the
// action can tell what it is about to do.
type Guard struct {
	// ReadOnly allows only what reads, and nothing that writes, such as an
	// HTTP POST or an UPDATE.
	ReadOnly bool
	// AllowHosts are the hosts actions may connect to: a name or an address,
	// with a port or without one for any port, or *.example.com for the
	// names under example.com. Empty allows any host.
	AllowHosts []string
	// AllowActions are the actions run under the guard although they do not
	// keep to it, as the person running probe asks.
	AllowActions []string
	// Keeps holds the kinds of guard each action says it keeps to itself,
	// keyed by the action as a step's uses names it: a built-in action by
	// what its package declares, an external one by its action.yml. An
	// action is refused under a guard it does not keep to every kind of,
	// unless AllowActions names it.
	Keeps map[string][]string
}

// The kinds of guard, as an action declares the ones it keeps to.
const (
	// KindReadOnly is ReadOnly: the action refuses what writes.
	KindReadOnly = "read-only"
	// KindAllowHost is AllowHosts: the action refuses a host the guard does
	// not allow.
	KindAllowHost = "allow-host"
)

// Active reports whether the guard limits anything.
func (g Guard) Active() bool {
	return g.ReadOnly || len(g.AllowHosts) > 0
}

// Kinds returns the kinds of guard that limit the run, in the order of the
// constants.
func (g Guard) Kinds() []string {
	var kinds []string
	if g.ReadOnly {
		kinds = append(kinds, KindReadOnly)
	}
	if len(g.AllowHosts) > 0 {
		kinds = append(kinds, KindAllowHost)
	}
	return kinds
}

// Runs reports whether the action named uses may run under the guard: it
// keeps to every kind of guard that limits the run, or the person running
// probe allowed it.
func (g Guard) Runs(uses string) bool {
	if slices.Contains(g.AllowActions, uses) {
		return true
	}
	return len(g.Missing(uses)) == 0
}

// Missing returns the kinds of guard that limit the run and that the action
// named uses does not keep to.
func (g Guard) Missing(uses string) []string {
	var missing []string
	for _, kind := range g.Kinds() {
		if !slices.Contains(g.Keeps[uses], kind) {
			missing = append(missing, kind)
		}
	}
	return missing
}

// WithKeeps returns a copy of the guard that also holds that the action
// named uses keeps to kinds, leaving the guard it was made from as it was.
func (g Guard) WithKeeps(uses string, kinds []string) Guard {
	keeps := make(map[string][]string, len(g.Keeps)+1)
	maps.Copy(keeps, g.Keeps)
	keeps[uses] = slices.Clone(kinds)
	g.Keeps = keeps
	return g
}

// AllowsHost reports whether the guard allows connecting to hostport, a
// host with or without a port.
func (g Guard) AllowsHost(hostport string) bool {
	if len(g.AllowHosts) == 0 {
		return true
	}
	host, port := splitHostPort(hostport)
	for _, allowed := range g.AllowHosts {
		h, p := splitHostPort(allowed)
		if p != "" && p != port {
			continue
		}
		if matchHost(h, host) {
			return true
		}
	}
	return false
}

// CheckHost returns a Refused error when the guard does not allow
// connecting to hostport.
func (g Guard) CheckHost(hostport string) error {
	if g.AllowsHost(hostport) {
		return nil
	}
	return Refuse("the host %s is not one the run allows (%s)", hostport, strings.Join(g.AllowHosts, ", "))
}

// splitHostPort splits s into its host, lowercased and without brackets,
// and its port, which is empty when s has none.
func splitHostPort(s string) (string, string) {
	if host, port, err := net.SplitHostPort(s); err == nil {
		return strings.ToLower(host), port
	}
	return strings.ToLower(strings.Trim(s, "[]")), ""
}

func matchHost(pattern, host string) bool {
	if suffix, ok := strings.CutPrefix(pattern, "*."); ok {
		return strings.HasSuffix(host, "."+suffix)
	}
	return pattern == host
}

// Refused is the error an action returns for what the guard of the run
// does not allow. The runner tells it from other errors of an action.
type Refused struct {
	Reason string
}

func (r *Refused) Error() string {
	return "refused: " + r.Reason
}

// Refuse returns a Refused error with the reason formatted.
func Refuse(format string, args ...any) error {
	return &Refused{Reason: fmt.Sprintf(format, args...)}
}

// IsRefused reports whether err is, or wraps, a Refused error.
func IsRefused(err error) bool {
	var r *Refused
	return errors.As(err, &r)
}

// refusedInfo marks the gRPC status a Refused error is carried in, apart
// from a PermissionDenied an action passes on from a server it called.
var refusedInfo = &errdetails.ErrorInfo{Domain: "probe", Reason: "GUARD_REFUSED"}

// toStatus turns a Refused error into the gRPC status that carries it to
// the runner, marked as a refusal, and leaves any other error as it is.
func toStatus(err error) error {
	var r *Refused
	if !errors.As(err, &r) {
		return err
	}
	s, detailErr := status.New(codes.PermissionDenied, r.Reason).WithDetails(refusedInfo)
	if detailErr != nil {
		return status.Error(codes.PermissionDenied, r.Reason)
	}
	return s.Err()
}

// fromStatus turns the gRPC status a Refused error was carried in back
// into one, and leaves any other error as it is: a PermissionDenied without
// the mark of a refusal stays an error of the action.
func fromStatus(err error) error {
	s, ok := status.FromError(err)
	if !ok || s.Code() != codes.PermissionDenied {
		return err
	}
	if hasInfo(s, refusedInfo) {
		return &Refused{Reason: s.Message()}
	}
	return err
}

func guardToPB(g Guard) *pb.Guard {
	if !g.Active() && len(g.AllowActions) == 0 && len(g.Keeps) == 0 {
		return nil
	}
	var keeps map[string]*pb.GuardKinds
	if len(g.Keeps) > 0 {
		keeps = make(map[string]*pb.GuardKinds, len(g.Keeps))
		for uses, kinds := range g.Keeps {
			keeps[uses] = &pb.GuardKinds{Kinds: kinds}
		}
	}
	return &pb.Guard{
		ReadOnly:     g.ReadOnly,
		AllowHosts:   g.AllowHosts,
		AllowActions: g.AllowActions,
		Keeps:        keeps,
	}
}

func guardFromPB(g *pb.Guard) Guard {
	var keeps map[string][]string
	if len(g.GetKeeps()) > 0 {
		keeps = make(map[string][]string, len(g.GetKeeps()))
		for uses, kinds := range g.GetKeeps() {
			keeps[uses] = kinds.GetKinds()
		}
	}
	return Guard{
		ReadOnly:     g.GetReadOnly(),
		AllowHosts:   g.GetAllowHosts(),
		AllowActions: g.GetAllowActions(),
		Keeps:        keeps,
	}
}
