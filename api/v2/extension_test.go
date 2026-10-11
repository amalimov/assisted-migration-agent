package v2_test

import (
	v2 "github.com/kubev2v/rvtools-analyser/api/v2"
	"github.com/kubev2v/rvtools-analyser/internal/models"
	"testing"
)

func TestVMConversionRetainsWorkbookFields(t *testing.T) {
	vm := models.VM{ID: "vm-1", Name: "example", PowerState: "poweredOn", ConnectionState: "connected", CpuCount: 2, CoresPerSocket: 1, MemoryMB: 4096, Labels: []string{"review"}}
	detail := v2.NewVirtualMachineDetailFromModel(vm)
	if detail.Id != vm.ID || detail.Name != vm.Name || detail.Labels == nil || len(*detail.Labels) != 1 {
		t.Fatalf("unexpected conversion: %+v", detail)
	}
}
