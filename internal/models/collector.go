package models

type CollectorStateType string

func (c CollectorStateType) IsRunning() bool {
	return c == CollectorStateCollecting || c == CollectorStateParsing
}

const (
	CollectorStateReady      CollectorStateType = "ready"
	CollectorStateCollecting CollectorStateType = "collecting"
	CollectorStateParsing    CollectorStateType = "parsing"
	CollectorStateCollected  CollectorStateType = "collected"
	CollectorStateError      CollectorStateType = "error"
)

type CollectorStatus struct {
	ID    string
	State CollectorStateType
	Error error
}
type CollectorResult struct {
	Err       error
	Completed bool
}
