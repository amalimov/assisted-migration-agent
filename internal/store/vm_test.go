package store_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/rvtools-analyser/internal/models"
	"github.com/kubev2v/rvtools-analyser/internal/store"
	"github.com/kubev2v/rvtools-analyser/internal/store/migrations"
	srvErrors "github.com/kubev2v/rvtools-analyser/pkg/errors"
	"github.com/kubev2v/rvtools-analyser/test"
)

var _ = Describe("VMStore", func() {
	var (
		ctx    context.Context
		s      *store.Store
		db     *sql.DB
		pool   *store.Pool
		tmpDir string
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		tmpDir, err = os.MkdirTemp("", "vm-store-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)
		collDB, err := pool.NewDatabase("coll", filepath.Join(tmpDir, "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		var rawDB *sql.DB
		Expect(collDB.Migrate(ctx, func(mCtx context.Context, sqlDB *sql.DB) error {
			rawDB = sqlDB
			st, stErr := collDB.Store()
			if stErr != nil {
				return stErr
			}
			if pErr := duckdb_parser.New(st.Querier(), test.NewMockValidator()).Init(); pErr != nil {
				return pErr
			}
			return migrations.RunCollection(mCtx, sqlDB, "collection")
		})).To(Succeed())
		db = rawDB
		pool.Add(collDB)
		s, err = collDB.Store()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if pool != nil {
			pool.Close()
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	// Helper to insert test data into vinfo table
	insertVM := func(id, name, powerState, cluster string, memory int32) {
		_, err := s.Querier().ExecContext(ctx, `
			INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory", "Template")
			VALUES (?, ?, ?, ?, ?, false)
		`, id, name, powerState, cluster, memory)
		Expect(err).NotTo(HaveOccurred())
	}

	// Helper to insert VM with template flag
	insertVMWithTemplate := func(id, name, powerState, cluster string, memory int32, isTemplate bool) {
		_, err := s.Querier().ExecContext(ctx, `
			INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory", "Template")
			VALUES (?, ?, ?, ?, ?, ?)
		`, id, name, powerState, cluster, memory, isTemplate)
		Expect(err).NotTo(HaveOccurred())
	}

	// Helper to insert disk data into vdisk table
	insertDisk := func(vmID string, capacityMiB int64) {
		_, err := s.Querier().ExecContext(ctx, `
			INSERT INTO vdisk ("VM ID", "Capacity MiB")
			VALUES (?, ?)
		`, vmID, capacityMiB)
		Expect(err).NotTo(HaveOccurred())
	}

	// Helper to insert concerns for a VM
	insertConcern := func(vmID, concernID, label, category string) {
		_, err := s.Querier().ExecContext(ctx, `
			INSERT INTO concerns ("VM_ID", "Concern_ID", "Label", "Category", "Assessment")
			VALUES (?, ?, ?, ?, 'Needs attention')
		`, vmID, concernID, label, category)
		Expect(err).NotTo(HaveOccurred())
	}

	// Helper to insert VM with folder information

	Context("List", func() {
		BeforeEach(func() {
			// Insert test VMs
			insertVM("vm-1", "web-server-1", "poweredOn", "cluster-a", 4096)
			insertVM("vm-2", "web-server-2", "poweredOn", "cluster-a", 8192)
			insertVM("vm-3", "db-server-1", "poweredOff", "cluster-b", 16384)
			insertVM("vm-4", "app-server-1", "poweredOn", "cluster-c", 8192)
			insertVM("vm-5", "app-server-2", "suspended", "cluster-c", 32768)

			// Insert disk data
			insertDisk("vm-1", 100)
			insertDisk("vm-2", 200)
			insertDisk("vm-3", 500)
			insertDisk("vm-4", 150)
			insertDisk("vm-5", 150)

			// Insert some concerns
			insertConcern("vm-3", "concern-1", "High CPU usage", "Warning")
			insertConcern("vm-3", "concern-2", "Outdated OS", "Warning")
			insertConcern("vm-5", "concern-3", "Network issue", "Warning")
		})

		// Given VMs in the database
		// When we list without filters
		// Then it should return all VMs
		It("should return all VMs without filters", func() {
			// Act
			vms, err := s.VM().List(ctx, nil)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(5))
		})

		Context("ByClusters", func() {
			// Given VMs in different clusters
			// When we filter by a single cluster
			// Then it should return only VMs in that cluster
			It("should filter by single cluster", func() {
				// Act
				vms, err := s.VM().List(ctx, store.ByFilter("cluster = 'cluster-a'"))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2))
				for _, vm := range vms {
					Expect(vm.Cluster).To(Equal("cluster-a"))
				}
			})

			// Given VMs in different clusters
			// When we filter by multiple clusters
			// Then it should return VMs in any of those clusters (OR)
			It("should filter by multiple clusters (OR)", func() {
				// Act
				vms, err := s.VM().List(ctx, store.ByFilter("cluster in ['cluster-a', 'cluster-b']"))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(3))
			})

			// Given VMs whose cluster names differ from the inventory cluster ids
			// When we filter by cluster_id
			// Then it should return only VMs in the cluster with that object id
			It("should filter by inventory cluster id", func() {
				_, err := s.Querier().ExecContext(ctx, `
					INSERT INTO vcluster ("Name", "Object ID")
					VALUES ('cluster-a', 'cluster-fb55d09a08ee0c0f'),
					       ('cluster-b', 'domain-c2')
				`)
				Expect(err).NotTo(HaveOccurred())

				vms, err := s.VM().List(ctx, store.ByFilter("cluster_id = 'cluster-fb55d09a08ee0c0f'"))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2))
				for _, vm := range vms {
					Expect(vm.Cluster).To(Equal("cluster-a"))
				}

				count, err := s.VM().Count(ctx, store.ByFilter("cluster_id = 'domain-c2'"))
				Expect(err).NotTo(HaveOccurred())
				Expect(count).To(Equal(1))

				none, err := s.VM().List(ctx, store.ByFilter("cluster_id = 'domain-c999'"))
				Expect(err).NotTo(HaveOccurred())
				Expect(none).To(BeEmpty())
			})
		})

		Context("ByStatus", func() {
			// Given VMs with different power states
			// When we filter by a single status
			// Then it should return only VMs with that status
			It("should filter by single status", func() {
				// Act
				vms, err := s.VM().List(ctx, store.ByFilter("powerstate = 'poweredOn'"))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(3))
				for _, vm := range vms {
					Expect(vm.PowerState).To(Equal("poweredOn"))
				}
			})

			// Given VMs with different power states
			// When we filter by multiple statuses
			// Then it should return VMs with any of those statuses (OR)
			It("should filter by multiple statuses (OR)", func() {
				// Act
				vms, err := s.VM().List(ctx, store.ByFilter("powerstate in ['poweredOn', 'poweredOff']"))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(4))
			})
		})

		Context("ByIssues", func() {
			// Given VMs with different issue counts
			// When we filter by minimum issue count of 2
			// Then it should return only VMs with at least 2 issues
			It("should filter VMs with at least N issues", func() {
				// Act
				vms, err := s.VM().List(ctx, store.ByFilter("issues_count >= 2"))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(1))
				Expect(vms[0].ID).To(Equal("vm-3"))
				Expect(vms[0].IssueCount).To(Equal(2))
			})

			// Given VMs with different issue counts
			// When we filter by minimum issue count of 1
			// Then it should return VMs with at least 1 issue
			It("should filter VMs with at least 1 issue", func() {
				// Act
				vms, err := s.VM().List(ctx, store.ByFilter("issues_count >= 1"))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2)) // vm-3 and vm-5
			})
		})

		Context("ByDiskSizeRange", func() {
			// Given VMs with different disk sizes
			// When we filter by disk size range
			// Then it should return only VMs within that range
			It("should filter by disk size range", func() {
				// Act
				vms, err := s.VM().List(ctx, store.ByFilter("total_disk_capacity >= 100 and total_disk_capacity < 200"))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(3))
				for _, vm := range vms {
					Expect(vm.DiskSize).To(BeNumerically(">=", 100))
					Expect(vm.DiskSize).To(BeNumerically("<", 200))
				}
			})

			// Given VMs with specific disk sizes
			// When we filter by a range that matches no VMs
			// Then it should return empty result
			It("should return empty when no VMs in range", func() {
				// Act
				vms, err := s.VM().List(ctx, store.ByFilter("total_disk_capacity >= 1000 and total_disk_capacity < 2000"))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(BeEmpty())
			})
		})

		Context("ByMemorySizeRange", func() {
			// Given VMs with different memory sizes
			// When we filter by memory size range
			// Then it should return only VMs within that range
			It("should filter by memory size range", func() {
				// Act
				vms, err := s.VM().List(ctx, store.ByFilter("memory >= 8000 and memory < 20000"))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(3))
				for _, vm := range vms {
					Expect(vm.Memory).To(BeNumerically(">=", 8000))
					Expect(vm.Memory).To(BeNumerically("<", 20000))
				}
			})
		})

		Context("WithLimit and WithOffset", func() {
			// Given multiple VMs in the database
			// When we list with a limit
			// Then it should return only that many results
			It("should limit results", func() {
				// Act
				vms, err := s.VM().List(ctx, nil, store.WithLimit(2))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2))
			})

			// Given multiple VMs in the database
			// When we list with offset and limit
			// Then it should return paginated results
			It("should offset results", func() {
				// Arrange
				firstPage, err := s.VM().List(ctx, nil, store.WithDefaultSort(), store.WithLimit(2))
				Expect(err).NotTo(HaveOccurred())
				Expect(firstPage).To(HaveLen(2))

				// Act
				secondPage, err := s.VM().List(ctx, nil, store.WithDefaultSort(), store.WithOffset(2), store.WithLimit(2))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(secondPage).To(HaveLen(2))
				for _, vm := range secondPage {
					Expect(vm.ID).NotTo(Equal(firstPage[0].ID))
					Expect(vm.ID).NotTo(Equal(firstPage[1].ID))
				}
			})
		})

		Context("WithSort", func() {
			// Given VMs with different names
			// When we sort by name ascending
			// Then results should be ordered alphabetically
			It("should sort by name ascending", func() {
				// Act
				vms, err := s.VM().List(ctx, nil, store.WithSort([]store.SortParam{{Field: "name", Desc: false}}))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(5))
				Expect(vms[0].Name).To(Equal("app-server-1"))
				Expect(vms[1].Name).To(Equal("app-server-2"))
			})

			// Given VMs with different memory sizes
			// When we sort by memory descending
			// Then results should be ordered from highest to lowest memory
			It("should sort by memory descending", func() {
				// Act
				vms, err := s.VM().List(ctx, nil, store.WithSort([]store.SortParam{{Field: "memory", Desc: true}}))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(5))
				Expect(vms[0].Memory).To(Equal(int32(32768)))
			})

			// Given VMs with different issue counts
			// When we sort by issues descending
			// Then results should be ordered from most to least issues
			It("should sort by issues descending", func() {
				// Act
				vms, err := s.VM().List(ctx, nil, store.WithSort([]store.SortParam{{Field: "issues", Desc: true}}))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(5))
				Expect(vms[0].IssueCount).To(Equal(2)) // vm-3 has 2 issues
			})

			// Given VMs with mixed datacenter values that do not match id order
			// When we sort by datacenter ascending
			// Then the VM that owns DC-A moves as a whole row (id and other fields stay together)
			It("should sort by datacenter ascending", func() {
				_, err := s.Querier().ExecContext(ctx, `
					UPDATE vinfo SET "Datacenter" = CASE "VM ID"
						WHEN 'vm-1' THEN 'DC-C'
						WHEN 'vm-2' THEN 'DC-B'
						WHEN 'vm-3' THEN 'DC-A'
						ELSE 'DC-B'
					END
				`)
				Expect(err).NotTo(HaveOccurred())

				// Act
				vms, err := s.VM().List(ctx, nil, store.WithSort([]store.SortParam{{Field: "datacenter", Desc: false}}))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(5))
				Expect(vms[0].Datacenter).To(Equal("DC-A"))
				Expect(vms[0].ID).To(Equal("vm-3"))
				Expect(vms[0].Name).To(Equal("db-server-1"))
			})

		})

		Context("combined filters", func() {
			// Given VMs in different clusters with different statuses
			// When we combine cluster and status filters
			// Then it should return VMs matching both conditions (AND)
			It("should combine cluster and status filters (AND)", func() {
				// Act
				vms, err := s.VM().List(ctx,
					store.ByFilter("cluster = 'cluster-a' and powerstate = 'poweredOn'"),
				)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2))
				for _, vm := range vms {
					Expect(vm.Cluster).To(Equal("cluster-a"))
					Expect(vm.PowerState).To(Equal("poweredOn"))
				}
			})

			// Given VMs in different clusters with different memory sizes
			// When we combine cluster and memory range filters
			// Then it should return VMs matching both conditions
			It("should combine cluster and memory range filters", func() {
				// Act
				vms, err := s.VM().List(ctx,
					store.ByFilter("cluster = 'cluster-a' and memory >= 4000 and memory < 10000"),
				)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2))
			})

			// Given VMs with different statuses
			// When we combine status filter with pagination
			// Then it should return paginated filtered results
			It("should combine multiple filters with pagination", func() {
				// Act
				vms, err := s.VM().List(ctx,
					store.ByFilter("powerstate = 'poweredOn'"),
					store.WithLimit(1),
					store.WithOffset(1),
				)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(1))
			})
		})

		Context("IsMigratable", func() {
			BeforeEach(func() {
				// Add critical concern to vm-3, making it non-migratable
				insertConcern("vm-3", "concern-critical", "RDM disk detected", "Critical")
			})

			// Given VMs with different issue categories
			// When we list VMs
			// Then VMs with critical concerns should have IsMigratable=false
			It("should set IsMigratable=false for VMs with critical concerns", func() {
				// Act
				vms, err := s.VM().List(ctx, nil, store.WithDefaultSort())

				// Assert
				Expect(err).NotTo(HaveOccurred())

				// Find vm-3 which has a critical concern
				var vm3Found bool
				for _, vm := range vms {
					if vm.ID == "vm-3" {
						vm3Found = true
						Expect(vm.IsMigratable).To(BeFalse(), "vm-3 should not be migratable due to critical concern")
					}
				}
				Expect(vm3Found).To(BeTrue(), "vm-3 should be in the list")
			})

			// Given VMs with only warning concerns
			// When we list VMs
			// Then VMs with only warning concerns should have IsMigratable=true
			It("should set IsMigratable=true for VMs with only warning concerns", func() {
				// Act
				vms, err := s.VM().List(ctx, nil, store.WithDefaultSort())

				// Assert
				Expect(err).NotTo(HaveOccurred())

				// Find vm-5 which has only warning concerns
				var vm5Found bool
				for _, vm := range vms {
					if vm.ID == "vm-5" {
						vm5Found = true
						Expect(vm.IsMigratable).To(BeTrue(), "vm-5 should be migratable (only warning concerns)")
					}
				}
				Expect(vm5Found).To(BeTrue(), "vm-5 should be in the list")
			})

			// Given VMs with no concerns
			// When we list VMs
			// Then VMs with no concerns should have IsMigratable=true
			It("should set IsMigratable=true for VMs with no concerns", func() {
				// Act
				vms, err := s.VM().List(ctx, nil, store.WithDefaultSort())

				// Assert
				Expect(err).NotTo(HaveOccurred())

				// Find vm-1 which has no concerns
				var vm1Found bool
				for _, vm := range vms {
					if vm.ID == "vm-1" {
						vm1Found = true
						Expect(vm.IsMigratable).To(BeTrue(), "vm-1 should be migratable (no concerns)")
					}
				}
				Expect(vm1Found).To(BeTrue(), "vm-1 should be in the list")
			})
		})

		Context("IsTemplate", func() {
			BeforeEach(func() {
				// Insert a template VM
				insertVMWithTemplate("vm-template", "template-server", "poweredOff", "cluster-a", 2048, true)
			})

			// Given VMs including templates
			// When we list VMs
			// Then template VMs should have IsTemplate=true
			It("should set IsTemplate=true for template VMs", func() {
				// Act
				vms, err := s.VM().List(ctx, nil, store.WithDefaultSort())

				// Assert
				Expect(err).NotTo(HaveOccurred())

				// Find the template VM
				var templateFound bool
				for _, vm := range vms {
					if vm.ID == "vm-template" {
						templateFound = true
						Expect(vm.IsTemplate).To(BeTrue(), "vm-template should be marked as template")
					}
				}
				Expect(templateFound).To(BeTrue(), "vm-template should be in the list")
			})

			// Given regular VMs
			// When we list VMs
			// Then regular VMs should have IsTemplate=false
			It("should set IsTemplate=false for regular VMs", func() {
				// Act
				vms, err := s.VM().List(ctx, nil, store.WithDefaultSort())

				// Assert
				Expect(err).NotTo(HaveOccurred())

				// Check that regular VMs are not templates
				for _, vm := range vms {
					if vm.ID != "vm-template" {
						Expect(vm.IsTemplate).To(BeFalse(), "%s should not be marked as template", vm.ID)
					}
				}
			})
		})
	})

	Context("Count", func() {
		BeforeEach(func() {
			insertVM("vm-1", "vm1", "poweredOn", "cluster-a", 4096)
			insertVM("vm-2", "vm2", "poweredOn", "cluster-a", 8192)
			insertVM("vm-3", "vm3", "poweredOff", "cluster-b", 16384)

			insertDisk("vm-1", 100)
			insertDisk("vm-2", 200)
			insertDisk("vm-3", 500)
		})

		// Given VMs in the database
		// When we count without filters
		// Then it should return the total count
		It("should count all VMs without filters", func() {
			// Act
			count, err := s.VM().Count(ctx, nil)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(3))
		})

		// Given VMs with different statuses
		// When we count with a status filter
		// Then it should return only the count of matching VMs
		It("should count VMs with filter", func() {
			// Act
			count, err := s.VM().Count(ctx, store.ByFilter("powerstate = 'poweredOn'"))

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(2))
		})
	})

	Context("Get", func() {
		BeforeEach(func() {
			err := test.InsertVMs(ctx, db)
			Expect(err).NotTo(HaveOccurred())
		})

		// Given a VM exists in the database
		// When we get it by ID
		// Then it should return full VM details
		It("should return full VM details by ID", func() {
			// Act
			vm, err := s.VM().Get(ctx, "vm-003")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vm).NotTo(BeNil())
			Expect(vm.ID).To(Equal("vm-003"))
			Expect(vm.Name).To(Equal("db-server-1"))
			Expect(vm.PowerState).To(Equal("poweredOn"))
			Expect(vm.Cluster).To(Equal("production"))
			Expect(vm.MemoryMB).To(Equal(int32(16384)))
			Expect(vm.Firmware).To(Equal("efi"))
		})

		// Given a VM ID that does not exist
		// When we get it by ID
		// Then it should return ResourceNotFoundError
		It("should return ResourceNotFoundError for non-existent ID", func() {
			// Act
			_, err := s.VM().Get(ctx, "non-existent")

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		// Given a VM with disks, NICs, and concerns
		// When we get it by ID
		// Then it should return correct disks, NICs, and issues
		It("should return correct disks, NICs, and issues from parser", func() {
			// Act - vm-003 has 2 disks, 2 NICs, and 2 concerns
			vm, err := s.VM().Get(ctx, "vm-003")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.Disks).To(HaveLen(2))
			Expect(vm.DiskSize).To(Equal(int64(500 + 500)))
			Expect(vm.NICs).To(HaveLen(2))
			Expect(vm.Issues).To(HaveLen(2))
			Expect(vm.Issues[0].Label).To(Equal("High memory usage"))
			Expect(vm.Issues[1].Label).To(Equal("Outdated VMware Tools"))
		})

		// Given a VM with only warning concerns
		// When we get it by ID
		// Then it should have IsMigratable=true
		It("should set IsMigratable=true for VMs with only warning concerns", func() {
			// Act - vm-003 has only warning concerns
			vm, err := s.VM().Get(ctx, "vm-003")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.IsMigratable).To(BeTrue(), "vm-003 should be migratable (only warning concerns)")
		})

		// Given a VM with critical concerns
		// When we get it by ID
		// Then it should have IsMigratable=false
		It("should set IsMigratable=false for VMs with critical concerns", func() {
			// Act - vm-007 has a critical concern (RDM disk detected)
			vm, err := s.VM().Get(ctx, "vm-007")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.IsMigratable).To(BeFalse(), "vm-007 should not be migratable (has critical concern)")
		})

		// Given a VM with no concerns
		// When we get it by ID
		// Then it should have IsMigratable=true
		It("should set IsMigratable=true for VMs with no concerns", func() {
			// Act - vm-001 has no concerns
			vm, err := s.VM().Get(ctx, "vm-001")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.IsMigratable).To(BeTrue(), "vm-001 should be migratable (no concerns)")
		})

		// Given a template VM
		// When we get it by ID
		// Then it should have IsTemplate=true
		It("should return IsTemplate=true for template VMs", func() {
			// Act - vm-010 is a template
			vm, err := s.VM().Get(ctx, "vm-010")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.IsTemplate).To(BeTrue(), "vm-010 should be marked as template")
		})

		// Given a VM with an unknown category
		// When we get it by ID
		// Then the category should be normalized to "Other"
		It("should normalize unknown categories to 'Other'", func() {
			// Arrange - Insert a concern with an unknown category
			_, err := s.Querier().ExecContext(ctx, `
				INSERT INTO concerns ("VM_ID", "Concern_ID", "Label", "Category", "Assessment")
				VALUES ('vm-001', 'test.unknown', 'Unknown Category Test', 'UnknownCategory', 'This is a test')
			`)
			Expect(err).NotTo(HaveOccurred())

			// Act
			vm, err := s.VM().Get(ctx, "vm-001")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.Issues).To(HaveLen(1))
			Expect(vm.Issues[0].Category).To(Equal("Other"), "Unknown category should be normalized to 'Other'")
			Expect(vm.Issues[0].Label).To(Equal("Unknown Category Test"))
		})

		// Given a VM with case-variant category names
		// When we get it by ID
		// Then categories should be normalized to proper case
		It("should normalize category case variants", func() {
			// Arrange - Insert concerns with different case variants
			_, err := s.Querier().ExecContext(ctx, `
				INSERT INTO concerns ("VM_ID", "Concern_ID", "Label", "Category", "Assessment")
				VALUES
					('vm-002', 'test.lowercase', 'Lowercase Test', 'critical', 'Test'),
					('vm-002', 'test.uppercase', 'Uppercase Test', 'WARNING', 'Test'),
					('vm-002', 'test.mixedcase', 'Mixed Test', 'InFoRmAtIoN', 'Test')
			`)
			Expect(err).NotTo(HaveOccurred())

			// Act
			vm, err := s.VM().Get(ctx, "vm-002")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.Issues).To(HaveLen(3))

			// Find each issue and check its normalized category
			for _, issue := range vm.Issues {
				switch issue.ID {
				case "test.lowercase":
					Expect(issue.Category).To(Equal("Critical"))
				case "test.uppercase":
					Expect(issue.Category).To(Equal("Warning"))
				case "test.mixedcase":
					Expect(issue.Category).To(Equal("Information"))
				}
			}
		})

		It("should return groups for a VM that belongs to groups", func() {
			// Arrange — insert two VMs; only vm-grp-1 is in cluster-x
			insertVM("vm-grp-1", "grouped-vm", "poweredOn", "cluster-x", 4096)
			insertVM("vm-grp-2", "other-vm", "poweredOn", "cluster-y", 2048)

			g, err := s.Group().Create(ctx, models.Group{
				Name:   "cluster-x-group",
				Filter: "cluster = 'cluster-x'",
			})
			Expect(err).NotTo(HaveOccurred())

			err = s.Group().RefreshMatches(ctx, g.ID)
			Expect(err).NotTo(HaveOccurred())

			// Act
			vm, err := s.VM().Get(ctx, "vm-grp-1")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.Groups).To(ConsistOf("cluster-x-group"), "vm-grp-1 should appear in cluster-x-group")

			// vm-grp-2 is in a different cluster and must not appear in the group
			vm2, err := s.VM().Get(ctx, "vm-grp-2")
			Expect(err).NotTo(HaveOccurred())
			Expect(vm2.Groups).To(BeEmpty(), "vm-grp-2 should not belong to any group")
		})
	})

	Context("Tags in List output", func() {
		BeforeEach(func() {
			insertVM("vm-1", "web-server", "poweredOn", "cluster-a", 4096)
			insertVM("vm-2", "db-server", "poweredOn", "cluster-a", 8192)
			insertVM("vm-3", "app-server", "poweredOff", "cluster-b", 16384)
		})

		It("should return empty groups when no groups match", func() {
			vms, err := s.VM().List(ctx, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(3))
			for _, vm := range vms {
				Expect(vm.Groups).To(BeEmpty())
			}
		})

		It("should derive groups from group_matches", func() {
			g, err := s.Group().Create(ctx, models.Group{
				Name:   "cluster-a-group",
				Filter: "cluster = 'cluster-a'",
			})
			Expect(err).NotTo(HaveOccurred())

			err = s.Group().RefreshMatches(ctx, g.ID)
			Expect(err).NotTo(HaveOccurred())

			vms, err := s.VM().List(ctx, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(3))

			groupsByID := make(map[string][]string)
			for _, vm := range vms {
				groupsByID[vm.ID] = vm.Groups
			}

			Expect(groupsByID["vm-1"]).To(ConsistOf("cluster-a-group"))
			Expect(groupsByID["vm-2"]).To(ConsistOf("cluster-a-group"))
			Expect(groupsByID["vm-3"]).To(BeEmpty())
		})

		It("should include VM in multiple groups", func() {
			g1, err := s.Group().Create(ctx, models.Group{
				Name:   "cluster-a-group",
				Filter: "cluster = 'cluster-a'",
			})
			Expect(err).NotTo(HaveOccurred())

			g2, err := s.Group().Create(ctx, models.Group{
				Name:   "all-group",
				Filter: "memory > 0",
			})
			Expect(err).NotTo(HaveOccurred())

			err = s.Group().RefreshMatches(ctx, g1.ID, g2.ID)
			Expect(err).NotTo(HaveOccurred())

			vms, err := s.VM().List(ctx, nil)
			Expect(err).NotTo(HaveOccurred())

			groupsByID := make(map[string][]string)
			for _, vm := range vms {
				groupsByID[vm.ID] = vm.Groups
			}

			Expect(groupsByID["vm-1"]).To(ConsistOf("cluster-a-group", "all-group"))
			Expect(groupsByID["vm-2"]).To(ConsistOf("cluster-a-group", "all-group"))
			Expect(groupsByID["vm-3"]).To(ConsistOf("all-group"))
		})

		It("should return groups when filter is applied", func() {
			g, err := s.Group().Create(ctx, models.Group{
				Name:   "cluster-a-group",
				Filter: "cluster = 'cluster-a'",
			})
			Expect(err).NotTo(HaveOccurred())

			err = s.Group().RefreshMatches(ctx, g.ID)
			Expect(err).NotTo(HaveOccurred())

			vms, err := s.VM().List(ctx, store.ByFilter("cluster = 'cluster-a'"))
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(2))

			for _, vm := range vms {
				Expect(vm.Groups).To(ConsistOf("cluster-a-group"))
			}
		})
	})

	Context("NIC IP addresses", func() {
		BeforeEach(func() {
			_, err := s.Querier().ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory", "Template")
				VALUES ('vm-ips', 'IP Test VM', 'poweredOn', 'cluster1', 4096, false)
			`)
			Expect(err).NotTo(HaveOccurred())

			_, err = s.Querier().ExecContext(ctx, `
				INSERT INTO vnetwork ("VM ID", "Network", "Mac Address", "IPv4 Address", "IPv6 Address")
				VALUES ('vm-ips', 'VM Network', '00:50:56:aa:bb:cc', '10.0.0.5', 'fe80::1')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return IPv4 and IPv6 addresses on each NIC", func() {
			vm, err := s.VM().Get(ctx, "vm-ips")

			Expect(err).NotTo(HaveOccurred())
			Expect(vm.NICs).To(HaveLen(1))
			Expect(vm.NICs[0].IPv4Address).To(Equal("10.0.0.5"))
			Expect(vm.NICs[0].IPv6Address).To(Equal("fe80::1"))
		})

		It("should return empty string for NICs with no IP", func() {
			_, err := s.Querier().ExecContext(ctx, `
				INSERT INTO vnetwork ("VM ID", "Network", "Mac Address")
				VALUES ('vm-ips', 'Management', '00:50:56:ff:ff:ff')
			`)
			Expect(err).NotTo(HaveOccurred())

			vm, err := s.VM().Get(ctx, "vm-ips")

			Expect(err).NotTo(HaveOccurred())
			Expect(vm.NICs).To(HaveLen(2))
			noIpNic := vm.NICs[1]
			Expect(noIpNic.IPv4Address).To(Equal(""))
			Expect(noIpNic.IPv6Address).To(Equal(""))
		})
	})

	Context("ListForComparison", func() {
		BeforeEach(func() {
			err := test.InsertVMs(ctx, db)
			Expect(err).NotTo(HaveOccurred())
		})

		// Given VMs in the database
		// When we call ListForComparison
		// Then it should return all VMs with non-empty IDs
		It("should return all VMs with non-empty VM IDs", func() {
			rows, err := s.VM().ListForComparison(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(rows).ToNot(BeEmpty())
			for _, r := range rows {
				Expect(r.VMID).ToNot(BeEmpty())
			}
		})

		// Given vm-007 has a Critical concern ("RDM disk detected") in the fixture
		// When we call ListForComparison
		// Then vm-007 should have Migratable=false and vm-001 (no concerns) should have Migratable=true
		It("should mark vm-007 (Critical concern) as non-migratable and vm-001 (no concerns) as migratable", func() {
			rows, err := s.VM().ListForComparison(ctx)
			Expect(err).ToNot(HaveOccurred())

			vmMap := make(map[string]bool)
			for _, r := range rows {
				vmMap[r.VMID] = r.Migratable
			}

			Expect(vmMap).To(HaveKey("vm-007"), "vm-007 should be present in the result")
			Expect(vmMap["vm-007"]).To(BeFalse(), "vm-007 should not be migratable (has Critical concern)")

			Expect(vmMap).To(HaveKey("vm-001"), "vm-001 should be present in the result")
			Expect(vmMap["vm-001"]).To(BeTrue(), "vm-001 should be migratable (no concerns)")
		})

		// Given vm-001 is marked as migration_excluded
		// When we call ListForComparison
		// Then vm-001 must not appear in the result
		It("should exclude VMs whose migration_excluded flag is true", func() {
			Expect(s.VM().UpdateMigrationExcluded(ctx, "vm-001", true)).To(Succeed())

			rows, err := s.VM().ListForComparison(ctx)
			Expect(err).ToNot(HaveOccurred())

			ids := make([]string, 0, len(rows))
			for _, r := range rows {
				ids = append(ids, r.VMID)
			}
			Expect(ids).NotTo(ContainElement("vm-001"))
		})
	})

	Context("CountDistinctClusters", func() {
		// Given no VMs in the database
		// When we call CountDistinctClusters
		// Then it should return zero
		It("should return zero when the database is empty", func() {
			count, err := s.VM().CountDistinctClusters(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(count).To(Equal(0))
		})

		// Given VMs across production, staging, and development clusters
		// When we call CountDistinctClusters
		// Then it should return 3
		It("should return 3 for the standard fixture (production, staging, development)", func() {
			err := test.InsertVMs(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			count, err := s.VM().CountDistinctClusters(ctx)
			Expect(err).ToNot(HaveOccurred())
			// Fixture VMs span: production (vm-001..004), staging (vm-005..007), development (vm-008..010)
			Expect(count).To(Equal(3))
		})
	})

	Context("FaultToleranceEnabled predicate", func() {
		// Helper to insert VM with FT State
		insertVMWithFTState := func(id, name, ftState string) {
			_, err := s.Querier().ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory", "Template", "FT State")
				VALUES (?, ?, 'poweredOn', 'cluster-test', 4096, false, ?)
			`, id, name, ftState)
			Expect(err).NotTo(HaveOccurred())
		}

		BeforeEach(func() {
			// Agent only writes two FT State values
			insertVMWithFTState("vm-enabled", "VM with FT enabled", "enabled")
			insertVMWithFTState("vm-notconfigured", "VM with FT not configured", "notConfigured")
		})

		// Given a VM with FT State = 'enabled'
		// When we query the VM
		// Then FaultToleranceEnabled should be true
		It("should return true for FT State 'enabled'", func() {
			vm, err := s.VM().Get(ctx, "vm-enabled")
			Expect(err).NotTo(HaveOccurred())
			Expect(vm).NotTo(BeNil())
			Expect(vm.FaultToleranceEnabled).To(BeTrue())
		})

		// Given a VM with FT State = 'notConfigured'
		// When we query the VM
		// Then FaultToleranceEnabled should be false
		It("should return false for FT State 'notConfigured'", func() {
			vm, err := s.VM().Get(ctx, "vm-notconfigured")
			Expect(err).NotTo(HaveOccurred())
			Expect(vm).NotTo(BeNil())
			Expect(vm.FaultToleranceEnabled).To(BeFalse())
		})
	})
})
