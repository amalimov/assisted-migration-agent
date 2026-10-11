# RVTools integration test

Run `make e2e`. The test generates workbooks, uploads them through the v2 router, polls the collector, lists VMs and inventory, compares two completed collections, and checks a VM export. It also verifies that two files in one upload create one collection and an invalid workbook reports an error without publishing a collection. Databases and workbooks are created in a temporary directory.

The parser needs the DuckDB Excel extension. Set `DUCKDB_EXCEL_EXTENSION` to a version-matched `excel.duckdb_extension` file. The CI workflow downloads and verifies DuckDB v1.4.3 Linux AMD64 before the test. When the file is unavailable locally, the integration test skips without trying to download at runtime.

`make test` also runs the local store, service, handler, scheduler, and filter suites. No vCenter simulator, SaaS backend, PostgreSQL, VM image, or VDDK artifact is involved.
