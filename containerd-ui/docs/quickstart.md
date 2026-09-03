# Quickstart

## 1. Install the Prerequisites

Make sure WSL2 is enabled on Windows and Ubuntu 24.04 is installed.

```powershell
wsl --install Ubuntu-24.04
```

After installation, verify that WSL and the Linux environment are available:

```powershell
wsl --list --verbose
```

The complete set of container environment and service checks is in [diagnostics.md](diagnostics.md).

## 2. Install the Minimum Components

The following must be available inside WSL:

- containerd
- nerdctl
- buildkitd
- cloudflared, if you plan to use Cloudflare Tunnel

The container access model and fallback behavior are described in [concepts.md](concepts.md). For a first run, remember that the containerd gRPC API is the primary path and WSL + nerdctl is the fallback.

Example installation:

```bash
sudo apt update
sudo apt install -y containerd nerdctl
```

For detailed BuildKit installation, see [installation.md](installation.md). For diagnostics and manual startup, see [troubleshooting.md](troubleshooting.md).

## 3. Build the Application

From the containerd-ui directory:

```powershell
cd "C:\Users\User\OneDrive\Desktop\project"
bash build.sh
```

The build should produce `containerd-ui.exe`.

## 4. Start the Application

```powershell
Start-Process .\containerd-ui.exe
```

## 5. Set the Project Path

After starting the application, open the Settings tab and select the project root containing `compose.yaml` or `docker-compose.yml`.

Supported path formats:

- Windows: `C:\Users\User\...`
- WSL: `/mnt/c/Users/User/...`

This step is required before building or deploying. Without a project path, the application cannot locate the project or start its containers.

> Important: [project-requirements.md](project-requirements.md) defines the requirements for the external `deploy_network`, the Compose file, and the project root. It is the authoritative reference for a valid deployment.

## 6. Check Component Status

Open the Status tab and make sure the main components are working: WSL, containerd, BuildKit, nerdctl, and Cloudflare Tunnel if needed.

The corresponding icons should be green. If any component is inactive, fix the environment first, then build the project. For all checks and commands, see [diagnostics.md](diagnostics.md).

> For the architecture and overall application behavior, see [concepts.md](concepts.md).

### Resource-Saving Mode

When `economy_mode` is enabled, inactive tabs pause background updates. This reduces CPU, WSL, and containerd usage. See [concepts.md](concepts.md) for details about tab lifecycle behavior.

## 7. Run the Project for the First Time

In the Containers tab, click Build to start the project for the first time. After the build, check that:

- the containers started;
- the services are attached to the correct network;
- the backend and frontend are reachable on their internal ports.

The build uses [configuration.md](configuration.md) and applies settings such as `squash_layers`, `compression`, `max_parallelism`, `buildkit_cache_ttl`, and `buildkit_max_size`. [concepts.md](concepts.md) describes BuildKit, progress reporting, and cancellation in detail.

## 8. Deploy if Needed

To publish the project on a domain, open the Deployment tab and choose:

- the domain;
- whether HTTPS is enabled;
- backend, frontend, or both;
- the backend prefix;
- a proxy: Traefik or Cloudflare.

Before deployment, the application runs pre-deployment checks for:

- DNS and domain validity;
- whether ports `80` and `443` are in use;
- required tools (`cloudflared`, `nerdctl`, `buildkitd`, and so on);
- routing and network configuration.

These checks help find problems before the proxy starts and reduce the risk of a broken deployment.

Important:

- Traefik requires ports `80` and `443` to be available;
- Cloudflare requires a valid JSON token, which the application validates;
- save the token through the deployment settings rather than pasting an arbitrary string.

See [project-requirements.md](project-requirements.md) for project and network requirements, and [diagnostics.md](diagnostics.md) for environment checks and commands.

## 9. Manage Containers

### Bulk Operations

The Containers tab supports bulk operations:

- **Start** with nothing selected starts all stopped containers;
- **Stop** with nothing selected stops all running containers;
- **Remove** with nothing selected removes all containers after confirmation.

When no container is selected, the application applies the action to every matching container in the current list. This is useful for quickly recovering the environment or restarting the entire project.

Bulk operations use `runContainerOperations` and a worker pool limited by `container_operation_concurrency`. This setting controls how many starts, stops, and removals run at once, reducing WSL/containerd load.

### Update a Container Image

The **Update Image** button replaces a container image without requiring you to recreate the workflow manually:

1. the application stops the container;
2. removes the old instance;
3. pulls the new image;
4. recreates the container while preserving volumes, ports, environment variables, and network settings.

This is effectively an in-place image replacement without data loss. Volumes and user data remain intact, while the container is recreated from the updated image with the same configuration.

### Clear Logs

The Logs tab has a **Clear Logs** button. It truncates the container log file to zero bytes without deleting the file, saving disk space while keeping the same log path.

### Resource-Saving Mode

When `economy_mode` is enabled, automatic refresh runs only for the active tab. Inactive panels do not request background state, reducing CPU, WSL, and containerd usage.

In short, updates pause while a tab is inactive, avoiding unnecessary requests to WSL/containerd.

## 10. If Something Does Not Work

Start by checking that:

- WSL is installed and running;
- the project contains a Compose file;
- services and ports are configured correctly;
- ports 80 and 443 are available;
- DNS points to the correct domain;
- the Cloudflare token is valid and saved correctly;
- the required tools (`buildkitd`, `nerdctl`, `cloudflared`) are available.

For detailed troubleshooting, see [Troubleshooting](troubleshooting.md), [Deployment](deployment.md), and [Configuration](configuration.md).
