package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	info := func(version string, settings ...debug.BuildSetting) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Path: "github.com/mozership/probe", Version: version}, Settings: settings}
	}
	rev := debug.BuildSetting{Key: "vcs.revision", Value: "f7352a519c79335bfd19e2c70f20da8230be16fa"}
	dirty := debug.BuildSetting{Key: "vcs.modified", Value: "true"}
	clean := debug.BuildSetting{Key: "vcs.modified", Value: "false"}

	tests := []struct {
		name        string
		version     string
		commit      string
		info        *debug.BuildInfo
		ok          bool
		wantVersion string
		wantCommit  string
	}{
		{
			name:    "ldflags win over build info",
			version: "1.12.0", commit: "abc123",
			info: info("v9.9.9", rev), ok: true,
			wantVersion: "1.12.0", wantCommit: "abc123",
		},
		{
			name:    "go install at a tag",
			version: "dev", commit: "unknown",
			info: info("v1.12.0"), ok: true,
			wantVersion: "1.12.0", wantCommit: "unknown",
		},
		{
			name:    "go install at a pseudo-version",
			version: "dev", commit: "unknown",
			info: info("v1.12.1-0.20261003041500-29c6aa7c0ffe"), ok: true,
			wantVersion: "1.12.1-0.20261003041500-29c6aa7c0ffe", wantCommit: "29c6aa7c0ffe",
		},
		{
			name:    "pseudo-version without a base tag",
			version: "dev", commit: "unknown",
			info: info("v0.0.0-20261003041500-29c6aa7c0ffe"), ok: true,
			wantVersion: "0.0.0-20261003041500-29c6aa7c0ffe", wantCommit: "29c6aa7c0ffe",
		},
		{
			name:    "a tag that only looks numeric is not a pseudo-version",
			version: "dev", commit: "unknown",
			info: info("v1.12.0-rc.1"), ok: true,
			wantVersion: "1.12.0-rc.1", wantCommit: "unknown",
		},
		{
			name:    "build from a clean checkout",
			version: "dev", commit: "unknown",
			info: info("(devel)", rev, clean), ok: true,
			wantVersion: "dev", wantCommit: "f7352a519c79335bfd19e2c70f20da8230be16fa",
		},
		{
			name:    "build from a modified checkout",
			version: "dev", commit: "unknown",
			info: info("(devel)", rev, dirty), ok: true,
			wantVersion: "dev", wantCommit: "f7352a519c79335bfd19e2c70f20da8230be16fa-dirty",
		},
		{
			name:    "no build info",
			version: "dev", commit: "unknown",
			info: nil, ok: false,
			wantVersion: "dev", wantCommit: "unknown",
		},
		{
			name:    "empty module version",
			version: "dev", commit: "unknown",
			info: info(""), ok: true,
			wantVersion: "dev", wantCommit: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, c := resolveVersion(tt.version, tt.commit, tt.info, tt.ok)
			if v != tt.wantVersion || c != tt.wantCommit {
				t.Errorf("resolveVersion() = %q, %q; want %q, %q", v, c, tt.wantVersion, tt.wantCommit)
			}
		})
	}
}
