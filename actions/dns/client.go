// Package dns is the dns action: it asks a DNS server for the records of a
// name, and returns what the server answered.
package dns

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/hostport"
	"github.com/mozership/probe/mapping"
)

const (
	defaultType     = "A"
	defaultTimeout  = 5 * time.Second
	protocolUDP     = "udp"
	protocolTCP     = "tcp"
	protocolTLS     = "tls"
	resolvConf      = "/etc/resolv.conf"
	defaultPort     = "53"
	defaultTLSPort  = "853"
	udpPayloadSize  = 1232
	statusSuccess   = 0
	statusFailure   = 1
	rcodeNoError    = "NOERROR"
	transferRefusal = "a zone transfer is not a query the dns action sends"
)

type Req struct {
	Name     string `map:"name"`
	Type     string `map:"type"`
	Server   string `map:"server"`
	Protocol string `map:"protocol"`
	Timeout  string `map:"timeout"`

	guard actionrpc.Guard
	// resolvConf is the file the system's resolvers are read from when no
	// server is given.
	resolvConf string
}

type Option func(*Req)

// WithGuard has the query sent only to a server the guard of the run allows.
func WithGuard(guard actionrpc.Guard) Option {
	return func(r *Req) {
		r.guard = guard
	}
}

// Request sends the query that with describes and returns the step's result.
// A server that answers with an error, such as NXDOMAIN, is a result for the
// step's test to check, and a server that does not answer is an error.
func Request(with map[string]any, opts ...Option) (map[string]any, error) {
	start := time.Now()

	req := &Req{resolvConf: resolvConf}
	if err := mapping.MapToStructByTags(with, req); err != nil {
		return map[string]any{}, fmt.Errorf("failed to parse request: %w", err)
	}
	for _, opt := range opts {
		opt(req)
	}

	question, timeout, err := req.normalize()
	if err != nil {
		return map[string]any{}, err
	}
	servers, err := req.servers()
	if err != nil {
		return map[string]any{}, err
	}
	// Every server the query may go to is checked before it goes to any.
	if len(req.guard.AllowHosts) > 0 {
		for _, server := range servers {
			if err := req.guard.CheckHost(server); err != nil {
				if req.Server == "" {
					return map[string]any{}, actionrpc.Refuse("%v: it is a resolver of this machine, from %s; give server to ask one the run allows", err, req.resolvConf)
				}
				return map[string]any{}, err
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	msg, server, protocol, err := req.exchange(ctx, question, servers)
	if err != nil {
		// The connection to the last server has the deadline of ctx, and
		// may find it passed a moment before ctx does: the error is then an
		// i/o timeout and ctx has no error yet. An i/o timeout before the
		// deadline is that of a server given a part of the time.
		deadline, _ := ctx.Deadline()
		passed := errors.Is(err, os.ErrDeadlineExceeded) && !time.Now().Before(deadline)
		if passed || errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("timed out after %s: %w", timeout, err)
		}
		return map[string]any{}, err
	}

	rcode := dns.RcodeToString[msg.Rcode]
	if rcode == "" {
		rcode = strconv.Itoa(msg.Rcode)
	}
	answers := make([]any, 0, len(msg.Answer))
	values := []any{}
	for _, rr := range msg.Answer {
		answer := record(rr)
		answers = append(answers, answer)
		if question.Qtype == dns.TypeANY || rr.Header().Rrtype == question.Qtype {
			values = append(values, answer["data"])
		}
	}

	status := statusFailure
	if rcode == rcodeNoError {
		status = statusSuccess
	}
	return map[string]any{
		"req": map[string]any{
			"name":     req.Name,
			"type":     req.Type,
			"server":   req.Server,
			"protocol": req.Protocol,
			"timeout":  req.Timeout,
		},
		"res": map[string]any{
			"rcode":         rcode,
			"answers":       answers,
			"values":        values,
			"authoritative": msg.Authoritative,
			"server":        server,
			"protocol":      protocol,
		},
		"rt":     time.Since(start).String(),
		"status": status,
	}, nil
}

// normalize checks the request and fills in what it leaves out, and returns
// the question it asks and how long it may take.
func (r *Req) normalize() (dns.Question, time.Duration, error) {
	if r.Name == "" {
		return dns.Question{}, 0, errors.New("name parameter is required")
	}

	if r.Type == "" {
		r.Type = defaultType
	}
	r.Type = strings.ToUpper(r.Type)
	qtype, ok := dns.StringToType[r.Type]
	if !ok || qtype == dns.TypeOPT || qtype == dns.TypeNone {
		return dns.Question{}, 0, fmt.Errorf("unsupported type: %s (use a record type such as A, AAAA, CNAME, MX, TXT, NS, SOA, SRV, PTR or CAA)", r.Type)
	}
	if qtype == dns.TypeAXFR || qtype == dns.TypeIXFR {
		return dns.Question{}, 0, fmt.Errorf("unsupported type: %s (%s)", r.Type, transferRefusal)
	}

	if r.Protocol == "" {
		r.Protocol = protocolUDP
	}
	r.Protocol = strings.ToLower(r.Protocol)
	switch r.Protocol {
	case protocolUDP, protocolTCP, protocolTLS:
	default:
		return dns.Question{}, 0, fmt.Errorf("unsupported protocol: %s (use udp, tcp or tls)", r.Protocol)
	}

	timeout := defaultTimeout
	if r.Timeout != "" {
		parsed, err := time.ParseDuration(r.Timeout)
		if err != nil {
			// A number alone is taken as seconds.
			if _, errInt := strconv.Atoi(r.Timeout); errInt != nil {
				return dns.Question{}, 0, fmt.Errorf("invalid timeout format: %s (use duration string like '5s' or integer seconds)", r.Timeout)
			}
			if parsed, err = time.ParseDuration(r.Timeout + "s"); err != nil {
				return dns.Question{}, 0, fmt.Errorf("invalid timeout: %s (%w)", r.Timeout, err)
			}
		}
		if parsed <= 0 {
			return dns.Question{}, 0, fmt.Errorf("invalid timeout: %s (must be above zero)", r.Timeout)
		}
		timeout = parsed
	}
	r.Timeout = timeout.String()

	// The name of a PTR query may be given as the address it is asked for.
	name := r.Name
	if qtype == dns.TypePTR && net.ParseIP(name) != nil {
		reverse, err := dns.ReverseAddr(name)
		if err != nil {
			return dns.Question{}, 0, fmt.Errorf("invalid name: %s (%w)", r.Name, err)
		}
		name = reverse
	}
	name = dns.Fqdn(name)
	if _, ok := dns.IsDomainName(name); !ok {
		return dns.Question{}, 0, fmt.Errorf("invalid name: %s (not a domain name)", r.Name)
	}

	return dns.Question{Name: name, Qtype: qtype, Qclass: dns.ClassINET}, timeout, nil
}

// servers returns the host and port of each server the query may go to, in
// the order they are tried: the one given, or the resolvers of this machine.
func (r *Req) servers() ([]string, error) {
	port := defaultPort
	if r.Protocol == protocolTLS {
		port = defaultTLSPort
	}
	if r.Server != "" {
		return []string{hostport.WithPort(r.Server, port)}, nil
	}

	conf, err := dns.ClientConfigFromFile(r.resolvConf)
	if err != nil {
		return nil, fmt.Errorf("no server given, and the resolvers of this machine cannot be read: %w", err)
	}
	if len(conf.Servers) == 0 {
		return nil, fmt.Errorf("no server given, and %s names no resolver", r.resolvConf)
	}
	// The port of resolv.conf is the one for plain DNS, not for DNS over TLS.
	if r.Protocol != protocolTLS && conf.Port != "" {
		port = conf.Port
	}
	servers := make([]string, len(conf.Servers))
	for i, server := range conf.Servers {
		servers[i] = net.JoinHostPort(server, port)
	}
	return servers, nil
}

// exchange asks each server in turn until one answers, and returns the
// answer with the server it came from and the protocol it came over. An
// answer cut short over UDP is asked for again over TCP, as dig does.
//
// Each server is given an equal part of the time ctx has left, so that one
// that does not answer leaves time for those after it, and the last, or the
// only one, has all that is left.
func (r *Req) exchange(ctx context.Context, question dns.Question, servers []string) (*dns.Msg, string, string, error) {
	query := new(dns.Msg)
	query.Id = dns.Id()
	query.RecursionDesired = true
	query.Question = []dns.Question{question}
	query.SetEdns0(udpPayloadSize, false)

	var errs []error
	for i, server := range servers {
		var until time.Time
		if deadline, ok := ctx.Deadline(); ok {
			until = time.Now().Add(time.Until(deadline) / time.Duration(len(servers)-i))
		}
		protocol := r.Protocol
		msg, err := send(ctx, query, server, protocol, until)
		if err == nil && msg.Truncated && protocol == protocolUDP {
			protocol = protocolTCP
			msg, err = send(ctx, query, server, protocol, until)
		}
		if err == nil {
			return msg, server, protocol, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", server, err))
		if ctx.Err() != nil {
			break
		}
	}
	return nil, "", "", fmt.Errorf("no answer from the DNS server: %w", errors.Join(errs...))
}

// send asks server once and waits for its answer until until, or as long as
// ctx lets it when until is zero.
func send(ctx context.Context, query *dns.Msg, server, protocol string, until time.Time) (*dns.Msg, error) {
	client := &dns.Client{}
	// Without a timeout of its own, the client waits 2 seconds whatever
	// time ctx has left.
	if !until.IsZero() {
		client.Timeout = max(time.Until(until), time.Millisecond)
	}
	switch protocol {
	case protocolTCP:
		client.Net = "tcp"
	case protocolTLS:
		client.Net = "tcp-tls"
		host, _, err := net.SplitHostPort(server)
		if err != nil {
			return nil, err
		}
		client.TLSConfig = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	}
	msg, _, err := client.ExchangeContext(ctx, query, server)
	return msg, err
}

// record returns rr as the step's result holds it: its name, type, TTL and
// data, with the parts of the data by name for the types that have several.
// Host names are given without the dot they end with.
func record(rr dns.RR) map[string]any {
	header := rr.Header()
	out := map[string]any{
		"name": host(header.Name),
		"type": dns.TypeToString[header.Rrtype],
		"ttl":  int(header.Ttl),
	}
	if out["type"] == "" {
		out["type"] = "TYPE" + strconv.Itoa(int(header.Rrtype))
	}

	switch v := rr.(type) {
	case *dns.A:
		out["data"] = v.A.String()
	case *dns.AAAA:
		out["data"] = v.AAAA.String()
	case *dns.CNAME:
		out["data"] = host(v.Target)
	case *dns.NS:
		out["data"] = host(v.Ns)
	case *dns.PTR:
		out["data"] = host(v.Ptr)
	case *dns.TXT:
		// A text longer than 255 bytes is sent in pieces, which are one text.
		out["data"] = strings.Join(v.Txt, "")
	case *dns.MX:
		out["preference"] = int(v.Preference)
		out["host"] = host(v.Mx)
		out["data"] = fmt.Sprintf("%d %s", v.Preference, host(v.Mx))
	case *dns.SRV:
		out["priority"] = int(v.Priority)
		out["weight"] = int(v.Weight)
		out["port"] = int(v.Port)
		out["target"] = host(v.Target)
		out["data"] = fmt.Sprintf("%d %d %d %s", v.Priority, v.Weight, v.Port, host(v.Target))
	case *dns.SOA:
		out["ns"] = host(v.Ns)
		out["mbox"] = host(v.Mbox)
		out["serial"] = int64(v.Serial)
		out["refresh"] = int64(v.Refresh)
		out["retry"] = int64(v.Retry)
		out["expire"] = int64(v.Expire)
		out["minimum"] = int64(v.Minttl)
		out["data"] = fmt.Sprintf("%s %s %d %d %d %d %d", host(v.Ns), host(v.Mbox), v.Serial, v.Refresh, v.Retry, v.Expire, v.Minttl)
	case *dns.CAA:
		out["flag"] = int(v.Flag)
		out["tag"] = v.Tag
		out["value"] = v.Value
		out["data"] = fmt.Sprintf("%d %s %s", v.Flag, v.Tag, v.Value)
	default:
		// Any other type is given as a zone file writes it.
		out["data"] = strings.TrimPrefix(rr.String(), header.String())
	}
	return out
}

// host returns a domain name without the dot it ends with. The root stays
// a dot.
func host(name string) string {
	if name == "." {
		return name
	}
	return strings.TrimSuffix(name, ".")
}
