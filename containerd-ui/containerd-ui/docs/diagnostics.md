# Environment Diagnostics

This is the single reference for environment checks and command-line diagnostics. The installation, deployment, and troubleshooting guides link here instead of repeating the same commands.

## 1. Check WSL

```powershell
wsl --list --verbose
```

If the distribution is not installed:

```powershell
wsl --install Ubuntu-24.04
```

Check from inside WSL:

```powershell
wsl -d Ubuntu-24.04 -- nerdctl version
wsl -d Ubuntu-24.04 -- systemctl is-active containerd
wsl -d Ubuntu-24.04 -- systemctl is-active buildkit
```

## 2. Check containerd and nerdctl

```bash
nerdctl info
systemctl status containerd
```

If the container runtime is not running:

```bash
sudo systemctl enable containerd
sudo systemctl start containerd
sudo systemctl status containerd
```

## 3. Check BuildKit

```bash
sudo systemctl status buildkit
sudo systemctl is-active buildkit
```

If the service is not running:

```bash
sudo systemctl start buildkit
```

For a direct start:

```bash
sudo buildkitd --addr unix:///run/buildkit/buildkitd.sock
```

## 4. Check Deployment Tools

The following tools must be available for deployment:

- `nerdctl`
- `containerd`
- `buildctl`
- `cloudflared` (if Cloudflare Tunnel is selected)

Check them with:

```bash
cloudflared --version
cloudflared tunnel list --help
```

To validate the credentials:

```bash
cloudflared tunnel list --credentials-file /path/to/credentials.json
```

## 5. Check Ports 80 and 443

Windows:

```powershell
netstat -ano | findstr :80
netstat -ano | findstr :443
```

Linux inside WSL:

```bash
sudo ss -tulpn | grep ':80\|:443'
```

Ports `80` and `443` must be available for Traefik.

## 6. Check DNS

```bash
nslookup example.com
```

or:

```bash
getent hosts example.com
```

Before deployment, the domain must resolve correctly and point to the target server.

## 7. Check the Project's External Network

Use the network name from `deploy_network` in `config.json`. The examples below use `my-project-network`; replace it with your value.

Check that the network exists:

```bash
nerdctl network ls
```

If it does not exist, create it manually:

```bash
nerdctl network create --driver bridge my-project-network
```

It must still be declared as external in the Compose file:

```yaml
networks:
  my-project-network:
    external: true
    name: my-project-network
```

## 8. Check the Project Path

The project path must point to the project root, not to the `compose.yaml` file.

You can verify it by checking the directory structure:

```text
project/
├── compose.yaml
├── backend/
├── frontend/
└── ...
```

## 9. When to Use This Guide

Use this guide when you need to quickly check:

- WSL and the Linux environment;
- `containerd`, `nerdctl`, `buildkitd`;
- ports and DNS;
- Cloudflare credentials;
- the network from `deploy_network`.

See also:

- [installation.md](installation.md)
- [deployment.md](deployment.md)
- [troubleshooting.md](troubleshooting.md)
- [project-requirements.md](project-requirements.md)
