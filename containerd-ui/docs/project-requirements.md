# Project Requirements

For builds and deployments to work correctly, the project must meet a few basic requirements. The application does not hard-code service names, networks, or ports: enter the actual values in the settings and in `config.json`.

## 1. Compose File

The project root must contain a `compose.yaml` or `docker-compose.yml` file.

```text
project/
├── compose.yaml
├── backend/
├── frontend/
└── ...
```

The project root is the directory containing the Compose file, not an individual service directory.

## 2. External Network for the Proxy and Services

The application uses the external network name from the `deploy_network` setting in `config.json`.
For compatibility, the default is `soul-dialogue`, but you can change it for another project from the
Settings tab or directly in the configuration:

```json
{
  "deploy_network": "my-project-network"
}
```

The network must be declared as external in Compose. A generic example:

```yaml
networks:
  my-project-network:
    external: true
    name: my-project-network
```

Services must be attached to this network:

```yaml
services:
  backend:
    networks:
      - my-project-network

  frontend:
    networks:
      - my-project-network
```

Replace `my-project-network` with the value of `deploy_network` and list the services that need access to it.
If the network is missing or declared as a regular rather than an external network, deployment and routing will not work correctly.

## 3. Backend and Frontend

For deployment, the project must define the services named by `deploy_service_backend` and `deploy_service_frontend`, along with the internal ports the application should use.

Typical values:

- backend service name: `backend` (example)
- frontend service name: `frontend` (example)
- backend port: `8000` (example)
- frontend port: `80` (example)

These values are compared with the settings in `config.json`.

## 4. DNS and Domain

For domain publishing, DNS must point to the target server, and the domain must be configured correctly for the selected proxy mode.

## 5. When to Use This Document

Use this guide when you need to:

- checking the project before a build;
- preparing a Compose file for deployment;
- confirming that the network selected in `deploy_network` is declared correctly;
- checking service names and their internal ports.

See also:

- [quickstart.md](quickstart.md)
- [deployment.md](deployment.md)
- [diagnostics.md](diagnostics.md)
