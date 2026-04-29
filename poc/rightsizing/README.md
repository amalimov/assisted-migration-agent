# rightsizing PoC

A spike that connects to a vCenter, discovers VMs, and queries historical
performance metrics using govmomi. Outputs a JSON right-sizing summary to stdout.
Progress logs go to stderr via the agent's zap logger.

**This is a spike — do not wire additional agent behavior on top of it.**

## Running

```bash
export VCENTER_URL='https://vcenter.example.com/sdk'
export VCENTER_USERNAME='user@vsphere.local'
export VCENTER_PASSWORD='...'
export VCENTER_INSECURE=true     # skip TLS verification (default: true)
export VM_NAME_FILTER=''         # optional: filter by name substring
export LOOKBACK=720h             # lookback window (default: 720h = 30 days)
export INTERVAL_ID=7200          # vSphere interval ID (default: 7200 = month)
export BATCH_SIZE=64             # VMs per QueryPerf call (default: 64)

go run . rightsizing
```

To separate progress logs from JSON output:

```bash
go run . --log-format json rightsizing 2>/dev/null | jq .
```

## Flags

Each flag falls back to its environment variable.

| Flag | Env var | Default | Description |
|------|---------|---------|-------------|
| `--vcenter-url` | `VCENTER_URL` | — | vCenter SDK URL |
| `--username` | `VCENTER_USERNAME` | — | Username |
| `--password` | `VCENTER_PASSWORD` | — | Password |
| `--insecure` | `VCENTER_INSECURE` | `true` | Skip TLS verification (set false in production) |
| `--name-filter` | `VM_NAME_FILTER` | `""` | VM name substring filter |
| `--cluster-id` | `CLUSTER_ID` | `""` | Scope to a cluster (MoRef value, e.g. `domain-c123`); empty = all clusters |
| `--lookback` | `LOOKBACK` | `720h` | Lookback (Go duration) |
| `--interval-id` | `INTERVAL_ID` | `7200` | vSphere interval ID |
| `--batch-size` | `BATCH_SIZE` | `64` | VMs per QueryPerf round-trip |
| `--db-path` | `DB_PATH` | `""` | DuckDB file path for persisting results; empty = no persistence |

## vSphere Interval IDs

| ID | Name | Sample cadence |
|----|------|---------------|
| `300` | Day | every 5 min |
| `1800` | Week | every 30 min |
| `7200` | Month | every 2 hours |
| `86400` | Year | every day |

## Metric Units

| Metric | Unit | Notes |
|--------|------|-------|
| `cpu.usagemhz.average` | MHz | absolute MHz consumed |
| `cpu.usage.average` | hundredths of % | 5000 = 50.00 % |
| `mem.active.average` | KB | actively used by guest |
| `mem.consumed.average` | KB | guest + hypervisor overhead |
| `disk.used.latest` | KB | actual disk occupancy |
| `disk.provisioned.latest` | KB | provisioned disk size |

## Persisting Results

Pass `--db-path` (or set `DB_PATH`) to write results to a DuckDB file:

```bash
go run . rightsizing --db-path ./rightsizing.duckdb
```

The schema is applied automatically. Two tables are created:

| Table | Contents |
|-------|----------|
| `rightsizing_reports` | One row per run: vcenter, cluster, window, expected vs written batch count |
| `rightsizing_metrics` | One row per VM × metric: all six aggregated stat fields |

Query results with the DuckDB CLI or any DuckDB-compatible tool:

```sql
-- Coverage: compare actual vs expected samples per VM and metric
SELECT r.vcenter, r.cluster_id, m.vm_name, m.metric_key,
       m.sample_count, r.expected_sample_count,
       ROUND(m.sample_count * 100.0 / r.expected_sample_count, 1) AS coverage_pct
FROM rightsizing_metrics m
JOIN rightsizing_reports r ON r.id = m.report_id
ORDER BY coverage_pct ASC;
```

## Running Tests

```bash
go test ./poc/rightsizing/... -v
```

No live vCenter required.

## Example Output

```json
{
  "vcenter": "https://vcenter.example.com/sdk",
  "window_start": "2026-03-27T12:00:00Z",
  "window_end": "2026-04-26T12:00:00Z",
  "interval_id": 7200,
  "expected_sample_count": 360,
  "vms": [
    {
      "name": "app-server-01",
      "moid": "vm-123",
      "metrics": {
        "cpu.usagemhz.average": {
          "sample_count": 360,
          "average": 650.2,
          "p95": 1700.4,
          "p99": 2400.1,
          "max": 4100.7,
          "latest": 550.3
        },
        "mem.consumed.average": {
          "sample_count": 360,
          "average": 2097152,
          "p95": 3145728,
          "p99": 3670016,
          "max": 4194304,
          "latest": 2621440
        }
      },
      "warnings": []
    }
  ],
  "warnings": []
}
```
