// Package mask hides secret values in everything Probe prints or writes.
package mask

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// redactedValue replaces the value of a header that carries credentials.
const redactedValue = "<redacted>"

// sensitiveKeys are header and parameter names whose values are credentials
// whatever they contain. A token obtained at run time, such as one returned by
// a login step, is never declared as a secret, but it reaches the target
// through one of these headers; a password written in a step's `with` need
// not be declared either. Their values are hidden wherever they are shown.
// cookies holds the cookies the http action sends and receives, by name.
var sensitiveKeys = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"cookie":              true,
	"set-cookie":          true,
	"cookies":             true,
	"password":            true,
	"key_passphrase":      true,
	"secret_access_key":   true,
	"session_token":       true,
}

// MinLength is the fewest characters a value must have to be masked inside
// other text. A shorter value, such as 1, ok or dev, is as likely to stand
// inside an unrelated word as for itself: replacing it wherever it appears
// mangles the whole output, and the letters missing from the words give it
// away anyway. Such a value is masked only where it is a whole value of a
// request or a response, which neither mangles nor gives away anything. A
// credential header or parameter shown by Map, and the password in a URL
// under dsn or url, are still replaced outright, whatever their length, since
// that depends on their names, not their values.
const MinLength = 4

// dsnKey names the parameter that holds a database URL. Only the password in
// it is a credential: the rest says which database was used, so it stays
// visible.
const dsnKey = "dsn"

// urlKey names the parameter that holds the URL an action connects to. It
// may carry a password as a database URL does, as redis://user:password@host
// does, and that password is hidden in the same way. Unlike a dsn it is not
// taken to hold a credential as a whole, since most URLs hold none and what
// is written there is worth showing in a message.
const urlKey = "url"

// holdsURL reports whether the parameter named key holds a URL whose
// password is a credential.
func holdsURL(key string) bool {
	k := strings.ToLower(key)
	return k == dsnKey || k == urlKey
}

// HoldsCredential reports whether the parameter or header named key holds a
// credential, whole or, as a database URL does, in part. A message about such
// a value, such as why its template could not be evaluated, must not show
// what was written there.
func HoldsCredential(key string) bool {
	k := strings.ToLower(key)
	return sensitiveKeys[k] || k == dsnKey
}

// basicAuthKey names the parameter of the http action that holds the username
// and password of HTTP Basic authentication. The action sends them in an
// Authorization header it builds itself, so the value of that header is never
// in the parameters the masker learns from, and is worked out here.
const basicAuthKey = "basic_auth"

// Masker hides secret values in everything Probe prints or writes. It starts
// with the secrets the workflow declares and learns the values of credential
// headers as actions are about to send them, so that a token obtained at run
// time is hidden too. A nil Masker hides only credential headers in the maps
// passed to Map, and learns nothing.
type Masker struct {
	mu       sync.RWMutex
	labels   map[string]string // secret value -> what it is replaced with
	replacer *strings.Replacer
	exact    map[string]string // short value -> its label, masked only whole
	tooShort []string          // declared secrets too short to mask in text
}

// New builds a Masker for the environment variables named in secrets.
// Each value is replaced by <secret:NAME>. A name that is not set, or set to
// an empty string, has nothing to hide and is skipped. A value shorter than
// MinLength is masked only where it is a whole value; TooShort names those
// secrets so that the caller can warn about them.
func New(secrets []string, env map[string]string) *Masker {
	m := &Masker{labels: make(map[string]string), exact: make(map[string]string)}
	seen := make(map[string]bool, len(secrets))
	for _, name := range secrets {
		value := env[name]
		if value != "" && tooShort(value) && !seen[name] {
			m.tooShort = append(m.tooShort, name)
		}
		seen[name] = true
		m.addLocked(value, "<secret:"+name+">")
	}
	m.rebuildLocked()
	return m
}

// TooShort returns the names of the declared secrets whose values are set but
// shorter than MinLength, and so are masked only where they are a whole value,
// once each in the order they were declared. It never returns the values.
func (m *Masker) TooShort() []string {
	if m == nil {
		return nil
	}
	return m.tooShort
}

// tooShort reports whether value has fewer than MinLength characters.
func tooShort(value string) bool {
	return utf8.RuneCountInString(value) < MinLength
}

// addLocked registers a value under the label it is replaced with, along with
// the escaped forms it takes when Go, JSON or a log line quotes it, since a
// secret containing a quote or a backslash would otherwise slip through in
// an error message or a dump. A value shorter than MinLength is kept apart,
// to be masked only where it is a whole value, and reported as not added,
// since the replacer need not change. Callers must hold mu or own m
// exclusively.
func (m *Masker) addLocked(value, label string) bool {
	if value == "" {
		return false
	}
	if tooShort(value) {
		for _, v := range escapedForms(value) {
			if _, exists := m.exact[v]; !exists {
				m.exact[v] = label
			}
		}
		return false
	}
	// A value holding a URL with a password, such as a declared DSN, is
	// also registered with that password redacted, as String finds it
	// after hiding the passwords of URLs, so that it is still replaced
	// with its label as a whole.
	forms := escapedForms(value)
	if r := redactURLPasswords(value); r != value {
		forms = append(forms, escapedForms(r)...)
	}
	added := false
	for _, v := range forms {
		if _, exists := m.labels[v]; exists {
			continue
		}
		m.labels[v] = label
		added = true
	}
	return added
}

// rebuildLocked recompiles the replacer from the registered values. A value
// that contains another is replaced first, or the shorter one would cut it
// apart and leave the rest of it visible.
func (m *Masker) rebuildLocked() {
	if len(m.labels) == 0 {
		m.replacer = nil
		return
	}
	values := make([]string, 0, len(m.labels))
	for v := range m.labels {
		values = append(values, v)
	}
	sort.Slice(values, func(i, j int) bool {
		if len(values[i]) != len(values[j]) {
			return len(values[i]) > len(values[j])
		}
		return values[i] < values[j]
	})
	oldnew := make([]string, 0, len(values)*2)
	for _, v := range values {
		oldnew = append(oldnew, v, m.labels[v])
	}
	m.replacer = strings.NewReplacer(oldnew...)
}

// escapedForms returns value as written and in the escaped forms it takes
// when Go quotes it, JSON encodes it, a log line escapes its double quotes,
// or a form body percent-encodes it.
// The escapes are also applied on top of each other, because an error that Go
// quoted is often quoted again by the logger that prints it.
func escapedForms(value string) []string {
	escapers := []func(string) string{
		func(s string) string {
			q := strconv.Quote(s)
			return q[1 : len(q)-1]
		},
		func(s string) string {
			var b strings.Builder
			enc := json.NewEncoder(&b)
			enc.SetEscapeHTML(false)
			_ = enc.Encode(s)
			e := strings.TrimSuffix(b.String(), "\n")
			return e[1 : len(e)-1]
		},
		func(s string) string {
			js, _ := json.Marshal(s)
			return string(js[1 : len(js)-1])
		},
		func(s string) string {
			return strings.ReplaceAll(s, `"`, `\"`)
		},
		// A form body sends each value percent-encoded.
		url.QueryEscape,
	}

	seen := map[string]bool{value: true}
	forms := []string{value}
	layer := []string{value}
	for range 2 {
		var next []string
		for _, f := range layer {
			for _, esc := range escapers {
				e := esc(f)
				if !seen[e] {
					seen[e] = true
					forms = append(forms, e)
					next = append(next, e)
				}
			}
		}
		layer = next
	}
	return forms
}

// Learn registers the values of credential headers and parameters found
// anywhere in data, and the password in a database URL, so that they are
// hidden from then on. It is called with an action's parameters
// before the action runs, which is what keeps them out of the action's own
// log records.
func (m *Masker) Learn(data map[string]any) {
	if m == nil || data == nil {
		return
	}
	var found []string
	collectSensitive(data, &found)
	if len(found) == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	added := false
	for _, v := range found {
		if m.addLocked(v, redactedValue) {
			added = true
		}
	}
	if added {
		m.rebuildLocked()
	}
}

func collectSensitive(v any, found *[]string) {
	switch val := v.(type) {
	case map[string]any:
		for k, e := range val {
			if auth, ok := e.(map[string]any); ok && strings.ToLower(k) == basicAuthKey {
				*found = append(*found, basicAuthValues(auth)...)
				// Everything in it but the username is a credential, also
				// under a key the action refuses, such as a mistyped pass:
				// the action logs its parameters before it checks them.
				for ak, av := range auth {
					if strings.ToLower(ak) != "username" {
						collectStrings(av, found)
					}
				}
				continue
			}
			if sensitiveKeys[strings.ToLower(k)] {
				collectStrings(e, found)
				continue
			}
			if dsn, ok := e.(string); ok && holdsURL(k) {
				*found = append(*found, dsnPassword(dsn)...)
				continue
			}
			collectSensitive(e, found)
		}
	case map[string]string:
		for k, s := range val {
			switch {
			case sensitiveKeys[strings.ToLower(k)]:
				*found = append(*found, s)
			case holdsURL(k):
				*found = append(*found, dsnPassword(s)...)
			}
		}
	case []any:
		for _, e := range val {
			collectSensitive(e, found)
		}
	}
}

// basicAuthValues returns the Authorization header the http action builds from
// auth, and the encoded credentials alone, as they are shown when the header
// is split. A username or password that is a number may reach the action as
// another number, so the header is worked out for each form it may take;
// without a username the action sends nothing.
func basicAuthValues(auth map[string]any) []string {
	usernames, passwords := scalarForms(auth["username"]), scalarForms(auth["password"])
	if len(usernames) == 0 || usernames[0] == "" {
		return nil
	}
	var found []string
	for _, u := range usernames {
		for _, p := range passwords {
			token := base64.StdEncoding.EncodeToString([]byte(u + ":" + p))
			found = append(found, "Basic "+token, token)
		}
	}
	return found
}

// scalarForms returns how a value of a parameter is written, by the workflow
// and by the action that receives it, which differ only for a number: an
// action receives every number as a float64 and gets back an integer when it
// holds one, so 9007199254740993 arrives as 9007199254740992. A missing
// value is written as empty.
func scalarForms(v any) []string {
	if v == nil {
		return []string{""}
	}
	if forms, ok := numberForms(v); ok {
		return forms
	}
	return []string{fmt.Sprint(v)}
}

// numberForms returns a number as written and as an action receives it,
// once when the two are the same, or false when v is not a number.
func numberForms(v any) ([]string, bool) {
	var f float64
	switch n := v.(type) {
	case int:
		f = float64(n)
	case int8:
		f = float64(n)
	case int16:
		f = float64(n)
	case int32:
		f = float64(n)
	case int64:
		f = float64(n)
	case uint:
		f = float64(n)
	case uint8:
		f = float64(n)
	case uint16:
		f = float64(n)
	case uint32:
		f = float64(n)
	case uint64:
		f = float64(n)
	case float32:
		f = float64(n)
	case float64:
		f = n
	default:
		return nil, false
	}
	written := fmt.Sprint(v)
	if n, ok := v.(float64); ok {
		written = strconv.FormatFloat(n, 'f', -1, 64)
	}
	received := strconv.FormatFloat(f, 'f', -1, 64)
	if f == math.Trunc(f) && f >= math.MinInt64 && f <= math.MaxInt64 {
		received = strconv.FormatInt(int64(f), 10)
	}
	if received == written {
		return []string{written}, true
	}
	return []string{written, received}, true
}

// HoldsURL reports whether the parameter named key holds a URL whose
// password is a credential, as dsn and url do.
func HoldsURL(key string) bool {
	return holdsURL(key)
}

// Userinfo returns the user info of a URL, what stands between its scheme
// and the last @ of its authority, as user:password, or nothing when it has
// none. It does not rely on url.Parse, which rejects the tcp(host:port)
// address a MySQL DSN may carry.
func Userinfo(rawURL string) string {
	_, rest, ok := strings.Cut(rawURL, "://")
	if !ok {
		return ""
	}
	// The authority ends where the path, query or fragment begins, and the
	// user info at the last @ before that, since an unescaped password may
	// itself contain an @.
	authority := rest
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		authority = rest[:i]
	}
	at := strings.LastIndex(authority, "@")
	if at < 0 {
		return ""
	}
	return authority[:at]
}

// redactURLPassword returns rawURL with the password in its user info
// replaced by <redacted>, whatever its length, or rawURL itself when it has
// none.
func redactURLPassword(rawURL string) string {
	scheme, rest, ok := strings.Cut(rawURL, "://")
	if !ok {
		return rawURL
	}
	userinfo := Userinfo(rawURL)
	user, password, ok := strings.Cut(userinfo, ":")
	if !ok || password == "" {
		return rawURL
	}
	return scheme + "://" + user + ":" + redactedValue + rest[len(userinfo):]
}

// urlAuthorityEnd holds the bytes that end the authority of a URL found in
// text: the start of its path, query or fragment, and what stands around a
// URL in a message or a log line. < and > are not allowed in a URL, and stop
// a scan at a label such as <redacted> that is already in the text.
var urlAuthorityEnd = func() (set [256]bool) {
	for _, c := range []byte("/?#\"'`<> \t\r\n") {
		set[c] = true
	}
	return set
}()

// redactURLPasswords returns s with the password of every URL in it, such
// as postgres://app:pw@db/main in a driver's error or a remote quoted in a
// response body, replaced by <redacted>, whatever its length and whether or
// not it was ever learned. As Userinfo does, the user info is taken to end
// at the last @ of the authority, so that a password holding an unescaped @
// is hidden whole, and the user name to end at its first colon.
//
// It runs on everything Probe writes, so it looks at each @, which is rare
// in most output, rather than at each URL: a URL with user info has an @
// right after "://" and the characters of a host name or an address.
func redactURLPasswords(s string) string {
	if strings.IndexByte(s, '@') < 0 || !strings.Contains(s, "://") {
		return s
	}
	var b strings.Builder
	done := 0
	for i := 0; i < len(s); {
		k := strings.IndexByte(s[i:], '@')
		if k < 0 {
			break
		}
		// The authority the @ stands in runs back to the "://" before
		// it and on to where its path, or the text around it, begins.
		start := i + k
		for start > i && !urlAuthorityEnd[s[start-1]] {
			start--
		}
		end := i + k + 1
		for end < len(s) && !urlAuthorityEnd[s[end]] {
			end++
		}
		i = end
		if !strings.HasSuffix(s[:start], "://") {
			continue
		}
		authority := s[start:end]
		at := strings.LastIndexByte(authority, '@')
		colon := strings.IndexByte(authority[:at], ':')
		if colon < 0 || colon+1 == at {
			continue
		}
		if done == 0 {
			b.Grow(len(s))
		}
		b.WriteString(s[done : start+colon+1])
		b.WriteString(redactedValue)
		done = start + at
	}
	if done == 0 {
		return s
	}
	b.WriteString(s[done:])
	return b.String()
}

// dsnPassword returns the password in a URL-style DSN, both as written and
// percent-decoded, or nothing when it has none.
func dsnPassword(dsn string) []string {
	_, password, ok := strings.Cut(Userinfo(dsn), ":")
	if !ok || password == "" {
		return nil
	}
	found := []string{password}
	if decoded, err := url.PathUnescape(password); err == nil && decoded != password {
		found = append(found, decoded)
	}
	return found
}

// collectStrings gathers the values a credential header or parameter holds;
// Set-Cookie, for one, can carry several.
func collectStrings(v any, found *[]string) {
	switch val := v.(type) {
	case string:
		*found = append(*found, val)
	// A password written without quotes, such as 123456, is a number in the
	// workflow, and reaches the action as the same digits. A boolean is not
	// learned: hiding every "true" in the output would hide nothing useful.
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		*found = append(*found, fmt.Sprint(val))
		forms, _ := numberForms(val)
		*found = append(*found, forms...)
	case []any:
		for _, e := range val {
			collectStrings(e, found)
		}
	case []string:
		*found = append(*found, val...)
	case map[string]any:
		for _, e := range val {
			collectStrings(e, found)
		}
	case map[string]string:
		for _, e := range val {
			*found = append(*found, e)
		}
	}
}

// String hides every secret value in s, and the password of every URL in it.
func (m *Masker) String(s string) string {
	if m == nil {
		return s
	}
	s = redactURLPasswords(s)
	m.mu.RLock()
	r := m.replacer
	m.mu.RUnlock()
	if r == nil {
		return s
	}
	return r.Replace(s)
}

// Map returns a copy of a request or response with secret values hidden in
// every string, and the values of credential headers replaced outright. The
// original is left untouched, since tests and outputs still need the real
// values. A nil map stays nil.
func (m *Masker) Map(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	out := make(map[string]any, len(data))
	for k, v := range data {
		if sensitiveKeys[strings.ToLower(k)] {
			out[k] = redacted(v)
			continue
		}
		if s, ok := v.(string); ok && holdsURL(k) {
			out[k] = m.leaf(redactURLPassword(s))
			continue
		}
		out[k] = m.value(v)
	}
	return out
}

// redacted returns what a credential v is shown as. A map, such as the
// cookies of a request, keeps its keys, which name the credentials and help
// to tell what was sent, and has its values hidden.
func redacted(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k := range val {
			out[k] = redactedValue
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(val))
		for k := range val {
			out[k] = redactedValue
		}
		return out
	default:
		return redactedValue
	}
}

// Value returns v, a value read out of a request or a response, with secret
// values hidden in every string at any depth, as Map hides them in a whole
// one. The original is left untouched.
func (m *Masker) Value(v any) any {
	return m.value(v)
}

func (m *Masker) value(v any) any {
	switch val := v.(type) {
	case string:
		return m.leaf(val)
	case map[string]any:
		return m.Map(val)
	case map[string]string:
		out := make(map[string]any, len(val))
		for k, s := range val {
			if sensitiveKeys[strings.ToLower(k)] {
				out[k] = redactedValue
				continue
			}
			if holdsURL(k) {
				s = redactURLPassword(s)
			}
			out[k] = m.leaf(s)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, e := range val {
			out[i] = m.value(e)
		}
		return out
	case []string:
		out := make([]any, len(val))
		for i, e := range val {
			out[i] = m.leaf(e)
		}
		return out
	default:
		return m.number(v)
	}
}

// number returns the label of a value too short to mask inside other text
// when v is a number written as that value, such as 123 parsed from a JSON
// body, and v itself otherwise.
func (m *Masker) number(v any) any {
	if m == nil {
		return v
	}
	forms, ok := numberForms(v)
	if !ok {
		return v
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, f := range forms {
		if label, ok := m.exact[f]; ok {
			return label
		}
	}
	return v
}

// leaf hides the secret values in s, a whole value of a request or a
// response. A value too short to mask inside other text is masked here when
// s is that value and nothing else.
func (m *Masker) leaf(s string) string {
	if m == nil {
		return s
	}
	m.mu.RLock()
	label, ok := m.exact[s]
	m.mu.RUnlock()
	if ok {
		return label
	}
	return m.String(s)
}

// Writer wraps w so that secret values are hidden in what is written to it.
// Every caller writes whole messages or lines, so a value is never split
// across two writes. The wrapper consults the masker on each write, so values
// learned later are hidden too.
func (m *Masker) Writer(w io.Writer) io.Writer {
	if m == nil {
		return w
	}
	return &maskingWriter{w: w, m: m}
}

type maskingWriter struct {
	w io.Writer
	m *Masker
}

func (mw *maskingWriter) Write(p []byte) (int, error) {
	if _, err := io.WriteString(mw.w, mw.m.String(string(p))); err != nil {
		return 0, err
	}
	// Report the input length: the caller wrote all of p, whatever it
	// became on the way out.
	return len(p), nil
}
