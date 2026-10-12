package grpc

import (
	"net"
	"net/url"
	"strings"

	"github.com/mozership/probe/actionrpc"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// hostlessSchemes are the gRPC target schemes that name no host the guard
// can check, such as a Unix socket.
var hostlessSchemes = []string{"unix:", "unix-abstract:", "vsock:", "ipv4:", "ipv6:", "xds:", "google-c2p:"}

// grpcHosts returns the hosts and ports a gRPC call to addr connects to, as
// grpc-go reads the target: host:port, or dns:/// or passthrough:/// before
// it, with the port 443 when none is given, and for dns://server/ the DNS
// server it asks, at port 53 when none is given. It returns false for a
// target that names no host, such as a Unix socket.
func grpcHosts(addr string) ([]string, bool) {
	lower := strings.ToLower(addr)
	for _, s := range hostlessSchemes {
		if strings.HasPrefix(lower, s) {
			return nil, false
		}
	}
	endpoint := addr
	var dnsServer string
	if strings.Contains(addr, "://") {
		u, err := url.Parse(addr)
		if err != nil {
			return nil, false
		}
		switch strings.ToLower(u.Scheme) {
		case "dns":
			dnsServer = u.Host
			endpoint = strings.TrimPrefix(u.Path, "/")
		case "passthrough":
			endpoint = strings.TrimPrefix(u.Path, "/")
		default:
			return nil, false
		}
	}
	if endpoint == "" {
		return nil, false
	}
	hosts := []string{withPort(endpoint, "443")}
	if dnsServer != "" {
		hosts = append(hosts, withPort(dnsServer, "53"))
	}
	return hosts, true
}

// withPort returns hostport with port when it names none.
func withPort(hostport, port string) string {
	if _, _, err := net.SplitHostPort(hostport); err == nil {
		return hostport
	}
	return net.JoinHostPort(strings.Trim(hostport, "[]"), port)
}

// checkGRPCHost returns a Refused error when the guard of the run does not
// allow the host the gRPC call connects to, or cannot tell it.
func (r *Req) checkGRPCHost() error {
	if len(r.guard.AllowHosts) == 0 {
		return nil
	}
	hosts, ok := grpcHosts(r.Addr)
	if !ok {
		return actionrpc.Refuse("the addr %s names no host the run can check against the hosts it allows", r.Addr)
	}
	for _, host := range hosts {
		if err := r.guard.CheckHost(host); err != nil {
			return err
		}
	}
	return nil
}

// checkConnectHost returns a Refused error when the guard of the run does
// not allow the host of base, the URL a Connect call goes under, taken at
// the port of its scheme when it names none.
func (r *Req) checkConnectHost(base string) error {
	if len(r.guard.AllowHosts) == 0 {
		return nil
	}
	u, err := url.Parse(base)
	if err != nil {
		return err
	}
	host := u.Host
	if u.Port() == "" {
		port := "80"
		if u.Scheme == "https" {
			port = "443"
		}
		host = net.JoinHostPort(u.Hostname(), port)
	}
	return r.guard.CheckHost(host)
}

// checkReadOnly returns a Refused error when the run is read-only and the
// method may write: when a definition of it at hand, the server's or the
// .proto files', does not declare it idempotency_level = NO_SIDE_EFFECTS,
// or when there is none to tell. Either may be nil.
func (r *Req) checkReadOnly(server, spec protoreflect.MethodDescriptor) error {
	if !r.guard.ReadOnly {
		return nil
	}
	if server == nil && spec == nil {
		return actionrpc.Refuse("the run is read-only, and no definition of %s/%s tells that it does not write; give proto.files that declare it idempotency_level = NO_SIDE_EFFECTS", r.Service, r.Method)
	}
	for _, d := range []struct {
		where string
		md    protoreflect.MethodDescriptor
	}{{"the server's definition", server}, {"the .proto files", spec}} {
		if d.md == nil {
			continue
		}
		level := idempotencyLevel(d.md)
		if level == descriptorpb.MethodOptions_NO_SIDE_EFFECTS {
			continue
		}
		name := level.String()
		if level == descriptorpb.MethodOptions_IDEMPOTENCY_UNKNOWN {
			name = "unset"
		}
		return actionrpc.Refuse("the run is read-only, and %s may write: its idempotency_level in %s is %s, not NO_SIDE_EFFECTS", d.md.FullName(), d.where, name)
	}
	return nil
}

// idempotencyLevel returns the idempotency_level md is declared with,
// IDEMPOTENCY_UNKNOWN when it is declared with none.
func idempotencyLevel(md protoreflect.MethodDescriptor) descriptorpb.MethodOptions_IdempotencyLevel {
	return protodesc.ToMethodDescriptorProto(md).GetOptions().GetIdempotencyLevel()
}
