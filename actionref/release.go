package actionref

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

var (
	// releaseURL is where GitHub serves an asset of a release: the address
	// up to the tag, the tag, and the name of the asset.
	releaseURL = regexp.MustCompile(`^(https://github\.com/[^/]+/[^/]+/releases/download/)([^/]+)/([^/]+)$`)
	// releaseTag is a tag that stands in a URL as it is written. A tag git
	// takes can have more, such as # and %, which would end the path or
	// start an escape.
	releaseTag = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	// platformPart is what {os} and {arch} stand for in the name of an asset.
	platformPart = `[a-z0-9]+`
	// betweenPlaceholders is what stands between {os} and {arch} in the
	// name of an asset, in either order.
	betweenPlaceholders = regexp.MustCompile(`\{(?:os|arch)\}(.*)\{(?:os|arch)\}`)
	// separator is a character that neither {os} nor {arch} stands for.
	separator = regexp.MustCompile(`[^a-z0-9]`)
	// flowIndicator is what a plain scalar cannot hold inside [ ].
	flowIndicator = regexp.MustCompile(`[,\[\]{}]`)
)

// Release returns the action.yml of the release tag from the action.yml of
// the release before it: runs.url names the same asset under tag, and
// runs.checksums are the digests that checksums lists for that asset. What
// else the action.yml declares is kept, so it stays the one place an action
// is described in.
//
// checksums is what sha256sum or GoReleaser writes, a line of which is
// "<digest>  <file>". A line is taken when its file is the asset runs.url
// names, with a platform in place of {os} and {arch}; any other is left out.
//
// The result is written in one form whatever form manifest has. The comment
// lines manifest starts with are kept, and no other comment is.
func Release(manifest []byte, tag string, checksums io.Reader) ([]byte, error) {
	m, err := ParseManifest(manifest)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	if !releaseTag.MatchString(tag) {
		return nil, fmt.Errorf("tag %q must be letters, digits, '.', '_', '+' and '-', starting with a letter or a digit", tag)
	}
	if m.Runs.URL == "" {
		return nil, fmt.Errorf("%s: runs.url is needed to release an action, such as https://github.com/<owner>/<repo>/releases/download/%s/<name>_{os}_{arch}", ManifestFile, tag)
	}
	parts := releaseURL.FindStringSubmatch(m.Runs.URL)
	if parts == nil {
		return nil, fmt.Errorf("%s: runs.url must be https://github.com/<owner>/<repo>/releases/download/<tag>/<asset> to take the tag %s, not %s", ManifestFile, tag, m.Runs.URL)
	}
	asset := parts[3]

	sums, err := readChecksums(checksums, asset)
	if err != nil {
		return nil, err
	}

	released := *m
	released.Runs.URL = parts[1] + tag + "/" + asset
	released.Runs.Checksums = sums

	out := append(headComment(manifest), released.encode()...)
	// What is written by hand here is read back, so that a value it does
	// not write right is an error rather than a broken release.
	if back, err := ParseManifest(out); err != nil || !reflect.DeepEqual(back, &released) {
		return nil, fmt.Errorf("%s: cannot be written back as it was read", ManifestFile)
	}
	return out, nil
}

// readChecksums returns the digest of each platform's asset among the lines
// of r, keyed by "<os>_<arch>".
func readChecksums(r io.Reader, asset string) (map[string]string, error) {
	if strings.Count(asset, "{os}") != 1 || strings.Count(asset, "{arch}") != 1 {
		return nil, fmt.Errorf("%s: runs.url must name the executable with one {os} and one {arch}, not %s", ManifestFile, asset)
	}
	// Without a character between them that neither stands for, where one
	// ends and the other starts cannot be told: linuxamd64 is not read as
	// linux and amd64.
	if between := betweenPlaceholders.FindStringSubmatch(asset); between == nil || !separator.MatchString(between[1]) {
		return nil, fmt.Errorf("%s: runs.url must have a character between {os} and {arch} that is not a lowercase letter or a digit, such as _, not %s", ManifestFile, asset)
	}
	pattern := regexp.QuoteMeta(asset)
	pattern = strings.Replace(pattern, `\{os\}`, `(?P<os>`+platformPart+`)`, 1)
	pattern = strings.Replace(pattern, `\{arch\}`, `(?P<arch>`+platformPart+`)`, 1)
	re := regexp.MustCompile("^" + pattern + "$")

	sums := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		// sha256sum marks a file read in binary mode with an asterisk.
		file := path.Base(strings.TrimPrefix(fields[1], "*"))
		match := re.FindStringSubmatch(file)
		if match == nil {
			continue
		}
		sum := strings.ToLower(fields[0])
		if !sha256Hex.MatchString(sum) {
			return nil, fmt.Errorf("checksum of %s must be a SHA-256 digest in hex, not %q", file, fields[0])
		}
		platform := match[re.SubexpIndex("os")] + "_" + match[re.SubexpIndex("arch")]
		if prev, ok := sums[platform]; ok && prev != sum {
			return nil, fmt.Errorf("checksums list %s twice, with different digests", file)
		}
		sums[platform] = sum
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(sums) == 0 {
		return nil, errors.New("checksums list no file named " + asset)
	}
	return sums, nil
}

// headComment returns the comment lines data starts with, and the blank
// lines among them.
func headComment(data []byte) []byte {
	var head []byte
	for line := range bytes.Lines(data) {
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 && trimmed[0] != '#' {
			break
		}
		head = append(head, line...)
	}
	if len(head) > 0 && head[len(head)-1] != '\n' {
		head = append(head, '\n')
	}
	return head
}

// encode writes m as an action.yml, with its keys in the order the
// documentation gives them and its checksums sorted by platform.
func (m *Manifest) encode() []byte {
	var b bytes.Buffer
	if m.Name != "" {
		fmt.Fprintf(&b, "name: %s\n", scalar(m.Name, false))
	}
	if m.Description != "" {
		fmt.Fprintf(&b, "description: %s\n", scalar(m.Description, false))
	}
	// An empty list is not left out as a missing one is: params: [] takes
	// no key, where no params takes any.
	if m.Guard != nil {
		fmt.Fprintf(&b, "guard: %s\n", flowList(m.Guard))
	}
	if m.Params != nil {
		fmt.Fprintf(&b, "params: %s\n", flowList(m.Params))
	}
	if m.Cleanup {
		b.WriteString("cleanup: true\n")
	}
	fmt.Fprintf(&b, "runs:\n  using: %s\n", scalar(m.Runs.Using, false))
	if m.Runs.URL != "" {
		fmt.Fprintf(&b, "  url: %s\n", scalar(m.Runs.URL, false))
	}
	if m.Runs.Path != "" {
		fmt.Fprintf(&b, "  path: %s\n", scalar(m.Runs.Path, false))
	}
	if len(m.Runs.Checksums) > 0 {
		b.WriteString("  checksums:\n")
		platforms := make([]string, 0, len(m.Runs.Checksums))
		for platform := range m.Runs.Checksums {
			platforms = append(platforms, platform)
		}
		slices.Sort(platforms)
		for _, platform := range platforms {
			// A digest of digits alone would be read as a number unquoted.
			fmt.Fprintf(&b, "    %s: %s\n", scalar(platform, false), strconv.Quote(m.Runs.Checksums[platform]))
		}
	}
	return b.Bytes()
}

func flowList(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = scalar(item, true)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// scalar writes s as a YAML scalar on one line: plain where it reads back
// as the same string, and quoted otherwise. flow is for one inside [ ].
func scalar(s string, flow bool) string {
	out, err := yaml.Marshal(s)
	plain := strings.TrimSuffix(string(out), "\n")
	if err != nil || strings.Contains(plain, "\n") || (flow && flowIndicator.MatchString(plain)) {
		return strconv.Quote(s)
	}
	return plain
}

// scaffoldTag stands in runs.url for the tag of a release until Release
// puts the first one there.
const scaffoldTag = "v0.0.0"

// Scaffold returns an action.yml to start a new action from, for the GitHub
// repository repo, as <owner>/<repo>, that will publish it. It names the
// action after the repository, without a leading "probe-", and the
// executable of each platform <repo>_{os}_{arch}, which is what GoReleaser
// names an unarchived one by default. It has no checksums: Release adds
// those of the first release.
func Scaffold(repo string) ([]byte, error) {
	parts := strings.Split(strings.TrimPrefix(repo, githubHost+"/"), "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("repository %q must be <owner>/<repo>", repo)
	}
	for _, p := range parts {
		if !pathPart.MatchString(p) || p == "." || p == ".." {
			return nil, fmt.Errorf("repository %q has an invalid path element %q", repo, p)
		}
	}
	owner, name := parts[0], parts[1]
	action := strings.TrimPrefix(name, "probe-")
	if action == "" {
		action = name
	}

	var b bytes.Buffer
	fmt.Fprintf(&b, "name: %s\n", scalar(action, false))
	b.WriteString(`# What the action does, in a line.
description: ""
# The kinds of guard the action keeps to itself. Without guard, a step that
# uses the action is refused under --read-only or --allow-host unless
# --allow-action names it.
# guard: [read-only, allow-host]
# The keys the action takes in with, for probe check to report any other.
# Without params, with is not checked.
# params: [name]
# Whether Probe calls the action once a run is over, for it to remove what
# its steps left behind, such as a server they started.
# cleanup: true
runs:
  using: binary
  # probe manifest <tag> <checksums-file> puts the tag of a release here and
  # the digests of its executables in checksums. The release must publish
  # the executable of each platform under this name.
`)
	fmt.Fprintf(&b, "  url: https://%s/%s/%s/releases/download/%s/%s_{os}_{arch}\n", githubHost, owner, name, scaffoldTag, name)

	if _, err := ParseManifest(b.Bytes()); err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	return b.Bytes(), nil
}
