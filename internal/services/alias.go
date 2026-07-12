// internal/services/alias.go
package services

import v1 "github.com/kubev2v/assisted-migration-agent/internal/services/v1"

// Types — true Go type aliases; methods, embedding, and type assertions all work.
type ApplicationService = v1.ApplicationService
type BenchmarkResult = v1.BenchmarkResult
type BenchmarkStrategy = v1.BenchmarkStrategy
type Collector = v1.Collector
type CollectorService = v1.CollectorService
type Console = v1.Console
type CredentialsService = v1.CredentialsService
type EventService = v1.EventService
type ExportService = v1.ExportService
type ForecasterService = v1.ForecasterService
type GroupGetParams = v1.GroupGetParams
type GroupListParams = v1.GroupListParams
type GroupService = v1.GroupService
type InspectorService = v1.InspectorService
type InventoryBuilder = v1.InventoryBuilder
type InventoryService = v1.InventoryService
type RightsizingService = v1.RightsizingService
type ServiceManager = v1.ServiceManager
type ServiceManagerOption = v1.ServiceManagerOption
type SortField = v1.SortField
type VddkService = v1.VddkService
type VMListParams = v1.VMListParams
type VMService = v1.VMService

// Constructors and option funcs — func vars, called identically to the originals.
var (
	NewApplicationService               = v1.NewApplicationService
	NewCollectorService                 = v1.NewCollectorService
	NewConsoleService                   = v1.NewConsoleService
	NewCredentialsService               = v1.NewCredentialsService
	NewEventService                     = v1.NewEventService
	NewExportService                    = v1.NewExportService
	NewForecasterService                = v1.NewForecasterService
	NewGroupService                     = v1.NewGroupService
	NewGroupServiceWithInventoryBuilder = v1.NewGroupServiceWithInventoryBuilder
	NewInspectorService                 = v1.NewInspectorService
	NewInventoryService                 = v1.NewInventoryService
	NewRightsizingService               = v1.NewRightsizingService
	NewServiceManager                   = v1.NewServiceManager
	NewVddkService                      = v1.NewVddkService
	NewVMService                        = v1.NewVMService
	WithConfig                          = v1.WithConfig
	WithConsoleClient                   = v1.WithConsoleClient
	WithKeyManager                      = v1.WithKeyManager
	WithStore                           = v1.WithStore
)
