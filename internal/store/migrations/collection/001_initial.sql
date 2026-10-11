CREATE TABLE IF NOT EXISTS inventory (
    id INTEGER PRIMARY KEY DEFAULT 1,
    data BLOB NOT NULL,
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now(),
    CHECK (id = 1)
);

CREATE TABLE IF NOT EXISTS groups (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now(),
    name VARCHAR NOT NULL UNIQUE,
    filter VARCHAR NOT NULL,
    description VARCHAR,
    inventory_data BLOB
);

CREATE TABLE IF NOT EXISTS group_matches (
    group_id UUID PRIMARY KEY,
    vm_ids VARCHAR[]
);

ALTER TABLE IF EXISTS vinfo ADD COLUMN IF NOT EXISTS "migration_excluded" BOOLEAN DEFAULT FALSE;
ALTER TABLE IF EXISTS vinfo ADD COLUMN IF NOT EXISTS "labels" VARCHAR DEFAULT '[]';
