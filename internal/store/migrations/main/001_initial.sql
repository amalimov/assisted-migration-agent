-- Collection markers describe imports that have not finished publishing.
CREATE TABLE IF NOT EXISTS collections (
    "database" VARCHAR PRIMARY KEY,
    state VARCHAR NOT NULL CHECK (state IN ('running', 'failed', 'pending_delete')),
    error VARCHAR
);
