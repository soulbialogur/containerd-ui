# Release notes

## Overview

Containerd UI is a Windows desktop tool for working with containerd, WSL2, nerdctl, BuildKit, and local project deployment flows.

## Requirements

- Windows 10/11
- WSL2 enabled
- Ubuntu 24.04 inside WSL
- `containerd`, `nerdctl`, and `buildkitd` installed in the WSL environment
- Optional: `cloudflared` for tunnel-based deployment

## Build instructions

### Local build

```powershell
go mod download
go build -ldflags "-s -w -H windowsgui" -o containerd-ui.exe .
```

### GitHub Actions

The repository includes a CI workflow that builds the Windows executable automatically on push and pull request.

## Release checklist

1. Update version information in the app if needed.
2. Verify the application starts correctly on a clean Windows machine.
3. Validate WSL dependencies and connectivity.
4. Build the executable locally or via CI.
5. Attach the `.exe` file to a GitHub Release.
6. Publish release notes and prerequisites.
