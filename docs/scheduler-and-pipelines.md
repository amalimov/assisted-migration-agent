# Scheduler and RVTools pipeline

`pkg/scheduler` executes work units, and `pkg/work` sequences them in a pipeline. `internal/services/collector.go` exposes the running pipeline's status and cancellation.

An RVTools import in `internal/services/rvtools_work.go` provisions a private collection database, ingests each uploaded workbook, builds the inventory, and marks the result complete. The finalizer adds a successful collection to the pool and removes its in-progress marker. On failure or cancellation it closes and removes the incomplete database. Uploaded temporary files are removed after processing.

The main database tracks in-progress collection markers. Completed collection databases remain on disk and are discovered at startup. Separate upload requests create separate collections, so comparison can read each database independently.
