package store

import sq "github.com/Masterminds/squirrel"

// vmGetQuery fetches a single VM by ID with full details (disks, NICs, concerns)
// from the RVTools workbook.
// filtered_vm is resolved first to scope all subsequent CTEs to one VM,
// preventing full-table aggregations.
const vmGetQuery = `
WITH filtered_vm AS (
    SELECT * FROM vinfo WHERE "VM ID" = ?
),
disks AS (
    SELECT
        dk."VM ID",
        LIST({
            'Key': dk."Disk Key",
            'UnitNumber': dk."Unit #",
            'File': COALESCE(dk."Path", dk."Disk Path"),
            'Capacity': COALESCE(dk."Capacity MiB", 0),
            'Shared': dk."Sharing mode",
            'RDM': dk."Raw",
            'Bus': dk."Shared Bus",
            'Mode': dk."Disk Mode",
            'Serial': dk."Disk UUID",
            'Thin': dk."Thin",
            'Controller': dk."Controller",
            'Label': dk."Label",
            'SCSIUnit': dk."SCSI Unit #",
            'Datastore': COALESCE(ds."Object ID", '')
        }) AS disks
    FROM vdisk dk
    JOIN filtered_vm fv ON dk."VM ID" = fv."VM ID"
    LEFT JOIN vdatastore ds ON ds."Name" = regexp_extract(COALESCE(dk."Path", dk."Disk Path"), '\[([^\]]+)\]', 1)
    GROUP BY dk."VM ID"
),
nics AS (
    SELECT
        vnetwork."VM ID",
        LIST({
            'Network': "Network",
            'MAC': "Mac Address",
            'Label': "NIC label",
            'Adapter': "Adapter",
            'Switch': "Switch",
            'Connected': "Connected",
            'StartsConnected': "Starts Connected",
            'Type': "Type",
            'IPv4Address': CASE WHEN "IPv4 Address" = 'VM' OR "IPv4 Address" IS NULL THEN '' ELSE "IPv4 Address" END,
            'IPv6Address': CASE WHEN "IPv6 Address" = 'VM' OR "IPv6 Address" IS NULL THEN '' ELSE "IPv6 Address" END
        } ORDER BY "Mac Address" ASC) AS nics
    FROM vnetwork
    JOIN filtered_vm fv ON vnetwork."VM ID" = fv."VM ID"
    GROUP BY vnetwork."VM ID"
),
concerns_agg AS (
    SELECT
        "VM_ID",
        LIST({
            'Id': "Concern_ID",
            'Label': "Label",
            'Category': "Category",
            'Assessment': "Assessment"
        }) AS concerns
    FROM concerns
    JOIN filtered_vm fv ON concerns."VM_ID" = fv."VM ID"
    GROUP BY "VM_ID"
),
groups_agg AS (
    SELECT
        u.vm_id,
        ARRAY_AGG(DISTINCT grp.name) AS groups
    FROM group_matches gm
    JOIN groups grp ON gm.group_id = grp.id
    , UNNEST(gm.vm_ids) AS u(vm_id)
    JOIN filtered_vm fv ON u.vm_id = fv."VM ID"
    GROUP BY u.vm_id
)
SELECT
    COALESCE(i."VM ID", '') AS "ID",
    COALESCE(i."VM", '') AS "Name",
    COALESCE(i."Folder ID", i."Folder", '') AS "Folder",
    COALESCE(i."Host", '') AS "Host",
    COALESCE(i."SMBIOS UUID", i."VM UUID", '') AS "UUID",
    COALESCE(i."Firmware", '') AS "Firmware",
    COALESCE(i."Powerstate", '') AS "PowerState",
    COALESCE(i."Connection state", '') AS "ConnectionState",
    COALESCE(i."FT State" IN ('enabled', 'running', 'starting', 'needSecondary', 'primary', 'secondary'), false) AS "FaultToleranceEnabled",
    COALESCE(i."CPUs", 0) AS "CpuCount",
    COALESCE(i."Memory", 0) AS "MemoryMB",
    COALESCE(i."OS according to the configuration file", '') AS "GuestName",
    COALESCE(i."OS according to the VMware Tools", '') AS "GuestNameFromVmwareTools",
    COALESCE(i."DNS Name", '') AS "HostName",
    COALESCE(i."Primary IP Address", '') AS "IpAddress",
    CAST(COALESCE(i."In Use MiB", 0) AS BIGINT) * 1024 * 1024 AS "StorageUsed",
    COALESCE(i."Template", false) AS "IsTemplate",
    COALESCE(i."CBT", false) AS "ChangeTrackingEnabled",
    COALESCE(i."EnableUUID", false) AS "DiskEnableUuid",
    COALESCE(i."Datacenter", '') AS "Datacenter",
    COALESCE(i."Cluster", '') AS "Cluster",
    COALESCE(i."HW version", '') AS "HWVersion",
    COALESCE(i."Total disk capacity MiB", 0) AS "TotalDiskCapacityMiB",
    COALESCE(i."Provisioned MiB", 0) AS "ProvisionedMiB",
    COALESCE(i."Resource pool", '') AS "ResourcePool",
    COALESCE(i."OsDiskComplexity", 0) AS "OsDiskComplexity",
    COALESCE(i."migration_excluded", false) AS "MigrationExcluded",
    COALESCE(CAST(i."labels" AS VARCHAR[]), [])::VARCHAR[] AS "Labels",
    COALESCE(g.groups, [])::VARCHAR[] AS "Groups",
    COALESCE(c."Hot Add", false) AS "CpuHotAddEnabled",
    COALESCE(c."Hot Remove", false) AS "CpuHotRemoveEnabled",
    COALESCE(c."Sockets", 0) AS "CpuSockets",
    COALESCE(c."Cores p/s", 0) AS "CoresPerSocket",
    COALESCE(m."Hot Add", false) AS "MemoryHotAddEnabled",
    COALESCE(m."Ballooned", 0) AS "BalloonedMemory",
    COALESCE(d.disks, []) AS "Disks",
    COALESCE(n.nics, []) AS "NICs",
    LIST_FILTER(
        LIST_VALUE(i."Network #1", i."Network #2", i."Network #3", i."Network #4",
                   i."Network #5", i."Network #6", i."Network #7", i."Network #8"),
        x -> x IS NOT NULL AND x != '' AND x != 'VM'
    ) AS "Networks",
    COALESCE(con.concerns, []) AS "Concerns"
FROM filtered_vm i
LEFT JOIN vcpu c ON i."VM ID" = c."VM ID"
LEFT JOIN vmemory m ON i."VM ID" = m."VM ID"
LEFT JOIN disks d ON i."VM ID" = d."VM ID"
LEFT JOIN nics n ON i."VM ID" = n."VM ID"
LEFT JOIN concerns_agg con ON i."VM ID" = con."VM_ID"
LEFT JOIN groups_agg g ON i."VM ID" = g.vm_id


`

// vmFilterOptionsQuery returns distinct clusters, datacenters, concern labels,
// and concern categories in a single row.
const vmFilterOptionsQuery = `
SELECT
    (SELECT COALESCE(list(DISTINCT "Cluster" ORDER BY "Cluster"), [])
     FROM vinfo WHERE "Cluster" IS NOT NULL AND "Cluster" != '') AS clusters,
    (SELECT COALESCE(list(DISTINCT "Datacenter" ORDER BY "Datacenter"), [])
     FROM vinfo WHERE "Datacenter" IS NOT NULL AND "Datacenter" != '') AS datacenters,
    (SELECT COALESCE(list(DISTINCT "Label" ORDER BY "Label"), [])
     FROM concerns WHERE "Label" IS NOT NULL AND "Label" != '') AS concern_labels,
    (SELECT COALESCE(list(DISTINCT "Category" ORDER BY "Category"), [])
     FROM concerns WHERE "Category" IS NOT NULL AND "Category" != '') AS concern_categories
`

// vmOutputQuery is the base aggregated output query that produces one row per VM.
// Filters should be applied via Where clauses on the VM ID.
var vmOutputQuery = sq.Select(
	`v."VM ID" AS id`,
	`v."VM" AS name`,
	`v."Powerstate" AS power_state`,
	`COALESCE(v."Cluster", '') AS cluster`,
	`COALESCE(v."Datacenter", '') AS datacenter`,
	`v."Memory" AS memory`,
	`COALESCE(d.total_disk, 0) AS disk_size`,
	`COALESCE(c.issues_count, 0) AS issue_count`,
	`COALESCE(v."Template", false) AS template`,
	`COALESCE(crit.critical_count, 0) = 0 AS migratable`,
	`COALESCE(g.groups, [])::VARCHAR[] AS groups`,
	`v."migration_excluded" AS migration_excluded`,
	`COALESCE(CAST(v."labels" AS VARCHAR[]), [])::VARCHAR[] AS labels`,
).From("vinfo v").
	LeftJoin(`(SELECT "VM_ID", COUNT(*) AS issues_count FROM concerns GROUP BY "VM_ID") c ON v."VM ID" = c."VM_ID"`).
	LeftJoin(`(SELECT "VM_ID", COUNT(*) AS critical_count FROM concerns WHERE "Category" = 'Critical' GROUP BY "VM_ID") crit ON v."VM ID" = crit."VM_ID"`).
	LeftJoin(`(SELECT "VM ID", SUM("Capacity MiB") AS total_disk FROM vdisk GROUP BY "VM ID") d ON v."VM ID" = d."VM ID"`).
	LeftJoin(`(
		SELECT u.vm_id, ARRAY_AGG(DISTINCT grp.name) AS groups
		FROM group_matches gm
		JOIN groups grp ON gm.group_id = grp.id
		, UNNEST(gm.vm_ids) AS u(vm_id)
		GROUP BY u.vm_id
	) g ON v."VM ID" = g.vm_id`)

// vmFilterSubquery is the base flat JOIN query for filtering.
// It joins all tables so WHERE clauses can reference any raw column.
// Filters should be applied via Where clauses, then use the result to get DISTINCT VM IDs.
var vmFilterSubquery = sq.Select(`DISTINCT v."VM ID"`).
	From("vinfo v").
	LeftJoin(`vcluster vc ON vc."Name" = v."Cluster"`).
	LeftJoin(`vdisk dk ON v."VM ID" = dk."VM ID"`).
	LeftJoin(`concerns c ON v."VM ID" = c."VM_ID"`).
	LeftJoin(`vcpu cpu ON v."VM ID" = cpu."VM ID"`).
	LeftJoin(`vmemory mem ON v."VM ID" = mem."VM ID"`).
	LeftJoin(`vnetwork net ON v."VM ID" = net."VM ID"`).
	LeftJoin(`(SELECT "VM_ID", COUNT(*) AS issues_count FROM concerns GROUP BY "VM_ID") cc ON v."VM ID" = cc."VM_ID"`).
	LeftJoin(`(SELECT "VM_ID", COUNT(*) AS critical_count FROM concerns WHERE "Category" = 'Critical' GROUP BY "VM_ID") crit ON v."VM ID" = crit."VM_ID"`).
	LeftJoin(`(SELECT "VM ID", SUM("Capacity MiB") AS total_disk FROM vdisk GROUP BY "VM ID") d ON v."VM ID" = d."VM ID"`).
	LeftJoin(`vdatastore ds ON ds."Name" = regexp_extract(COALESCE(dk."Path", dk."Disk Path"), '\[([^\]]+)\]', 1)`).
	LeftJoin(`(
        SELECT u.vm_id, ARRAY_AGG(DISTINCT grp.name) AS groups
        FROM group_matches gm JOIN groups grp ON gm.group_id = grp.id,
        UNNEST(gm.vm_ids) AS u(vm_id)
        GROUP BY u.vm_id
    ) g ON v."VM ID" = g.vm_id`)
