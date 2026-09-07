# Environment Setup

## Requirements

- Windows 10/11
- WSL2
- Ubuntu 24.04
- access to PowerShell
- permission to install packages in WSL
- Go 1.26+ — required to build the application from source

## Install WSL

```powershell
wsl --install Ubuntu-24.04
```

Verify it:

```powershell
wsl --list --verbose
```

## Verify the Container Environment

For the complete list of verification commands and scenarios, see [diagnostics.md](diagnostics.md). It covers WSL, `containerd`, `nerdctl`, `buildkitd`, ports, DNS, and Cloudflare credentials.

If anything is missing, install or start the services manually, then check their status again using the diagnostics guide.

## Install containerd and nerdctl

```bash
sudo apt update
sudo apt install -y containerd nerdctl
```

After installation, verify them:

```bash
nerdctl version
nerdctl info
```

## Install and Start BuildKit

BuildKit is required to build images in the application. Install the package and make sure the service is available in WSL:

```bash
sudo apt install -y buildkit
sudo systemctl enable buildkit
sudo systemctl start buildkit
```

The complete set of verification commands and startup/error scenarios is available in [diagnostics.md](diagnostics.md). If the daemon will not start or keeps failing, also see [troubleshooting.md](troubleshooting.md). The application can start `buildkitd` automatically when a build begins if it is not already running.

## Start containerd

```bash
sudo systemctl enable containerd
sudo systemctl start containerd
sudo systemctl status containerd
```

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

```text
Windows
└── WSL Ubuntu 24.04
    ├── containerd
    ├── nerdctl
    ├── buildkitd
    ├── cloudflared
    └── app project
```

## How the Application Accesses Containers

The main container access model and fallback behavior are described in [concepts.md](concepts.md). The short version is that the containerd gRPC API has priority, while WSL + nerdctl is used as a fallback when gRPC fails or is unavailable.

## Where Configuration Is Stored

Deployment settings, project paths, and environment parameters are stored in `config.json` next to `containerd-ui.exe`. This includes the WSL distribution, proxy, domains, services, and internal application ports.

## Next Steps

Once the environment is ready, continue with the [Quickstart](quickstart.md) or [Configuration](configuration.md) guide.
