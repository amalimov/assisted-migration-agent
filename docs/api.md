# Local API (v2)

Base URL: `http://localhost:8000/api/v2` in both server modes. The `prod` mode also serves the bundled UI over HTTP. The complete request and response schema is in [openapi.yaml](../api/v2/openapi.yaml).

| Operation | Purpose |
| --- | --- |
| `POST /collector/rvtools` | Upload one or more `.xlsx` files as multipart `files` parts; starts an asynchronous collection. |
| `GET /collector` | Read `ready`, `collecting`, `parsing`, `collected`, or `error` status. |
| `DELETE /collector` | Cancel a running import. |
| `GET /collections` | List completed collections and their IDs. |
| `DELETE /collections` | Delete all collections. |
| `GET /collections/compare/{aId}/{bId}` | Compare VM and cluster totals across collections. |
| `GET /collections/compare/{aId}/{bId}/{dimension}` | Page through VM IDs only in one side for `total`, `migratable`, or `non-migratable`. |
| `GET /inventory`, `GET /collections/{id}/inventory` | Read latest or selected inventory. |
| `GET /virtualmachines`, `GET /collections/{id}/virtualmachines` | List VMs with optional `byExpression`, sorting, and paging. |
| `GET /virtualmachines/{vmId}`, `GET /collections/{id}/virtualmachines/{vmId}` | Read VM details. |
| `PATCH /virtualmachines/{vmId}` | Update labels or migration exclusion in the latest collection. |
| `POST /virtualmachines/batch-update-exclusion` | Change exclusion for several VMs. |
| `GET /virtualmachines/filter-options`, `GET /virtualmachines/labels` | Read filter values and labels. |
| `PATCH /virtualmachines/labels/{label}`, `DELETE /virtualmachines/labels/{label}` | Manage labels. |
| `GET /groups`, `POST /groups`, `GET/PATCH/DELETE /groups/{groupId}` | Manage groups in the latest collection. |
| `GET /collections/{id}/groups`, `GET /collections/{id}/groups/{groupId}` | Read groups in a selected collection. |
| `GET /collections/{id}/export` | Export workbook-backed scopes as ZIP of CSVs or XLSX. Scopes: `overview`, `hosts`, `clusters`, `datastores`, `vms`, `network`, `groups`. |
| `GET /agent`, `GET /version` | Read local source status and build version. |

For upload, use `curl -F 'files=@your-export.xlsx' http://localhost:8000/api/v2/collector/rvtools`. Each separate upload is a separate collection, allowing comparison. One multipart request containing several files creates one collection.

Comparison matches VMs by the workbook's `VM ID`. Use exports from the same vCenter for meaningful VM-level diffs; IDs from separate vCenters can overlap.
