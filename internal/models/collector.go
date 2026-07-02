package models

var CollectorRequiredPrivileges = []string{
	"System.Read",
	"System.View",
}

// CollectorStateType represents the current state of the collector.
type CollectorStateType string

const (
	// CollectorStateReady - credentials saved, waiting for collection request
	CollectorStateReady CollectorStateType = "ready"
	// CollectorStateConnecting - verifying credentials with vCenter
	CollectorStateConnecting CollectorStateType = "connecting"
	// CollectorStateCollecting - async collection in progress
	CollectorStateCollecting CollectorStateType = "collecting"
	// CollectorStateParsing - parsing collected data into duckdb
	CollectorStateParsing CollectorStateType = "parsing"
	// CollectorStateCollected - collection complete (auto-transitions to ready)
	CollectorStateCollected CollectorStateType = "collected"
	// CollectorStateError - error during connecting or collecting
	CollectorStateError CollectorStateType = "error"

	// Rightsizing phases — reported while the post-collection metrics run is active.
	// All map to CollectorLegacyStateCollecting so the SaaS side sees a single in-progress state.
	CollectorStateRightsizingConnecting  CollectorStateType = "rightsizing-connecting"
	CollectorStateRightsizingDiscovering CollectorStateType = "rightsizing-discovering"
	CollectorStateRightsizingQuerying    CollectorStateType = "rightsizing-querying"
	CollectorStateRightsizingPersisting  CollectorStateType = "rightsizing-persisting"

	// V1 agent status
	CollectorLegacyStateWaitingForCredentials CollectorStateType = "waiting-for-credentials"
	CollectorLegacyStateCollecting            CollectorStateType = "gathering-initial-inventory"
	CollectorLegacyStateError                 CollectorStateType = "error"
	CollectorLegacyStateCollected             CollectorStateType = "up-to-date"
)

func (c CollectorStateType) ToV1() CollectorStateType {
	switch c {
	case CollectorStateReady:
		return CollectorLegacyStateWaitingForCredentials
	case CollectorStateConnecting, CollectorStateCollecting, CollectorStateParsing:
		return CollectorLegacyStateCollecting
	case CollectorStateCollected:
		return CollectorLegacyStateCollected
	case CollectorLegacyStateError:
		return CollectorLegacyStateError
	case CollectorStateRightsizingConnecting,
		CollectorStateRightsizingDiscovering,
		CollectorStateRightsizingQuerying,
		CollectorStateRightsizingPersisting:
		return CollectorLegacyStateCollecting
	default:
		return "unknown state"
	}
}

// CollectorStatus holds the current collector state and metadata.
type CollectorStatus struct {
	State CollectorStateType
	Error error
}

// PersistentVMData holds VM fields that survive across collection runs.
// Keyed by vmmoid in CollectorResult.VMPersistentData so the copy-forward
// pipeline unit can match new vinfo rows without knowing their hashed VM IDs.
type PersistentVMData struct {
	MigrationExcluded bool
	Labels            []string
}

// CollectorResult is the shared result struct threaded through collector work units.
type CollectorResult struct {
	SQLitePath       string
	Inventory        []byte
	CollectionID     int64 // ID of the collection row created for this run; set by setupCollection
	PrevCollectionID int64 // ID of the previously active collection; set by setupCollection (0 if none)
	// VMPersistentData carries per-VM fields from the previous collection so the
	// copy-forward pipeline unit can restore them after reIDVMs hashes the VM IDs.
	VMPersistentData map[string]PersistentVMData // key: vmmoid
}
