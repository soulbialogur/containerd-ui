# Working with Images and Updating the Application

This guide explains how to manage images independently of the project, build and update the application, and account for hardware and licensing requirements.

## 1. Working with Images

### Overview

Images in this project are built with `nerdctl build` and can be created for:

- local project runs;
- an individual service (`backend`, `frontend`, `postgres`);
- reuse in another environment or under a different tag.

The main project build flow is described in [quickstart.md](quickstart.md) and [deployment.md](deployment.md). This page focuses on images as independent artifacts.

### Common Commands

List images:

```bash
nerdctl images
```

Build an individual image:

```bash
cd /path/to/project
nerdctl build --progress=plain --tag my-project/backend:latest --file backend/Dockerfile ./backend
```

Build with a custom name and tag:

```bash
nerdctl build --progress=plain --tag my-registry/my-project/backend:v1.2.0 --file backend/Dockerfile ./backend
```

Verify that the image was built and is visible locally:

```bash
nerdctl image inspect my-project/backend:latest
```

Remove an image:

```bash
nerdctl rmi my-project/backend:latest
```

Remove untagged images:

```bash
nerdctl images -q | xargs -r nerdctl rmi
```

> Important: removing an image that is used by a container may fail. Stop the container or remove its instance first.

### Tagging Policy

For local development, tags like these are convenient:

- `my-project/backend:latest`
- `my-project/frontend:latest`
- `my-project/postgres:latest`

For a release, use versioned tags:

- `my-project/backend:v1.2.0`
- `my-project/frontend:v1.2.0`
- `my-project/postgres:v1.2.0`

This makes it easier to:

- distinguish local builds from releases;
- roll back to an earlier build;
- identify the version that is actually running.

### Clean Up Images and Cache

For local cleanup, use:

```bash
nerdctl image prune
nerdctl builder prune
```

To clean BuildKit according to size and TTL limits, use the settings in [configuration.md](configuration.md) and the Cleanup section of the UI.

## 2. Update the Application

### Basic Update Flow

Updating the application usually involves four steps:

1. back up the current configuration;
2. update the source code;
3. rebuild the UI binary and/or containers;
4. verify the environment and start the project again.

### Recommended Flow

```bash
git pull --rebase
```

Then rebuild the UI:

```bash
cd containerd-ui
bash build.sh
```

If only the project stack and containers have changed:

```bash
cd /path/to/project
nerdctl compose build
nerdctl compose up -d --force-recreate
```

If only the backend/frontend code changed and the image has already been built:

```bash
nerdctl compose up -d --build
```

### What to Back Up Before Updating

Before updating, back up:

- `config.json` next to `containerd-ui.exe`;
- `.containerd-data/` with proxy configuration and ACME data;
- domain, token, and Let's Encrypt/Cloudflare email values;
- secret files and `.env` settings, if applicable.

### Verify After Updating

After updating, verify that:

- WSL and containerd are still available;
- BuildKit is running;
- the project builds and starts without errors;
- DNS and ports `80/443` have no conflicts;
- the external network from `deploy_network` exists and is attached correctly.

The complete set of checks is available in [diagnostics.md](diagnostics.md).

## 3. Hardware Requirements

### Recommended Environment

For reliable operation of the container UI and WSL2, we recommend:

- Windows 11 or a current version of Windows 10;
- at least 8 GB of RAM, with 16 GB preferred;
- an SSD with sufficient free space;
- 4 or more logical CPU cores;
- a stable network connection for downloading images and certificates.

### What Matters Most

- RAM: headroom is important during active image builds or when running several containers;
- SSD: speeds up builds and image reads;
- free disk space: BuildKit and Docker/nerdctl caches can grow quickly;
- network access: especially important for Cloudflare Tunnel and Let's Encrypt.

### On Resource-Constrained Machines

On less powerful machines:

- reduce `container_operation_concurrency`;
- reduce `max_parallelism`;
- enable `economy_mode`;
- keep `buildkit_cache_ttl` and cache sizes under control.

Parameter selection is described in [configuration.md](configuration.md).

## 4. Licensing

The project is available under the GNU Affero General Public License v3 or under a separate commercial license based on a written agreement with the rights holder. AGPLv3 does not prohibit commercial use, but its terms must be followed when software is distributed or functionality is provided over a network. The commercial option does not alter the terms of AGPLv3.

You must also account for the separate licenses of third-party components, libraries, container images, and utilities used by the project or the WSL environment.

## 5. Further Reading

- [quickstart.md](quickstart.md)
- [installation.md](installation.md)
- [configuration.md](configuration.md)
- [deployment.md](deployment.md)
- [diagnostics.md](diagnostics.md)
- [troubleshooting.md](troubleshooting.md)
- [concepts.md](concepts.md)
