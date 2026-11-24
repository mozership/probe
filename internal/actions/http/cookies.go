package http

import (
	"errors"
	"fmt"
	"maps"
	hp "net/http"
	"net/http/cookiejar"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
)

// stateCookiesKey names the cookies in the state the action keeps in a job.
const stateCookiesKey = "cookies"

// storedCookie is a cookie a server set, as the Set-Cookie header it came in,
// the URL of the response it came with, and when it came, which together are
// what a jar needs to set it again later as it was set then. The header is
// kept as it came, since one the jar refused, such as one with a domain that
// is not valid, must be refused again rather than set without the domain.
type storedCookie struct {
	URL       string
	SetCookie string
	// Received is when the cookie was set, in Unix seconds, which a Max-Age
	// counts from.
	Received int64
}

// cookieAt returns the URL the cookie was set from and the cookie as it
// stands at now, with a Max-Age counted from when it was set: one that has
// run out removes the cookie. ok is false when the cookie cannot be read.
func (s storedCookie) cookieAt(now time.Time) (u *url.URL, c *hp.Cookie, ok bool) {
	u, err := url.Parse(s.URL)
	if err != nil {
		return nil, nil, false
	}
	c, err = hp.ParseSetCookie(s.SetCookie)
	if err != nil {
		return nil, nil, false
	}
	if c.MaxAge > 0 {
		c.MaxAge -= int(now.Unix() - s.Received)
		if c.MaxAge <= 0 {
			c.MaxAge = -1
		}
	}
	return u, c, true
}

// removedAt reports whether c, as cookieAt returns it, is removed or has
// expired at now. A Max-Age is used in place of Expires when there is one.
func removedAt(c *hp.Cookie, now time.Time) bool {
	switch {
	case c.MaxAge < 0:
		return true
	case c.MaxAge > 0:
		return false
	default:
		return !c.Expires.IsZero() && !c.Expires.After(now)
	}
}

// cookieJar is the jar of one request. It sends the cookies kept in the job
// and those the step gives, and records the cookies the server sets along the
// way, on redirects included.
type cookieJar struct {
	jar *cookiejar.Jar
	// host is the host of the request the step makes. The cookies the step
	// gives are sent to it alone, not to another host a redirect leads to.
	host     string
	explicit []*hp.Cookie

	mu       sync.Mutex
	received []storedCookie
	values   map[string]string
	now      func() time.Time
}

// newCookieJar builds the jar of a request to rawURL, holding the cookies
// stored in the job and sending explicit to the host of rawURL.
func newCookieJar(rawURL string, explicit map[string]string, stored []storedCookie) (*cookieJar, error) {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, err
	}
	host := ""
	if u, err := url.Parse(rawURL); err == nil {
		host = u.Host
	}
	j := &cookieJar{jar: jar, host: host, values: map[string]string{}, now: time.Now}
	for _, name := range slices.Sorted(maps.Keys(explicit)) {
		j.explicit = append(j.explicit, &hp.Cookie{Name: name, Value: explicit[name]})
	}
	now := j.now()
	for _, s := range stored {
		if u, c, ok := s.cookieAt(now); ok {
			jar.SetCookies(u, []*hp.Cookie{c})
		}
	}
	return j, nil
}

// Cookies returns the cookies to send to u: those in the jar, with the ones
// the step gives in place of any of the same name when u is on the host the
// step requests.
func (j *cookieJar) Cookies(u *url.URL) []*hp.Cookie {
	cs := j.jar.Cookies(u)
	if len(j.explicit) == 0 || u.Host != j.host {
		return cs
	}
	out := slices.DeleteFunc(cs, func(c *hp.Cookie) bool {
		return slices.ContainsFunc(j.explicit, func(e *hp.Cookie) bool { return e.Name == c.Name })
	})
	return append(out, j.explicit...)
}

// SetCookies stores the cookies a response to u set, and records them with
// the time they came, as the Set-Cookie headers they came in.
func (j *cookieJar) SetCookies(u *url.URL, cookies []*hp.Cookie) {
	j.jar.SetCookies(u, cookies)
	j.mu.Lock()
	defer j.mu.Unlock()
	received := j.now().Unix()
	for _, c := range cookies {
		j.values[c.Name] = c.Value
		// Raw is the header as it came; a cookie built otherwise has none.
		header := c.Raw
		if header == "" {
			header = c.String()
		}
		j.received = append(j.received, storedCookie{URL: u.String(), SetCookie: header, Received: received})
	}
}

// Received returns the names and values of the cookies the server set, the
// last one of each name winning.
func (j *cookieJar) Received() map[string]string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return maps.Clone(j.values)
}

// Stored returns the cookies to keep in the job: those kept before and those
// set now, with only the last of each cookie, and none that has been removed
// or has expired.
func (j *cookieJar) Stored(before []storedCookie) []storedCookie {
	j.mu.Lock()
	all := append(slices.Clone(before), j.received...)
	j.mu.Unlock()
	return compactCookies(all, j.now())
}

// compactCookies keeps the last of each cookie in all, a cookie being told
// apart by its name, domain and path as a jar tells it apart, and drops those
// that are removed or expired by then.
func compactCookies(all []storedCookie, now time.Time) []storedCookie {
	type entry struct {
		cookie storedCookie
		parsed *hp.Cookie
	}
	var keys []string
	last := map[string]entry{}
	for _, s := range all {
		u, c, ok := s.cookieAt(now)
		if !ok {
			continue
		}
		p := c.Path
		if p == "" || !strings.HasPrefix(p, "/") {
			p = defaultCookiePath(u.Path)
		}
		// A domain cookie is the same cookie whichever host set it; a
		// host-only one belongs to the host that set it.
		domain := strings.ToLower(strings.TrimPrefix(c.Domain, "."))
		if domain == "" {
			domain = strings.ToLower(u.Hostname())
		}
		key := strings.Join([]string{c.Name, domain, p}, "\x00")
		if _, seen := last[key]; !seen {
			keys = append(keys, key)
		}
		last[key] = entry{cookie: s, parsed: c}
	}

	var out []storedCookie
	for _, k := range keys {
		e := last[k]
		if removedAt(e.parsed, now) {
			continue
		}
		out = append(out, e.cookie)
	}
	return out
}

// defaultCookiePath is the path a cookie without one applies to, as RFC 6265
// section 5.1.4 defines it from the path of the request.
func defaultCookiePath(p string) string {
	i := strings.LastIndex(p, "/")
	if !strings.HasPrefix(p, "/") || i == 0 {
		return "/"
	}
	return p[:i]
}

// cookiesFromState returns the cookies kept in state.
func cookiesFromState(state map[string]any) []storedCookie {
	list, _ := state[stateCookiesKey].([]any)
	var out []storedCookie
	for _, v := range list {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		u, _ := m["url"].(string)
		sc, _ := m["set_cookie"].(string)
		if u == "" || sc == "" {
			continue
		}
		out = append(out, storedCookie{URL: u, SetCookie: sc, Received: unixSeconds(m["received"])})
	}
	return out
}

// cookiesToState returns the state that keeps cookies.
func cookiesToState(cookies []storedCookie) map[string]any {
	list := make([]any, 0, len(cookies))
	for _, c := range cookies {
		list = append(list, map[string]any{"url": c.URL, "set_cookie": c.SetCookie, "received": c.Received})
	}
	return map[string]any{stateCookiesKey: list}
}

// unixSeconds returns a time kept in the state as Unix seconds, which reaches
// the action as an int64, or as another number when it did not travel.
func unixSeconds(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}

// takeCookies removes cookies and keep_cookies from m and returns the cookies
// the step gives and whether the job keeps the cookies the server sets.
func takeCookies(m map[string]any) (map[string]string, bool, error) {
	keep := false
	if v, ok := m["keep_cookies"]; ok {
		delete(m, "keep_cookies")
		b, ok := v.(bool)
		if !ok {
			return nil, false, errors.New("keep_cookies must be true or false")
		}
		keep = b
	}

	v, ok := m["cookies"]
	if !ok {
		return nil, keep, nil
	}
	delete(m, "cookies")
	fields, ok := v.(map[string]any)
	if !ok {
		return nil, false, errors.New("cookies must be a map of cookie names and values")
	}
	out := make(map[string]string, len(fields))
	for name, value := range fields {
		s, err := formValue(value)
		if err != nil {
			return nil, false, fmt.Errorf("cookies.%s %w", name, err)
		}
		if err := (&hp.Cookie{Name: name, Value: s}).Valid(); err != nil {
			return nil, false, fmt.Errorf("cookies.%s: %w", name, err)
		}
		out[name] = s
	}
	return out, keep, nil
}
