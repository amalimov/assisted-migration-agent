-- Create index on vinfo.collection_id for efficient collection-scoped VM queries.
-- Must be a separate migration from 025 because DuckDB does not allow CREATE INDEX
-- in the same transaction as outstanding UPDATE statements on the same table.
CREATE INDEX IF NOT EXISTS idx_vinfo_collection_id ON vinfo (collection_id);
