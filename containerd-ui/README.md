# Containerd UI
  
An app for managing containers, builds, and deployments through WSL2, containerd, nerdctl, and BuildKit.
  
## What It Does
  
Containerd UI is a Windows wrapper around your container environment. It helps you:
  
- manage containers, images, and volumes
- build and rebuild your project
- keep an eye on WSL/containerd resource usage
- set up proxying and domain deployment
- diagnose and clean up your environment
## Supported Environment

The supported runtime stack is:

- Windows 10/11;
- WSL2;
- the bundled `Alpine-ContainerdUI` distribution in WSL2;
- OpenRC inside Alpine;
- containerd;
- nerdctl;
- BuildKit (`buildkitd` and `buildctl`).

The application uses `Alpine-ContainerdUI` and detects its shell, OpenRC, `apk`, and available tools automatically.
  
## Documentation
  
You'll find detailed guides in the [docs](docs/README.md) folder:
  
- [Quick Start](docs/quickstart.md)
- [Setting Up Your Environment](docs/installation.md)
- [Configuration](docs/configuration.md)
- [Deploying to a Domain](docs/deployment.md)
- [Troubleshooting](docs/troubleshooting.md)
## Quick Start

### Bundled Alpine + OpenRC

1. Install WSL2, then run the bundled offline setup executable:
   ```powershell
   .\dist\Alpine-ContainerdUI-Setup.exe
   ```
2. The `Alpine-ContainerdUI` image includes OpenRC, containerd, nerdctl, BuildKit, Go, MinGW-w64, and OpenGL development files.
3. Install `cloudflared` if you need Cloudflare Tunnel.
4. Build the app if needed:
   ```powershell
   wsl -d Alpine-ContainerdUI
   ```
   ```bash
   cd /mnt/c/Users/User/OneDrive/Рабочий\ стол/project
   sh containerd-ui/build.sh
   ```
5. Launch `containerd-ui/dist/containerd-ui.exe`; the app automatically selects `Alpine-ContainerdUI`.
6. Point it to your project root and check that the environment status looks good.

To build an offline WSL runtime installer, run `.\build-offline-installer.ps1` from this directory. The setup executable is written to `dist\Alpine-ContainerdUI-Setup.exe`; it installs the runtime distro used by the app.
## Architecture
 
The app uses a "two-layer access" approach: containerd's gRPC API handles the main management tasks, with WSL + nerdctl acting as a fallback. For the full breakdown of how this works, see [docs/concepts.md](docs/concepts.md).
 
## Settings
 
Your main settings live in `config.json`, sitting right next to the executable. More on that in [docs/configuration.md](docs/configuration.md).
 
## Key Features
 
- Container management with bulk operations
- Project builds with progress tracking
- CPU/RAM/disk monitoring and service status checks
- Full support for networks, volumes, images, and logs
- BuildKit integration with cache cleanup
- Traefik + Let's Encrypt and Cloudflare Tunnel support
- Pre-deployment diagnostics with automatic rollback on failure
- Result caching and tab lifecycle management via `economy_mode`
### Quick Links to Key Features
 
- [Caching and automatic invalidation](docs/configuration.md#кэш-и-автоматическая-инвалидизация)
- [Tab lifecycle and resource-saving mode](docs/configuration.md#режим-экономии-ресурсов)
- [Progress tracking and cancelling long operations](docs/configuration.md#прогресс-и-отмена-длительных-операций)
## Links
 
- [Full Documentation](docs/README.md)
- [Quick Start](docs/quickstart.md)
- [Setting Up Your Environment](docs/installation.md)
- [Configuration](docs/configuration.md)
- [Deploying to a Domain](docs/deployment.md)
- [Troubleshooting](docs/troubleshooting.md)
