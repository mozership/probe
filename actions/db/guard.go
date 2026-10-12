package db

import (
	"fmt"
	"maps"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
	"github.com/mozership/probe/actionrpc"
)

// defaultPorts are the ports of the servers a DSN without one connects to.
var defaultPorts = map[string]string{"mysql": "3306", "postgres": "5432"}

// readVerbs are the words a statement allowed under a read-only guard
// starts with.
var readVerbs = []string{"SELECT", "SHOW", "DESCRIBE", "DESC", "EXPLAIN", "WITH"}

// checkGuard returns a Refused error when the guard of the run does not
// allow the query: a server it does not allow, or, under --read-only, a
// statement that is not one that reads, or more than one statement. A
// SQLite database is a local file, which no host names.
//
// A statement that reads by its first word may still write, as a WITH that
// deletes does, so that under --read-only the query also runs in a
// read-only transaction, where the database refuses a write itself.
//
// Under --read-only the query is also refused when it holds a semicolon
// anywhere but at its end, so that it cannot hold a second statement, such
// as one that ends the read-only transaction or turns query_only off.
func (r *Req) checkGuard(guard actionrpc.Guard) error {
	if len(guard.AllowHosts) > 0 {
		hosts, ok, err := r.hosts()
		if err != nil {
			return actionrpc.Refuse("%v", err)
		}
		if ok {
			for _, host := range hosts {
				if err := guard.CheckHost(host); err != nil {
					return err
				}
			}
		}
	}
	if !guard.ReadOnly {
		return nil
	}
	if err := r.checkConnectOptions(); err != nil {
		return err
	}
	query := strings.TrimRight(strings.TrimSpace(r.Query), "; \t\r\n")
	words := strings.Fields(query)
	if len(words) == 0 {
		return actionrpc.Refuse("the query holds no statement")
	}
	// A semicolon is refused wherever it is, in a string or a comment too:
	// what ends a string or a comment differs from one database to another,
	// and from one connection setting to another, so that a semicolon the
	// guard took for text could end a statement and start one that writes.
	if strings.Contains(query, ";") {
		return actionrpc.Refuse("the query holds a semicolon, which may end a statement and start another, and the run is read-only; give one statement that reads, without one")
	}
	word := strings.ToUpper(words[0])
	if !slices.Contains(readVerbs, word) {
		return actionrpc.Refuse("the statement %s may write, and the run is read-only; only %s are run", word, strings.Join(readVerbs, ", "))
	}
	return nil
}

// hosts returns the host and port of each server the DSN may connect to,
// and false for a SQLite database, which is a local file. Each is told by
// the driver's own reading of the DSN it connects with. For PostgreSQL
// they are the servers lib/pq resolves the DSN to, the same DSN it then
// connects with: its host, hostaddr and port parameters, a service file, and
// PGHOST, PGHOSTADDR, PGPORT and the like count, and every host of a list.
// A DSN it cannot tell the servers of is an error, which refuses it.
func (r *Req) hosts() ([]string, bool, error) {
	switch r.Driver {
	case "sqlite":
		return nil, false, nil
	case "postgres":
		cfg, err := pq.NewConfig(r.DSN)
		if err != nil {
			return nil, true, fmt.Errorf("the DSN cannot be read for its server: %w", err)
		}
		servers := []pq.ConfigMultihost{{Host: cfg.Host, Hostaddr: cfg.Hostaddr, Port: cfg.Port}}
		servers = append(servers, cfg.Multi...)
		var out []string
		for _, s := range servers {
			// hostaddr is the address dialed, when it is given.
			host := s.Host
			if s.Hostaddr.IsValid() {
				host = s.Hostaddr.String()
			}
			if host == "" {
				host = "localhost"
			}
			port := strconv.Itoa(int(s.Port))
			if s.Port == 0 {
				port = defaultPorts[r.Driver]
			}
			out = append(out, net.JoinHostPort(host, port))
		}
		return out, true, nil
	}
	// For MySQL it is the address go-sql-driver reads from the DSN the URL
	// is turned into, which is what it dials: a path can hold an @tcp(...)
	// of its own that the driver takes for the address.
	_, driverDSN, err := parseDSN(r.DSN)
	if err != nil {
		return nil, true, err
	}
	cfg, err := mysql.ParseDSN(driverDSN)
	if err != nil {
		return nil, true, fmt.Errorf("the DSN cannot be read for its server: %w", err)
	}
	if cfg.Net != "tcp" {
		return nil, true, fmt.Errorf("the DSN connects over %s, which the run cannot check against the hosts it allows", cfg.Net)
	}
	return []string{cfg.Addr}, true, nil
}

// sqliteReadOptions are the parameters of a SQLite DSN allowed under a
// read-only guard: none of them runs a statement when the connection opens.
var sqliteReadOptions = []string{"mode", "cache", "immutable", "_txlock", "_time_format"}

// checkConnectOptions returns a Refused error when the DSN would have the
// driver run a statement as the connection opens, before the query runs in
// its read-only transaction: for MySQL, multiStatements, or a system
// variable, which go-sql-driver sets with a statement of its own; for
// SQLite, any parameter but those known not to run one, such as _pragma.
// PostgreSQL sends its parameters at startup, where no statement runs.
func (r *Req) checkConnectOptions() error {
	switch r.Driver {
	case "mysql":
		_, driverDSN, err := parseDSN(r.DSN)
		if err != nil {
			return actionrpc.Refuse("the DSN cannot be read: %v", err)
		}
		cfg, err := mysql.ParseDSN(driverDSN)
		if err != nil {
			return actionrpc.Refuse("the DSN cannot be read: %v", err)
		}
		if cfg.MultiStatements {
			return actionrpc.Refuse("the DSN allows multiStatements, and the run is read-only")
		}
		if len(cfg.Params) > 0 {
			return actionrpc.Refuse("the DSN sets %s on connecting, with a statement run before the read-only transaction, and the run is read-only", strings.Join(slices.Sorted(maps.Keys(cfg.Params)), ", "))
		}
	case "sqlite":
		_, query, _ := strings.Cut(r.DSN, "?")
		params, err := url.ParseQuery(query)
		if err != nil {
			return actionrpc.Refuse("the DSN cannot be read: %v", err)
		}
		for _, name := range slices.Sorted(maps.Keys(params)) {
			if !slices.Contains(sqliteReadOptions, name) {
				return actionrpc.Refuse("the DSN sets %s, which may run a statement on connecting, and the run is read-only; only %s are allowed", name, strings.Join(sqliteReadOptions, ", "))
			}
		}
	}
	return nil
}
