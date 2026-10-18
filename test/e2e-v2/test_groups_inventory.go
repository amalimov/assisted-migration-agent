package main

import (
	"fmt"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gm "github.com/onsi/gomega"

	v2 "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/pkg/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test/e2e-v2/service"

	"github.com/google/uuid"
)

var _ = ginkgo.Describe("Group inventory v2 e2e tests", ginkgo.Ordered, func() {
	var (
		agentSvc *service.AgentSvc
		allVMs   []v2.VirtualMachine
		totalVMs int
	)

	ginkgo.BeforeAll(func() {
		ginkgo.GinkgoWriter.Println("Starting postgres...")
		err := infraManager.StartPostgres()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start postgres")
		time.Sleep(2 * time.Second)

		ginkgo.GinkgoWriter.Println("Starting vcsim...")
		err = infraManager.StartVcsim()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start vcsim")

		agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)

		agentID := uuid.NewString()
		ginkgo.GinkgoWriter.Printf("Starting agent %s in disconnected mode (v2)...\n", agentID)
		_, err = infraManager.StartAgent(infra.AgentConfig{
			AgentID:        agentID,
			SourceID:       uuid.NewString(),
			Mode:           "disconnected",
			ConsoleURL:     cfg.AgentProxyUrl,
			UpdateInterval: "1s",
			APIVersion:     "v2",
		})
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")

		gm.Eventually(func() error {
			_, err := agentSvc.Status()
			return err
		}, 30*time.Second, 1*time.Second).Should(gm.BeNil(), "agent did not become ready")

		ginkgo.GinkgoWriter.Println("Storing credentials...")
		_, err = agentSvc.StoreCredentials(infra.VcsimURL, infra.VcsimUsername, infra.VcsimPassword)
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to store credentials")

		ginkgo.GinkgoWriter.Println("Starting collector...")
		_, err = agentSvc.StartCollector()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start collector")

		gm.Eventually(func() int {
			collections, err := agentSvc.ListCollections()
			if err != nil {
				return 0
			}
			return len(collections.Collections)
		}, 120*time.Second, 2*time.Second).Should(gm.BeNumerically(">", 0), "expected at least 1 collection")

		pageSize := 100
		result, err := agentSvc.ListLatestVMs(&service.VMListParams{PageSize: &pageSize})
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to list VMs after collection")
		allVMs = result.VirtualMachines
		totalVMs = result.Total
		gm.Expect(totalVMs).To(gm.Equal(50), "vcsim model should produce 50 VMs")

		ginkgo.GinkgoWriter.Printf("Group inventory v2 test setup complete — %d VMs collected\n", totalVMs)
	})

	ginkgo.AfterAll(func() {
		ginkgo.GinkgoWriter.Println("Cleaning up group inventory v2 tests...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopVcsim()
		_ = infraManager.StopPostgres()
	})

	firstCluster := func() string {
		gm.Expect(len(allVMs)).To(gm.BeNumerically(">", 0))
		return allVMs[0].Cluster
	}

	ginkgo.It("should include inventory in GroupResponse for groups with matched VMs", func() {
		cluster := firstCluster()
		group, err := agentSvc.CreateGroup(
			"cluster-inventory-test-v2",
			fmt.Sprintf("cluster = '%s'", cluster),
			"Test group for inventory",
		)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		resp, err := agentSvc.GetGroup(group.Id, nil, nil, nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())

		gm.Expect(resp.Inventory).ToNot(gm.BeNil(), "inventory should be populated")
		gm.Expect(resp.Inventory.Vcenter).ToNot(gm.BeNil(), "inventory should contain vcenter")

		vcenter := resp.Inventory.Vcenter
		gm.Expect(vcenter.Infra.Hosts).ToNot(gm.BeNil(), "vcenter infra should contain hosts")
		gm.Expect(vcenter.Infra.Datastores).ToNot(gm.BeEmpty(), "vcenter infra should contain datastores")
		gm.Expect(vcenter.Infra.Networks).ToNot(gm.BeEmpty(), "vcenter infra should contain networks")

		ginkgo.GinkgoWriter.Printf("Group %s inventory - VCenter ID: %s\n", group.Name, resp.Inventory.VcenterId)
	})

	ginkgo.It("should scope inventory to matched VMs only", func() {
		cluster := firstCluster()
		group, err := agentSvc.CreateGroup(
			"scoped-inventory-test-v2",
			fmt.Sprintf("cluster = '%s'", cluster),
			"Test scoped inventory",
		)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		pageSize := 100
		resp, err := agentSvc.GetGroup(group.Id, nil, nil, &pageSize)
		gm.Expect(err).ToNot(gm.HaveOccurred())

		gm.Expect(resp.Inventory).ToNot(gm.BeNil())
		gm.Expect(resp.Inventory.Vcenter).ToNot(gm.BeNil())

		vcenter := resp.Inventory.Vcenter
		gm.Expect(vcenter.Infra.Hosts).ToNot(gm.BeNil())
		gm.Expect(vcenter.Infra.Datastores).ToNot(gm.BeEmpty())
		gm.Expect(vcenter.Infra.Networks).ToNot(gm.BeEmpty())

		expectedVMs := 0
		for _, vm := range allVMs {
			if vm.Cluster == cluster {
				expectedVMs++
			}
		}

		gm.Expect(resp.Total).To(gm.Equal(expectedVMs),
			"Group should match exactly %d VMs from cluster %s", expectedVMs, cluster)
	})

	ginkgo.It("should rebuild inventory when group filter is updated", func() {
		cluster := firstCluster()
		group, err := agentSvc.CreateGroup(
			"rebuild-inventory-test-v2",
			fmt.Sprintf("cluster = '%s'", cluster),
			"Test inventory rebuild",
		)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		resp1, err := agentSvc.GetGroup(group.Id, nil, nil, nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		gm.Expect(resp1.Inventory).ToNot(gm.BeNil())

		newFilter := "memory >= 16384"
		_, err = agentSvc.UpdateGroup(group.Id, v2.UpdateGroupRequest{Filter: &newFilter})
		gm.Expect(err).ToNot(gm.HaveOccurred())

		resp2, err := agentSvc.GetGroup(group.Id, nil, nil, nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())

		gm.Expect(resp2.Inventory).ToNot(gm.BeNil())
		gm.Expect(resp2.Group.Filter).To(gm.Equal(newFilter))

		ginkgo.GinkgoWriter.Printf("Initial VM count: %d, Updated VM count: %d\n", resp1.Total, resp2.Total)
	})

	ginkgo.It("should have nil inventory when no VMs match", func() {
		group, err := agentSvc.CreateGroup(
			"empty-inventory-test-v2",
			"name = 'nonexistent-vm-name-12345'",
			"Test empty inventory",
		)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		resp, err := agentSvc.GetGroup(group.Id, nil, nil, nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())

		gm.Expect(resp.Total).To(gm.Equal(0))
		gm.Expect(resp.Inventory).To(gm.BeNil(), "empty group should have nil inventory")
	})

	ginkgo.It("should clear inventory when filter updated to match no VMs", func() {
		cluster := firstCluster()
		group, err := agentSvc.CreateGroup(
			"clear-inventory-test-v2",
			fmt.Sprintf("cluster = '%s'", cluster),
			"Test inventory clearing",
		)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		resp1, err := agentSvc.GetGroup(group.Id, nil, nil, nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		gm.Expect(resp1.Inventory).ToNot(gm.BeNil(), "initial inventory should be populated")
		gm.Expect(resp1.Total).To(gm.BeNumerically(">", 0), "should have matching VMs initially")

		emptyFilter := "name = 'nonexistent-vm-999999'"
		_, err = agentSvc.UpdateGroup(group.Id, v2.UpdateGroupRequest{Filter: &emptyFilter})
		gm.Expect(err).ToNot(gm.HaveOccurred())

		resp2, err := agentSvc.GetGroup(group.Id, nil, nil, nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())

		gm.Expect(resp2.Total).To(gm.Equal(0), "should match no VMs after update")
		gm.Expect(resp2.Inventory).To(gm.BeNil(), "inventory should be cleared when no VMs match")
	})

	ginkgo.It("should populate inventory when filter updated from matching-no-VMs to matching VMs", func() {
		group, err := agentSvc.CreateGroup(
			"populate-inventory-test-v2",
			"name = 'nonexistent-vm-999999'",
			"Test inventory population",
		)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		resp1, err := agentSvc.GetGroup(group.Id, nil, nil, nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		gm.Expect(resp1.Total).To(gm.Equal(0))
		gm.Expect(resp1.Inventory).To(gm.BeNil(), "precondition: group should start with nil inventory")

		cluster := firstCluster()
		newFilter := fmt.Sprintf("cluster = '%s'", cluster)
		_, err = agentSvc.UpdateGroup(group.Id, v2.UpdateGroupRequest{Filter: &newFilter})
		gm.Expect(err).ToNot(gm.HaveOccurred())

		resp2, err := agentSvc.GetGroup(group.Id, nil, nil, nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())

		gm.Expect(resp2.Total).To(gm.BeNumerically(">", 0), "should match VMs after update")
		gm.Expect(resp2.Inventory).ToNot(gm.BeNil(), "inventory should be populated once VMs match")
	})

	ginkgo.It("should have independent inventories for different groups", func() {
		firstClusterName := firstCluster()
		var secondCluster string
		for _, vm := range allVMs {
			if vm.Cluster != firstClusterName {
				secondCluster = vm.Cluster
				break
			}
		}
		gm.Expect(secondCluster).ToNot(gm.BeEmpty(), "need at least 2 clusters")

		group1, err := agentSvc.CreateGroup(
			"cluster1-inventory-v2",
			fmt.Sprintf("cluster = '%s'", firstClusterName),
			"Cluster 1",
		)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group1.Id) }()

		group2, err := agentSvc.CreateGroup(
			"cluster2-inventory-v2",
			fmt.Sprintf("cluster = '%s'", secondCluster),
			"Cluster 2",
		)
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group2.Id) }()

		resp1, err := agentSvc.GetGroup(group1.Id, nil, nil, nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())

		resp2, err := agentSvc.GetGroup(group2.Id, nil, nil, nil)
		gm.Expect(err).ToNot(gm.HaveOccurred())

		gm.Expect(resp1.Inventory).ToNot(gm.BeNil())
		gm.Expect(resp2.Inventory).ToNot(gm.BeNil())
	})

	ginkgo.It("should include inventory when listing VMs within a group", func() {
		group, err := agentSvc.CreateGroup("all-vms-inventory-v2", "memory > 0", "All VMs")
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		pageSize := 10
		resp, err := agentSvc.GetGroup(group.Id, nil, nil, &pageSize)
		gm.Expect(err).ToNot(gm.HaveOccurred())

		gm.Expect(resp.Inventory).ToNot(gm.BeNil())
		gm.Expect(len(resp.Vms)).To(gm.Equal(10))
		gm.Expect(resp.Total).To(gm.Equal(totalVMs))

		gm.Expect(resp.Inventory.Vcenter).ToNot(gm.BeNil())
		vcenter := resp.Inventory.Vcenter
		gm.Expect(vcenter.Infra.Datastores).ToNot(gm.BeEmpty())
	})

	ginkgo.It("should maintain inventory across pagination requests", func() {
		group, err := agentSvc.CreateGroup("paginated-inventory-v2", "memory > 0", "Paginated test")
		gm.Expect(err).ToNot(gm.HaveOccurred())
		defer func() { _, _ = agentSvc.DeleteGroup(group.Id) }()

		page1 := 1
		page2 := 2
		pageSize := 10

		resp1, err := agentSvc.GetGroup(group.Id, nil, &page1, &pageSize)
		gm.Expect(err).ToNot(gm.HaveOccurred())

		resp2, err := agentSvc.GetGroup(group.Id, nil, &page2, &pageSize)
		gm.Expect(err).ToNot(gm.HaveOccurred())

		gm.Expect(resp1.Inventory).ToNot(gm.BeNil())
		gm.Expect(resp2.Inventory).ToNot(gm.BeNil())

		gm.Expect(resp1.Inventory.VcenterId).To(gm.Equal(resp2.Inventory.VcenterId))
		gm.Expect(resp1.Inventory.Vcenter).ToNot(gm.BeNil())
		gm.Expect(resp2.Inventory.Vcenter).ToNot(gm.BeNil())
	})
})
