# RVTools Migration Planner Agent

This agent imports RVTools `.xlsx` workbooks into local DuckDB collections. It serves inventory, VM and group queries, labels, migration exclusion, comparison, and CSV/XLSX exports through a local API. No vCenter or Console connection is needed.

## Build and start

```bash
make build
make setup-opa-policies
mkdir -p data
bin/agent run --data-folder "$PWD/data" --opa-policies-folder "$PWD/policies"
```

The server listens on `127.0.0.1:8000` over HTTP by default. Add `--server-mode prod --server-statics-folder /path/to/ui` to serve the UI on the same address. `make image` builds an image with the UI, policies, and DuckDB Excel extension. `make run.container` starts it with a persistent volume and publishes port 8000 on the host loopback address. Use `--server-listen-address` to choose a different IP address; the container listens on `0.0.0.0` internally.

The DuckDB Excel extension must be installed or cached in the data directory for workbook imports. The container seeds the matching extension at startup, so imports work without runtime network access.

## Import a workbook

From the repository root, with the example export present:

```bash
curl -sS -X POST -F 'files=@RVTools_export_all_2026-08-31_15.58.31.xlsx' http://localhost:8000/api/v2/collector/rvtools
```

Each request creates one collection. Several `files` parts in one request are combined into one collection. Poll for `"status":"collected"`, then list VMs:

```bash
curl -sS http://localhost:8000/api/v2/collector
curl -sS 'http://localhost:8000/api/v2/virtualmachines?page=1&pageSize=20'
```

List collection IDs with `GET /api/v2/collections`. After two imports, compare them with `GET /api/v2/collections/compare/{aId}/{bId}` and `GET /api/v2/collections/compare/{aId}/{bId}/total`. Completed collections are loaded from the data directory after restart.

For a comparison fixture without RVTools workbooks, run the upstream fixture command with [sample-scenario.yaml](testdata/create-test-collections/sample-scenario.yaml) into an empty data folder, then start this agent with that folder. `go run` uses the upstream revision pinned in `go.mod`. The command writes two collections that can be listed and compared. Its first run may download DuckDB's `sqlite_scanner` extension:

```bash
go run github.com/kubev2v/assisted-migration-agent/tools/create-test-collections --data-folder="$PWD/sample-data" testdata/create-test-collections/sample-scenario.yaml
```

## Development

`make test` runs the Go tests. `make e2e` runs the RVTools upload and comparison test. Set `DUCKDB_EXCEL_EXTENSION` to the path of a DuckDB v1.4.3 Linux AMD64 Excel extension when the extension is not already present in `local-data/`; the test copies it into a temporary data directory. See [API documentation](docs/api.md) and [filter expressions](docs/filter-by-expression.md).
