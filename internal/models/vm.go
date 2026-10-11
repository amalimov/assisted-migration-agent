package models

// VirtualMachineSummary represents a lightweight VM record for list views.
type VirtualMachineSummary struct {
	ID                string
	Name              string
	VCenterID         string
	PowerState        string
	Cluster           string
	Datacenter        string
	Memory            int32 // MB
	DiskSize          int64 // MB (stored as MiB in DB, treated as MB)
	IssueCount        int
	IsMigratable      bool
	IsTemplate        bool
	Groups            []string
	MigrationExcluded bool
	Labels            []string
	Tags              []string
}

type VM struct {
	ID              string
	Name            string
	VCenterID       string
	UUID            string
	Firmware        string
	PowerState      string
	ConnectionState string
	Host            string
	Folder          string
	Datacenter      string
	Cluster         string

	CpuCount       int32
	CoresPerSocket int32
	CpuAffinity    []int32
	MemoryMB       int32

	GuestName string
	GuestID   string
	HostName  string
	IPAddress string

	DiskSize    int64 // total disk size in MB (for list view)
	StorageUsed int64

	IsTemplate            bool
	IsMigratable          bool
	MigrationExcluded     bool
	FaultToleranceEnabled bool
	NestedHVEnabled       bool

	ToolsStatus        string
	ToolsRunningStatus string

	Disks         []Disk
	NICs          []NIC
	Devices       []Device
	GuestNetworks []GuestNetwork

	Issues []Issue

	Labels []string
	Groups []string
}

type Issue struct {
	ID          string
	Label       string
	Description string
	Category    string
}

type Disk struct {
	Key      int32
	File     string
	Capacity int64
	Shared   bool
	RDM      bool
	Bus      string
	Mode     string
}

type NIC struct {
	MAC         string
	Network     string
	Index       int
	IPv4Address string
	IPv6Address string
}

type Device struct {
	Kind string
}

type GuestNetwork struct {
	Device       string
	MAC          string
	IP           string
	PrefixLength int32
	Network      string
}

// VMFilterOptions holds the distinct values available for filtering VMs.
type VMFilterOptions struct {
	Clusters          []string
	Datacenters       []string
	ConcernLabels     []string
	ConcernCategories []string
}
