package store_test

import (
	"context"
	"database/sql"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/filter"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("GroupStore", func() {
	var (
		ctx context.Context
		s   *store.Store
		db  *sql.DB
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())

		err = migrations.Run(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		s = store.NewStore(db, test.NewMockValidator())
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	// Helper to insert VM into vinfo table (collection_id=0 matches the stub collectionID used in legacy tests).
	insertVM := func(id, name, folder string) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory", "Template", "Folder", collection_id)
			VALUES (?, ?, 'poweredOn', 'cluster-a', 4096, false, ?, 0)
		`, id, name, folder)
		Expect(err).NotTo(HaveOccurred())
	}

	// Helper to get matched VM IDs for a group from group_matches
	getMatchedVMIDs := func(groupID uuid.UUID) []string {
		ids, err := s.Group().GetMatchedIDs(ctx, groupID)
		if err != nil {
			return nil
		}
		return ids
	}

	Context("List", func() {
		It("should return empty list when no groups exist", func() {
			groups, err := s.Group().List(ctx, nil, 0, 0)

			Expect(err).NotTo(HaveOccurred())
			Expect(groups).To(BeEmpty())
		})

		It("should return all groups", func() {
			g1 := models.Group{Name: "group1", Filter: "memory >= 8GB"}
			g2 := models.Group{Name: "group2", Filter: "cluster = 'prod'"}
			_, err := s.Group().Create(ctx, g1)
			Expect(err).NotTo(HaveOccurred())
			_, err = s.Group().Create(ctx, g2)
			Expect(err).NotTo(HaveOccurred())

			groups, err := s.Group().List(ctx, nil, 0, 0)

			Expect(err).NotTo(HaveOccurred())
			Expect(groups).To(HaveLen(2))
			groupNames := []string{groups[0].Name, groups[1].Name}
			Expect(groupNames).To(ConsistOf("group1", "group2"))
		})

		It("should filter by name", func() {
			_, err := s.Group().Create(ctx, models.Group{Name: "prod-cluster", Filter: "cluster = 'prod'"})
			Expect(err).NotTo(HaveOccurred())
			_, err = s.Group().Create(ctx, models.Group{Name: "staging-cluster", Filter: "cluster = 'staging'"})
			Expect(err).NotTo(HaveOccurred())

			f, err := filter.ParseWithGroupMap([]byte("name = 'prod-cluster'"))
			Expect(err).NotTo(HaveOccurred())

			groups, err := s.Group().List(ctx, []sq.Sqlizer{f}, 0, 0)

			Expect(err).NotTo(HaveOccurred())
			Expect(groups).To(HaveLen(1))
			Expect(groups[0].Name).To(Equal("prod-cluster"))
		})

		It("should paginate results", func() {
			for i := 0; i < 5; i++ {
				_, err := s.Group().Create(ctx, models.Group{
					Name: fmt.Sprintf("group-%d", i), Filter: "memory > 0",
				})
				Expect(err).NotTo(HaveOccurred())
			}

			groups, err := s.Group().List(ctx, nil, 2, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(groups).To(HaveLen(2))

			groups, err = s.Group().List(ctx, nil, 2, 2)
			Expect(err).NotTo(HaveOccurred())
			Expect(groups).To(HaveLen(2))

			groups, err = s.Group().List(ctx, nil, 2, 4)
			Expect(err).NotTo(HaveOccurred())
			Expect(groups).To(HaveLen(1))
		})
	})

	Context("Count", func() {
		It("should return 0 when no groups exist", func() {
			count, err := s.Group().Count(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(0))
		})

		It("should count all groups", func() {
			_, err := s.Group().Create(ctx, models.Group{Name: "g1", Filter: "memory > 0"})
			Expect(err).NotTo(HaveOccurred())
			_, err = s.Group().Create(ctx, models.Group{Name: "g2", Filter: "memory > 0"})
			Expect(err).NotTo(HaveOccurred())

			count, err := s.Group().Count(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(2))
		})

		It("should count with name filter", func() {
			_, err := s.Group().Create(ctx, models.Group{Name: "prod-vms", Filter: "memory > 0"})
			Expect(err).NotTo(HaveOccurred())
			_, err = s.Group().Create(ctx, models.Group{Name: "staging-vms", Filter: "memory > 0"})
			Expect(err).NotTo(HaveOccurred())

			f, err := filter.ParseWithGroupMap([]byte("name = 'prod-vms'"))
			Expect(err).NotTo(HaveOccurred())

			count, err := s.Group().Count(ctx, f)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(1))
		})
	})

	Context("Get", func() {
		It("should return ResourceNotFoundError when group does not exist", func() {
			nonExistentID := uuid.New()
			_, err := s.Group().Get(ctx, nonExistentID)

			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		It("should return existing group", func() {
			// Arrange
			g := models.Group{Name: "testgroup", Filter: "memory >= 16GB", Description: "Test description"}
			created, err := s.Group().Create(ctx, g)
			Expect(err).NotTo(HaveOccurred())

			// Act
			retrieved, err := s.Group().Get(ctx, created.ID)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.ID).To(Equal(created.ID))
			Expect(retrieved.Name).To(Equal("testgroup"))
			Expect(retrieved.Filter).To(Equal("memory >= 16GB"))
			Expect(retrieved.Description).To(Equal("Test description"))
		})
	})

	Context("Create", func() {
		It("should create group and return with ID and timestamps", func() {
			// Arrange
			g := models.Group{Name: "newgroup", Filter: "cluster in ['prod', 'staging']", Description: "Production clusters"}

			// Act
			created, err := s.Group().Create(ctx, g)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(created.ID).NotTo(Equal(uuid.Nil), "ID should be a non-zero UUID")
			Expect(created.Name).To(Equal("newgroup"))
			Expect(created.Filter).To(Equal("cluster in ['prod', 'staging']"))
			Expect(created.Description).To(Equal("Production clusters"))
			Expect(created.CreatedAt).NotTo(BeZero())
			Expect(created.UpdatedAt).NotTo(BeZero())
		})

		It("should create multiple groups with unique IDs", func() {
			g1 := models.Group{Name: "group1", Filter: "filter1"}
			g2 := models.Group{Name: "group2", Filter: "filter2"}

			created1, err := s.Group().Create(ctx, g1)
			Expect(err).NotTo(HaveOccurred())
			Expect(created1.ID).NotTo(Equal(uuid.Nil), "ID should be a non-zero UUID")

			created2, err := s.Group().Create(ctx, g2)
			Expect(err).NotTo(HaveOccurred())
			Expect(created2.ID).NotTo(Equal(uuid.Nil), "ID should be a non-zero UUID")

			Expect(created1.ID).NotTo(Equal(created2.ID))
		})

		It("should return DuplicateResourceError when creating group with duplicate name", func() {
			g := models.Group{Name: "duplicate-name", Filter: "filter1"}
			_, err := s.Group().Create(ctx, g)
			Expect(err).NotTo(HaveOccurred())

			// Try to create another group with the same name
			g2 := models.Group{Name: "duplicate-name", Filter: "filter2"}
			_, err = s.Group().Create(ctx, g2)
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsDuplicateResourceError(err)).To(BeTrue())
		})
	})

	Context("Update", func() {
		It("should return ResourceNotFoundError when group does not exist", func() {
			nonExistentID := uuid.New()
			g := models.Group{Name: "updated"}
			_, err := s.Group().Update(ctx, nonExistentID, g)

			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		It("should update group name", func() {
			// Arrange
			g := models.Group{Name: "original", Filter: "original filter"}
			created, err := s.Group().Create(ctx, g)
			Expect(err).NotTo(HaveOccurred())

			// Act
			update := models.Group{Name: "updated", Filter: "original filter"}
			updated, err := s.Group().Update(ctx, created.ID, update)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Name).To(Equal("updated"))
			Expect(updated.Filter).To(Equal("original filter"))
		})

		It("should update group filter", func() {
			// Arrange
			g := models.Group{Name: "mygroup", Filter: "old filter"}
			created, err := s.Group().Create(ctx, g)
			Expect(err).NotTo(HaveOccurred())

			// Act
			update := models.Group{Name: "mygroup", Filter: "new filter"}
			updated, err := s.Group().Update(ctx, created.ID, update)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Name).To(Equal("mygroup"))
			Expect(updated.Filter).To(Equal("new filter"))
		})

		It("should update both name and filter", func() {
			// Arrange
			g := models.Group{Name: "original", Filter: "original"}
			created, err := s.Group().Create(ctx, g)
			Expect(err).NotTo(HaveOccurred())

			// Act
			update := models.Group{Name: "newname", Filter: "newfilter"}
			updated, err := s.Group().Update(ctx, created.ID, update)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Name).To(Equal("newname"))
			Expect(updated.Filter).To(Equal("newfilter"))
		})

		It("should update description", func() {
			// Arrange
			g := models.Group{Name: "mygroup", Filter: "filter", Description: "original description"}
			created, err := s.Group().Create(ctx, g)
			Expect(err).NotTo(HaveOccurred())

			// Act
			update := models.Group{Name: "mygroup", Filter: "filter", Description: "updated description"}
			updated, err := s.Group().Update(ctx, created.ID, update)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Description).To(Equal("updated description"))
		})

		It("should update UpdatedAt timestamp", func() {
			// Arrange
			g := models.Group{Name: "mygroup", Filter: "filter"}
			created, err := s.Group().Create(ctx, g)
			Expect(err).NotTo(HaveOccurred())

			// Act
			update := models.Group{Name: "updated", Filter: "filter"}
			updated, err := s.Group().Update(ctx, created.ID, update)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.UpdatedAt).To(BeTemporally(">=", created.UpdatedAt))
		})

		It("should return DuplicateResourceError when updating to existing name", func() {
			// Arrange: create two groups
			g1 := models.Group{Name: "first-group", Filter: "filter1"}
			created1, err := s.Group().Create(ctx, g1)
			Expect(err).NotTo(HaveOccurred())

			g2 := models.Group{Name: "second-group", Filter: "filter2"}
			_, err = s.Group().Create(ctx, g2)
			Expect(err).NotTo(HaveOccurred())

			// Act: try to update first group to have the same name as second
			update := models.Group{Name: "second-group", Filter: "filter1"}
			_, err = s.Group().Update(ctx, created1.ID, update)

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsDuplicateResourceError(err)).To(BeTrue())
		})
	})

	Context("Delete", func() {
		It("should return ResourceNotFoundError when group does not exist", func() {
			nonExistentID := uuid.New()
			err := s.Group().Delete(ctx, nonExistentID)

			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		It("should delete existing group", func() {
			// Arrange
			g := models.Group{Name: "todelete", Filter: "filter"}
			created, err := s.Group().Create(ctx, g)
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = s.Group().Delete(ctx, created.ID)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			// Verify group no longer exists
			_, err = s.Group().Get(ctx, created.ID)
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		It("should not affect other groups when deleting", func() {
			// Arrange
			g1 := models.Group{Name: "group1", Filter: "filter1"}
			g2 := models.Group{Name: "group2", Filter: "filter2"}
			created1, err := s.Group().Create(ctx, g1)
			Expect(err).NotTo(HaveOccurred())
			created2, err := s.Group().Create(ctx, g2)
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = s.Group().Delete(ctx, created1.ID)
			Expect(err).NotTo(HaveOccurred())

			// Assert
			groups, err := s.Group().List(ctx, nil, 0, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(groups).To(HaveLen(1))
			Expect(groups[0].ID).To(Equal(created2.ID))
		})
	})

	Context("RefreshMatches", func() {
		BeforeEach(func() {
			insertVM("vm-1", "web-server", "production")
			insertVM("vm-2", "db-server", "production")
			insertVM("vm-3", "staging-app", "staging")
			insertVM("vm-4", "dev-server", "development")
		})

		It("should do nothing when no groups exist", func() {
			err := s.Group().RefreshMatches(ctx, 0)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should store matching VM IDs for a group", func() {
			g, err := s.Group().Create(ctx, models.Group{
				Name:   "prod-group",
				Filter: "folder = 'production'",
			})
			Expect(err).NotTo(HaveOccurred())

			err = s.Group().RefreshMatches(ctx, 0, g.ID)
			Expect(err).NotTo(HaveOccurred())

			ids := getMatchedVMIDs(g.ID)
			Expect(ids).To(ConsistOf("vm-1", "vm-2"))
		})

		It("should store matches for multiple groups independently", func() {
			g1, err := s.Group().Create(ctx, models.Group{
				Name:   "prod-group",
				Filter: "folder = 'production'",
			})
			Expect(err).NotTo(HaveOccurred())

			g2, err := s.Group().Create(ctx, models.Group{
				Name:   "all-servers",
				Filter: "name ~ /server/",
			})
			Expect(err).NotTo(HaveOccurred())

			err = s.Group().RefreshMatches(ctx, 0)
			Expect(err).NotTo(HaveOccurred())

			ids1 := getMatchedVMIDs(g1.ID)
			Expect(ids1).To(ConsistOf("vm-1", "vm-2"))

			ids2 := getMatchedVMIDs(g2.ID)
			Expect(ids2).To(ConsistOf("vm-1", "vm-2", "vm-4"))
		})

		It("should refresh only specified group IDs", func() {
			g1, err := s.Group().Create(ctx, models.Group{
				Name:   "prod-group",
				Filter: "folder = 'production'",
			})
			Expect(err).NotTo(HaveOccurred())

			g2, err := s.Group().Create(ctx, models.Group{
				Name:   "staging-group",
				Filter: "folder = 'staging'",
			})
			Expect(err).NotTo(HaveOccurred())

			// Refresh all first
			err = s.Group().RefreshMatches(ctx, 0)
			Expect(err).NotTo(HaveOccurred())

			Expect(getMatchedVMIDs(g1.ID)).To(ConsistOf("vm-1", "vm-2"))
			Expect(getMatchedVMIDs(g2.ID)).To(ConsistOf("vm-3"))

			// Refresh only g1 — g2 should remain unchanged
			err = s.Group().RefreshMatches(ctx, 0, g1.ID)
			Expect(err).NotTo(HaveOccurred())

			Expect(getMatchedVMIDs(g1.ID)).To(ConsistOf("vm-1", "vm-2"))
			Expect(getMatchedVMIDs(g2.ID)).To(ConsistOf("vm-3"))
		})

		It("should rebuild matches when filter changes", func() {
			g, err := s.Group().Create(ctx, models.Group{
				Name:   "prod-group",
				Filter: "folder = 'production'",
			})
			Expect(err).NotTo(HaveOccurred())

			err = s.Group().RefreshMatches(ctx, 0, g.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getMatchedVMIDs(g.ID)).To(ConsistOf("vm-1", "vm-2"))

			g.Filter = "folder = 'staging'"
			_, err = s.Group().Update(ctx, g.ID, *g)
			Expect(err).NotTo(HaveOccurred())

			err = s.Group().RefreshMatches(ctx, 0, g.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getMatchedVMIDs(g.ID)).To(ConsistOf("vm-3"))
		})

		It("should return empty list after group is deleted and matches cleared", func() {
			g, err := s.Group().Create(ctx, models.Group{
				Name:   "prod-group",
				Filter: "folder = 'production'",
			})
			Expect(err).NotTo(HaveOccurred())

			err = s.Group().RefreshMatches(ctx, 0, g.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getMatchedVMIDs(g.ID)).To(ConsistOf("vm-1", "vm-2"))

			err = s.Group().Delete(ctx, g.ID)
			Expect(err).NotTo(HaveOccurred())

			err = s.Group().DeleteMatches(ctx, g.ID)
			Expect(err).NotTo(HaveOccurred())

			Expect(getMatchedVMIDs(g.ID)).To(BeEmpty())
		})
	})

	Context("GetGroupsContainingVM", func() {
		BeforeEach(func() {
			insertVM("vm-1", "web-server", "production")
			insertVM("vm-2", "db-server", "production")
			insertVM("vm-3", "staging-app", "staging")
			insertVM("vm-4", "dev-server", "development")
		})

		It("should return empty slice when VM is not in any group", func() {
			g, err := s.Group().Create(ctx, models.Group{
				Name:   "prod-group",
				Filter: "folder = 'production'",
			})
			Expect(err).NotTo(HaveOccurred())

			err = s.Group().RefreshMatches(ctx, 0, g.ID)
			Expect(err).NotTo(HaveOccurred())

			groupIDs, err := s.Group().GetGroupsContainingVM(ctx, "vm-3")
			Expect(err).NotTo(HaveOccurred())
			Expect(groupIDs).To(BeEmpty())
		})

		It("should return group ID when VM is in single group", func() {
			g, err := s.Group().Create(ctx, models.Group{
				Name:   "prod-group",
				Filter: "folder = 'production'",
			})
			Expect(err).NotTo(HaveOccurred())

			err = s.Group().RefreshMatches(ctx, 0, g.ID)
			Expect(err).NotTo(HaveOccurred())

			groupIDs, err := s.Group().GetGroupsContainingVM(ctx, "vm-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(groupIDs).To(HaveLen(1))
			Expect(groupIDs[0]).To(Equal(g.ID))
		})

		It("should return multiple group IDs when VM is in multiple groups", func() {
			g1, err := s.Group().Create(ctx, models.Group{
				Name:   "prod-group",
				Filter: "folder = 'production'",
			})
			Expect(err).NotTo(HaveOccurred())

			g2, err := s.Group().Create(ctx, models.Group{
				Name:   "all-servers",
				Filter: "name ~ /server/",
			})
			Expect(err).NotTo(HaveOccurred())

			err = s.Group().RefreshMatches(ctx, 0)
			Expect(err).NotTo(HaveOccurred())

			// vm-1 is in production folder AND has 'server' in name
			groupIDs, err := s.Group().GetGroupsContainingVM(ctx, "vm-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(groupIDs).To(HaveLen(2))
			Expect(groupIDs).To(ContainElements(g1.ID, g2.ID))

			// vm-4 only has 'server' in name
			groupIDs, err = s.Group().GetGroupsContainingVM(ctx, "vm-4")
			Expect(err).NotTo(HaveOccurred())
			Expect(groupIDs).To(HaveLen(1))
			Expect(groupIDs[0]).To(Equal(g2.ID))
		})

		It("should return empty slice when no groups exist", func() {
			groupIDs, err := s.Group().GetGroupsContainingVM(ctx, "vm-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(groupIDs).To(BeEmpty())
		})

		It("should return empty slice for non-existent VM", func() {
			g, err := s.Group().Create(ctx, models.Group{
				Name:   "prod-group",
				Filter: "folder = 'production'",
			})
			Expect(err).NotTo(HaveOccurred())

			err = s.Group().RefreshMatches(ctx, 0, g.ID)
			Expect(err).NotTo(HaveOccurred())

			groupIDs, err := s.Group().GetGroupsContainingVM(ctx, "non-existent-vm")
			Expect(err).NotTo(HaveOccurred())
			Expect(groupIDs).To(BeEmpty())
		})

		It("should handle groups with no matches", func() {
			g, err := s.Group().Create(ctx, models.Group{
				Name:   "empty-group",
				Filter: "folder = 'non-existent-folder'",
			})
			Expect(err).NotTo(HaveOccurred())

			err = s.Group().RefreshMatches(ctx, 0, g.ID)
			Expect(err).NotTo(HaveOccurred())

			// VM should not be found in the empty group
			groupIDs, err := s.Group().GetGroupsContainingVM(ctx, "vm-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(groupIDs).To(BeEmpty())
		})
	})

	Context("RefreshMatches with collection_id", func() {
		var group *models.Group
		const col1 int64 = 1
		const col2 int64 = 2

		BeforeEach(func() {
			var err error
			group, err = s.Group().Create(ctx, models.Group{Name: "test-group", Filter: "name = 'match-vm'"})
			Expect(err).NotTo(HaveOccurred())

			// Insert a vinfo row for collection 1 that matches the group filter.
			_, err = db.ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM", collection_id, vmmoid)
				VALUES ('hash-col1', 'match-vm', ?, 'vm-1')
			`, col1)
			Expect(err).NotTo(HaveOccurred())

			// Insert a vinfo row for collection 2 that also matches.
			_, err = db.ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM", collection_id, vmmoid)
				VALUES ('hash-col2', 'match-vm', ?, 'vm-1')
			`, col2)
			Expect(err).NotTo(HaveOccurred())
		})

		It("generates matches only for the given collection", func() {
			Expect(s.Group().RefreshMatches(ctx, col1)).To(Succeed())

			var vmIDs store.StringArray
			err := db.QueryRowContext(ctx,
				`SELECT vm_ids FROM group_matches WHERE group_id = ? AND collection_id = ?`,
				group.ID, col1).Scan(&vmIDs)
			Expect(err).NotTo(HaveOccurred())
			Expect([]string(vmIDs)).To(ConsistOf("hash-col1"))

			// Collection 2 must have no matches yet.
			var count int
			Expect(db.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM group_matches WHERE group_id = ? AND collection_id = ?`,
				group.ID, col2).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(0))
		})

		It("does not delete matches from other collections when refreshing one", func() {
			Expect(s.Group().RefreshMatches(ctx, col1)).To(Succeed())
			Expect(s.Group().RefreshMatches(ctx, col2)).To(Succeed())

			// col1 matches still present.
			var count int
			Expect(db.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM group_matches WHERE collection_id = ?`, col1).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(1))
		})
	})

	Context("DeleteMatchesForCollection", func() {
		It("removes all group_matches rows for a collection", func() {
			group, err := s.Group().Create(ctx, models.Group{Name: "del-test", Filter: "name = 'x'"})
			Expect(err).NotTo(HaveOccurred())

			_, err = db.ExecContext(ctx, `
				INSERT INTO group_matches (group_id, collection_id, vm_ids)
				VALUES (?, 10, ['vm-x']), (?, 20, ['vm-y'])
			`, group.ID, group.ID)
			Expect(err).NotTo(HaveOccurred())

			Expect(s.Group().DeleteMatchesForCollection(ctx, 10)).To(Succeed())

			var count int
			Expect(db.QueryRowContext(ctx, `SELECT COUNT(*) FROM group_matches WHERE collection_id = 10`).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(0))

			// Collection 20 is untouched.
			Expect(db.QueryRowContext(ctx, `SELECT COUNT(*) FROM group_matches WHERE collection_id = 20`).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(1))
		})
	})

	Context("Group Inventory", func() {
		var created *models.Group

		BeforeEach(func() {
			var err error
			created, err = s.Group().Create(ctx, models.Group{Name: "inv-group", Filter: "memory >= 1"})
			Expect(err).NotTo(HaveOccurred())
		})

		It("saves and retrieves a blob", func() {
			data := []byte(`{"vms":[{"id":"vm-1"}]}`)
			Expect(s.Group().SaveGroupInventory(ctx, created.ID, 1, data)).To(Succeed())

			got, err := s.Group().GetGroupInventory(ctx, created.ID, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(data))
		})

		It("returns nil (not an error) for a missing collection", func() {
			got, err := s.Group().GetGroupInventory(ctx, created.ID, 9999)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeNil())
		})

		It("upserts on second save", func() {
			Expect(s.Group().SaveGroupInventory(ctx, created.ID, 1, []byte(`{"v":1}`))).To(Succeed())
			Expect(s.Group().SaveGroupInventory(ctx, created.ID, 1, []byte(`{"v":2}`))).To(Succeed())

			got, err := s.Group().GetGroupInventory(ctx, created.ID, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(MatchJSON(`{"v":2}`))
		})

		It("deletes all rows for a collection without affecting other collections", func() {
			created2, err := s.Group().Create(ctx, models.Group{Name: "g2", Filter: "memory >= 2"})
			Expect(err).NotTo(HaveOccurred())

			Expect(s.Group().SaveGroupInventory(ctx, created.ID, 10, []byte(`{}`))).To(Succeed())
			Expect(s.Group().SaveGroupInventory(ctx, created2.ID, 10, []byte(`{}`))).To(Succeed())
			Expect(s.Group().SaveGroupInventory(ctx, created.ID, 11, []byte(`{}`))).To(Succeed())

			Expect(s.Group().DeleteGroupInventoryForCollection(ctx, 10)).To(Succeed())

			r1, err := s.Group().GetGroupInventory(ctx, created.ID, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(r1).To(BeNil())

			rKeep, err := s.Group().GetGroupInventory(ctx, created.ID, 11)
			Expect(err).NotTo(HaveOccurred())
			Expect(rKeep).NotTo(BeNil())
		})
	})
})
