# Domain Deployment

## Pre-Deployment Checks

Before deployment starts, the application runs a set of checks automatically. You can also run them manually from the UI for diagnosis.

### Check Tools

Checks that the required components are available:

- `nerdctl`;
- `containerd`;
- `buildctl`;
- `cloudflared` — if Cloudflare Tunnel is selected.

### Check Ports 80/443

Checks whether ports `80` and `443` are available and whether WSL or Windows has a conflict. The check covers both the Linux environment and a host-level TCP dial. This matters because services such as IIS may listen on Windows and block Traefik even when WSL shows no conflict.

### Check DNS

Checks that the domain resolves correctly and points to the target server. The result is cached for 30 seconds to avoid repeating the same request during deployment retries.

### General Behavior

These checks run automatically before deployment, but you can also run them manually to diagnose problems before the proxy starts. If a requirement is not met, the application shows a warning and blocks deployment until the environment is fixed.

The complete set of environment checks and commands is available in [diagnostics.md](diagnostics.md).

## Supported Options

The application supports two deployment methods:

- Traefik + Let's Encrypt
- Cloudflare Tunnel

The container access architecture and WSL fallback are described in [concepts.md](concepts.md). For deployment, the important point is that gRPC is the primary path and WSL is the reliable fallback. Deployment settings and paths are stored in `config.json` next to `containerd-ui.exe`.

## 1. Prepare the Project

The project must contain `compose.yaml` or `docker-compose.yml` before deployment.

The complete project requirements are in [project-requirements.md](project-requirements.md), including `deploy_network`, service connections, and the project root. The list below covers only the conditions critical for deployment.

### Project Requirements

- `compose.yaml` or `docker-compose.yml` must exist;
- the network from `deploy_network` must be declared with `external: true` and the matching `name`;
- the services from `deploy_service_backend` and `deploy_service_frontend` must be attached to this network;
- service internal ports must be correct;
- the domain DNS must point to the server;
- the project must be run from the root containing the Compose file.

## 2. Choose a Proxy

The Deployment tab lets you choose one of these modes:

### Traefik + Let's Encrypt

Suitable for standard domain publishing.

Requirements:

- ports 80 and 443 are available;
- a valid Let's Encrypt email is configured;
- the domain DNS points to the server.

### Cloudflare Tunnel

Suitable when you do not want to expose ports 80/443 directly on the server.

Requirements:

- `cloudflared` is installed;
- a valid Cloudflare JSON token is available;
- the domain is already configured in Cloudflare.

## 3. Configure Routes

Specify:

- the domain, for example `example.com`;
- the backend prefix, for example `/api`;
- which services to publish: backend, frontend, or both.

### Routing Notes

- Traefik removes the backend prefix automatically with the `stripPrefix` middleware;
- Cloudflare does not remove the prefix automatically, so the backend must handle prefixed paths correctly.

With Cloudflare Tunnel, `stripPrefix` does not work in the same way as it does in Traefik. If the backend expects a URL without a prefix but receives `/api/...`, routing will be incorrect and some HTTP routes will stop working. The backend must therefore accept prefixed requests such as `/api` and remove the prefix before handling the final route. Otherwise the route may appear missing or return the wrong content even though the service is running.

> Important: with Cloudflare Tunnel, do not rely on `stripPrefix` as an automatic path cleanup step. If the prefix must be removed, the backend must do it; otherwise dynamic routes, Swagger, static files, and API endpoints may behave incorrectly.

## 4. Pre-Deployment Diagnostics

Before deployment starts, the application runs pre-deployment diagnostics. They are available through dedicated buttons and are used both before deployment and during manual environment checks.

For the complete set of commands and checks, see [diagnostics.md](diagnostics.md). It covers WSL, ports, DNS, tools, and the configured network.

### Check Tools Button

Checks that all required components are available:

- `nerdctl`;
- `containerd`;
- `buildctl`;
- `cloudflared` — if Cloudflare Tunnel is selected.

If a component is missing, the application shows a clear message and blocks deployment until the environment is fixed.

### Check Ports 80/443 Button

Checks whether ports `80` and `443` are in use. This is critical for Traefik, which needs the standard HTTP/HTTPS ports. Deployment is blocked until an occupied port is freed. The check also dials `localhost` to detect Windows services such as IIS that may not be visible from WSL.

### Check DNS Button

Checks that the domain resolves correctly and points to the target server. The result is cached for 30 seconds to avoid repeated DNS requests during deployment retries, making quick redeployments and diagnostics more predictable.

### Pre-Deployment Check Flow

Before deployment, the application checks:

- domain DNS; the result is cached for 30 seconds;
- whether ports `80/443` are available for Traefik;
- required tools: `nerdctl`, `containerd`, and `buildctl`;
- `cloudflared` when Cloudflare is selected;
- the `deploy_network` declaration in the Compose file;
- that the services exist and use the configured internal ports.

### Project External Network

The network from `deploy_network` must exist and be declared as external in the Compose file. The full example and connection rules are in [project-requirements.md](project-requirements.md). If the network is missing, the application will try to create it, but the Compose file must still meet the requirements or deployment may fail.

## 5. Cloudflare Tunnel

To use Cloudflare, obtain a JSON token in the Cloudflare Dashboard:

- Zero Trust → Networks → Tunnels
- select or create a tunnel;
- download the credentials file or obtain a JSON token;
- save it in the application with **Save Token**.

After saving, the application stores `.containerd-data/cloudflare/credentials.json` and runs:

```bash
cloudflared tunnel list --credentials-file /path/to/credentials.json
```

If the command fails, the token or credentials file is invalid and deployment should not be started. An invalid token blocks deployment so the proxy is not launched with bad credentials.

The `cloudflared` installation check and basic client status are also described in [installation.md](installation.md). This section focuses on configuring and validating the Cloudflare connection before deployment.

## 6. After Deployment

Deployment proceeds in this order:

1. the proxy (`Traefik` or `cloudflared`) starts;
2. its startup and network initialization are verified;
3. the project services start;
4. the application checks their status with `nerdctl compose ps`;
5. if services fail to start or become unavailable, the application rolls back automatically.

Before building the project, the application can also manage `buildkitd` automatically. It starts an inactive daemon and may stop it afterward if this session started it. This removes the need to start `buildkitd` manually before every build.

Additionally:

- Traefik creates and stores certificates in `.containerd-data/traefik/acme.json`;
- Cloudflare runs the tunnel in a separate container;
- the logs show the operation result;
- on failure, the application can roll back and stop the active proxy.

## 7. Rollback

If service startup fails, the application:

- stops the proxy container (Traefik or cloudflared);
- removes the proxy container;
- restores the original system state.

Rollback cleans up only the proxy part of the deployment and does not remove the project. The project's core infrastructure is preserved; only the external proxy layer is reverted.

## 8. Proxy Logs

The UI includes a Proxy Logs button that shows the last 200 lines from the proxy container. This helps diagnose Traefik or Cloudflare Tunnel issues without opening the container manually. Logs include initialization, TLS/HTTP errors, routing problems, and tunnel status.

## 9. Deployment Files

The project root contains a `.containerd-data/` directory with subdirectories for common deployment artifacts:

```text
.containerd-data/
├── traefik/
│   ├── acme.json
│   ├── dynamic.yml
│   └── compose.yaml
├── cloudflare/
│   ├── credentials.json
│   └── compose.yaml
└── ...
```

This keeps proxy configuration, Compose files, and proxy data separate from the main project and makes rollback straightforward.

## 10. Practical Tips

- use a real email address for Let's Encrypt;
- verify the backend/frontend names in Compose;
- verify the services' internal ports;
- always free ports 80 and 443 for Traefik;
- use only a valid JSON token for Cloudflare;
- always verify that the network specified by `deploy_network` exists before deployment.

For troubleshooting details, see [Troubleshooting](troubleshooting.md).
