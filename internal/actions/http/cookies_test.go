package http

import (
	"maps"
	hp "net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mozership/probe/actionrpc"
)

// newLoginServer serves /login, which sets a session cookie and redirects to
// /home, /home, which answers 200 only with that cookie, /logout, which
// removes it, and /echo, which answers with the Cookie header it got.
func newLoginServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := hp.NewServeMux()
	mux.HandleFunc("/login", func(w hp.ResponseWriter, r *hp.Request) {
		hp.SetCookie(w, &hp.Cookie{Name: "session", Value: "abc", Path: "/", MaxAge: 3600, HttpOnly: true})
		hp.Redirect(w, r, "/home", hp.StatusFound)
	})
	mux.HandleFunc("/home", func(w hp.ResponseWriter, r *hp.Request) {
		if c, err := r.Cookie("session"); err != nil || c.Value != "abc" {
			w.WriteHeader(hp.StatusUnauthorized)
		}
		_, _ = w.Write([]byte(r.Header.Get("Cookie")))
	})
	mux.HandleFunc("/logout", func(w hp.ResponseWriter, r *hp.Request) {
		hp.SetCookie(w, &hp.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1})
	})
	mux.HandleFunc("/echo", func(w hp.ResponseWriter, r *hp.Request) {
		_, _ = w.Write([]byte(r.Header.Get("Cookie")))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func resOf(t *testing.T, ret map[string]any) map[string]any {
	t.Helper()
	res, ok := ret["res"].(map[string]any)
	if !ok {
		t.Fatalf("no res in %v", ret)
	}
	return res
}

func TestRequestSendsCookiesSetOnARedirect(t *testing.T) {
	srv := newLoginServer(t)

	ret, err := Request(map[string]any{
		"url":     srv.URL,
		"post":    "/login",
		"headers": map[string]any{"cookie": "theme=dark"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := resOf(t, ret)
	if res["code"] != 200 {
		t.Errorf("the cookie set on the redirect should reach /home, got %v", res["code"])
	}
	// A cookie written in headers is sent on as well.
	if body, _ := res["body"].(string); !strings.Contains(body, "theme=dark") {
		t.Errorf("the cookie header should be sent on to /home, got %q", body)
	}
	if !reflect.DeepEqual(res["cookies"], map[string]string{"session": "abc"}) {
		t.Errorf("res.cookies = %#v", res["cookies"])
	}
}

func TestRequestWithStateKeepsCookies(t *testing.T) {
	srv := newLoginServer(t)

	_, state, err := requestWithState(map[string]any{"url": srv.URL, "post": "/login", "keep_cookies": true}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state == nil {
		t.Fatal("keep_cookies should leave the cookies in the state")
	}

	ret, state2, err := requestWithState(map[string]any{"url": srv.URL, "get": "/home", "keep_cookies": true}, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code := resOf(t, ret)["code"]; code != 200 {
		t.Errorf("the kept cookie should be sent, got %v", code)
	}
	if !reflect.DeepEqual(state2, state) {
		t.Errorf("a request that sets no cookie should keep the state, got %v, want %v", state2, state)
	}

	// Without keep_cookies the step neither sends nor changes them.
	ret, state3, err := requestWithState(map[string]any{"url": srv.URL, "get": "/home"}, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code := resOf(t, ret)["code"]; code != 401 {
		t.Errorf("the kept cookie should not be sent without keep_cookies, got %v", code)
	}
	if state3 != nil {
		t.Errorf("without keep_cookies the state should be left as it was, got %v", state3)
	}

	// A removed cookie leaves the state.
	_, state4, err := requestWithState(map[string]any{"url": srv.URL, "get": "/logout", "keep_cookies": true}, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(state4, map[string]any{"cookies": []any{}}) {
		t.Errorf("state after logout = %v", state4)
	}
}

// TestRequestKeepsOnlyCookiesTheJarTakes checks that a cookie the jar
// refuses, such as one with a domain that is not valid, is not sent in a
// later step either.
func TestRequestKeepsOnlyCookiesTheJarTakes(t *testing.T) {
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		if r.URL.Path == "/set" {
			w.Header().Add("Set-Cookie", "bad=1; Domain=bad_domain; Path=/")
			w.Header().Add("Set-Cookie", "good=1; Path=/")
			return
		}
		_, _ = w.Write([]byte(r.Header.Get("Cookie")))
	}))
	defer srv.Close()

	_, state, err := requestWithState(map[string]any{"url": srv.URL, "get": "/set", "keep_cookies": true}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ret, _, err := requestWithState(map[string]any{"url": srv.URL, "get": "/echo", "keep_cookies": true}, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body := resOf(t, ret)["body"]; body != "good=1" {
		t.Errorf("Cookie header = %q, want only the cookie the jar took", body)
	}
}

func TestRequestSendsGivenCookies(t *testing.T) {
	srv := newLoginServer(t)
	_, state, err := requestWithState(map[string]any{"url": srv.URL, "post": "/login", "keep_cookies": true}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ret, _, err := requestWithState(map[string]any{
		"url":          srv.URL,
		"get":          "/echo",
		"keep_cookies": true,
		"cookies":      map[string]any{"session": "other", "lang": "ja", "n": float64(1)},
	}, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body, _ := resOf(t, ret)["body"].(string)
	got := strings.Split(body, "; ")
	want := []string{"lang=ja", "n=1", "session=other"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Cookie header = %q, want the given cookies in place of the kept one", body)
	}
	req := ret["req"].(map[string]any)
	if _, ok := req["cookies"]; ok {
		t.Error("cookies should not be carried into req")
	}
}

func TestRequestDoesNotSendGivenCookiesToAnotherHost(t *testing.T) {
	var got string
	other := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		got = r.Header.Get("Cookie")
	}))
	defer other.Close()
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		hp.Redirect(w, r, other.URL, hp.StatusFound)
	}))
	defer srv.Close()

	if _, err := Request(map[string]any{
		"url":     srv.URL,
		"get":     "/",
		"cookies": map[string]any{"token": "t"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("the given cookies should stay with the host of the request, got %q", got)
	}
}

func TestCompactCookies(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour).Format(hp.TimeFormat)
	future := now.Add(time.Hour).Format(hp.TimeFormat)
	all := []storedCookie{
		{URL: "http://a.test/login", SetCookie: "session=1; Path=/"},
		{URL: "http://a.test/app/x", SetCookie: "pref=1"},
		{URL: "http://a.test/login", SetCookie: "session=2; Path=/"},
		{URL: "http://a.test/login", SetCookie: "session=3; Path=/admin"},
		{URL: "http://b.test/login", SetCookie: "session=4; Path=/"},
		{URL: "http://a.test/", SetCookie: "old=1; Expires=" + past},
		{URL: "http://a.test/", SetCookie: "later=1; Expires=" + future},
		{URL: "http://a.test/", SetCookie: "gone=1"},
		{URL: "http://a.test/", SetCookie: "gone=; Max-Age=0"},
		{URL: "http://a.test/app/y", SetCookie: "pref=2"},
	}
	want := []storedCookie{
		{URL: "http://a.test/login", SetCookie: "session=2; Path=/"},
		{URL: "http://a.test/app/y", SetCookie: "pref=2"},
		{URL: "http://a.test/login", SetCookie: "session=3; Path=/admin"},
		{URL: "http://b.test/login", SetCookie: "session=4; Path=/"},
		{URL: "http://a.test/", SetCookie: "later=1; Expires=" + future},
	}
	if got := compactCookies(all, now); !reflect.DeepEqual(got, want) {
		t.Errorf("compactCookies() =\n%v\nwant\n%v", got, want)
	}
}

// TestCompactCookiesDomainCookie checks that a domain cookie is told apart
// by its domain alone, so that one host removes what another set.
func TestCompactCookiesDomainCookie(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	all := []storedCookie{
		{URL: "http://a.example.com/", SetCookie: "session=1; Domain=example.com; Path=/"},
		{URL: "http://a.example.com/", SetCookie: "host=1; Path=/"},
		{URL: "http://b.example.com/", SetCookie: "session=; Domain=.EXAMPLE.com; Path=/; Max-Age=0"},
		{URL: "http://b.example.com/", SetCookie: "host=2; Path=/"},
	}
	want := []storedCookie{
		{URL: "http://a.example.com/", SetCookie: "host=1; Path=/"},
		{URL: "http://b.example.com/", SetCookie: "host=2; Path=/"},
	}
	if got := compactCookies(all, now); !reflect.DeepEqual(got, want) {
		t.Errorf("compactCookies() =\n%v\nwant\n%v", got, want)
	}
}

func TestCookieJarCountsMaxAgeFromWhenItWasSet(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	j, err := newCookieJar("http://a.test/", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	j.now = func() time.Time { return now }
	u := mustParseURL(t, "http://a.test/login")
	j.SetCookies(u, []*hp.Cookie{{Name: "session", Value: "abc", Path: "/", MaxAge: 60}})

	stored := j.Stored(nil)
	if len(stored) != 1 || stored[0].Received != now.Unix() {
		t.Fatalf("Stored() = %v", stored)
	}

	// Half a minute later, half of the minute is left.
	_, c, ok := stored[0].cookieAt(now.Add(30 * time.Second))
	if !ok || c.MaxAge != 30 {
		t.Errorf("cookieAt() = %v, %v, want a Max-Age of 30", c, ok)
	}
	if got := compactCookies(stored, now.Add(30*time.Second)); len(got) != 1 {
		t.Errorf("the cookie should be kept before it runs out, got %v", got)
	}

	// Once the minute is over, it is removed.
	_, c, ok = stored[0].cookieAt(now.Add(time.Minute))
	if !ok || c.MaxAge != -1 {
		t.Errorf("cookieAt() = %v, %v, want the cookie removed", c, ok)
	}
	if got := compactCookies(stored, now.Add(time.Minute)); len(got) != 0 {
		t.Errorf("the cookie should be dropped once it runs out, got %v", got)
	}
}

// TestCookiesTravelInState checks that the cookies kept come back from the
// state as they went in, with the time they were set as it arrives from an
// action in a process of its own.
func TestCookiesTravelInState(t *testing.T) {
	in := []storedCookie{{URL: "http://a.test/", SetCookie: "a=1; Max-Age=60", Received: 1759665600}}
	state := cookiesToState(in)
	if got := cookiesFromState(state); !reflect.DeepEqual(got, in) {
		t.Errorf("cookiesFromState() = %v, want %v", got, in)
	}
	sent := map[string]any{"cookies": []any{map[string]any{"url": "http://a.test/", "set_cookie": "a=1; Max-Age=60", "received": float64(1759665600)}}}
	if got := cookiesFromState(sent); !reflect.DeepEqual(got, in) {
		t.Errorf("cookiesFromState() = %v, want %v", got, in)
	}
}

func TestDefaultCookiePath(t *testing.T) {
	for p, want := range map[string]string{
		"":       "/",
		"x":      "/",
		"/":      "/",
		"/login": "/",
		"/app/x": "/app",
		"/a/b/":  "/a/b",
	} {
		if got := defaultCookiePath(p); got != want {
			t.Errorf("defaultCookiePath(%q) = %q, want %q", p, got, want)
		}
	}
}

func TestRequestCookiesRejected(t *testing.T) {
	tests := []struct {
		name    string
		data    map[string]any
		wantErr string
	}{
		{
			name:    "keep_cookies that is not a boolean",
			data:    map[string]any{"keep_cookies": "yes"},
			wantErr: "keep_cookies must be true or false",
		},
		{
			name:    "cookies that are not a map",
			data:    map[string]any{"cookies": "a=b"},
			wantErr: "cookies must be a map of cookie names and values",
		},
		{
			name:    "a cookie value that is a map",
			data:    map[string]any{"cookies": map[string]any{"a": map[string]any{}}},
			wantErr: "cookies.a must be a string, a number or a boolean",
		},
		{
			name:    "a cookie name that is not a token",
			data:    map[string]any{"cookies": map[string]any{"a b": "c"}},
			wantErr: "cookies.a b: http: invalid Cookie.Name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
				called = true
			}))
			defer srv.Close()

			data := map[string]any{"url": srv.URL, "method": "GET"}
			maps.Copy(data, tt.data)
			_, _, err := requestWithState(data, nil)
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("error = %v, want %q", err, tt.wantErr)
			}
			if called {
				t.Error("the request should not be sent")
			}
		})
	}
}

func mustParseURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// requestWithState makes the request of a step with the state kept in the job.
func requestWithState(data, state map[string]any) (map[string]any, map[string]any, error) {
	return RequestStep(actionrpc.Call{With: data, State: state})
}
