package actionref

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// DocFile is the file of an action's directory that describes it to the
// people and the agents that use it.
const DocFile = "README.md"

// maxDocSize is the most of a README that is read.
const maxDocSize = 1 << 20

// Doc is the description of an external action, as probe guide prints it.
type Doc struct {
	// Uses names the action: a remote one pinned, as
	// github.com/<owner>/<repo>[/<dir>]@<commit SHA>, and a local one as the
	// absolute path of its directory.
	Uses string
	// Local is set for an action in a directory of this machine.
	Local bool
	// Version is the tag of the release runs.url downloads from, such as
	// v0.1.2, or empty when runs.url names none.
	Version string
	// Latest is set when the commit was found as that of the latest
	// release, rather than given.
	Latest bool
	// File is what Text is read from: DocFile, or ManifestFile when the
	// action has no DocFile.
	File string
	// Text is the description, in Markdown.
	Text string
	// Warning tells what makes the commit found as the latest release
	// doubtful, or is empty.
	Warning string
}

// ReadDoc reads the description of uses with the Resolver shared by the
// whole process.
func ReadDoc(uses, baseDir string) (*Doc, error) {
	return defaultResolver.Doc(uses, baseDir)
}

// Doc returns the description of the external action uses names: the
// README.md of its directory, or what its action.yml declares when it has
// none. A local action's path is taken relative to baseDir, or to the
// working directory when baseDir is empty. A remote action is read at the
// commit it is pinned to, or, without one, as
// github.com/<owner>/<repo>[/<dir>], at its latest release: the last commit
// of the default branch that changed its action.yml, which the release
// workflow of an action commits with the digests of each release.
//
// What a remote action is read at a commit is kept in the cache, as the
// files of a commit do not change, so that it is read again offline. It is
// not run: unlike the action.yml a run reads, which holds the digests the
// executable is checked against, nothing in it has to be trusted.
func (r *Resolver) Doc(uses, baseDir string) (*Doc, error) {
	if isLocal(uses) {
		dir := uses
		if !filepath.IsAbs(dir) && baseDir != "" {
			dir = filepath.Join(baseDir, dir)
		}
		dir, err := filepath.Abs(dir)
		if err != nil {
			return nil, err
		}
		return localDoc(dir)
	}

	latest := !strings.Contains(uses, "@")
	pinned := uses
	if latest {
		ref, err := parseRepository(uses)
		if err != nil {
			return nil, err
		}
		sha, err := r.latestCommit(ref)
		if err != nil {
			return nil, fmt.Errorf("action %s: %w", uses, err)
		}
		pinned = uses + "@" + sha
	}
	ref, err := Parse(pinned)
	if err != nil {
		return nil, err
	}

	manifest, readme, err := r.docFiles(ref)
	if err != nil {
		return nil, fmt.Errorf("action %s: %w", pinned, err)
	}
	m, err := ParseManifest(manifest)
	if err != nil {
		return nil, fmt.Errorf("action %s: %s: %w", pinned, ManifestFile, err)
	}
	d := newDoc(m, readme)
	d.Uses, d.Latest = pinned, latest
	if latest {
		d.Warning = r.releaseWarning(ref, d.Version)
	}
	return d, nil
}

func localDoc(dir string) (*Doc, error) {
	m, err := readLocalManifest(dir)
	if err != nil {
		return nil, fmt.Errorf("action %s: %w", dir, err)
	}
	readme, err := readFileLimited(filepath.Join(dir, DocFile), maxDocSize)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("action %s: %w", dir, err)
	}
	d := newDoc(m, readme)
	d.Uses, d.Local = dir, true
	return d, nil
}

// newDoc describes the action of m with readme, or, when it is nil, with
// what m declares.
func newDoc(m *Manifest, readme []byte) *Doc {
	d := &Doc{Version: downloadTag(m.Runs.URL)}
	if readme != nil {
		d.File, d.Text = DocFile, string(readme)
		return d
	}
	d.File, d.Text = ManifestFile, manifestText(m)
	return d
}

// manifestText writes what m declares as Markdown, for an action with no
// README: its name, its description, and the keys of with and res.
func manifestText(m *Manifest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", m.Name)
	if m.Description != "" {
		fmt.Fprintf(&b, "\n%s\n", m.Description)
	}
	list := func(heading string, keys []string) {
		if len(keys) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n## %s\n\n", heading)
		for _, k := range keys {
			fmt.Fprintf(&b, "- `%s`\n", k)
		}
	}
	list("Parameters", m.Params)
	list("Response", m.Res)
	return b.String()
}

// downloadTag returns the tag of the GitHub release a URL downloads from,
// as .../releases/download/<tag>/..., unescaped, or empty.
func downloadTag(u string) string {
	_, rest, ok := strings.Cut(u, "/releases/download/")
	if !ok {
		return ""
	}
	tag, _, _ := strings.Cut(rest, "/")
	// A tag with a slash, such as pkg/v1.0.0, is escaped in the path.
	if unescaped, err := url.PathUnescape(tag); err == nil {
		return unescaped
	}
	return tag
}

// parseRepository parses github.com/<owner>/<repo>[/<dir>], an action
// named without a commit.
func parseRepository(uses string) (Ref, error) {
	ref, err := Parse(uses + "@" + strings.Repeat("0", 40))
	if err != nil {
		return Ref{}, err
	}
	ref.SHA = ""
	return ref, nil
}

// docFiles returns the action.yml and the README of ref, nil when it has
// none, from the cache or from GitHub.
func (r *Resolver) docFiles(ref Ref) (manifest, readme []byte, err error) {
	dir, cacheErr := r.docCacheDir(ref)
	if cacheErr == nil {
		if manifest, err := os.ReadFile(filepath.Join(dir, ManifestFile)); err == nil {
			readme, err := os.ReadFile(filepath.Join(dir, DocFile))
			if err == nil {
				return manifest, readme, nil
			}
			if _, err := os.Stat(filepath.Join(dir, noDocFile)); err == nil {
				return manifest, nil, nil
			}
		}
	}

	var buf bytes.Buffer
	if err := r.fetch(r.rawURL(ref, ManifestFile), &buf, maxManifestSize); err != nil {
		return nil, nil, err
	}
	manifest = bytes.Clone(buf.Bytes())
	buf.Reset()
	switch err := r.fetch(r.rawURL(ref, DocFile), &buf, maxDocSize); {
	case err == nil:
		readme = bytes.Clone(buf.Bytes())
	case !isNotFound(err):
		return nil, nil, err
	}

	if cacheErr == nil {
		// A cache that cannot be written leaves the files to be fetched
		// again, which is all it is for.
		_ = writeDocCache(dir, manifest, readme)
	}
	return manifest, readme, nil
}

// noDocFile marks in the cache a commit that has no README.
const noDocFile = ".no-readme"

func (r *Resolver) docCacheDir(ref Ref) (string, error) {
	dir, err := r.cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "docs", ref.Owner, ref.Repo, ref.SHA, filepath.FromSlash(ref.Dir)), nil
}

// writeDocCache writes the files of a commit to dir, the README, or the
// mark that there is none, before action.yml, which a read takes for the
// sign that the rest is there.
func writeDocCache(dir string, manifest, readme []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if readme != nil {
		if err := writeFileAtomic(filepath.Join(dir, DocFile), readme); err != nil {
			return err
		}
	} else if err := writeFileAtomic(filepath.Join(dir, noDocFile), nil); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, ManifestFile), manifest)
}

func writeFileAtomic(name string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(name), ".tmp-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), name)
}

func (r *Resolver) rawURL(ref Ref, file string) string {
	return strings.TrimSuffix(r.rawBaseURL(), "/") + "/" + path.Join(ref.Owner, ref.Repo, ref.SHA, ref.Dir, file)
}

// latestCommit returns the last commit of the default branch of ref that
// changed its action.yml.
func (r *Resolver) latestCommit(ref Ref) (string, error) {
	q := url.Values{"path": {path.Join(ref.Dir, ManifestFile)}, "per_page": {"1"}}
	var commits []struct {
		SHA string `json:"sha"`
	}
	err := r.api(path.Join("repos", ref.Owner, ref.Repo, "commits")+"?"+q.Encode(), &commits)
	if isNotFound(err) {
		return "", fmt.Errorf("no repository %s/%s on GitHub", ref.Owner, ref.Repo)
	}
	if err != nil {
		return "", err
	}
	if len(commits) == 0 || !commitSHA.MatchString(commits[0].SHA) {
		return "", fmt.Errorf("no commit of the default branch has %s", path.Join(ref.Dir, ManifestFile))
	}
	return commits[0].SHA, nil
}

// releaseWarning tells when version, the release the action.yml of the
// commit found as the latest release downloads from, is not the latest
// release of ref: the commit changed action.yml without a release, and may
// declare what no release does yet. A latest release that cannot be told
// warns of nothing.
func (r *Resolver) releaseWarning(ref Ref, version string) string {
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := r.api(path.Join("repos", ref.Owner, ref.Repo, "releases", "latest"), &release); err != nil || release.TagName == "" {
		return ""
	}
	if release.TagName == version {
		return ""
	}
	if version == "" {
		return fmt.Sprintf("the %s of this commit names no release in runs.url, and the latest release is %s", ManifestFile, release.TagName)
	}
	return fmt.Sprintf("the %s of this commit downloads %s, but the latest release is %s: it may have been changed after a release, and declare what no release does yet", ManifestFile, version, release.TagName)
}

// maxAPISize is the most of a response of the REST API that is read.
const maxAPISize = 1 << 20

// api decodes into v what the REST API of GitHub answers to a GET of
// endpoint, a path with its query.
func (r *Resolver) api(endpoint string, v any) error {
	u := strings.TrimSuffix(r.apiBaseURL(), "/") + "/" + endpoint
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "probe")
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := r.token(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := r.client().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return &statusError{url: u, status: res.Status, code: res.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxAPISize+1))
	if err != nil {
		return err
	}
	if len(data) > maxAPISize {
		return fmt.Errorf("GET %s: response is larger than %d bytes", u, maxAPISize)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("GET %s: %w", u, err)
	}
	return nil
}

func (r *Resolver) apiBaseURL() string {
	if r.APIBaseURL != "" {
		return r.APIBaseURL
	}
	return "https://api.github.com"
}

func (r *Resolver) token() string {
	if r.Token != "" {
		return r.Token
	}
	return os.Getenv("GITHUB_TOKEN")
}

// readFileLimited reads name, failing when it is longer than limit bytes.
func readFileLimited(name string, limit int64) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", name, limit)
	}
	return data, nil
}
