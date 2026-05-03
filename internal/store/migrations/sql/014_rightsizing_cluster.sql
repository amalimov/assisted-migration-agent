ALTER TABLE rightsizing_vm_utilization ADD COLUMN IF NOT EXISTS cluster_id            VARCHAR;
ALTER TABLE rightsizing_vm_utilization ADD COLUMN IF NOT EXISTS provisioned_cpus      INTEGER;
ALTER TABLE rightsizing_vm_utilization ADD COLUMN IF NOT EXISTS provisioned_memory_mb INTEGER;
ALTER TABLE rightsizing_vm_utilization ADD COLUMN IF NOT EXISTS provisioned_disk_kb   DOUBLE;
