# Contributing

The agent has one inventory input: RVTools Excel workbooks. The parser is provided by `github.com/kubev2v/migration-planner/pkg/duckdb_parser`. A multipart request to `POST /api/v2/collector/rvtools` creates an isolated DuckDB collection; the asynchronous pipeline publishes it to the pool only after ingestion and inventory building finish.

## Local workflow

1. Run `make setup-opa-policies` once, then `make run`.
2. Run `make test` and `make e2e` before opening a change. Set `DUCKDB_EXCEL_EXTENSION` if the Excel extension is not already available locally.
3. Run `make generate` after changing `api/v2/openapi.yaml` or `internal/config/config.go`, then `make check-format`.

Generated API files are `api/v2/spec.gen.go` and `api/v2/types.gen.go`. Configuration helpers are generated in `internal/config/zz_generated.configuration.go`. Edit their sources and regenerate them.

Collection and main database migrations live in `internal/store/migrations`. Add a numbered migration for existing installations when changing a schema; changing an old migration only affects new databases. Tests use temporary databases and generated workbooks.

The generic pipeline, scheduler, and filter parser come from the pinned `github.com/kubev2v/assisted-migration-agent` module. Local `internal/filter` maps workbook fields to SQL columns; local `internal/server` and `internal/config` host the HTTP API and UI. See [scheduler and pipeline notes](docs/scheduler-and-pipelines.md).
