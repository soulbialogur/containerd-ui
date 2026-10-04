# Environment Diagnostics

This is the single reference for environment checks and command-line diagnostics. The installation, deployment, and troubleshooting guides link here instead of repeating the same commands.

## 1. Check WSL

```powershell
wsl --list --verbose
```

If the bundled distribution is not installed, run `Alpine-ContainerdUI-Setup.exe` to import it into WSL2. Do not install Alpine from the Microsoft Store.

```powershell
wsl --list --verbose
```

Check from inside WSL:

```powershell
wsl -d Alpine-ContainerdUI -- nerdctl version
wsl -d Alpine-ContainerdUI -- rc-service containerd status
wsl -d Alpine-ContainerdUI -- buildctl debug workers
```

The only supported distribution name in `config.json` is `"wsl_distro": "Alpine-ContainerdUI"`. Do not use the UNC path `\\wsl.localhost\\Alpine-ContainerdUI` as the distribution name.

## 2. Check containerd and nerdctl

```bash
nerdctl info
rc-service containerd status
```

If the container runtime is not running:

```bash
doas rc-update add containerd default
doas rc-service containerd start
doas rc-service containerd status
doas rc-service containerd-ui-grpc-proxy status
busybox netstat -lnt | grep ':50051'
```

## 3. Check BuildKit

```bash
buildctl debug workers
```

If the service is not running:

```bash
buildkitd --addr unix:///run/buildkit/buildkitd.sock
```

For a direct start:

```bash
doas buildkitd --addr unix:///run/buildkit/buildkitd.sock
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

## 5. Check Build Tools

These tools are included in the bundled distribution and are needed only to build `containerd-ui.exe` from source:

```bash
go version
x86_64-w64-mingw32-gcc --version
x86_64-w64-mingw32-g++ --version
```

The repository build script uses these tools to compile a Windows `amd64` binary:

```powershell
cd "C:\Users\User\OneDrive\Desktop\ai-chatbot-website"
bash containerd-ui/build.sh
```

## 6. Check Ports 80 and 443

Windows:

```powershell
netstat -ano | findstr :80
netstat -ano | findstr :443
```

Linux inside WSL:

```bash
doas ss -tulpn | grep ':80\|:443'
```

Ports `80` and `443` must be available for Traefik.

## 7. Check DNS

```bash
nslookup example.com
```

or:

```bash
getent hosts example.com
```

Before deployment, the domain must resolve correctly and point to the target server.

## 8. Check the Project's External Network

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

## 9. Check the Project Path

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
