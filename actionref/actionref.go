// Package actionref resolves an action that a step's `uses` names outside
// Probe to the executable that serves it.
//
// Such an action is published in a GitHub repository and pinned to a commit:
//
//	uses: github.com/<owner>/<repo>[/<dir>]@<40-character commit SHA>
//
// or lives in a local directory:
//
//	uses: ./path/to/action
//
// Either way the directory holds an action.yml that says which executable
// serves the action. The executable speaks the protocol in package actionrpc.
package actionref

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-yaml"
)

// ManifestFile is the file in an action's directory that describes it.
const ManifestFile = "action.yml"

// UsingBinary is the only kind of action an action.yml can describe: an
// executable that serves the action over the actionrpc protocol.
const UsingBinary = "binary"

const githubHost = "github.com"

var (
	commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)
	pathPart  = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

// IsExternal reports whether uses names an action outside Probe. No built-in
// action has a slash in its name.
func IsExternal(uses string) bool {
	return strings.Contains(uses, "/")
}

// Ref is a parsed `uses` that names an external action.
type Ref struct {
	// Local is the directory of a local action, as written. The other fields
	// are empty when it is set.
	Local string
	// Owner and Repo name the GitHub repository of a remote action.
	Owner string
	Repo  string
	// Dir is the directory of the action within the repository, slash
	// separated, or empty for the repository root.
	Dir string
	// SHA is the commit the action is pinned to.
	SHA string
}

// Parse parses uses as a reference to an external action.
func Parse(uses string) (Ref, error) {
	if isLocal(uses) {
		return Ref{Local: uses}, nil
	}

	rest, ok := strings.CutPrefix(uses, githubHost+"/")
	if !ok {
		return Ref{}, fmt.Errorf("unsupported action %q: use github.com/<owner>/<repo>@<commit SHA> or a local path starting with ./", uses)
	}

	repoPath, sha, ok := strings.Cut(rest, "@")
	if !ok || sha == "" {
		return Ref{}, fmt.Errorf("action %q must be pinned to a commit: append @<40-character commit SHA>", uses)
	}
	if !commitSHA.MatchString(sha) {
		return Ref{}, fmt.Errorf("action %q must be pinned to a full 40-character commit SHA, not %q: tags and branches can be moved", uses, sha)
	}

	parts := strings.Split(repoPath, "/")
	if len(parts) < 2 {
		return Ref{}, fmt.Errorf("action %q must name a repository as github.com/<owner>/<repo>", uses)
	}
	for _, p := range parts {
		if !pathPart.MatchString(p) || p == "." || p == ".." {
			return Ref{}, fmt.Errorf("action %q has an invalid path element %q", uses, p)
		}
	}

	return Ref{
		Owner: parts[0],
		Repo:  parts[1],
		Dir:   strings.Join(parts[2:], "/"),
		SHA:   sha,
	}, nil
}

func isLocal(uses string) bool {
	return strings.HasPrefix(uses, "./") || strings.HasPrefix(uses, "../") || filepath.IsAbs(uses)
}

// Manifest is the content of an action.yml.
type Manifest struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Runs        Runs   `yaml:"runs"`
	// Guard are the kinds of guard the action keeps to itself, such as
	// read-only and allow-host. Probe takes the action at its word and runs
	// it under a guard of those kinds; a kind Probe does not know is left
	// alone, so the action is refused under a guard it does not name.
	Guard []string `yaml:"guard"`
	// Params are the keys the action takes in with, for probe check to
	// check a step's with against. Nil, when action.yml does not say, takes
	// any key.
	Params []string `yaml:"params"`
	// Res are the top-level fields of the res the action returns, for probe
	// check to report a step that reads another. Nil, when action.yml does
	// not say, leaves what a step reads of res unchecked.
	Res []string `yaml:"res"`
	// Cleanup says that the action is to be told once a run is over, for it
	// to remove what its steps left behind in it.
	Cleanup bool `yaml:"cleanup"`
}

// Runs says how an action is run.
type Runs struct {
	// Using is the kind of action. It must be "binary".
	Using string `yaml:"using"`
	// URL is where the executable is downloaded from. {os} and {arch} are
	// replaced with the GOOS and GOARCH of the machine running Probe.
	URL string `yaml:"url"`
	// Path is the executable relative to the action's directory, with the
	// same placeholders as URL. Only a local action can use it.
	Path string `yaml:"path"`
	// Checksums maps "<os>_<arch>" to the SHA-256 digest of the executable
	// for that platform, in hex. A URL needs one for the running platform.
	Checksums map[string]string `yaml:"checksums"`
}

// ParseManifest parses the content of an action.yml.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m.Runs.Using != UsingBinary {
		return nil, fmt.Errorf("runs.using must be %q, not %q", UsingBinary, m.Runs.Using)
	}
	if (m.Runs.URL == "") == (m.Runs.Path == "") {
		return nil, errors.New("runs must have exactly one of url and path")
	}
	for platform, sum := range m.Runs.Checksums {
		if !sha256Hex.MatchString(sum) {
			return nil, fmt.Errorf("runs.checksums.%s must be a SHA-256 digest in lowercase hex", platform)
		}
	}
	return &m, nil
}

// Executable is the program that serves an action.
type Executable struct {
	// Path is the executable's location on this machine.
	Path string
	// SHA256 is the digest the executable must have when it is started, or
	// empty for a local action that does not pin one.
	SHA256 []byte
}

// Resolver finds and downloads the executables of external actions. A
// Resolver remembers what it resolved, so an action is fetched once however
// many steps use it. Its zero value is ready to use.
type Resolver struct {
	// CacheDir is where downloaded actions are kept. It defaults to probe/actions
	// under the user's cache directory.
	CacheDir string
	// Client fetches action.yml files and executables. It defaults to a client
	// with a five-minute timeout.
	Client *http.Client
	// RawBaseURL is where file contents of a GitHub repository are served. It
	// defaults to https://raw.githubusercontent.com.
	RawBaseURL string
	// OS and Arch are the platform to resolve for. They default to the
	// platform Probe runs on.
	OS   string
	Arch string

	mu   sync.Mutex
	memo map[string]*resolution
}

type resolution struct {
	once sync.Once
	exe  *Executable
	err  error

	manifestOnce sync.Once
	manifest     *Manifest
	manifestErr  error
}

var defaultResolver = &Resolver{}

// Resolve resolves uses with a Resolver shared by the whole process.
func Resolve(uses, baseDir string) (*Executable, error) {
	return defaultResolver.Resolve(uses, baseDir)
}

// ReadManifest reads the action.yml of uses with the Resolver shared by the
// whole process, which Resolve then uses for the same action.
func ReadManifest(uses, baseDir string) (*Manifest, error) {
	return defaultResolver.Manifest(uses, baseDir)
}

// Full returns uses as it names the action from any directory: a local
// action as the absolute path of its directory, taken relative to baseDir,
// or to the working directory when baseDir is empty, and a remote one as it
// is. Two uses that name the same action have the same one, whichever way
// each is written: a symbolic link on the way to a local action is followed.
func Full(uses, baseDir string) (string, error) {
	_, ref, key, err := defaultResolver.entry(uses, baseDir)
	if err != nil || ref.Local == "" {
		return key, err
	}
	// A directory that is not there is left as it is written; resolving
	// the action tells that it is missing.
	if dir, err := filepath.EvalSymlinks(key); err == nil {
		return dir, nil
	}
	return key, nil
}

// Resolve returns the executable of the external action uses names. A local
// action's path is taken relative to baseDir, or to the working directory
// when baseDir is empty.
func (r *Resolver) Resolve(uses, baseDir string) (*Executable, error) {
	res, ref, key, err := r.entry(uses, baseDir)
	if err != nil {
		return nil, err
	}
	res.once.Do(func() {
		var m *Manifest
		if m, res.err = r.Manifest(uses, baseDir); res.err != nil {
			r.forget(key, res)
			return
		}
		if ref.Local != "" {
			res.exe, res.err = r.localExecutable(ref.Local, m)
		} else {
			res.exe, res.err = r.download(m)
		}
		if res.err != nil {
			res.err = fmt.Errorf("action %s: %w", uses, res.err)
			r.forget(key, res)
		}
	})
	return res.exe, res.err
}

// Manifest returns the action.yml of the external action uses names, without
// fetching its executable, so that what it declares can be read before it is
// run. A remote one is read at the pinned commit.
func (r *Resolver) Manifest(uses, baseDir string) (*Manifest, error) {
	res, ref, key, err := r.entry(uses, baseDir)
	if err != nil {
		return nil, err
	}
	res.manifestOnce.Do(func() {
		if ref.Local != "" {
			res.manifest, res.manifestErr = readLocalManifest(ref.Local)
		} else {
			res.manifest, res.manifestErr = r.fetchManifest(ref)
		}
		if res.manifestErr != nil {
			res.manifestErr = fmt.Errorf("action %s: %w", uses, res.manifestErr)
			r.forget(key, res)
		}
	})
	return res.manifest, res.manifestErr
}

// entry returns what is remembered of uses, with its reference, a local path
// made absolute against baseDir, and the key it is remembered by.
func (r *Resolver) entry(uses, baseDir string) (*resolution, Ref, string, error) {
	ref, err := Parse(uses)
	if err != nil {
		return nil, Ref{}, "", err
	}

	key := uses
	if ref.Local != "" {
		dir := ref.Local
		if !filepath.IsAbs(dir) && baseDir != "" {
			dir = filepath.Join(baseDir, dir)
		}
		if dir, err = filepath.Abs(dir); err != nil {
			return nil, Ref{}, "", err
		}
		ref.Local = dir
		key = dir
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.memo == nil {
		r.memo = map[string]*resolution{}
	}
	res, ok := r.memo[key]
	if !ok {
		res = &resolution{}
		r.memo[key] = res
	}
	return res, ref, key, nil
}

// forget drops what is remembered of key, so that a later run in the same
// process tries again once the cause of a failure is fixed.
func (r *Resolver) forget(key string, res *resolution) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.memo[key] == res {
		delete(r.memo, key)
	}
}

func readLocalManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return nil, err
	}
	m, err := ParseManifest(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	return m, nil
}

func (r *Resolver) localExecutable(dir string, m *Manifest) (*Executable, error) {
	if m.Runs.URL != "" {
		return r.download(m)
	}

	exe := &Executable{Path: filepath.Join(dir, filepath.FromSlash(r.expand(m.Runs.Path)))}
	info, err := os.Stat(exe.Path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return nil, fmt.Errorf("%s is not an executable file", exe.Path)
	}
	got, err := fileSHA256(exe.Path)
	if err != nil {
		return nil, err
	}
	if sum, ok := m.Runs.Checksums[r.platform()]; ok {
		if exe.SHA256, _ = hex.DecodeString(sum); !bytes.Equal(got, exe.SHA256) {
			return nil, fmt.Errorf("%s has SHA-256 %x, but %s says %s", exe.Path, got, ManifestFile, sum)
		}
	}
	return exe, nil
}

// fetchManifest reads action.yml at the pinned commit from GitHub every
// run, rather than from the cache: it holds the digest the executable is
// checked against, so a copy that anyone could have changed on disk cannot
// be trusted in its place.
func (r *Resolver) fetchManifest(ref Ref) (*Manifest, error) {
	url := strings.TrimSuffix(r.rawBaseURL(), "/") + "/" + path.Join(ref.Owner, ref.Repo, ref.SHA, ref.Dir, ManifestFile)
	var buf bytes.Buffer
	if err := r.fetch(url, &buf, maxManifestSize); err != nil {
		return nil, err
	}

	m, err := ParseManifest(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	if m.Runs.Path != "" {
		return nil, fmt.Errorf("%s: runs.path is only for local actions; a remote action needs runs.url and runs.checksums", ManifestFile)
	}
	return m, nil
}

// download fetches the executable that m points to, unless an executable
// with the expected digest is already in the cache. Executables are cached by
// digest, so actions that ship the same build share it.
func (r *Resolver) download(m *Manifest) (*Executable, error) {
	platform := r.platform()
	sum, ok := m.Runs.Checksums[platform]
	if !ok {
		return nil, fmt.Errorf("%s has no checksum for %s: the action does not support this platform", ManifestFile, platform)
	}
	want, _ := hex.DecodeString(sum)

	cacheDir, err := r.cacheDir()
	if err != nil {
		return nil, err
	}
	exe := &Executable{Path: filepath.Join(cacheDir, "sha256", sum), SHA256: want}

	if got, err := fileSHA256(exe.Path); err == nil && bytes.Equal(got, want) {
		// The content is right, but the mode may not be, as after a cache
		// is restored by a tool that drops it.
		if err := os.Chmod(exe.Path, 0o755); err != nil {
			return nil, err
		}
		return exe, nil
	}

	if err := os.MkdirAll(filepath.Dir(exe.Path), 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(exe.Path), ".tmp-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	// The executable is hashed while it is written, so a large one is never
	// held in memory.
	url := r.expand(m.Runs.URL)
	h := sha256.New()
	err = r.fetch(url, io.MultiWriter(tmp, h), 0)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	if got := h.Sum(nil); !bytes.Equal(got, want) {
		return nil, fmt.Errorf("%s has SHA-256 %x, but %s says %s", url, got, ManifestFile, sum)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp.Name(), exe.Path); err != nil {
		return nil, err
	}
	return exe, nil
}

// maxManifestSize is the most of an action.yml that is read.
const maxManifestSize = 1 << 20

// fetch writes what url serves to w. When limit is positive, a response
// longer than limit bytes is an error.
func (r *Resolver) fetch(url string, w io.Writer, limit int64) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "probe")

	res, err := r.client().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, res.Status)
	}

	var body io.Reader = res.Body
	if limit > 0 {
		body = io.LimitReader(res.Body, limit+1)
	}
	n, err := io.Copy(w, body)
	if err != nil {
		return err
	}
	if limit > 0 && n > limit {
		return fmt.Errorf("GET %s: response is larger than %d bytes", url, limit)
	}
	return nil
}

func (r *Resolver) expand(s string) string {
	return strings.NewReplacer("{os}", r.goos(), "{arch}", r.goarch()).Replace(s)
}

func (r *Resolver) platform() string {
	return r.goos() + "_" + r.goarch()
}

func (r *Resolver) goos() string {
	if r.OS != "" {
		return r.OS
	}
	return runtime.GOOS
}

func (r *Resolver) goarch() string {
	if r.Arch != "" {
		return r.Arch
	}
	return runtime.GOARCH
}

func (r *Resolver) cacheDir() (string, error) {
	if r.CacheDir != "" {
		return r.CacheDir, nil
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("no cache directory for external actions: %w", err)
	}
	return filepath.Join(dir, "probe", "actions"), nil
}

func (r *Resolver) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

func (r *Resolver) rawBaseURL() string {
	if r.RawBaseURL != "" {
		return r.RawBaseURL
	}
	return "https://raw.githubusercontent.com"
}

func fileSHA256(name string) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}
