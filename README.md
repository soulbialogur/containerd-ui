<div align="center">

[![Go Version](https://img.shields.io/badge/Go-1.26.5+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![Build Status](https://github.com/soulbialogur/containerd-ui/actions/workflows/main.yml/badge.svg)](https://github.com/soulbialogur/containerd-ui/actions)

# 🚀 Containerd UI

**Containerd UI** is a native Windows application (Go + Fyne) that provides a graphical interface for managing containers via WSL2, containerd, nerdctl and BuildKit.

**Status: Stable.** Version 1.2.4 uses the bundled `Alpine-ContainerdUI` runtime image in WSL2.

It is built on a two‑layer architecture: the primary channel is the containerd gRPC API, with a fallback to WSL + nerdctl.

The tool is ideal for local development, building, and deploying projects in a Windows environment without having to switch between terminals. The interface is available in **English and Russian**.

</div>

---

## 🚀 Features

- **Container management**  
Start, stop, remove, perform batch operations, update images, and inspect status, uptime and health.

- **Project building**  
Support for `nerdctl compose` with BuildKit, a visual progress bar, and cooperative build cancellation.

- **Interface localization**  
Switch between English and Russian in one click from the Settings tab. The choice is stored in `config.json` and survives restarts.

- **Resource monitoring**  
Display CPU, RAM, disk usage, network I/O, and per-container statistics.

- **Network and volume management**  
View, create, and delete networks and volumes. Volume sizes are calculated asynchronously so the list appears without waiting for disk scans.

- **Image and log inspection**  
Image sizes and creation times are normalized when loaded. Live container logs use a cancellable stream instead of polling the full log every second.

- **System cleanup**  
6 cleanup modes: cache, dangling images, unused volumes/networks, untagged images, BuildKit cache, and a full “general” cleanup.

- **Deploy to a domain**  
Choose between Traefik + Let's Encrypt and Cloudflare Tunnel. Built‑in pre‑deployment diagnostics (DNS checks, port 80/443 availability, tool presence).

- **Smart caching**  
Centralised CacheManager with event‑based invalidation and metric collection (hit rate).

- **Resource saving**  
`economy_mode` pauses background updates for inactive tabs; live log streams stop when disabled or when the selected container changes.

- **Environment status**  
Checks WSL, containerd, BuildKit, and nerdctl. Cloudflared is optional and only needed for Cloudflare Tunnel deployments.

---

## ⚙️ System Requirements

- Windows 10/11 with WSL2 enabled. If WSL2 is not installed yet, enable it with `wsl --install`.
- No separate Alpine Linux, OpenRC, containerd, nerdctl, or BuildKit installation is required. The `Alpine-ContainerdUI` setup executable installs the complete prepared runtime image in one step.
- `cloudflared` is optional and only needed when using Cloudflare Tunnel.

---

## 🚀 Quick Start

**Prepare WSL2**

```powershell
wsl --install
```

Run this only if WSL2 is not already installed. Restart Windows if prompted.

**Install the complete runtime**

Download and run the `Alpine-ContainerdUI` setup executable from [the v1.2.4 release](https://github.com/soulbialogur/containerd-ui/releases/tag/1.2.4). It installs the complete prepared WSL2 image, including Alpine Linux, OpenRC, containerd, nerdctl, and BuildKit. You do not need to install these components separately. An existing Alpine distribution is not replaced.

After installation, open Containerd UI and select `Alpine-ContainerdUI` in Settings if it was not selected automatically.

**Download and launch the application**

Download `containerd-ui.exe` from [the v1.2.4 release](https://github.com/soulbialogur/containerd-ui/releases/tag/1.2.4), then launch it. The application icon is embedded in the executable. The app build output, when building from source, is `containerd-ui/dist/containerd-ui.exe`.

**Build the application from source (optional)**

From PowerShell, run the build script inside the bundled Alpine distribution:

```powershell
wsl -d Alpine-ContainerdUI --cd "C:\path\to\repository\containerd-ui" -- sh build.sh
```

The script cross-compiles the Windows amd64 executable and writes it to `containerd-ui/dist/containerd-ui.exe`.

`cloudflared` is not bundled; install it using the [official Cloudflare instructions](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/) only if you use Cloudflare Tunnel.

---

## 📚 Documentation

Detailed guides and reference information are in the [docs](../docs/) folder:

- [Documentation overview](../docs/README.md)
- [Quick Start](../docs/quickstart.md)
- [Environment Setup](../docs/installation.md)
- [Configuration](../docs/configuration.md)
- [Domain Deployment](../docs/deployment.md)
- [Diagnostics](../docs/diagnostics.md)
- [Images and Updates](../docs/images-and-updates.md)
- [Project Requirements](../docs/project-requirements.md)
- [Troubleshooting](../docs/troubleshooting.md)
- [Architecture and Concepts](../docs/concepts.md)

---

## 📄 License

**Containerd UI** is licensed under the **GNU Affero General Public License, Version 3** (or any later version at your option).

Copyright (C) 2026 Bolsinov Nikita Aleksandrovich

The full license text is available from the Free Software Foundation:  
[https://www.gnu.org/licenses/agpl-3.0.html](https://www.gnu.org/licenses/agpl-3.0.html)  
Plain text version: [https://www.gnu.org/licenses/agpl-3.0.txt](https://www.gnu.org/licenses/agpl-3.0.txt)

### Dual Licensing

Subject to the terms of the GNU Affero General Public License, this project is also available under a **separate commercial license**. A commercial license may be obtained by written agreement with the copyright holder:

- **Bolsinov Nikita Aleksandrovich**  
📧 **soulbialogur@gmail.com**

The commercial license is an alternative licensing option. It does not modify, restrict, or replace any rights granted by the GNU Affero General Public License. No fee, royalty, or other charge is required to exercise rights granted under the GNU Affero General Public License.

### Attribution and Source Reference

When you distribute this project or a modified version of it under the GNU Affero General Public License, you must retain all existing copyright notices and this license notice. You must also include the following attribution in the documentation, about dialog, or other prominent notices supplied with the covered work:

> **Containerd UI**  
> Copyright (C) 2026 Bolsinov Nikita Aleksandrovich  
> Original project: [https://github.com/soulbialogur/containerd-ui](https://github.com/soulbialogur/containerd-ui)

This requirement applies to distribution or public display of the covered work and does not restrict the freedoms granted by the GNU Affero General Public License.

### Project Identity and Trademarks

The names **"Containerd UI"**, the author's name, and the project logos or marks may not be used to endorse or promote a modified work without prior written permission, except for accurate factual statements about the origin of the work and ordinary copyright or license notices.

This notice does not grant trademark rights and does not restrict the freedoms granted by the GNU Affero General Public License.

---

## ✉️ Contacts

For any questions, development suggestions, or security reports, please email:

📧 **soulbialogur@gmail.com**

> 🔒 **Security:**  
> Please report vulnerabilities **exclusively** to this address.  
> **Do not create public issues** describing security problems — this helps avoid risks for users.

---

## 🧡 About the Project

<div align="center">

Made with ❤️ for developers who work with containers in a Windows environment.  
Your feedback and support help make the project better!

</div>
