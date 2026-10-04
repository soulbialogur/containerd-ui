# Environment Setup

## Requirements

- Windows 10/11
- WSL2
- the bundled `Alpine-ContainerdUI` distribution inside WSL2
- access to PowerShell
- permission to install packages in WSL
- Go 1.26.5 or newer inside Alpine — required to build the application from source (included in the bundled image)
- MinGW-w64 inside Alpine — required for the Windows CGO cross-build (included in the bundled image)

## Runtime Stack

The only supported runtime is the bundled `Alpine-ContainerdUI` WSL distribution. The application automatically migrates old `Alpine` selections to the bundled distro when it is installed.

## Install WSL

### Alpine-ContainerdUI (Required)

Run `Alpine-ContainerdUI-Setup.exe` to import the bundled offline image into WSL2. This image provides OpenRC, containerd, nerdctl, BuildKit, Go, MinGW-w64, and OpenGL development files.

Verify it:

```powershell
wsl --list --verbose
```

## Verify the Container Environment

For the complete list of verification commands and scenarios, see [diagnostics.md](diagnostics.md). It covers WSL, `containerd`, `nerdctl`, `buildkitd`, ports, DNS, and Cloudflare credentials.

If anything is missing, install or start the services manually, then check their status again using the diagnostics guide.

## Verify containerd and nerdctl

The bundled image already contains these components:

```powershell
wsl -d Alpine-ContainerdUI -- nerdctl version
wsl -d Alpine-ContainerdUI -- nerdctl info
wsl -d Alpine-ContainerdUI -- rc-service containerd status
```

## Verify BuildKit

```powershell
wsl -d Alpine-ContainerdUI -- buildctl --version
wsl -d Alpine-ContainerdUI -- buildkitd --version
```

If you intentionally removed or modified packages in the image, install them from Alpine:

```bash
apk add --no-cache containerd containerd-openrc nerdctl buildkit cni-plugins doas socat
nerdctl version
nerdctl info
buildctl --version
buildkitd --version
```

The application starts `buildkitd` on demand when a build begins; it is not enabled as a persistent OpenRC service. Verification and startup checks are available in [diagnostics.md](diagnostics.md) and [troubleshooting.md](troubleshooting.md).

## Start containerd

### Alpine

```bash
rc-update add containerd default
rc-service containerd start
rc-service containerd status
```

The application installer also configures the standard Unix socket, creates the OpenRC `containerd-ui-grpc-proxy` service that forwards TCP port `50051` to that socket, and adds an OpenRC bootstrap command to `/etc/wsl.conf`. These are required for the Windows UI to reach the Containerd management API after WSL starts. Prefer **Install all components** in the Status tab to configure them automatically.

## Install Cloudflare Tunnel

If you plan to use Cloudflare Tunnel, install `cloudflared` using the official instructions:

- https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/

Important: `cloudflared` must be available in the WSL `PATH`. If the binary is installed but not visible to the shell, the application cannot validate the token correctly and will stop deployment before starting the proxy.

The complete token validation and setup flow is described in [deployment.md](deployment.md). Here, it is enough to install the binary and verify that it is available in `PATH`.

## Verify Ports

For Traefik + Let's Encrypt, ports `80` and `443` must be available. [deployment.md](deployment.md) explains how the check works and why a conflict may exist at either the Windows or WSL level. For commands and troubleshooting, also see [diagnostics.md](diagnostics.md).

If either port is in use, free it or stop the service that is occupying it.

## Verify the Project's External Network

Network and Compose requirements are collected in [project-requirements.md](project-requirements.md). It explains the external `network`, how to attach the `backend`/`frontend` services, and how to identify the correct project root.

If the network does not exist, the application may create it automatically, but the Compose file must still declare it as `external: true` with the name from `deploy_network`; otherwise the environment may behave incorrectly or deployment will fail.

For quick environment checks and commands, see [diagnostics.md](diagnostics.md).

## Recommended Environment Layout

### Alpine-ContainerdUI

```text
Windows
└── WSL Alpine-ContainerdUI
    ├── containerd
    ├── nerdctl
    ├── buildkitd
    ├── cloudflared
    └── app project
```

## Build the Windows Application from Source

The script runs `go mod tidy`, then cross-compiles a Windows `amd64` executable with `CGO_ENABLED=1` using the bundled Go and MinGW toolchain. Start the bundled distribution from PowerShell, then run the script in its shell:

```powershell
wsl -d Alpine-ContainerdUI
```

```bash
cd /mnt/c/Users/User/OneDrive/Desktop/ai-chatbot-website
sh containerd-ui/build.sh
```

The output is `containerd-ui/dist/containerd-ui.exe`. The application icon is embedded in the executable, so no separate `app.ico` file is needed beside it. To verify the bundled toolchain:

```bash
go version
x86_64-w64-mingw32-gcc --version
```

## Build the Offline WSL Runtime Installer

From PowerShell in this directory, run:

```powershell
.\build-offline-installer.ps1
```

The script downloads an official Alpine minirootfs, provisions OpenRC, containerd, nerdctl, BuildKit, CNI plugins, Go, MinGW-w64, and Mesa/OpenGL development files in a temporary WSL distro, then packages the exported image into `dist\Alpine-ContainerdUI-Setup.exe`. Building requires internet access, WSL2, and Go. The resulting installer does not download runtime packages: it imports the bundled image as `Alpine-ContainerdUI` and refuses to overwrite an existing distro with that name. The app automatically selects the bundled runtime. MinGW's Windows OpenGL headers and `opengl32` cross-link are checked during image creation.

## How the Application Accesses Containers

The main container access model and fallback behavior are described in [concepts.md](concepts.md). The short version is that the containerd gRPC API has priority, while WSL + nerdctl is used as a fallback when gRPC fails or is unavailable.

## Where Configuration Is Stored

Deployment settings, project paths, environment parameters, and the interface language are stored in `config.json` next to `containerd-ui.exe`. This includes the WSL distribution, proxy, domains, services, internal application ports, and the `language` field (`ru` or `en`).

If the `language` field is missing or invalid, the application falls back to Russian. The language can be changed in the Settings tab at any time.

## Next Steps

Once the environment is ready, continue with the [Quickstart](quickstart.md) or [Configuration](configuration.md) guide.
