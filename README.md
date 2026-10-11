# RVTools Migration Planner Agent

This agent imports RVTools `.xlsx` workbooks into local DuckDB collections. It serves inventory, VM and group queries, labels, migration exclusion, comparison, and CSV/XLSX exports through a local API. No vCenter or Console connection is needed.

## Build and start

```bash
make build
make setup-opa-policies
mkdir -p data
bin/agent run --data-folder "$PWD/data" --opa-policies-folder "$PWD/policies"
```

The default server uses HTTP on port 8000. Add `--server-mode prod --server-statics-folder /path/to/ui` to serve the UI over HTTPS with a self-signed certificate. `make image` builds an image with the UI, policies, and DuckDB Excel extension. `make run.container` starts it with a persistent volume.

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

## Development

`make test` runs the Go tests. `make e2e` runs the RVTools upload and comparison test. Set `DUCKDB_EXCEL_EXTENSION` to the path of a DuckDB v1.4.3 Linux AMD64 Excel extension when the extension is not already present in `local-data/`; the test copies it into a temporary data directory. See [API documentation](docs/api.md) and [filter expressions](docs/filter-by-expression.md).
