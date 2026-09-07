# Containerd UI
 
An app for managing containers, builds, and deployments through WSL2, containerd, nerdctl, and BuildKit.
 
> This project's documentation is written mainly for Russian-speaking users — all the core instructions, configuration details, and troubleshooting guides are in Russian. If you have any questions about installation, using the app, or licensing, reach out at soulbialogur@gmail.com.
 
## What It Does
 
Containerd UI is a Windows wrapper around your container environment. It helps you:
 
- manage containers, images, and volumes
- build and rebuild your project
- keep an eye on WSL/containerd resource usage
- set up proxying and domain deployment
- diagnose and clean up your environment
## Documentation
 
You'll find detailed guides in the [docs](docs/README.md) folder:
 
- [Quick Start](docs/quickstart.md)
- [Setting Up Your Environment](docs/installation.md)
- [Configuration](docs/configuration.md)
- [Deploying to a Domain](docs/deployment.md)
- [Troubleshooting](docs/troubleshooting.md)
## Quick Start
 
1. Install WSL2 and Ubuntu 24.04.
2. Inside WSL, install `containerd`, `nerdctl`, and `buildkitd`.
3. Install `cloudflared` if you need it.
4. Build the app:
```powershell
cd "C:\Users\User\OneDrive\Рабочий стол\project"
bash build.sh
```
 
5. Launch `containerd-ui.exe`.
6. Point it to your project root and check that the environment status looks good.
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
