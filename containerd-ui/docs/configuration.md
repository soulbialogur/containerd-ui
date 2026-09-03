# Configuration

The application stores its settings in `config.json`, next to `containerd-ui.exe`.

## How System Access Works

The access model is described in [concepts.md](concepts.md): the containerd gRPC API is the primary path, and WSL + nerdctl is the fallback. This guide focuses on configuration fields and their defaults.

## Main Settings

According to `DefaultConfig()` in `config.go`, the key defaults are:

- `compression` = `"zstd"`
- `deployment_proxy` = `"traefik"`

Example configuration file:

```json
{
  "project_path": "C:\\Users\\User\\OneDrive\\Desktop\\project",
  "wsl_distro": "Ubuntu-24.04",
  "cd_port": 50051,
  "cd_namespace": "default",
  "scripts_path": "scripts/containerd",
  "db_volume_name": "my-project-postgres-data",
  "systemd_service": "containerd",
  "nerdctl_path": "",
  "log_tail": 100,
  "wsl_cache_ttl": 2,
  "containers_cache_ttl": 3,
  "images_cache_ttl": 5,
  "volumes_cache_ttl": 5,
  "auto_refresh_interval": 3,
  "economy_mode": false,
  "squash_layers": false,
  "compression": "zstd",
  "compression_level": 6,
  "max_wsl_cache_size": 10485760,
  "wsl_cache_cleanup_at": 25,
  "default_cpu_limit": "",
  "default_memory_limit": "",
  "max_parallelism": 0,
  "container_operation_concurrency": 4,
  "buildkit_cache_ttl": 24,
  "buildkit_max_size": "5g",
  "deployment_proxy": "traefik",
  "deploy_email": "admin@example.com",
  "deploy_network": "my-project-network",
  "deploy_service_backend": "backend",
  "deploy_service_frontend": "frontend",
  "deploy_service_backend_port": 8000,
  "deploy_service_frontend_port": 80
}
```

> `config.json` may also contain compatibility and reserved fields. They are not active UI settings and can be left in place for backward compatibility. They are listed below under “Reserved / Legacy Fields”.

## Complete Field Reference

| Field | Description | Default |
|---|---|---:|
| `project_path` | Path to the project root containing `compose.yaml` or `docker-compose.yml` | `""` |
| `wsl_distro` | WSL distribution where Linux commands run | `Ubuntu-24.04` |
| `cd_port` | gRPC port for the container runtime (`containerd`) | `50051` |
| `cd_namespace` | Namespace for the containerd API | `default` |
| `scripts_path` | Subdirectory containing container operation scripts | `scripts/containerd` |
| `db_volume_name` | PostgreSQL volume name shown in the Database tab | `""` |
| `systemd_service` | systemd service name for the container environment | `containerd` |
| `nerdctl_path` | Path to `nerdctl`; if empty, `PATH` is used | `""` |
| `log_tail` | Number of log lines shown in the UI | `100` |
| `wsl_cache_ttl` | WSL command cache lifetime in seconds | `2` |
| `containers_cache_ttl` | Container list cache TTL in seconds | `3` |
| `images_cache_ttl` | Image list cache TTL in seconds | `5` |
| `volumes_cache_ttl` | Volume list cache TTL in seconds | `5` |
| `auto_refresh_interval` | Automatic refresh interval in seconds | `3` |
| `economy_mode` | Disable background updates for inactive tabs | `false` |
| `squash_layers` | Squash layers during builds with `--squash` | `false` |
| `compression` | Compression type (`gzip`, `zstd`, `none`) | `zstd` |
| `compression_level` | Compression level from 1 to 9 | `6` |
| `max_wsl_cache_size` | Maximum WSL cache size in bytes | `10485760` |
| `wsl_cache_cleanup_at` | Cache cleanup threshold by entry count | `25` |
| `default_cpu_limit` | CPU limit for new containers, for example `"0.5"`; applies when creating containers, including image updates | `""` |
| `default_memory_limit` | Memory limit for new containers, for example `"512m"`; applies when creating containers, including image updates | `""` |
| `max_parallelism` | Maximum concurrent builds; `0` = unlimited | `0` |
| `container_operation_concurrency` | Number of concurrent container start/stop/remove operations. Read through `GetContainerOperationConcurrency()` and used for bulk container actions | `4` |
| `buildkit_cache_ttl` | Remove BuildKit cache entries older than N hours; `0` disables age-based cleanup | `24` |
| `buildkit_max_size` | Maximum BuildKit cache size, for example `"5g"` | `"5g"` |
| `deployment_proxy` | Selected proxy: `traefik` or `cloudflare` | `traefik` |
| `deploy_email` | Email for Let's Encrypt / ACME certificate issuance | `""` |
| `deploy_network` | External network name for the proxy and services | `soul-dialogue` |
| `deploy_service_backend` | Backend service name in Compose | `backend` |
| `deploy_service_frontend` | Frontend service name in Compose | `frontend` |
| `deploy_service_backend_port` | Backend internal container port | `8000` |
| `deploy_service_frontend_port` | Frontend internal container port | `80` |

## Practical Recommendations

The defaults work for most users, but some settings should be adjusted to match the system.

### `compression_level`

- `1` — fastest, with less compression;
- `6` — a good balance between speed and image size;
- `9` — maximum compression, but noticeably slower.

For everyday development, `6` is a sensible compromise. Use `7-9` for large images when disk space matters, or `3-5` when build speed matters most.

### `max_wsl_cache_size`

This is the total WSL cache size limit in bytes. The default `10485760` (10 MB) is small for active development. If the project changes frequently and you see many repeated reads, increase the limit to `50MB`, `100MB`, or more, while ensuring that the cache does not grow without bounds.

Good guidelines:

- small projects: `10MB`-`50MB`;
- large projects with many images or containers: `100MB` or more;
- if WSL is under heavy load, reduce the limit and clean old entries more aggressively.

### `buildkit_cache_ttl`

This setting removes old BuildKit cache entries based on their age in hours.

- `0` — disable age-based cleanup;
- `24` — a typical value for regular cleanup;
- higher values suit stable environments where the cache is reused frequently.

### `container_operation_concurrency`

For a small project, `4` is a good value. On powerful systems, you can increase it to `6-8`, but this increases WSL/containerd load and may cause CPU and I/O spikes.

### `economy_mode`

This flag is useful when:

- many tabs are open;
- the project is large or WSL is slow;
- containers are used heavily and background refresh is expensive.

When `economy_mode` is enabled, inactive tabs pause timers and background requests, making the UI quieter and reducing system activity.

## Reserved / Legacy Fields

`config.json` may contain fields kept only for backward compatibility. They are not used by the current UI and normally should not be edited manually:

- `cache_container`
- `cache_image`
- `cache_volume`
- `cache_stats`
- `cache_container_status`
- `cache_split_image`
- `cache_human_size`
- `max_cache_entries`
- `retry_initial_delay`
- `retry_max_delay`
- `retry_multiplier`
- `retry_max_attempts`

These values are generally internal cache and retry parameters. In the current user configuration, treat them as reserved or legacy fields. They are not used by the UI and can be left unchanged; the active settings are described in the main configuration section above.

## Change Settings

All settings can be changed from the Settings tab:

- set the project path;
- choose the WSL distribution;
- change the gRPC port and namespace;
- configure the proxy and domain settings;
- set the Let's Encrypt email;
- configure services, internal ports, and limits;
- enable or disable resource-saving mode.

## After Editing `config.json` Manually

Restart the application after editing the file manually. It rereads `config.json` on startup and applies the new values.

## Cache and Automatic Invalidation

WSL and containerd caches are updated automatically. When the state of containers, images, volumes, or networks changes, related cached results are invalidated and reread during the next UI refresh.

This prevents stale data during container builds, starts, stops, and cleanup. Invalidation is event-driven as well as TTL-based: when containers, images, volumes, or networks are created, removed, or changed, `CacheManager` marks the cache stale and triggers a fresh read on the next refresh.

### WSL Caching and Metrics (`CacheManager`)

`CacheManager` is the global cache manager. It stores:

- `CacheEvent` invalidation events and their types (`CacheEventContainers`, `CacheEventImages`, `CacheEventVolumes`, `CacheEventStats`);
- subscribers that react to events and update the UI state;
- performance metrics: `hits`, `misses`, `errors`, and the resulting `hitRate`.

Events are published after state changes, after which `Invalidate()` calls the matching cache cleanup methods, such as `CDInvalidateContainersCache()` and `CDInvalidateImagesCache()`. This centralizes cache management and reduces stale data after bulk operations and environment cleanup.

The application also collects cache metrics: hits, misses, and usage statistics. These metrics help diagnose performance and show when caching speeds up the UI versus when frequent cleanup and rereading reduce its benefits.

## WSL Command Caching

The application caches WSL command results to reduce repeated calls to the Linux environment. It uses:

- `wsl_cache_ttl` — cache lifetime in seconds;
- `max_wsl_cache_size` — maximum cache size in bytes;
- `wsl_cache_cleanup_at` — entry-count threshold for cleaning old records.

### Caching, Resource Saving, and Progress

The main mechanisms are described in [concepts.md](concepts.md). This section lists only the settings users can configure and see in the UI:

- `wsl_cache_ttl`, `max_wsl_cache_size`, `wsl_cache_cleanup_at` — WSL cache settings;
- `economy_mode` and `auto_refresh_interval` — resource-saving behavior;
- `buildkit_cache_ttl`, `buildkit_max_size` — BuildKit cleanup;
- `container_operation_concurrency` — concurrent container operation limit;
- `max_parallelism` — concurrent build limit.

Detailed behavior and usage scenarios are described in [concepts.md](concepts.md) and [quickstart.md](quickstart.md).

## Important Rules

- `project_path` must point to the project root, not to the Compose file;
- Traefik requires ports `80` and `443` to be available;
- `deploy_service_backend_port` and `deploy_service_frontend_port` must match the containers' actual internal ports;
- `deployment_proxy` must be `traefik` or `cloudflare`;
- `deploy_email` should be a real, working email address;
- the project must contain an external network named by `deploy_network`; see [project-requirements.md](project-requirements.md) for its requirements.

## When Fields Are Empty

When values are not set, the application uses these defaults:

- `wsl_distro = Ubuntu-24.04`
- `deployment_proxy = traefik`
- `deploy_network = soul-dialogue` (for older configurations; explicitly setting the project network name is recommended)
- `deploy_service_backend = backend`
- `deploy_service_frontend = frontend`
- `deploy_service_backend_port = 8000`
- `deploy_service_frontend_port = 80`

## Checking Changes

After saving the settings, the application reads the updated `config.json` on the next startup or state refresh.

To make a quick manual change, open the file next to the binary and edit the JSON.

For deployment details, see [Domain Deployment](deployment.md).
