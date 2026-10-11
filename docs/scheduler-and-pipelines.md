# Scheduler and RVTools pipeline

The upstream `github.com/kubev2v/assisted-migration-agent/pkg/scheduler` package executes work units, and its `pkg/work` package sequences them in a pipeline. Local `internal/services/collector.go` exposes the running pipeline's status and cancellation.

An RVTools import in `internal/services/rvtools_work.go` provisions a private collection database, ingests each uploaded workbook, builds the inventory, and marks the result complete. The finalizer adds a successful collection to the pool and removes its in-progress marker. On failure or cancellation it closes and removes the incomplete database. Uploaded temporary files are removed after processing.

The main database tracks in-progress collection markers. Completed collection databases remain on disk and are discovered at startup. Separate upload requests create separate collections, so comparison can read each database independently.
