package rightsizing

import "time"

// Config holds all runtime parameters for the spike.
type Config struct {
	VCenterURL string
	Username   string
	Password   string
	Insecure   bool
	NameFilter string
	ClusterID  string // MoRef value of a ClusterComputeResource, e.g. "domain-c123"; empty = all clusters
	MaxVMs     int
	Lookback   time.Duration
	IntervalID int // vSphere historical interval in seconds (300=day, 1800=week, 7200=month, 86400=year)
}
