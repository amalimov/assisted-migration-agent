-- ============================================================
-- Phase 2: Collection-versioned inventory and vmmoid support
-- ============================================================

-- Step A: Create a synthetic collection for any existing inventory row.
-- Only runs when an inventory row with id=1 exists (pre-Phase-1 agent databases).
-- The sequence assigns the ID; we read it back via RETURNING in the application,
-- but for the migration we rely on it being the first ID (1) on a fresh sequence.
INSERT INTO collections (vcenter_id, state, active, started_at, finished_at, created_at, updated_at)
SELECT
    'default',
    'done',
    true,
    created_at,
    updated_at,
    created_at,
    updated_at
FROM inventory
WHERE id = 1
AND NOT EXISTS (SELECT 1 FROM collections WHERE vcenter_id = 'default' AND active = true);

-- Step B: Recreate inventory keyed by collection_id.
CREATE TABLE IF NOT EXISTS inventory_new (
    collection_id BIGINT PRIMARY KEY,
    data          BLOB NOT NULL,
    created_at    TIMESTAMP NOT NULL DEFAULT now(),
    updated_at    TIMESTAMP NOT NULL DEFAULT now()
);

-- Migrate the existing single row into the synthetic collection.
INSERT INTO inventory_new (collection_id, data, created_at, updated_at)
SELECT
    (SELECT id FROM collections WHERE vcenter_id = 'default' AND active = true LIMIT 1),
    data,
    created_at,
    updated_at
FROM inventory
WHERE id = 1;

DROP TABLE inventory;
ALTER TABLE inventory_new RENAME TO inventory;

-- Step C: group_inventory — per-collection group blobs.
CREATE TABLE IF NOT EXISTS group_inventory (
    group_id      UUID      NOT NULL,
    collection_id BIGINT    NOT NULL,
    inventory_data BLOB,
    created_at    TIMESTAMP NOT NULL DEFAULT now(),
    updated_at    TIMESTAMP NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, collection_id)
);

-- Migrate existing group inventory_data blobs to the synthetic collection.
INSERT INTO group_inventory (group_id, collection_id, inventory_data, created_at, updated_at)
SELECT
    id,
    (SELECT id FROM collections WHERE vcenter_id = 'default' AND active = true LIMIT 1),
    inventory_data,
    updated_at,
    updated_at
FROM groups
WHERE inventory_data IS NOT NULL;

-- Step D: Drop inventory_data from groups — it now lives in group_inventory.
ALTER TABLE groups DROP COLUMN IF EXISTS inventory_data;

-- Step E: Add vmmoid (stable cross-collection vSphere MOID) and collection_id to vinfo.
-- Separated from the CREATE INDEX below because DuckDB cannot create an index
-- in the same transaction as outstanding UPDATE statements on the same table.
ALTER TABLE vinfo ADD COLUMN IF NOT EXISTS vmmoid VARCHAR;
UPDATE vinfo SET vmmoid = "VM ID" WHERE vmmoid IS NULL;

ALTER TABLE vinfo ADD COLUMN IF NOT EXISTS collection_id BIGINT;
UPDATE vinfo
SET collection_id = (
    SELECT COALESCE(
        (SELECT id FROM collections WHERE vcenter_id = 'default' AND active = true LIMIT 1),
        0
    )
)
WHERE collection_id IS NULL;

-- Step F: Add collection_id to rightsizing_reports so rightsizing runs can be
-- linked to the collection they were computed from (used for cascade deletion).
ALTER TABLE rightsizing_reports ADD COLUMN IF NOT EXISTS collection_id BIGINT DEFAULT 0;
UPDATE rightsizing_reports SET collection_id = 0 WHERE collection_id IS NULL;

-- Step G: Rebuild group_matches with (group_id, collection_id) composite PK.
-- DuckDB cannot ALTER a PRIMARY KEY, so we recreate the table.
-- Migration 025 has not been applied to any environment; this edit is safe.
CREATE TABLE IF NOT EXISTS group_matches_new (
    group_id      UUID    NOT NULL,
    collection_id BIGINT  NOT NULL DEFAULT 0,
    vm_ids        VARCHAR[],
    PRIMARY KEY (group_id, collection_id)
);

-- Migrate existing rows into the synthetic-initial collection.
INSERT INTO group_matches_new (group_id, collection_id, vm_ids)
SELECT
    group_id,
    COALESCE(
        (SELECT id FROM collections WHERE vcenter_id = 'default' AND active = true LIMIT 1),
        0
    ),
    vm_ids
FROM group_matches
WHERE vm_ids IS NOT NULL;

DROP TABLE group_matches;
ALTER TABLE group_matches_new RENAME TO group_matches;
