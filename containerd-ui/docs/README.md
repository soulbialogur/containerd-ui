# Containerd UI Documentation

This is a concise guide to the application, organized by task so you can quickly find the instructions you need.

## Sections

- [Quickstart](quickstart.md) — get the application running from scratch.
- [Environment Setup](installation.md) — WSL, containerd, nerdctl, BuildKit, and Cloudflare Tunnel.
- [Configuration](configuration.md) — what `config.json` contains and how to configure a project.
- [Domain Deployment](deployment.md) — Traefik, Let's Encrypt, and Cloudflare.
- [Troubleshooting](troubleshooting.md) — common errors and how to diagnose them.
- [Core Concepts](concepts.md) — architecture, caching, resource saving, BuildKit, and lifecycle behavior.
- [Environment Diagnostics](diagnostics.md) — all WSL, DNS, port, network, and tool checks.
- [Project Requirements](project-requirements.md) — `compose.yaml`, the configurable network, services, and domain.
- [Images and Application Updates](images-and-updates.md) — building, tagging, updating, hardware, and licensing.
- [License](../../Others/LICENSE) — usage terms and additional project conditions.

## Quick Navigation

To understand how the application works, start with [concepts.md](concepts.md). To check the environment before starting or deploying, open [diagnostics.md](diagnostics.md). If the project is not ready for deployment, review [project-requirements.md](project-requirements.md).

## What the Application Does

- manage containers, images, and volumes;
- manage networks, including creating and deleting them and viewing attached containers;
- monitor system resources in real time: RAM, CPU, disk, and container statistics;
- start and build projects with `nerdctl compose`;
- display service logs and status;
- clean the system by category: cache, dangling images, unused volumes, networks, untagged images, BuildKit cache, or perform a full cleanup;
- show progress and cancel long-running operations, including builds and image updates;
- run bulk container operations when nothing is selected: start, stop, or remove all matching containers;
- update a container image without losing data;
- clear logs for an individual container;
- manage `buildkitd` from the UI by starting or stopping the daemon;
- run pre-deployment checks for DNS, ports `80`/`443`, required tools, and environment availability;
- roll back deployments and view proxy logs;
- configure WSL and containerd;
- run Traefik or Cloudflare Tunnel for domain deployment;
- use two infrastructure access layers: the containerd gRPC API and the WSL + nerdctl fallback;
- run container operations in parallel with a configurable concurrency limit.

## Two Infrastructure Access Layers

The architecture, cache, and lifecycle are described in detail in [concepts.md](concepts.md). This page only provides a short reference to avoid repeating the same information in every guide.

## Where Settings Are Stored

Deployment settings, project paths, and environment parameters are stored in `config.json` next to `containerd-ui.exe`. It is the single configuration point for WSL, containers, the proxy, and deployment.

## Where to Start

For a first run, start with [Quickstart](quickstart.md).
To prepare the environment, open [Environment Setup](installation.md).
To configure a domain or deployment, go to [Domain Deployment](deployment.md).

## Networks Tab

The Networks tab shows container networks, their drivers, status, and attached containers. This makes it easy to see which services communicate with one another and where routing or hostname problems may occur.

The application can create user-defined networks and remove unused ones, while protecting the system networks (`bridge`, `host`, `none`) from accidental deletion. You can also see which containers use a network before cleaning it up.

The interface shows:

- the network name and driver;
- the containers attached to the network;
- whether the required services are connected;
- network relationships before cleanup or deployment.

If a network is not attached to the services, the usual causes are an incorrect `compose.yaml`, a missing `deploy_network`, or an incorrect project root.

## Resources Tab

The Resources tab displays WSL and container metrics: RAM, CPU, disk, network I/O, and per-service memory usage. The data refreshes automatically, making it easy to spot overloads, memory leaks, and rising background activity.

The tab reports:

- RAM: total usage, available memory, and the limit;
- CPU: core count and current load;
- disk: used and available space on `/`;
- network I/O: inbound and outbound traffic per container;
- PIDs / threads: container process activity and overload risk.

How to interpret the data:

- high `CPU` usage combined with low available RAM often indicates that WSL or the containers are overloaded;
- disk usage growing without obvious activity may indicate accumulated logs, images, or cache;
- a large number of threads or processes inside a container is often linked to a failed restart or a hung service.

## For Developers

Here is a brief map of the internal architecture:

- `ui/` — the presentation layer: tables, tabs, dialogs, progress bars, and events;
- `wsl/` — the client layer for WSL, `nerdctl`, `containerd`, `buildkitd`, checks, and caches;
- `main.go` — UI initialization and tab registration;
- `CacheManager` — centralized cache invalidation and metrics;
- `OperationManager` — status and progress tracking for long-running operations;
- `config.go` — the central `AppConfig` and default values.

When changing functionality, start at the UI call site, then follow it into `wsl/*` and check whether it publishes the required cache invalidation event or passes a `cancelCh` cancellation signal.

## Status Tab

The Status tab shows the state of the key components. The icons mean:

- ✅ — everything is working;
- ⚠️ — a warning is present;
- ❌ — the component is unavailable or not responding.

The tab also includes **Start Buildkitd** and **Stop Buildkitd** buttons for manual BuildKit control. You can disable automatic status refresh with the checkbox to reduce system load.

## Database Tab

The Database tab shows the size and files in the PostgreSQL volume. The volume name comes from the application configuration, so you can quickly verify that the database is working and data is being persisted.

## System Cleanup

The Cleanup tab provides six operations:

1. **Cache and dangling images** — removes temporary files, nerdctl cache, and logs older than seven days.
2. **Unused volumes** — removes volumes that are not attached to any container.
3. **Unused networks** — removes user-defined networks without active containers, excluding `bridge`, `host`, and `none`.
4. **Untagged images** — removes images without a repository or tag.
5. **BuildKit cache** — cleans the cache according to its TTL and/or size limit, when configured.
6. **Full cleanup** — runs all of the operations above in sequence.

All operations run asynchronously, and their results appear in the UI output area while they run and after they finish.
