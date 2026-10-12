---
title: Installation
description: Learn how to install Probe on your system
weight: 10
---

# Installation

Probe is a lightweight, single-binary tool that can be installed in several ways. Choose the method that works best for your environment.

## System Requirements

- **Operating System**: Linux, macOS
- **Architecture**: amd64, arm64
- **Dependencies**: None (statically linked binary)

## Installation Methods

Probe is a single binary. Pick whichever of the three methods below fits how the rest of your tooling is installed.

### 1. Download Pre-built Binaries

The easiest way to install Probe is to download a pre-built binary from the GitHub releases page.

1. Visit the [Probe releases page](https://github.com/mozership/probe/releases)
2. Download the archive for your system:
   - **Linux amd64**: `probe_linux_x86_64.tar.gz`
   - **Linux arm64**: `probe_linux_arm64.tar.gz`
   - **macOS amd64**: `probe_darwin_x86_64.tar.gz`
   - **macOS arm64**: `probe_darwin_arm64.tar.gz`

3. Extract the `probe` binary from it:
   ```bash
   tar -xzf probe_linux_x86_64.tar.gz
   ```

4. Move it to a directory in your PATH:
   ```bash
   sudo mv probe /usr/local/bin/probe
   ```

### 2. Install with Go

If you have Go 1.26.6 or later installed, you can install Probe directly:

```bash
go install github.com/mozership/probe/cmd/probe@latest
```

This will install the `probe` binary to your `$GOPATH/bin` directory.

### 3. Build from Source

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
Probe Version 1.13.0 (commit: 2d9d511ce7b4c9eec32ea85e2a25337e40a5e40c)
```

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

For Apple Silicon Macs (M1/M2), use `probe_darwin_arm64.tar.gz` for better performance.
