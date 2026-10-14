# Assisted Migration Agent and RVTools Analyzer

This repository builds two products. The Assisted Migration Agent collects vSphere inventory and reports to the Migration Planner console. The standalone RVTools Analyzer reads RVTools exports locally and serves its own API and web UI.

| Product | Command | HTTP API | Container build |
| --- | --- | --- | --- |
| Agent | `cmd/agent` | `/api/v2` | `make image` |
| RVTools Analyzer | `cmd/rvtools-analyzer` | `/api/v1` | `make image.analyzer` |

Each command has its own flags and startup path. The Agent's temporary `--rvtools-mode` interface has been removed.

## Build

```bash
make build
# Or build both products explicitly:
make build.agent build.analyzer
```

`make build` and `make build.agent` produce `bin/agent`; `make build.analyzer` produces `bin/rvtools-analyzer`.
Set `PLANNER_AGENT_VERSION` or `ANALYZER_VERSION` when building a specific product; `PLANNER_AGENT_GIT_COMMIT` and `ANALYZER_GIT_COMMIT` control their reported commits.

## RVTools Analyzer

Prepare OPA validation policies, then start the Analyzer with a persistent data folder:

```bash
make setup-opa-policies
bin/rvtools-analyzer run \
  --opa-policies-folder ./policies \
  --data-folder ./analyzer-data
```

The web UI is served at `http://localhost:8000/` in development mode. Upload one or more RVTools `.xlsx` files there or send them to `POST /api/v1/collector/rvtools`. Use `GET /api/v1/collector` to check collection progress, `GET /api/v1/collections` to list analyses, and `GET /api/v1/virtualmachines` to query the latest analysis. `GET /api/v1/status` reports Analyzer readiness. The full Analyzer contract is in [`api/analyzer/v1/openapi.yaml`](api/analyzer/v1/openapi.yaml).

The Analyzer uses its own data folder and does not require Agent or source IDs, console credentials, or a separately hosted UI. `--server-http-port` changes the listening port. `--server-mode prod` enables HTTPS with a self-signed certificate. Native runs may need network access once to obtain DuckDB's SQLite scanner extension; the Analyzer container includes that extension.

Build the container locally with `make image.analyzer`. Its embedded UI, OPA policies, and DuckDB extension are packaged in the image. Mount `/var/lib/analyzer` to persist uploads and collections. The Agent container remains available through `make image`.

## Agent run

### Basic Usage

```bash
bin/agent run --agent-id <uuid> --source-id <uuid> --opa-policies-folder <path>
```

Required flags:
- `--agent-id` and `--source-id` must be valid UUIDs
- `--opa-policies-folder` must point to a directory containing OPA policy files for VM validation

### Examples

Run in disconnected mode (default):

```bash
bin/agent run \
  --agent-id 550e8400-e29b-41d4-a716-446655440000 \
  --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8 \
  --opa-policies-folder /path/to/policies
```

Run in connected mode with authentication:

```bash
bin/agent run \
  --agent-id 550e8400-e29b-41d4-a716-446655440000 \
  --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8 \
  --opa-policies-folder /path/to/policies \
  --mode connected \
  --authentication-enabled \
  --authentication-jwt-filepath /path/to/jwt
```

Run in production mode with persistent storage:

```bash
bin/agent run \
  --agent-id 550e8400-e29b-41d4-a716-446655440000 \
  --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8 \
  --opa-policies-folder /var/lib/agent/policies \
  --server-mode prod \
  --server-statics-folder /var/www/statics \
  --data-folder /var/lib/agent
```

## Command Line Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--agent-id` | *required* | Unique identifier (UUID) for this agent |
| `--source-id` | *required* | Source identifier (UUID) for this agent |
| `--mode` | `disconnected` | `connected` \| `disconnected` |
| `--data-folder` | — | Path to persistent data folder (uses in-memory if not set) |
| `--opa-policies-folder` | *required* | Path to OPA policies folder for VM validation |
| `--version` | `v0.0.0` | Agent version to report to console |
| `--legacy-status-enabled` | `true` | Use legacy status like waiting-for-credentials |
| `--server-http-port` | `8000` | HTTP server port |
| `--server-mode` | `dev` | `dev` \| `prod` (prod enables HTTPS with self-signed certs) |
| `--server-statics-folder` | — | Path to static files (required when `--server-mode=prod`) |
| `--console-url` | `http://localhost:7443` | Migration planner console URL |
| `--console-update-interval` | `5s` | Status update interval |
| `--authentication-enabled` | `true` | Enable console authentication |
| `--authentication-jwt-filepath` | — | Path to JWT file (required when `--authentication-enabled`) |
| `--log-format` | `console` | `console` \| `json` |
| `--log-level` | `debug` | `debug` \| `info` \| `warn` \| `error` |

## Development

### Local Setup

Before running the agent locally, set up the required dependencies:

```bash
# Download OPA policies (required for VM validation)
make setup-opa-policies

# Build the Alpine filler image (required for forecaster benchmarks)
# Prerequisites: qemu-img, curl, guestfish, genisoimage
make build-filler-image
```

When running locally, point the agent to the filler image assets:

```bash
AGENT_ASSETS_DIR=pkg/vmware/assets make run
```

> Both of these are handled automatically during container builds (see Containerfile).

### Tests

Run tests:

```bash
make test
```

Run linter:

```bash
make lint
```

Format code:

```bash
make format
```

Run all validations:

```bash
make validate-all
```

## E2E Tests

E2E tests require a built agent container image. Before running tests, build the latest image:

```bash
make image
```

### Make Targets

| Target | Description |
|--------|-------------|
| `make e2e` | Run e2e tests (default: container mode) |
| `make e2e.container` | Run e2e tests in container mode using Podman |
| `make e2e.vm` | Run e2e tests in VM mode (infrastructure managed externally) |
| `make e2e.container.clean` | Remove all e2e test containers and volumes |

### E2E Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-infra-mode` | `container` | Infrastructure mode: `container` (Podman) or `vm` (externally managed) |
| `-agent-image` | — | Agent container image to test |
| `-backend-image` | — | Backend container image (migration-planner-api) |
| `-backend-agent-endpoint` | `http://localhost:7443` | Agent endpoint on backend (port 7443) |
| `-backend-user-endpoint` | `http://localhost:3443` | User endpoint on backend (port 3443) |
| `-agent-proxy-url` | `http://localhost:8080` | Agent proxy URL |
| `-agent-api-url` | `https://localhost:8000` | Agent local API URL |
| `-podman-socket` | `unix:///run/user/1000/podman/podman.sock` | Podman socket path |
| `-iso-path` | — | Path to directory containing `rhcos-live-iso.x86_64.iso` |

Containers are automatically kept running when tests fail, allowing you to inspect logs and debug issues. Use `make e2e.container.clean` to remove them after debugging.

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `E2E_AGENT_IMAGE` | `quay.io/kubev2v/assisted-migration-agent:latest` | Agent image to use |
| `E2E_BACKEND_IMAGE` | `quay.io/kubev2v/migration-planner-api:latest` | Backend image to use |
| `E2E_ISO_PATH` | Current directory | Path containing the RHCOS ISO |
| `E2E_INFRA_MODE` | `container` | Infrastructure mode |

### Examples

Run e2e tests with a custom agent image:

```bash
make image
E2E_AGENT_IMAGE=quay.io/myrepo/agent:dev make e2e
```

Clean up after failed tests:

```bash
make e2e.container.clean
```

## Testing Against Stage

To test the agent against the staging backend, you need to generate a JWT token and run the agent with authentication enabled.

### Generate JWT Token

Use the migration-planner CLI to generate a JWT:

```bash
../migration-planner/bin/planner sso token \
  --org 12345678 \
  --private-key ./privatekey \
  --username your-username \
  --source-id <source-uuid> \
  --kid <key-id> > ./jwt
```

### Run Agent with Stage Backend

The `--agent-id` can be any valid UUID. Generate one with `uuidgen`.

```bash
bin/agent run \
  --legacy-status-enabled \
  --agent-id $(uuidgen) \
  --source-id <source-uuid> \
  --opa-policies-folder ./policies \
  --data-folder . \
  --authentication-enabled \
  --authentication-jwt-filepath ./jwt \
  --console-url https://console.stage.redhat.com/api/migration-assessment/
```
