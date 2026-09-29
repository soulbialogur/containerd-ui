# Release notes

## Overview

Containerd UI is a Windows desktop application for managing a local container environment based on WSL2, containerd, nerdctl, and BuildKit. It is designed to help developers inspect the state of the runtime, manage containers and images, clean up stale resources, and validate deployment readiness before publishing a project.

## Supported environment

The application is currently validated for the following setup:

- Windows 10/11
- WSL2 enabled
- Debian-based distro inside WSL2
- systemd enabled in the distro
- containerd running in WSL
- nerdctl installed and configured
- buildkitd / buildctl available
- optional: cloudflared for tunnel-based deployments
- Go toolchain for building the Windows binary from source

## Current features

- WSL, containerd, and BuildKit health checks
- containers, images, volumes, networks, and logs inspection
- cleanup flows for stale volumes, dangling images, and caches
- project-level deployment validation and rollback support
- automatic resource diagnostics before publishing
- BuildKit start/stop controls from the UI
- tab lifecycle management and economy mode for reduced idle activity

## Requirements

Before running or packaging the project, verify the following:

- WSL2 and Debian are installed and the distro is selected
- containerd is running inside WSL
- nerdctl is available in PATH
- buildkitd and buildctl are available in the WSL environment
- if deployment uses a tunnel, cloudflared is installed
- the host machine has the Go toolchain if building from source

## Build instructions

### Recommended local build

From the project root, run:

```bash
bash containerd-ui/build.sh
```

This script prepares the environment and builds the Windows executable for the app.

### Manual build

If you want to build directly with Go:

```powershell
go mod download
go build -ldflags "-s -w -H windowsgui" -o containerd-ui.exe .
```

If you are building inside WSL, use the project folder and run the same build process from there.

### GitHub Actions

The repository includes CI workflows for Windows builds. They are intended to produce the release-ready executable automatically on push and pull request events.

## Release checklist

1. Verify the app starts correctly on a clean Windows machine.
2. Confirm WSL2 dependencies are available and reachable.
3. Run the smoke checks for containerd, BuildKit, and nerdctl status.
4. Validate that the project builds without warnings in the release configuration.
5. Build the final executable with the release script or CI workflow.
6. Test the generated .exe before publishing.
7. Attach the binary to the GitHub Release.
8. Publish release notes and required prerequisites.

## Notes for maintainers

- Prefer testing the application on a real WSL2 machine before shipping a release.
- Keep release notes aligned with the current UI features and runtime requirements.
- If a dependency is removed or a new prerequisite is introduced, update the docs and the release checklist in the same change.
- Do not publish a release with stale build instructions or unsupported runtime assumptions.
