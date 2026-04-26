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
export MAX_VMS=5                 # max VMs to query (default: 5)
export LOOKBACK=720h             # lookback window (default: 720h = 30 days)
export INTERVAL_ID=7200          # vSphere interval ID (default: 7200 = month)

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
| `--max-vms` | `MAX_VMS` | `5` | Max VMs |
| `--lookback` | `LOOKBACK` | `720h` | Lookback (Go duration) |
| `--interval-id` | `INTERVAL_ID` | `7200` | vSphere interval ID |

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
