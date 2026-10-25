package dns

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/mozership/probe/actionrpc"
)

// zone is what the test server answers from, by name and type.
var zone = map[string][]string{
	"example.test. A":             {"example.test. 300 IN A 192.0.2.1", "example.test. 300 IN A 192.0.2.2"},
	"example.test. AAAA":          {"example.test. 300 IN AAAA 2001:db8::1"},
	"example.test. MX":            {"example.test. 300 IN MX 10 mail.example.test.", "example.test. 300 IN MX 20 backup.example.test."},
	"example.test. TXT":           {`example.test. 300 IN TXT "v=spf1 include:_spf.example.test " "-all"`},
	"example.test. NS":            {"example.test. 300 IN NS ns1.example.test."},
	"example.test. SOA":           {"example.test. 300 IN SOA ns1.example.test. hostmaster.example.test. 2026101001 7200 3600 1209600 300"},
	"example.test. CAA":           {`example.test. 300 IN CAA 0 issue "letsencrypt.org"`},
	"_sip._tcp.example.test. SRV": {"_sip._tcp.example.test. 300 IN SRV 10 60 5060 sip.example.test."},
	"1.2.0.192.in-addr.arpa. PTR": {"1.2.0.192.in-addr.arpa. 300 IN PTR example.test."},
	"example.test. DNSKEY":        {"example.test. 300 IN DNSKEY 257 3 13 mdsswUyr3DPW132mOi8V9xESWE8jTo0dxCjjnopKl+GqJxpVXckHAeF+KkxLbxILfDLUT0rAK9iUzy1L53eKGQ=="},
	// The answer to www holds the alias and what it points to.
	"www.example.test. A": {"www.example.test. 300 IN CNAME example.test.", "example.test. 300 IN A 192.0.2.1"},
}

// startServer serves zone over UDP and TCP on one port of the loopback, and
// returns its address and the number of queries it took over each.
func startServer(t *testing.T, big bool) (string, *atomic.Int32, *atomic.Int32) {
	t.Helper()

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := pc.LocalAddr().String()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}

	var udp, tcp atomic.Int32
	handler := func(network string) dns.HandlerFunc {
		return func(w dns.ResponseWriter, r *dns.Msg) {
			if network == "udp" {
				udp.Add(1)
			} else {
				tcp.Add(1)
			}
			m := new(dns.Msg)
			m.SetReply(r)
			m.Authoritative = true
			q := r.Question[0]
			// An answer too large for UDP is cut short there.
			if big && network == "udp" {
				m.Truncated = true
				_ = w.WriteMsg(m)
				return
			}
			lines, ok := zone[q.Name+" "+dns.TypeToString[q.Qtype]]
			switch {
			case ok:
				for _, line := range lines {
					rr, err := dns.NewRR(line)
					if err != nil {
						panic(err)
					}
					m.Answer = append(m.Answer, rr)
				}
			case strings.HasSuffix(q.Name, "example.test."):
				// The name is there, with no record of the type.
				if q.Name != "example.test." {
					m.Rcode = dns.RcodeNameError
				}
			default:
				m.Rcode = dns.RcodeRefused
			}
			_ = w.WriteMsg(m)
		}
	}
	for _, srv := range []*dns.Server{
		{PacketConn: pc, Handler: handler("udp")},
		{Listener: ln, Handler: handler("tcp")},
	} {
		started := make(chan struct{})
		srv.NotifyStartedFunc = func() { close(started) }
		go func() { _ = srv.ActivateAndServe() }()
		<-started
		t.Cleanup(func() { _ = srv.Shutdown() })
	}
	return addr, &udp, &tcp
}

func res(t *testing.T, ret map[string]any) map[string]any {
	t.Helper()
	r, ok := ret["res"].(map[string]any)
	if !ok {
		t.Fatalf("result has no res: %v", ret)
	}
	return r
}

func TestRequestRecords(t *testing.T) {
	server, _, _ := startServer(t, false)

	tests := []struct {
		name   string
		with   map[string]any
		values []any
		first  map[string]any
	}{
		{
			name:   "A is the default type",
			with:   map[string]any{"name": "example.test"},
			values: []any{"192.0.2.1", "192.0.2.2"},
			first:  map[string]any{"name": "example.test", "type": "A", "ttl": 300, "data": "192.0.2.1"},
		},
		{
			name:   "a type in lowercase, and a name that ends with a dot",
			with:   map[string]any{"name": "example.test.", "type": "aaaa"},
			values: []any{"2001:db8::1"},
			first:  map[string]any{"name": "example.test", "type": "AAAA", "ttl": 300, "data": "2001:db8::1"},
		},
		{
			name:   "MX",
			with:   map[string]any{"name": "example.test", "type": "MX"},
			values: []any{"10 mail.example.test", "20 backup.example.test"},
			first:  map[string]any{"name": "example.test", "type": "MX", "ttl": 300, "data": "10 mail.example.test", "preference": 10, "host": "mail.example.test"},
		},
		{
			name:   "TXT sent in pieces is one text",
			with:   map[string]any{"name": "example.test", "type": "TXT"},
			values: []any{"v=spf1 include:_spf.example.test -all"},
			first:  map[string]any{"name": "example.test", "type": "TXT", "ttl": 300, "data": "v=spf1 include:_spf.example.test -all"},
		},
		{
			name:   "NS",
			with:   map[string]any{"name": "example.test", "type": "NS"},
			values: []any{"ns1.example.test"},
			first:  map[string]any{"name": "example.test", "type": "NS", "ttl": 300, "data": "ns1.example.test"},
		},
		{
			name:   "SOA",
			with:   map[string]any{"name": "example.test", "type": "SOA"},
			values: []any{"ns1.example.test hostmaster.example.test 2026101001 7200 3600 1209600 300"},
			first: map[string]any{
				"name": "example.test", "type": "SOA", "ttl": 300,
				"data": "ns1.example.test hostmaster.example.test 2026101001 7200 3600 1209600 300",
				"ns":   "ns1.example.test", "mbox": "hostmaster.example.test",
				"serial": int64(2026101001), "refresh": int64(7200), "retry": int64(3600), "expire": int64(1209600), "minimum": int64(300),
			},
		},
		{
			name:   "CAA",
			with:   map[string]any{"name": "example.test", "type": "CAA"},
			values: []any{"0 issue letsencrypt.org"},
			first:  map[string]any{"name": "example.test", "type": "CAA", "ttl": 300, "data": "0 issue letsencrypt.org", "flag": 0, "tag": "issue", "value": "letsencrypt.org"},
		},
		{
			name:   "SRV",
			with:   map[string]any{"name": "_sip._tcp.example.test", "type": "SRV"},
			values: []any{"10 60 5060 sip.example.test"},
			first: map[string]any{
				"name": "_sip._tcp.example.test", "type": "SRV", "ttl": 300, "data": "10 60 5060 sip.example.test",
				"priority": 10, "weight": 60, "port": 5060, "target": "sip.example.test",
			},
		},
		{
			name:   "PTR by the address it is asked for",
			with:   map[string]any{"name": "192.0.2.1", "type": "PTR"},
			values: []any{"example.test"},
			first:  map[string]any{"name": "1.2.0.192.in-addr.arpa", "type": "PTR", "ttl": 300, "data": "example.test"},
		},
		{
			name:   "a type with no fields of its own, as a zone file writes it",
			with:   map[string]any{"name": "example.test", "type": "DNSKEY"},
			values: []any{"257 3 13 mdsswUyr3DPW132mOi8V9xESWE8jTo0dxCjjnopKl+GqJxpVXckHAeF+KkxLbxILfDLUT0rAK9iUzy1L53eKGQ=="},
		},
		{
			name:   "values leave out the alias an answer came through",
			with:   map[string]any{"name": "www.example.test"},
			values: []any{"192.0.2.1"},
			first:  map[string]any{"name": "www.example.test", "type": "CNAME", "ttl": 300, "data": "example.test"},
		},
		{
			name:   "over TCP",
			with:   map[string]any{"name": "example.test", "protocol": "TCP"},
			values: []any{"192.0.2.1", "192.0.2.2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.with["server"] = server
			ret, err := Request(tt.with)
			if err != nil {
				t.Fatalf("Request() error = %v", err)
			}
			r := res(t, ret)
			if r["rcode"] != "NOERROR" || ret["status"] != 0 {
				t.Errorf("rcode = %v, status = %v, want NOERROR and 0", r["rcode"], ret["status"])
			}
			if !reflect.DeepEqual(r["values"], tt.values) {
				t.Errorf("values = %#v, want %#v", r["values"], tt.values)
			}
			if tt.first != nil {
				if got := r["answers"].([]any)[0]; !reflect.DeepEqual(got, tt.first) {
					t.Errorf("first answer = %#v, want %#v", got, tt.first)
				}
			}
			if r["server"] != server || r["authoritative"] != true {
				t.Errorf("server = %v, authoritative = %v, want %s and true", r["server"], r["authoritative"], server)
			}
			if rt, _ := ret["rt"].(string); rt == "" {
				t.Error("rt should be set")
			}
		})
	}
}

func TestRequestErrorAnswerIsAResult(t *testing.T) {
	server, _, _ := startServer(t, false)

	tests := []struct {
		name  string
		with  map[string]any
		rcode string
		want  int
	}{
		{"a name that does not exist", map[string]any{"name": "nope.example.test"}, "NXDOMAIN", 1},
		{"a server that refuses", map[string]any{"name": "other.test"}, "REFUSED", 1},
		// A name with no record of the type is no error.
		{"no record of the type", map[string]any{"name": "example.test", "type": "PTR"}, "NOERROR", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.with["server"] = server
			ret, err := Request(tt.with)
			if err != nil {
				t.Fatalf("Request() error = %v", err)
			}
			r := res(t, ret)
			if r["rcode"] != tt.rcode || ret["status"] != tt.want {
				t.Errorf("rcode = %v, status = %v, want %s and %d", r["rcode"], ret["status"], tt.rcode, tt.want)
			}
			if len(r["answers"].([]any)) != 0 || len(r["values"].([]any)) != 0 {
				t.Errorf("answers = %v, values = %v, want none", r["answers"], r["values"])
			}
		})
	}
}

func TestRequestAsksAgainOverTCPWhenCutShort(t *testing.T) {
	server, udp, tcp := startServer(t, true)

	ret, err := Request(map[string]any{"name": "example.test", "server": server})
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	r := res(t, ret)
	if r["protocol"] != "tcp" || !reflect.DeepEqual(r["values"], []any{"192.0.2.1", "192.0.2.2"}) {
		t.Errorf("protocol = %v, values = %v, want the answer over tcp", r["protocol"], r["values"])
	}
	if udp.Load() != 1 || tcp.Load() != 1 {
		t.Errorf("queries: %d over UDP and %d over TCP, want one of each", udp.Load(), tcp.Load())
	}
}

func TestRequestInvalid(t *testing.T) {
	tests := []struct {
		name    string
		with    map[string]any
		wantErr string
	}{
		{"no name", map[string]any{}, "name parameter is required"},
		{"unknown type", map[string]any{"name": "example.test", "type": "NOPE"}, "unsupported type: NOPE"},
		{"zone transfer", map[string]any{"name": "example.test", "type": "AXFR"}, "zone transfer"},
		{"unknown protocol", map[string]any{"name": "example.test", "protocol": "quic"}, "unsupported protocol: quic"},
		{"timeout that is no duration", map[string]any{"name": "example.test", "timeout": "soon"}, "invalid timeout soon"},
		{"timeout of zero", map[string]any{"name": "example.test", "timeout": "0"}, "invalid timeout 0"},
		{"name that is no domain name", map[string]any{"name": "a..b"}, "invalid name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Request(tt.with)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Request() error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRequestNoAnswerIsAnError(t *testing.T) {
	// A port of the loopback that nothing listens on.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := pc.LocalAddr().String()
	_ = pc.Close()

	_, err = Request(map[string]any{"name": "example.test", "server": server, "timeout": "300ms"})
	if err == nil || !strings.Contains(err.Error(), "no answer from the DNS server") || !strings.Contains(err.Error(), server) {
		t.Errorf("Request() error = %v, want one naming %s", err, server)
	}
}

func TestRequestTimesOut(t *testing.T) {
	// A server that takes the query and never answers.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	_, err = Request(map[string]any{"name": "example.test", "server": pc.LocalAddr().String(), "timeout": "200ms"})
	if err == nil || !strings.Contains(err.Error(), "timed out after 200ms") {
		t.Errorf("Request() error = %v, want a timeout", err)
	}
}

func TestRequestWaitsAsLongAsItsTimeout(t *testing.T) {
	// A server that takes the query and never answers, waited for longer
	// than the 2 seconds the DNS client waits by default.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	start := time.Now()
	_, err = Request(map[string]any{"name": "example.test", "server": pc.LocalAddr().String(), "timeout": "2300ms"})
	if err == nil || !strings.Contains(err.Error(), "timed out after 2.3s") {
		t.Errorf("Request() error = %v, want a timeout", err)
	}
	if waited := time.Since(start); waited < 2300*time.Millisecond {
		t.Errorf("Request() waited %s, want the 2.3s of its timeout", waited)
	}
}

func TestExchangeLeavesTimeForTheNextServer(t *testing.T) {
	server, _, _ := startServer(t, false)
	// A server that takes the query and never answers.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	silent := pc.LocalAddr().String()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	question := dns.Question{Name: "example.test.", Qtype: dns.TypeA, Qclass: dns.ClassINET}
	start := time.Now()
	_, got, _, err := (&Req{Protocol: "udp"}).exchange(ctx, question, []string{silent, server})
	if err != nil {
		t.Fatalf("exchange() error = %v, want the second server asked in the time left", err)
	}
	if got != server {
		t.Errorf("exchange() answered from %s, want %s", got, server)
	}
	// The silent server had half of the time, not all of it.
	if waited := time.Since(start); waited < 400*time.Millisecond || waited > 900*time.Millisecond {
		t.Errorf("exchange() took %s, want about the half second of the first server", waited)
	}
}

// useResolvConf has a request read the resolvers from a file of the test.
func useResolvConf(t *testing.T, content string) Option {
	t.Helper()
	path := filepath.Join(t.TempDir(), "resolv.conf")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return func(r *Req) { r.resolvConf = path }
}

func TestServersOfTheMachine(t *testing.T) {
	conf := useResolvConf(t, "nameserver 192.0.2.53\nnameserver 2001:db8::53\n")
	for protocol, want := range map[string][]string{
		"udp": {"192.0.2.53:53", "[2001:db8::53]:53"},
		"tls": {"192.0.2.53:853", "[2001:db8::53]:853"},
	} {
		r := &Req{Protocol: protocol}
		conf(r)
		if got, err := r.servers(); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("servers() over %s = %v, %v, want %v", protocol, got, err, want)
		}
	}
}

func TestExchangeAsksTheNextServerWhenOneDoesNotAnswer(t *testing.T) {
	server, _, _ := startServer(t, false)

	// A port of the loopback that nothing listens on.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dead := pc.LocalAddr().String()
	_ = pc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	question := dns.Question{Name: "example.test.", Qtype: dns.TypeA, Qclass: dns.ClassINET}
	msg, got, _, err := (&Req{Protocol: "udp"}).exchange(ctx, question, []string{dead, server})
	if err != nil {
		t.Fatalf("exchange() error = %v", err)
	}
	if got != server || len(msg.Answer) != 2 {
		t.Errorf("exchange() answered from %s with %d records, want %s with 2", got, len(msg.Answer), server)
	}
}

func TestRequestWithoutResolvers(t *testing.T) {
	_, err := Request(map[string]any{"name": "example.test"}, useResolvConf(t, "# none\n"))
	if err == nil || !strings.Contains(err.Error(), "names no resolver") {
		t.Errorf("Request() error = %v, want one saying no resolver is named", err)
	}

	_, err = Request(map[string]any{"name": "example.test"}, func(r *Req) { r.resolvConf = filepath.Join(t.TempDir(), "none") })
	if err == nil || !strings.Contains(err.Error(), "cannot be read") {
		t.Errorf("Request() error = %v, want one saying the resolvers cannot be read", err)
	}
}

func TestRequestUnderAGuard(t *testing.T) {
	server, udp, tcp := startServer(t, false)
	host, port, _ := net.SplitHostPort(server)

	// A server the guard allows is asked.
	for _, allow := range []string{server, host} {
		guard := actionrpc.Guard{ReadOnly: true, AllowHosts: []string{allow}}
		ret, err := Request(map[string]any{"name": "example.test", "server": server}, WithGuard(guard))
		if err != nil {
			t.Fatalf("Request() under --allow-host %s error = %v", allow, err)
		}
		if res(t, ret)["rcode"] != "NOERROR" {
			t.Errorf("under --allow-host %s: rcode = %v", allow, res(t, ret)["rcode"])
		}
	}
	before := udp.Load() + tcp.Load()

	// A server it does not allow is sent nothing.
	guard := actionrpc.Guard{AllowHosts: []string{"198.51.100.1"}}
	_, err := Request(map[string]any{"name": "example.test", "server": server}, WithGuard(guard))
	if !actionrpc.IsRefused(err) {
		t.Fatalf("Request() error = %v, want it refused", err)
	}

	// Nor is a resolver of the machine, though one of them is allowed.
	conf := useResolvConf(t, "nameserver "+host+"\nnameserver 198.51.100.2\noptions port:"+port+"\n")
	guard = actionrpc.Guard{AllowHosts: []string{host}}
	_, err = Request(map[string]any{"name": "example.test"}, WithGuard(guard), conf)
	if !actionrpc.IsRefused(err) || !strings.Contains(err.Error(), "give server") {
		t.Fatalf("Request() error = %v, want it refused, saying to give server", err)
	}
	if after := udp.Load() + tcp.Load(); after != before {
		t.Errorf("the server took %d queries that should have been refused", after-before)
	}
}

func TestServers(t *testing.T) {
	tests := []struct {
		server   string
		protocol string
		want     string
	}{
		{"192.0.2.53", "udp", "192.0.2.53:53"},
		{"192.0.2.53:5353", "udp", "192.0.2.53:5353"},
		{"dns.example.test", "tcp", "dns.example.test:53"},
		{"dns.example.test", "tls", "dns.example.test:853"},
		{"2001:db8::53", "udp", "[2001:db8::53]:53"},
		{"[2001:db8::53]", "tls", "[2001:db8::53]:853"},
		{"[2001:db8::53]:5353", "udp", "[2001:db8::53]:5353"},
	}
	for _, tt := range tests {
		got, err := (&Req{Server: tt.server, Protocol: tt.protocol}).servers()
		if err != nil || len(got) != 1 || got[0] != tt.want {
			t.Errorf("servers() of %s over %s = %v, %v, want %s", tt.server, tt.protocol, got, err, tt.want)
		}
	}
}

func TestParams(t *testing.T) {
	want := []string{"name", "type", "server", "protocol", "timeout"}
	if got := Spec().Params; !reflect.DeepEqual(got, want) {
		t.Errorf("Spec().Params = %v, want %v", got, want)
	}
}
