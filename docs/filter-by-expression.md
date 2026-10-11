# VM filter expressions

Use `byExpression` on `GET /api/v2/virtualmachines` or `GET /api/v2/collections/{id}/virtualmachines`. Group filters use the same VM fields. The parser comes from `github.com/kubev2v/assisted-migration-agent/pkg/filter` and accepts comparisons such as `=`, `!=`, `<`, `>`, `<=`, and `>=`, logical `and`/`or`, `like`, and array membership. This page lists the fields supported by this agent's local mapper.

```bash
curl -G 'http://localhost:8000/api/v2/virtualmachines' --data-urlencode "byExpression=cluster = 'production' and memory >= 4096"
```

The fields below are mapped by `internal/filter/mappers.go`. Values come from RVTools workbook tables and local metadata.

| Source | Fields |
| --- | --- |
| VM identity and location | `id`, `name`, `folder_id`, `folder`, `host`, `smbios_uuid`, `vm_uuid`, `datacenter`, `cluster`, `cluster_id`, `resource_pool` |
| VM state and OS | `powerstate` (alias `status`), `connection_state`, `ft_state`, `firmware`, `os_config`, `os_tools`, `dns_name`, `ip_address`, `hw_version` |
| VM capacity | `cpus`, `memory`, `storage_used`, `total_disk_capacity`, `provisioned`, `issues_count` |
| VM flags and local metadata | `template`, `cbt`, `enable_uuid`, `migratable`, `migration_excluded`, `labels`, `groups` |
| Disks | `disk.path`, `disk.sharing`, `disk.shared_bus`, `disk.mode`, `disk.controller`, `disk.label`, `disk.key`, `disk.capacity`, `disk.raw`, `disk.thin` |
| Inventory concerns | `concern.label`, `concern.category`, `concern.assessment` |
| CPU and memory | `cpu.sockets`, `cpu.cores_per_socket`, `cpu.hot_add`, `cpu.hot_remove`, `mem.ballooned`, `mem.hot_add` |
| Network adapters | `net.network`, `net.mac`, `net.nic_label`, `net.adapter`, `net.switch`, `net.type`, `net.ipv4`, `net.ipv6`, `net.cluster`, `net.connected`, `net.starts_connected` |
| Datastores | `datastore.name`, `datastore.address`, `datastore.object_id`, `datastore.mha`, `datastore.type`, `datastore.hosts`, `datastore.free`, `datastore.capacity` |

Group list filtering uses `name`, `description`, and `filter` against saved groups. The canonical field types and exact SQL mappings are in [mappers.go](../internal/filter/mappers.go).
