---
title: Installation
description: Learn how to install Probe on your system
weight: 10
---

# Installation

Probe is a single binary with no runtime dependencies. Homebrew is the shortest way to get it; where there is no Homebrew, such as in a container or in the sandbox of a coding agent, Go or a few commands that download a release do the same.

## System Requirements

- **Operating System**: Linux, macOS
- **Architecture**: amd64, arm64
- **Dependencies**: None (statically linked binary)

## Installation Methods

Pick whichever of the methods below fits how the rest of your tooling is installed.

### 1. Homebrew

```bash
brew install mozership/tap/probe
```

The cask is in [mozership/homebrew-tap](https://github.com/mozership/homebrew-tap), written by each release with the SHA-256 of its archives, which Homebrew verifies before installing. `brew upgrade` brings Probe up to date.

### 2. Install with Go

If you have Go 1.26.6 or later installed, you can install Probe directly:

```bash
go install github.com/mozership/probe/cmd/probe@latest
```

This will install the `probe` binary to your `$GOPATH/bin` directory.

### 3. Download a Release

The archives are on the [releases page](https://github.com/mozership/probe/releases), with `checksums.txt` beside them:

- **Linux amd64**: `probe_linux_x86_64.tar.gz`
- **Linux arm64**: `probe_linux_arm64.tar.gz`
- **macOS amd64**: `probe_darwin_x86_64.tar.gz`
- **macOS arm64**: `probe_darwin_arm64.tar.gz`

These commands download the latest one, verify it against `checksums.txt`, and put `probe` in a directory in your PATH. They ask nothing, so they can go into a Dockerfile or the setup script of a sandbox as they are:

```bash
curl -fsSLO https://github.com/mozership/probe/releases/latest/download/probe_linux_x86_64.tar.gz
curl -fsSLO https://github.com/mozership/probe/releases/latest/download/checksums.txt
sha256sum -c --ignore-missing checksums.txt
tar -xzf probe_linux_x86_64.tar.gz probe
sudo mv probe /usr/local/bin/probe
```

On macOS, `shasum -a 256 -c --ignore-missing checksums.txt` verifies it. For a release other than the latest, replace `latest/download` with `download/v1.26.0`.

### 4. GitHub Actions

[`mozership/probe-action`](https://github.com/mozership/probe-action) downloads Probe, verifies its checksum, caches it, and runs your workflows:

```yaml
- uses: actions/checkout@v4
- uses: mozership/probe-action@v1.3.0
  with:
    path: probe/api.yml
    options: --report github-summary
```

With `install-only: true` it only puts `probe` in `PATH`, for later steps to run themselves.

### 5. Build from Source

To build Probe from source:

```bash
git clone https://github.com/mozership/probe.git
cd probe
go build -o probe ./cmd/probe
sudo mv probe /usr/local/bin/
```

## Verify Installation

After installation, verify that Probe is working correctly:

```bash
probe --version
```

You should see output similar to:
```
Probe Version 1.26.0 (commit: 77d72480c9bbc97cf32e202e69d9af6fdd9d1098)
```

## For a Coding Agent

An agent that works in a fresh sandbox has to install Probe itself. Put one of the install commands above in the file it reads its instructions from, such as `AGENTS.md` or `CLAUDE.md`, and install the skill in the project so that it knows how to write a workflow:

```bash
probe skill install
```

[Working with coding agents](/guide/how-tos/coding-agents) has the instructions to give the agent.

## Next Steps

Now that you have Probe installed, you're ready to:

1. **[Create your first workflow](/guide/introduction/quickstart)** - Get started with a simple example
2. **[Learn the basics](/guide/introduction/understanding-probe)** - Understand core concepts
3. **[Explore examples](/guide/tutorials/api-testing-pipeline)** - See practical use cases

## Troubleshooting

Installation problems are almost always about the file's permissions or where the shell looks for it.

### Permission Denied

If you get a "permission denied" error on Linux/macOS:

```bash
chmod +x probe
```

### Command Not Found

If the `probe` command is not found, ensure the binary is in your PATH:

```bash
echo $PATH
which probe
```

### ARM64 on Apple Silicon

Homebrew picks the arm64 archive on an Apple Silicon Mac. When downloading by hand, pick `probe_darwin_arm64.tar.gz` rather than the x86_64 archive.
