package store

// Export COPY queries build migration-planning CSV views, not raw table dumps.
// They join workbook inventory with concerns and local groups and
// compute migration_status (Ready/Review/Blocked/Excluded) aligned with the VM API.

type exportScopeSpec struct {
	filename string
	query    string
}

// Shared SQL for overview and vms scopes: total disk per VM from vdisk.
const vmDiskTotalsJoin = `
			LEFT JOIN (
				SELECT "VM ID", SUM("Capacity MiB") AS total_disk_mib
				FROM vdisk
				GROUP BY "VM ID"
			) d ON v."VM ID" = d."VM ID"
`

// vmMigrationStatusExpr classifies each VM for migration planning (same rules as the VM list API).
const vmMigrationStatusExpr = `
				CASE
					WHEN v."migration_excluded" = TRUE THEN 'Excluded'
					WHEN NOT COALESCE(c_crit.critical_count > 0, FALSE) AND COALESCE(c.issues_count, 0) = 0 THEN 'Ready'
					WHEN NOT COALESCE(c_crit.critical_count > 0, FALSE) AND COALESCE(c.issues_count, 0) > 0 THEN 'Review'
					ELSE 'Blocked'
				END AS migration_status`

// Assessment fields use only persisted workbook data and local metadata.
const vmAssessmentSelectCols = `
` + vmMigrationStatusExpr + `,
                COALESCE(c_issues.critical_issues, '') AS critical_issues,
                COALESCE(c.issues_count, 0) AS issue_count,
                COALESCE(grps.groups, '') AS groups,
                COALESCE(array_to_string(v."labels", '; '), '') AS labels`

const vmAssessmentJoins = `
            LEFT JOIN (SELECT "VM_ID", COUNT(*) AS issues_count FROM concerns GROUP BY "VM_ID") c
                ON v."VM ID" = c."VM_ID"
            LEFT JOIN (SELECT "VM_ID", COUNT(*) AS critical_count FROM concerns
                WHERE "Category" = 'Critical' GROUP BY "VM_ID") c_crit
                ON v."VM ID" = c_crit."VM_ID"
            LEFT JOIN (SELECT "VM_ID", string_agg("Label", '; ') AS critical_issues FROM concerns
                WHERE "Category" IN ('Critical', 'Warning') GROUP BY "VM_ID") c_issues
                ON v."VM ID" = c_issues."VM_ID"
            LEFT JOIN (
                SELECT g.vm_id, string_agg(DISTINCT grp.name, '; ') AS groups
                FROM group_matches gm JOIN groups grp ON gm.group_id = grp.id,
                UNNEST(gm.vm_ids) AS g(vm_id)
                GROUP BY g.vm_id
            ) grps ON v."VM ID" = grps.vm_id
`

// overviewQuery — scope "overview": one row per VM with core inventory and migration readiness summary.
// Joins vinfo, disk totals, and vmAssessment* fragments. Default export scope.
var overviewQuery = `

		COPY (
			SELECT
				v."VM" AS name,
				v."VM ID" AS id,
				v."Datacenter" AS datacenter,
				v."Cluster" AS cluster,
				v."Host" AS host,
				v."CPUs" AS cpu_count,
				v."Memory" AS memory_mib,
				ROUND(v."Memory" / 1024.0, 2) AS memory_gib,
				COALESCE(d.total_disk_mib, 0) AS disk_mib,
				ROUND(COALESCE(d.total_disk_mib, 0) / 1024.0, 2) AS disk_gib,
				v."OS according to the VMware Tools" AS guest_os,

				-- Migration status (computed: Ready/Review/Blocked/Excluded)
` + vmAssessmentSelectCols + `

			FROM vinfo v
` + vmDiskTotalsJoin + `
` + vmAssessmentJoins + `

			ORDER BY v."VM"
		) TO ? (FORMAT CSV, HEADER TRUE)
	
`

// vmsQuery — scope "vms": full VM detail export including hardware, storage, networks, and assessment columns.
// Same assessment joins as overview; adds wide vinfo columns and up to eight network fields.
var vmsQuery = `

		COPY (
			SELECT
				v."VM" AS name,
				v."VM ID" AS id,
				v."Folder" AS folder,
				v."Template" AS is_template,
				v."Datacenter" AS datacenter,
				v."Cluster" AS cluster,
				v."Host" AS host,
				v."Resource pool" AS resource_pool,

				-- Hardware
				v."CPUs" AS cpu_count,
				v."Memory" AS memory_mib,
				ROUND(v."Memory" / 1024.0, 2) AS memory_gib,
				v."HW version" AS hardware_version,
				v."Firmware" AS firmware,

				-- Storage
				COALESCE(d.total_disk_mib, 0) AS disk_capacity_mib,
				ROUND(COALESCE(d.total_disk_mib, 0) / 1024.0, 2) AS disk_capacity_gib,
				v."In Use MiB" AS disk_used_mib,
				ROUND(v."In Use MiB" / 1024.0, 2) AS disk_used_gib,
				v."Provisioned MiB" AS disk_provisioned_mib,
				ROUND(v."Provisioned MiB" / 1024.0, 2) AS disk_provisioned_gib,
				v."OsDiskComplexity" AS os_disk_complexity,

				-- State
				v."Powerstate" AS power_state,
				v."Connection state" AS connection_state,
				v."FT State" AS fault_tolerance_state,

				-- OS and Guest
				v."OS according to the configuration file" AS configured_os,
				v."OS according to the VMware Tools" AS guest_os,
				v."DNS Name" AS dns_name,
				v."Primary IP Address" AS primary_ip,

				-- Features
				v."CBT" AS change_block_tracking,
				v."EnableUUID" AS uuid_enabled,

				-- Identifiers
				v."SMBIOS UUID" AS smbios_uuid,
				v."VM UUID" AS vm_uuid,
				v."VI SDK UUID" AS vi_sdk_uuid,

				-- Networks
				COALESCE(v."Network #1", '') AS network_1,
				COALESCE(v."Network #2", '') AS network_2,
				COALESCE(v."Network #3", '') AS network_3,
				COALESCE(v."Network #4", '') AS network_4,
				COALESCE(v."Network #5", '') AS network_5,
				COALESCE(v."Network #6", '') AS network_6,
				COALESCE(v."Network #7", '') AS network_7,
				COALESCE(v."Network #8", '') AS network_8,

				-- Migration Assessment
				v."migration_excluded" AS migration_excluded,
` + vmAssessmentSelectCols + `

			FROM vinfo v
` + vmDiskTotalsJoin + `
` + vmAssessmentJoins + `

			ORDER BY v."VM"
		) TO ? (FORMAT CSV, HEADER TRUE)
	
`

// hostsQuery — scope "hosts": ESXi host inventory with aggregated VM count per host.
const hostsQuery = `

		COPY (
			SELECT
				COALESCE(h."Host", '') AS name,
				COALESCE(h."Object ID", '') AS id,
				COALESCE(h."Datacenter", '') AS datacenter,
				COALESCE(h."Cluster", '') AS cluster,
				h."# CPU" AS cpu_sockets,
				h."# Cores" AS cpu_cores,
				h."# Memory" AS memory_mib,
				ROUND(h."# Memory" / 1024.0, 2) AS memory_gib,
				COALESCE(h."Vendor", '') AS vendor,
				COALESCE(h."Model", '') AS model,
				COALESCE(h."Config status", '') AS config_status,
				COALESCE(vm_counts.vm_count, 0) AS vm_count

			FROM vhost h

			-- VM count per host
			LEFT JOIN (
				SELECT "Host", COUNT(*) AS vm_count
				FROM vinfo
				GROUP BY "Host"
			) vm_counts ON h."Host" = vm_counts."Host"

			ORDER BY h."Cluster", h."Host"
		) TO ? (FORMAT CSV, HEADER TRUE)
	
`

// clustersQuery — scope "clusters": cluster settings plus host and VM counts per cluster.
const clustersQuery = `

		COPY (
			SELECT
				COALESCE(c."Name", '') AS name,
				COALESCE(c."Object ID", '') AS id,
				COALESCE(c."DrsEnabled", false) AS drs_enabled,
				COALESCE(c."DrsDefaultVmBehavior", 'None') AS drs_default_vm_behavior,
				COALESCE(host_counts.host_count, 0) AS host_count,
				COALESCE(vm_counts.vm_count, 0) AS vm_count

			FROM vcluster c

			-- Host count per cluster
			LEFT JOIN (
				SELECT "Cluster", COUNT(*) AS host_count
				FROM vhost
				GROUP BY "Cluster"
			) host_counts ON c."Name" = host_counts."Cluster"

			-- VM count per cluster
			LEFT JOIN (
				SELECT "Cluster", COUNT(*) AS vm_count
				FROM vinfo
				GROUP BY "Cluster"
			) vm_counts ON c."Name" = vm_counts."Cluster"

			ORDER BY c."Name"
		) TO ? (FORMAT CSV, HEADER TRUE)
	
`

// datastoresQuery — scope "datastores": capacity, free space, used_percent, and VM count per datastore.
// VM count is derived by parsing datastore names from vdisk paths.
const datastoresQuery = `

		COPY (
			SELECT
				COALESCE(ds."Name", '') AS name,
				COALESCE(ds."Object ID", '') AS id,
				COALESCE(ds."Address", '') AS address,
				COALESCE(ds."Type", '') AS type,
				ds."Capacity MiB" AS capacity_mib,
				ROUND(ds."Capacity MiB" / 1024.0, 2) AS capacity_gib,
				ds."Free MiB" AS free_mib,
				ROUND(ds."Free MiB" / 1024.0, 2) AS free_gib,
				CASE
					WHEN ds."Capacity MiB" > 0
					THEN ROUND((ds."Capacity MiB" - ds."Free MiB") / ds."Capacity MiB" * 100, 2)
					ELSE 0
				END AS used_percent,
				COALESCE(ds."MHA", false) AS mha_enabled,
				COALESCE(ds."Hosts", '') AS hosts,
				COALESCE(vm_counts.vm_count, 0) AS vm_count

			FROM vdatastore ds

			-- VM count per datastore (extract datastore name from disk path)
			LEFT JOIN (
				SELECT
					regexp_extract(COALESCE("Path", "Disk Path"), '\[([^\]]+)\]', 1) AS datastore_name,
					COUNT(DISTINCT "VM ID") AS vm_count
				FROM vdisk
				GROUP BY datastore_name
			) vm_counts ON ds."Name" = vm_counts.datastore_name

			ORDER BY ds."Name"
		) TO ? (FORMAT CSV, HEADER TRUE)
	
`

// networkQuery — scope "network": one row per NIC from vnetwork with stable nic_index ordering per VM.
const networkQuery = `

		COPY (
			SELECT
				COALESCE(v."VM", '') AS vm_name,
				COALESCE(n."VM ID", '') AS vm_id,
				COALESCE(n."Cluster", '') AS cluster,
				ROW_NUMBER() OVER (
					PARTITION BY n."VM ID"
					ORDER BY
						-- Extract numeric suffix for proper ordering (e.g., "Network adapter 2" < "Network adapter 10")
						TRY_CAST(regexp_extract(n."NIC label", '\d+$', 0) AS INTEGER),
						n."NIC label"
				) - 1 AS nic_index,
				COALESCE(n."Network", '') AS network,
				COALESCE(n."Switch", '') AS switch,
				COALESCE(n."Type", '') AS type,
				COALESCE(n."NIC label", '') AS nic_label,
				COALESCE(n."Adapter", '') AS adapter,
				COALESCE(n."Mac Address", '') AS mac_address,
				COALESCE(n."IPv4 Address", '') AS ipv4_address,
				COALESCE(n."IPv6 Address", '') AS ipv6_address,
				COALESCE(n."Connected", false) AS connected,
				COALESCE(n."Starts Connected", false) AS starts_connected

			FROM vnetwork n

			-- Join with vinfo to get VM name
			LEFT JOIN vinfo v ON n."VM ID" = v."VM ID"

			ORDER BY n."Cluster", v."VM", nic_index
		) TO ? (FORMAT CSV, HEADER TRUE)
	
`

// groupsQuery — scope "groups": user-defined VM groups with metadata and matched VM count.
const groupsQuery = `

		COPY (
			SELECT
				COALESCE(g.id::VARCHAR, '') AS group_id,
				COALESCE(g.name, '') AS group_name,
				COALESCE(g.description, '') AS description,
				COALESCE(g.created_at::VARCHAR, '') AS created_at,
				COALESCE(g.updated_at::VARCHAR, '') AS updated_at,
				COALESCE(array_length(gm.vm_ids, 1), 0) AS vm_count

			FROM groups g

			LEFT JOIN group_matches gm ON g.id = gm.group_id

			ORDER BY g.name
		) TO ? (FORMAT CSV, HEADER TRUE)
	
`

var exportScopes = map[string]exportScopeSpec{
	"overview":   {filename: "overview.csv", query: overviewQuery},
	"hosts":      {filename: "hosts.csv", query: hostsQuery},
	"clusters":   {filename: "clusters.csv", query: clustersQuery},
	"datastores": {filename: "datastores.csv", query: datastoresQuery},
	"vms":        {filename: "vms.csv", query: vmsQuery},
	"network":    {filename: "networks.csv", query: networkQuery},
	"groups":     {filename: "groups.csv", query: groupsQuery},
}
