<div align="center">

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![Build Status](https://github.com/soulbialogur/containerd-ui/actions/workflows/main.yml/badge.svg)](https://github.com/soulbialogur/containerd-ui/actions)
# 🚀 Containerd UI

**Containerd UI** is a native Windows application (Go + Fyne) that provides a graphical interface for managing containers via WSL2, containerd, nerdctl and BuildKit.

It is built on a two‑layer architecture: the primary channel is the containerd gRPC API, with a fallback to WSL + nerdctl.

The tool is ideal for local development, building, and deploying projects in a Windows environment without having to switch between terminals.

</div>

---

## 🚀 Features

- **Container management**  
  Start, stop, remove, perform batch operations, and update images without data loss.

- **Project building**  
  Support for `nerdctl compose` with BuildKit, a visual progress bar, and cooperative build cancellation.

- **Real‑time resource monitoring**  
  Display CPU, RAM, disk I/O, network I/O, and per‑container statistics.

- **Network and volume management**  
  View, create, and delete networks and volumes with protection against accidental changes to system resources.

- **System cleanup**  
  6 cleanup modes: cache, dangling images, unused volumes/networks, untagged images, BuildKit cache, and a full “general” cleanup.

- **Deploy to a domain**  
  Choose between Traefik + Let's Encrypt and Cloudflare Tunnel. Built‑in pre‑deployment diagnostics (DNS checks, port 80/443 availability, tool presence).

- **Smart caching**  
  Centralised CacheManager with event‑based invalidation and metric collection (hit rate).

- **Resource saving**  
  `economy_mode` automatically disables background updates for inactive tabs.

- **Environment status**  
  Instant verification of WSL, containerd, BuildKit, nerdctl, and cloudflared health.

---

## ⚙️ System Requirements

- Windows 10/11 with WSL2 installed
- Ubuntu 24.04 distribution inside WSL
- Components installed inside WSL:
  - `containerd`
  - `nerdctl`
  - `buildkitd`
- (Optional) `cloudflared` for using Cloudflare Tunnel

---

## 🚀 Quick Start

**Install WSL and the distribution**

<pre><code>wsl --install Ubuntu-24.04</code></pre>

**Install the container stack inside WSL**

<pre><code>sudo apt update &amp;&amp; sudo apt install -y containerd nerdctl buildkit
sudo systemctl enable --now containerd buildkit</code></pre>

**Build the application**

<pre><code>cd containerd-ui
bash build.sh</code></pre>

Run `containerd-ui.exe` and point it to the root of your project (where `compose.yaml` is located).

Check the status in the “Status” tab – all icons should be green, then you can build and deploy your project.

---

## 📚 Documentation

Detailed guides and reference information can be found in the [`containerd-ui/docs/`](containerd-ui/docs/) folder:

- [📄 README](containerd-ui/docs/README.md) — documentation overview and navigation.
- [🚀 Quick Start](containerd-ui/docs/quickstart.md) — installation and first run.
- [⚙️ Environment Setup](containerd-ui/docs/installation.md) — configuring WSL, containerd and BuildKit.
- [🛠️ Configuration](containerd-ui/docs/configuration.md) — application parameters and settings.
- [🌐 Deploy to a Domain](containerd-ui/docs/deployment.md) — instructions for Traefik and Cloudflare Tunnel.
- [🩺 Diagnostics](containerd-ui/docs/diagnostics.md) — health checks and troubleshooting.
- [🧩 Images and Updates](containerd-ui/docs/images-and-updates.md) — working with images and the update process.
- [📋 Project Requirements](containerd-ui/docs/project-requirements.md) — required dependencies and structure.
- [🛟 Troubleshooting](containerd-ui/docs/troubleshooting.md) — FAQ and common errors.
- [🧠 Architecture and Concepts](containerd-ui/docs/concepts.md) — internal design and working principles.

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
