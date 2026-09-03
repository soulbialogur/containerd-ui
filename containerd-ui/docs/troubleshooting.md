# Troubleshooting

## Access Model

The complete access architecture is described in [concepts.md](concepts.md). Use [diagnostics.md](diagnostics.md) for environment checks and commands.

## WSL Is Not Found

WSL and distribution checks are collected in [diagnostics.md](diagnostics.md). If the distribution is missing, install it using the instructions there.

## containerd or nerdctl Is Not Working

The complete set of checks for `containerd`, `nerdctl`, services, and the environment is in [diagnostics.md](diagnostics.md). After the basic checks, review the status and logs of the affected service.

## BuildKit Will Not Start

First verify the installation using [installation.md](installation.md). For startup and diagnostic commands, see [diagnostics.md](diagnostics.md), which also includes manual startup options and service checks.

If the service still does not respond, check the logs:

```bash
sudo journalctl -u buildkit -n 100 --no-pager
```

## Cannot Connect to containerd

Make sure `containerd` is running and available inside WSL:

```bash
wsl -d Ubuntu-24.04 -- systemctl status containerd
wsl -d Ubuntu-24.04 -- ss -lnt | grep 50051
```

If the port is not open, check whether it is blocked by a firewall or iptables. In most cases, starting the container runtime and running `nerdctl info` again resolves the issue.

## Ports 80/443 Are in Use

The port-checking logic and Windows/WSL conflict details are described in [deployment.md](deployment.md). For a quick diagnosis, use the commands in [diagnostics.md](diagnostics.md), then stop the process holding the port.

## Cloudflare token invalid

Token and credential-file validation is described in [deployment.md](deployment.md), including the Cloudflare Dashboard steps and this command:

```bash
cloudflared tunnel list --credentials-file /path/to/credentials.json
```

If the command fails, refresh the token in the Cloudflare Dashboard and save it in the application again. Also verify that `cloudflared` is installed and available in `PATH`.

## Project External Network Error

If the network from `deploy_network` is not found, first check [project-requirements.md](project-requirements.md), which explains the correct `external` block, service connections, and project root. To create the network locally, use:

```bash
nerdctl network create --driver bridge my-project-network
```

The Compose file must still declare the network correctly as external. Replace `my-project-network` with the value of `deploy_network`; otherwise deployment and routing will fail.

## The Compose File Has No Configured External Network

The correct declaration and an example are in [project-requirements.md](project-requirements.md), which also shows how to attach services to `deploy_network`.

## The Project Path Contains Spaces or Non-Latin Characters

The application supports these paths, but make sure WSL mounts the directory correctly. A typical Windows path looks like this:

```bash
/mnt/c/Users/User/OneDrive/Desktop/project
```

If the application cannot see the project, verify that the configured path uses the actual WSL format and that the directory was mounted correctly.

## The WSL Cache Is Full

Check these cache settings:

- `max_wsl_cache_size`
- `wsl_cache_cleanup_at`

You can clear the cache with the **Clear Cache** button or increase the limit in `config.json`. You can also remove old WSL data and refresh the state so the application builds a new cache.

If the cache fills up quickly, increase `max_wsl_cache_size` and adjust `wsl_cache_cleanup_at` to match the project size. These settings are applied dynamically during the next cleanup pass; the environment does not need to be recreated.

## The Progress Bar Does Not Update

This is often caused by an inactive tab or enabled `economy_mode`. In this mode, inactive tabs pause updates, so the UI may look stuck while background operations continue. Make the tab active and temporarily disable `economy_mode` to diagnose the issue.

If automatic refresh works only on the active tab, check `economy_mode` and `auto_refresh_interval`. Temporarily disabling economy mode will show whether the issue is simply background updates being paused for inactive panels.

## An Operation Is Stuck

If an operation does not finish, click **Cancel**. Cancellation is cooperative: the application asks the operation to stop at a safe point through `cancelCh` rather than forcibly interrupting it. This can take a few seconds if a long command or WSL/containerd response is already in progress.

## The Application Cannot See the Project

Make sure `project_path` points to the project root rather than the `compose.yaml` file.

## Ports 80/443 Are in Use

The explanation and verification procedure are in [deployment.md](deployment.md). For a quick check, use the command from [diagnostics.md](diagnostics.md), then stop the conflicting process or service listening on ports 80/443.

## Traefik Will Not Start

For the complete deployment checklist, see [deployment.md](deployment.md) and [diagnostics.md](diagnostics.md). Check DNS, the `deploy_network`, the ACME email, and ports 80/443.

## Cloudflare Tunnel Does Not Work

Token and credential validation is described in [deployment.md](deployment.md). Basic CLI checks:

```bash
cloudflared --version
cloudflared tunnel list --credentials-file /path/to/credentials.json
```

## The Compose Network Check Fails

Make sure the file contains:

```yaml
networks:
  my-project-network:
    external: true
    name: my-project-network
```

Also make sure the backend and frontend are attached to this network.

## Configuration Is Not Applied

Make sure `config.json` is next to `containerd-ui.exe`.

Restart the application after editing the file manually.

## Useful Commands

```powershell
wsl -d Ubuntu-24.04 -- nerdctl ps -a
wsl -d Ubuntu-24.04 -- nerdctl compose -f /path/to/compose.yaml config
```

## Application Logs

When the application is started from a command prompt, error, initialization, build, and deployment messages are printed to the console. This makes it easier to identify the failing stage and the unresponsive component.

Example:

```powershell
cd "C:\Users\User\OneDrive\Desktop\project"
.
\containerd-ui.exe
```

If the application crashes or hangs, check the console as well as the UI. It often reveals problems with WSL, the project path, the Compose file, BuildKit, or the Cloudflare token.

## If the Problem Persists

- review the deployment log in the application;
- check the Traefik / Cloudflared logs;
- verify that the project and its Compose file meet the network environment requirements.
