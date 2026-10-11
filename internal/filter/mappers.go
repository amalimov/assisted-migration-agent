// Package filter maps RVTools workbook fields to the generic filter DSL.
//
// This package contains MapFunc implementations that map VM-related filter field names
// to their corresponding SQL column references in the assisted-migration-agent database schema.
package filter

import (
	"fmt"
	"strings"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/pkg/filter"
)

// DefaultMapper maps VM-related filter field names to SQL column references in the
// flat filter subquery (see internal/store VMStore). This is the primary mapper for
// VM filtering operations.
//
// Flat names reference vinfo columns; dotted names reference joined tables:
//
// vinfo (v) — flat names:
//
//	id, name, folder_id, folder, host, smbios_uuid, vm_uuid, firmware,
//	powerstate (alias: status), connection_state, ft_state, cpus, memory,
//	os_config, os_tools, dns_name, ip_address, storage_used, template,
//	cbt, enable_uuid, datacenter, cluster, cluster_id, hw_version, total_disk_capacity,
//	provisioned, resource_pool, labels, groups
//
// vdisk (dk) — disk.* prefix:
//
//	disk.key, disk.path, disk.capacity, disk.sharing, disk.raw,
//	disk.shared_bus, disk.mode, disk.thin, disk.controller, disk.label
//
// concerns (c) — concern.* prefix:
//
//	concern.label, concern.category, concern.assessment
//
// vcpu (cpu) — cpu.* prefix:
//
//	cpu.hot_add, cpu.hot_remove, cpu.sockets, cpu.cores_per_socket
//
// vmemory (mem) — mem.* prefix:
//
//	mem.hot_add, mem.ballooned
//
// vnetwork (net) — net.* prefix:
//
//	net.network, net.mac, net.nic_label, net.adapter, net.switch,
//	net.connected, net.starts_connected, net.type, net.ipv4, net.ipv6,
//	net.cluster
//
// vdatastore (ds) — datastore.* prefix:
//
//	datastore.name, datastore.hosts, datastore.address, datastore.object_id,
//	datastore.free, datastore.mha, datastore.capacity, datastore.type
var DefaultMapper filter.MapFunc = func(name string) (string, filter.FieldType, error) {
	switch strings.ToLower(name) {
	// vinfo (v) — string fields
	case "id":
		return `v."VM ID"`, filter.StringField, nil
	case "name":
		return `v."VM"`, filter.StringField, nil
	case "folder_id":
		return `v."Folder ID"`, filter.StringField, nil
	case "folder":
		return `v."Folder"`, filter.StringField, nil
	case "host":
		return `v."Host"`, filter.StringField, nil
	case "smbios_uuid":
		return `v."SMBIOS UUID"`, filter.StringField, nil
	case "vm_uuid":
		return `v."VM UUID"`, filter.StringField, nil
	case "firmware":
		return `v."Firmware"`, filter.StringField, nil
	case "powerstate", "status":
		return `v."Powerstate"`, filter.StringField, nil
	case "connection_state":
		return `v."Connection state"`, filter.StringField, nil
	case "ft_state":
		return `v."FT State"`, filter.StringField, nil
	case "os_config":
		return `v."OS according to the configuration file"`, filter.StringField, nil
	case "os_tools":
		return `v."OS according to the VMware Tools"`, filter.StringField, nil
	case "dns_name":
		return `v."DNS Name"`, filter.StringField, nil
	case "ip_address":
		return `v."Primary IP Address"`, filter.StringField, nil
	case "hw_version":
		return `v."HW version"`, filter.StringField, nil
	case "resource_pool":
		return `v."Resource pool"`, filter.StringField, nil
	case "datacenter":
		return `v."Datacenter"`, filter.StringField, nil
	case "cluster":
		return `v."Cluster"`, filter.StringField, nil
	// Inventory cluster key: vSphere Object ID, or the generated cluster-{hash}.
	// Joined in vmFilterSubquery; this is not the cluster name in v."Cluster".
	case "cluster_id":
		return `vc."Object ID"`, filter.StringField, nil

	// vinfo (v) — numeric fields
	case "cpus":
		return `v."CPUs"`, filter.NumericField, nil
	case "memory":
		return `v."Memory"`, filter.NumericField, nil
	case "storage_used":
		return `v."In Use MiB"`, filter.NumericField, nil
	case "total_disk_capacity":
		return `d.total_disk`, filter.NumericField, nil
	case "provisioned":
		return `v."Provisioned MiB"`, filter.NumericField, nil
	case "issues_count":
		return `cc."issues_count"`, filter.NumericField, nil

	// vinfo (v) — boolean fields
	case "template":
		return `v."Template"`, filter.BooleanField, nil
	case "cbt":
		return `v."CBT"`, filter.BooleanField, nil
	case "enable_uuid":
		return `v."EnableUUID"`, filter.BooleanField, nil
	case "migratable":
		return `(COALESCE(crit.critical_count, 0) = 0)`, filter.BooleanField, nil
	case "migration_excluded":
		return `v."migration_excluded"`, filter.BooleanField, nil

	// vinfo (v) — array fields
	case "labels":
		return `v."labels"`, filter.ArrayField, nil
	case "groups":
		return `g.groups`, filter.ArrayField, nil

	// vdisk (dk) — disk.* prefix
	case "disk.path":
		return `dk."Disk Path"`, filter.StringField, nil
	case "disk.sharing":
		return `dk."Sharing mode"`, filter.StringField, nil
	case "disk.shared_bus":
		return `dk."Shared Bus"`, filter.StringField, nil
	case "disk.mode":
		return `dk."Disk Mode"`, filter.StringField, nil
	case "disk.controller":
		return `dk."Controller"`, filter.StringField, nil
	case "disk.label":
		return `dk."Label"`, filter.StringField, nil
	case "disk.key":
		return `dk."Disk Key"`, filter.NumericField, nil
	case "disk.capacity":
		return `dk."Capacity MiB"`, filter.NumericField, nil
	case "disk.raw":
		return `dk."Raw"`, filter.BooleanField, nil
	case "disk.thin":
		return `dk."Thin"`, filter.BooleanField, nil

	// concerns (c) — concern.* prefix
	case "concern.label":
		return `c."Label"`, filter.StringField, nil
	case "concern.category":
		return `c."Category"`, filter.StringField, nil
	case "concern.assessment":
		return `c."Assessment"`, filter.StringField, nil

	// vcpu (cpu) — cpu.* prefix
	case "cpu.sockets":
		return `cpu."Sockets"`, filter.NumericField, nil
	case "cpu.cores_per_socket":
		return `cpu."Cores p/s"`, filter.NumericField, nil
	case "cpu.hot_add":
		return `cpu."Hot Add"`, filter.BooleanField, nil
	case "cpu.hot_remove":
		return `cpu."Hot Remove"`, filter.BooleanField, nil

	// vmemory (mem) — mem.* prefix
	case "mem.ballooned":
		return `mem."Ballooned"`, filter.NumericField, nil
	case "mem.hot_add":
		return `mem."Hot Add"`, filter.BooleanField, nil

	// vnetwork (net) — net.* prefix
	case "net.network":
		return `net."Network"`, filter.StringField, nil
	case "net.mac":
		return `net."Mac Address"`, filter.StringField, nil
	case "net.nic_label":
		return `net."NIC label"`, filter.StringField, nil
	case "net.adapter":
		return `net."Adapter"`, filter.StringField, nil
	case "net.switch":
		return `net."Switch"`, filter.StringField, nil
	case "net.type":
		return `net."Type"`, filter.StringField, nil
	case "net.ipv4":
		return `net."IPv4 Address"`, filter.StringField, nil
	case "net.ipv6":
		return `net."IPv6 Address"`, filter.StringField, nil
	case "net.cluster":
		return `net."Cluster"`, filter.StringField, nil
	case "net.connected":
		return `net."Connected"`, filter.BooleanField, nil
	case "net.starts_connected":
		return `net."Starts Connected"`, filter.BooleanField, nil

	// vdatastore (ds) — datastore.* prefix
	case "datastore.name":
		return `ds."Name"`, filter.StringField, nil
	case "datastore.address":
		return `ds."Address"`, filter.StringField, nil
	case "datastore.object_id":
		return `ds."Object ID"`, filter.StringField, nil
	case "datastore.mha":
		return `ds."MHA"`, filter.StringField, nil
	case "datastore.type":
		return `ds."Type"`, filter.StringField, nil
	case "datastore.hosts":
		return `ds."Hosts"`, filter.NumericField, nil
	case "datastore.free":
		return `ds."Free MiB"`, filter.NumericField, nil
	case "datastore.capacity":
		return `ds."Capacity MiB"`, filter.NumericField, nil

	default:
		return "", 0, fmt.Errorf("unknown filter field: %s", name)
	}
}

// GroupMapper maps group-related filter field names to SQL column references
// in the groups table.
//
// Supported fields:
//   - name: group name
//   - description: group description
//   - filter: group filter expression
var GroupMapper filter.MapFunc = func(name string) (string, filter.FieldType, error) {
	switch strings.ToLower(name) {
	case "name":
		return "name", filter.StringField, nil
	case "description":
		return "description", filter.StringField, nil
	case "filter":
		return "filter", filter.StringField, nil
	default:
		return "", 0, fmt.Errorf("unknown group filter field: %s", name)
	}
}

// ParseWithDefaultMap parses a filter expression using the DefaultMapper (VM fields).
// This is a convenience wrapper around filter.Parse() for VM filtering operations.
func ParseWithDefaultMap(src []byte) (sq.Sqlizer, error) {
	return filter.Parse(src, DefaultMapper)
}

// ParseWithGroupMap parses a filter expression using the GroupMapper (group fields).
// This is a convenience wrapper around filter.Parse() for group filtering operations.
func ParseWithGroupMap(src []byte) (sq.Sqlizer, error) {
	return filter.Parse(src, GroupMapper)
}
