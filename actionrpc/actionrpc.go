// Package actionrpc is the protocol between the workflow runner and its
// actions. Each action runs in a process of its own, started from the probe
// binary, and is called over go-plugin's gRPC transport.
package actionrpc

import (
	"context"
	"fmt"
	"math"
	"os"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"github.com/mozership/probe/mask"
	"github.com/mozership/probe/pb"
	"github.com/mozership/probe/truncate"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

var (
	// BuiltinCmd is the subcommand of the probe binary that serves an action.
	BuiltinCmd = "builtin-actions"
	// Handshake is the go-plugin handshake both sides of an action agree on.
	Handshake = plugin.HandshakeConfig{ProtocolVersion: 1, MagicCookieKey: "probe", MagicCookieValue: "actions"}
	// PluginMap is the set of plugins the runner can dispense.
	PluginMap = map[string]plugin.Plugin{"actions": &Plugin{}}
)

// Action is what an action implements: it runs once with the step's `with`
// parameters and returns its result.
type Action interface {
	Run(with map[string]any) (map[string]any, error)
}

// StepAction is an Action that is told about the step it runs for, and may
// keep state in a job from one step that uses it to the next, such as the
// cookies a server set. What it does with either is up to the action.
type StepAction interface {
	Action
	RunStep(call Call) (result, newState map[string]any, err error)
}

// Cleaner is an Action that removes what its steps left behind in a run,
// such as the containers they started, once the run is over. The runner
// calls Cleanup only on an action whose action.yml sets cleanup, and once
// however many steps used it, so the action has to find what to remove by
// the run alone, such as by a label of the run's ID that its steps put on
// what they made.
//
// Cleanup is called when the run ends by a signal as well, and then a step
// of the action may still be running, or may have been stopped halfway. It
// can be called again for the same run, and for a run that left nothing, so
// it must not fail for what is already gone.
type Cleaner interface {
	Action
	Cleanup(call CleanupCall) error
}

// CleanupCall is what a Cleaner is given to clean up after a run.
type CleanupCall struct {
	// RunID names the run that is over, as Step.RunID named it to each of
	// its steps.
	RunID string
}

// Cleanups is given to an action by a runner that cleans up once the run is
// over. It gathers the external actions that are to be cleaned up then, for
// an action that runs steps of its own to pass on those its steps used. A
// nil Cleanups gathers nothing.
type Cleanups struct {
	mu   sync.Mutex
	uses []string
}

// Add names external actions to clean up once the run is over, each as a
// step's uses names it in full, with a local one as an absolute path. One
// already named is not named again.
func (c *Cleanups) Add(uses ...string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, u := range uses {
		if !slices.Contains(c.uses, u) {
			c.uses = append(c.uses, u)
		}
	}
}

// List returns the actions named so far, in the order they were first named.
func (c *Cleanups) List() []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.uses)
}

// Process is a process an action left running in the background, for the
// runner to stop once the run is over.
type Process struct {
	// PID leads a process group of its own, which is stopped with it.
	PID int
	// Log is the file its output goes to, which is removed once it is
	// stopped; empty for none.
	Log string
}

// Processes gathers the processes an action leaves running in the
// background, which the runner stops once the run is over: an action runs in
// a process that exits once its step is done, and one it starts in a session
// of its own to outlive it would otherwise run on. A nil Processes gathers
// nothing.
type Processes struct {
	mu    sync.Mutex
	procs []Process
}

// Add records processes the action left running.
func (p *Processes) Add(procs ...Process) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.procs = append(p.procs, procs...)
}

// List returns the processes recorded so far, in the order they were added.
func (p *Processes) List() []Process {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.procs)
}

func processesToPB(procs []Process) []*pb.Process {
	out := make([]*pb.Process, len(procs))
	for i, p := range procs {
		out[i] = &pb.Process{Pid: int64(p.PID), Log: p.Log}
	}
	return out
}

func processesFromPB(procs []*pb.Process) []Process {
	out := make([]Process, len(procs))
	for i, p := range procs {
		out[i] = Process{PID: int(p.GetPid()), Log: p.GetLog()}
	}
	return out
}

// Call is what a StepAction is given to run once for a step.
type Call struct {
	// With holds the step's with parameters, as Action.Run is given them.
	With map[string]any
	// State is the state the action left in the job, or nil when it left
	// none. The runner keeps the state the action returns without reading
	// it, and gives it to the action again in the next step of the job that
	// uses it. The state starts empty in each run of a job, and a nil state
	// returned keeps it as it was.
	State map[string]any
	// Step is the step the action runs for.
	Step Step
	// Guard is what the run allows the action to do. An action that keeps
	// to it returns a Refused error for what it does not allow.
	Guard Guard
	// Actions are the names the workflow gives its external actions, each
	// with the action it stands for in full, for an action that runs steps
	// of its own, as embedded does, to read a uses by. A local action is an
	// absolute path here.
	Actions map[string]string
	// Cleanups is not nil when the runner cleans up once the run is over:
	// it calls Cleanup of the action, when its action.yml sets cleanup, and
	// of those named here. It is nil for a runner that does not, such as a
	// Probe from before there was a cleanup, under which what a step leaves
	// behind stays; an action can refuse to leave anything then.
	Cleanups *Cleanups
	// Processes records the processes the action leaves running in the
	// background, for the runner to stop once the run is over. A runner
	// from before there were processes does not hear of them, and leaves
	// them running.
	Processes *Processes
	// Contract records the check the action made of the step against a
	// contract, such as an OpenAPI document, which the runner takes as the
	// step's test. A runner from before there were contracts to report
	// does not hear of it, and takes the step as unchecked.
	Contract *Contract
}

// Step tells an action about the step it runs for.
type Step struct {
	// RunID names the run of probe, the same for every step of every job in
	// it, and for a job run by the embedded action.
	RunID   string
	JobID   string
	JobName string
	// Index is the position of the step in the job, from 0.
	Index int
	// ID is the id the step is given, or step_ and where it is written in
	// its job when it has none, as step_1.
	ID   string
	Name string
	// Repeat is which run of a repeated job this is, from 0.
	Repeat int
	// Attempt is which attempt of a retried step this is, from 1.
	Attempt int
}

func stepToPB(s Step) *pb.Step {
	return &pb.Step{
		RunId:   s.RunID,
		JobId:   s.JobID,
		JobName: s.JobName,
		Index:   int64(s.Index),
		Id:      s.ID,
		Name:    s.Name,
		Repeat:  int64(s.Repeat),
		Attempt: int64(s.Attempt),
	}
}

func stepFromPB(s *pb.Step) Step {
	return Step{
		RunID:   s.GetRunId(),
		JobID:   s.GetJobId(),
		JobName: s.GetJobName(),
		Index:   int(s.GetIndex()),
		ID:      s.GetId(),
		Name:    s.GetName(),
		Repeat:  int(s.GetRepeat()),
		Attempt: int(s.GetAttempt()),
	}
}

// Plugin serves an Action over gRPC, and is what the runner dispenses to call
// one.
type Plugin struct {
	plugin.Plugin
	Impl Action
}

func (p *Plugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	log := hclog.New(&hclog.LoggerOptions{
		Level:      hclog.Trace,
		Output:     os.Stderr,
		JSONFormat: true,
	})
	pb.RegisterActionsServer(s, &Server{Impl: p.Impl, log: log})
	return nil
}

func (p *Plugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	return &Client{client: pb.NewActionsClient(c)}, nil
}

// Client calls an Action in the process that serves it. Each of its methods
// calls the method of the same name of StepAction or Cleaner, and takes a
// ctx as well, to give up on the call by. It is not an Action itself: the
// runner always calls an action for a step, and one that is not a
// StepAction is run with the parameters alone.
type Client struct {
	client pb.ActionsClient
}

// RunStep calls the action for a step, and returns the state it leaves,
// which is nil when the action keeps none. An action that is not a
// StepAction is run with the parameters alone. It gives up once ctx is done.
func (m *Client) RunStep(ctx context.Context, call Call) (map[string]any, map[string]any, error) {
	// Convert map[string]any directly to protobuf.Struct
	withStruct, err := structpb.NewStruct(call.With)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to convert parameters to protobuf struct: %v", err)
	}
	req := &pb.RunRequest{With: withStruct, Step: stepToPB(call.Step), Guard: guardToPB(call.Guard), Actions: call.Actions, Cleanup: call.Cleanups != nil}
	if call.State != nil {
		req.State, err = structpb.NewStruct(call.State)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to convert state to protobuf struct: %v", err)
		}
	}

	runRes, err := m.client.Run(ctx, req)
	if err != nil {
		if d := detailsOf(err); d != nil {
			call.Cleanups.Add(d.GetCleanups()...)
			call.Processes.Add(processesFromPB(d.GetProcesses())...)
		}
		return nil, nil, fromRPC(err)
	}
	call.Cleanups.Add(runRes.GetCleanups()...)
	call.Processes.Add(processesFromPB(runRes.GetProcesses())...)
	if check := contractFromPB(runRes.GetContract()); check != nil {
		call.Contract.Report(*check)
	}

	// Convert protobuf.Struct back to map[string]any
	// Apply convertFloatToInt to restore integer types that were converted to float64 by protobuf
	result := map[string]any{}
	if runRes.Result != nil {
		result = structToMap(runRes.Result)
	}
	var newState map[string]any
	if runRes.State != nil {
		newState = structToMap(runRes.State)
	}
	return result, newState, nil
}

// Cleanup tells the action that the run is over, for it to remove what its
// steps left behind. It gives up once ctx is done.
func (m *Client) Cleanup(ctx context.Context, call CleanupCall) error {
	_, err := m.client.Cleanup(ctx, &pb.CleanupRequest{RunId: call.RunID})
	if err != nil {
		return fromRPC(err)
	}
	return nil
}

// actionError is an error of an action as the runner receives it: in the
// gRPC status it was sent in. It reads as what the action said, without the
// code and the wording gRPC puts around it, which tell a person nothing of
// why the step failed. The status is kept, so that status.Code and
// status.FromError still read it.
type actionError struct{ s *status.Status }

func (e *actionError) Error() string {
	if msg := e.s.Message(); msg != "" {
		return msg
	}
	return "the action failed with " + e.s.Code().String()
}

// GRPCStatus returns the status the error was sent in.
func (e *actionError) GRPCStatus() *status.Status {
	return e.s
}

// actionErrorInfo marks the gRPC status an action's error is carried in, as
// one the action returned rather than one of the connection to it, which
// gRPC gives a code an action can return as well.
var actionErrorInfo = &errdetails.ErrorInfo{Domain: "probe", Reason: "ACTION_ERROR"}

// sentByAction returns err in the gRPC status it is sent in, marked as the
// action's error. A refusal of the guard has a mark of its own.
func sentByAction(err error) error {
	s, ok := status.FromError(err)
	if !ok {
		s = status.New(codes.Unknown, err.Error())
	}
	if hasInfo(s, refusedInfo) {
		return err
	}
	if d, detailErr := s.WithDetails(actionErrorInfo); detailErr == nil {
		return d.Err()
	}
	return err
}

// fromAction reports whether err is an error the action returned, as
// sentByAction marked it. An action built against an earlier version of
// this package does not mark its errors.
func fromAction(err error) bool {
	s, ok := status.FromError(err)
	return ok && hasInfo(s, actionErrorInfo)
}

// hasInfo reports whether s carries info among its details.
func hasInfo(s *status.Status, info *errdetails.ErrorInfo) bool {
	for _, d := range s.Details() {
		if i, ok := d.(*errdetails.ErrorInfo); ok && i.GetDomain() == info.GetDomain() && i.GetReason() == info.GetReason() {
			return true
		}
	}
	return false
}

// fromRPC returns err, the error of a call of an action, as the action's
// own error: a refusal of the guard as one, and any other status as an
// actionError. An error that is not a status is left as it is.
func fromRPC(err error) error {
	err = fromStatus(err)
	if IsRefused(err) {
		return err
	}
	if s, ok := status.FromError(err); ok {
		return &actionError{s: s}
	}
	return err
}

// withLeftBehind returns err carrying the actions to clean up and the
// processes left running, which the response would have carried had the
// action not failed: what an action left behind before it failed is to be
// undone all the same.
func withLeftBehind(err error, uses []string, procs []Process) error {
	if len(uses) == 0 && len(procs) == 0 {
		return err
	}
	s, ok := status.FromError(err)
	if !ok {
		s = status.New(codes.Unknown, err.Error())
	}
	if d, detailErr := s.WithDetails(&pb.RunResponse{Cleanups: uses, Processes: processesToPB(procs)}); detailErr == nil {
		return d.Err()
	}
	return err
}

// detailsOf returns what err carries of the response withLeftBehind gave
// it, or nil when it carries none.
func detailsOf(err error) *pb.RunResponse {
	s, ok := status.FromError(err)
	if !ok {
		return nil
	}
	for _, d := range s.Details() {
		if res, ok := d.(*pb.RunResponse); ok {
			return res
		}
	}
	return nil
}

// structToMap converts s to a map, with the integers protobuf turned into
// float64 restored.
func structToMap(s *structpb.Struct) map[string]any {
	m := s.AsMap()
	if converted, ok := convertFloatToInt(m).(map[string]any); ok {
		return converted
	}
	return m
}

// Server answers the runner's calls with an Action. It logs at trace level
// alone: its records tell how a call went through it, and what it returns,
// an error as well, the runner tells itself.
type Server struct {
	Impl Action
	log  hclog.Logger
}

// Run runs the action with the request's parameters.
func (m *Server) Run(ctx context.Context, req *pb.RunRequest) (_ *pb.RunResponse, err error) {
	var cleanups *Cleanups
	if req.GetCleanup() {
		cleanups = &Cleanups{}
	}
	processes := &Processes{}
	contract := &Contract{}
	// What an action left behind is to be undone however the call fails,
	// also when it is its result that cannot be sent.
	defer func() {
		if err != nil {
			err = sentByAction(withLeftBehind(err, cleanups.List(), processes.List()))
		}
	}()

	// The state is left out: it may hold credentials, such as cookies, that
	// the runner has not learned to hide.
	if m.log != nil {
		m.log.Trace("ActionsServer.Run called", "request", req.GetWith())
	}

	// Convert protobuf.Struct to map[string]any
	// Apply convertFloatToInt to restore integer types that were converted to float64 by protobuf
	withMap := make(map[string]any)
	if req.With != nil {
		withMap = structToMap(req.With)
	}

	var v, newState map[string]any
	if sa, ok := m.Impl.(StepAction); ok {
		call := Call{With: withMap, Step: stepFromPB(req.GetStep()), Guard: guardFromPB(req.GetGuard()), Actions: req.GetActions(), Cleanups: cleanups, Processes: processes, Contract: contract}
		if req.State != nil {
			call.State = structToMap(req.State)
		}
		v, newState, err = sa.RunStep(call)
	} else {
		v, err = m.Impl.Run(withMap)
	}
	if err != nil {
		if m.log != nil {
			m.log.Trace("Action execution failed", "error", err)
		}
		return &pb.RunResponse{}, toStatus(err)
	}

	// The result is logged as LogOutcome logs it, credentials hidden: the
	// runner learns those in a result only once it has the result, after
	// these records have gone out.
	if m.log != nil {
		m.log.Trace("ActionsServer received from action", "result", forLog(v))
	}

	// Convert map[string]any to protobuf.Struct
	convertedResult, err := convertForProtobuf(v)
	if err != nil {
		if m.log != nil {
			m.log.Trace("ActionsServer cannot send the action's result", "error", err)
		}
		return &pb.RunResponse{}, fmt.Errorf("cannot send the action's result: %w", err)
	}
	resultMap, ok := convertedResult.(map[string]any)
	if !ok {
		if m.log != nil {
			m.log.Trace("ActionsServer convertForProtobuf did not return map[string]any", "type", fmt.Sprintf("%T", convertedResult))
		}
		return &pb.RunResponse{}, fmt.Errorf("convertForProtobuf returned invalid type: expected map[string]any, got %T", convertedResult)
	}

	resultStruct, err := structpb.NewStruct(resultMap)
	if err != nil {
		if m.log != nil {
			m.log.Trace("ActionsServer failed to convert result to protobuf struct", "error", err)
		}
		return &pb.RunResponse{}, fmt.Errorf("failed to convert result to protobuf struct: %v", err)
	}

	res := &pb.RunResponse{Result: resultStruct, Cleanups: cleanups.List(), Processes: processesToPB(processes.List()), Contract: contractToPB(contract.Check())}
	if newState != nil {
		convertedState, err := convertForProtobuf(newState)
		if err != nil {
			return &pb.RunResponse{}, fmt.Errorf("cannot send the action's state: %w", err)
		}
		stateMap, _ := convertedState.(map[string]any)
		res.State, err = structpb.NewStruct(stateMap)
		if err != nil {
			return &pb.RunResponse{}, fmt.Errorf("failed to convert state to protobuf struct: %v", err)
		}
	}
	return res, nil
}

// Cleanup has the action clean up after the run the request names. An action
// that is not a Cleaner has nothing to do it with, which is an error: the
// runner calls only an action that asked to be.
func (m *Server) Cleanup(ctx context.Context, req *pb.CleanupRequest) (*pb.CleanupResponse, error) {
	c, ok := m.Impl.(Cleaner)
	if !ok {
		return &pb.CleanupResponse{}, status.Error(codes.Unimplemented, "the action does not clean up: it has no Cleanup")
	}
	if m.log != nil {
		m.log.Trace("ActionsServer.Cleanup called", "run_id", req.GetRunId())
	}
	if err := c.Cleanup(CleanupCall{RunID: req.GetRunId()}); err != nil {
		if m.log != nil {
			m.log.Trace("Action cleanup failed", "error", err)
		}
		return &pb.CleanupResponse{}, err
	}
	return &pb.CleanupResponse{}, nil
}

// Sendable returns a copy of v as an action in a process of its own sends
// it and the runner receives it: maps keyed by strings, lists, and plain
// values, with whole numbers as int64. The maps and lists are new, so the
// copy shares none of them with v. It fails for a value that cannot be sent,
// as sending it would.
func Sendable(v map[string]any) (map[string]any, error) {
	if v == nil {
		return nil, nil
	}
	c, err := convertForProtobuf(v)
	if err != nil {
		return nil, err
	}
	m, _ := c.(map[string]any)
	s, err := structpb.NewStruct(m)
	if err != nil {
		return nil, err
	}
	return structToMap(s), nil
}

// convertForProtobuf converts unsupported types to protobuf-compatible types.
// A map whose keys are not strings is keyed by their printed form instead;
// keys that cannot be printed faithfully are an error, so that no entry of a
// result is dropped without a word.
func convertForProtobuf(value any) (any, error) {
	if value == nil {
		return nil, nil
	}

	switch v := value.(type) {
	case time.Duration:
		// Convert Duration to string
		return v.String(), nil
	case time.Time:
		// Convert Time to RFC3339 string format
		return v.Format(time.RFC3339), nil
	case map[string]string:
		// Convert map[string]string to map[string]any
		result := make(map[string]any, len(v))
		for k, val := range v {
			result[k] = val
		}
		return result, nil
	case map[string]any:
		// Recursively convert nested maps
		result := make(map[string]any, len(v))
		for k, val := range v {
			c, err := convertForProtobuf(val)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			result[k] = c
		}
		return result, nil
	case []any:
		// Recursively convert arrays
		return convertSlice(reflect.ValueOf(v))
	case []string:
		// Convert []string to []any
		result := make([]any, len(v))
		for i, str := range v {
			result[i] = str
		}
		return result, nil
	}

	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Slice:
		return convertSlice(rv)
	case reflect.Pointer:
		if rv.IsNil() {
			return nil, nil
		}
		// Dereference pointer and recurse
		return convertForProtobuf(rv.Elem().Interface())
	case reflect.Struct:
		// Convert struct to map[string]any
		result := make(map[string]any)
		rt := rv.Type()
		for i := 0; i < rv.NumField(); i++ {
			field := rt.Field(i)
			// Skip unexported fields
			if !field.IsExported() {
				continue
			}
			// Use struct tag if available, otherwise use field name
			fieldName := field.Name
			if mapTag := field.Tag.Get("map"); mapTag != "" {
				fieldName = mapTag
			}
			c, err := convertForProtobuf(rv.Field(i).Interface())
			if err != nil {
				return nil, fmt.Errorf("%s: %w", fieldName, err)
			}
			result[fieldName] = c
		}
		return result, nil
	case reflect.Map:
		return convertMap(rv)
	}
	// Return as-is for supported types (string, int, float64, bool, etc.)
	return value, nil
}

func convertSlice(rv reflect.Value) (any, error) {
	result := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		c, err := convertForProtobuf(rv.Index(i).Interface())
		if err != nil {
			return nil, fmt.Errorf("[%d]: %w", i, err)
		}
		result[i] = c
	}
	return result, nil
}

// convertMap converts a map of any key type to one keyed by strings. A key
// that is a string, a number or a bool is printed as fmt.Sprint does, which
// tells apart any two keys of one type. Any other key, or two keys that print
// alike, such as 1 and "1" in a map[any]any, is an error.
func convertMap(rv reflect.Value) (any, error) {
	result := make(map[string]any, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		key := iter.Key()
		if key.Kind() == reflect.Interface {
			key = key.Elem()
		}
		switch key.Kind() {
		case reflect.String, reflect.Bool,
			reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
		default:
			return nil, fmt.Errorf("map key of type %s cannot be sent: use string keys", iter.Key().Type())
		}
		name := fmt.Sprint(key.Interface())
		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("map keys collide as %q: use string keys", name)
		}
		c, err := convertForProtobuf(iter.Value().Interface())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		result[name] = c
	}
	return result, nil
}

// convertFloatToInt converts float64 values that represent integers back to int64.
// This is needed because protobuf.Struct converts all numbers to float64,
// which causes large integers like Unix timestamps to display in E notation.
func convertFloatToInt(value any) any {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case float64:
		// Check if the float64 is actually an integer value. float64 of
		// MaxInt64 rounds up to 2^63, which int64 cannot hold, so the upper
		// bound is exclusive.
		if v == math.Trunc(v) && v >= math.MinInt64 && v < -float64(math.MinInt64) {
			return int64(v)
		}
		return v
	case map[string]any:
		result := make(map[string]any)
		for k, val := range v {
			result[k] = convertFloatToInt(val)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, val := range v {
			result[i] = convertFloatToInt(val)
		}
		return result
	default:
		return value
	}
}

// LogParams records the parameters an action received, truncating long
// values so that a large request body does not flood the log. Actions are
// separate processes, so their log records reach the workflow runner as JSON
// on stderr and are re-filtered there by the runner's own level. The record
// is at trace level, shown only with -vv: a verbose run shows the step's
// request and response, which the parameters would only say again.
func LogParams(log hclog.Logger, msg string, with map[string]any) {
	log.Trace(msg, "params", forLog(with))
}

// LogOutcome records how an action finished. subject names what was
// attempted, for example "http request", and is used to build the message.
// It is recorded at trace level, as LogParams records the parameters: a
// verbose run shows the step's response, and the error the action returns,
// which the runner prints itself.
func LogOutcome(log hclog.Logger, subject string, ret map[string]any, err error) {
	if err != nil {
		log.Trace(subject+" failed", "error", err)
		return
	}
	log.Trace(subject+" completed successfully", "result", forLog(ret))
}

// forLog returns a copy of data fit to log: credentials hidden, then long
// strings truncated, with its maps and lists kept as such, for the runner to
// lay them out. The runner hides credentials in the log as well, but only
// where it finds them whole, and a truncated value is no longer whole, so
// they are hidden here first.
func forLog(data map[string]any) map[string]any {
	m := mask.New(nil, nil)
	m.Learn(data)
	out, _ := truncate.Value(m.Map(data), truncate.MaxLogLength).(map[string]any)
	return out
}
