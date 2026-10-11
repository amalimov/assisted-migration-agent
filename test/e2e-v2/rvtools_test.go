package e2e_v2_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kubev2v/migration-planner/pkg/opa"
	"github.com/xuri/excelize/v2"

	v2 "github.com/kubev2v/rvtools-analyser/api/v2"
	"github.com/kubev2v/rvtools-analyser/internal/config"
	handlers "github.com/kubev2v/rvtools-analyser/internal/handlers/v2"
	services "github.com/kubev2v/rvtools-analyser/internal/services"
	"github.com/kubev2v/rvtools-analyser/internal/store"
	"github.com/kubev2v/rvtools-analyser/internal/store/migrations"
)

// workbook generates a small RVTools-shaped source with stable VM IDs.
func workbook(t *testing.T, ids []string) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	sheet := func(name string, headers []string, rows [][]string) {
		t.Helper()
		if _, err := f.NewSheet(name); err != nil {
			t.Fatal(err)
		}
		for col, v := range headers {
			cell, _ := excelize.CoordinatesToCellName(col+1, 1)
			if err := f.SetCellValue(name, cell, v); err != nil {
				t.Fatal(err)
			}
		}
		for row, values := range rows {
			for col, v := range values {
				cell, _ := excelize.CoordinatesToCellName(col+1, row+2)
				if err := f.SetCellValue(name, cell, v); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	vmRows := make([][]string, 0, len(ids))
	for _, id := range ids {
		vmRows = append(vmRows, []string{id, id, "12345678-1234-1234-1234-123456789abc", "esxi-host-1", "2", "4096", "poweredOn", "cluster1", "datacenter1"})
	}
	sheet("vInfo", []string{"VM", "VM ID", "VI SDK UUID", "Host", "CPUs", "Memory", "Powerstate", "Cluster", "Datacenter"}, vmRows)
	sheet("vHost", []string{"Datacenter", "Cluster", "# Cores", "# CPU", "Object ID", "# Memory", "Model", "Vendor", "Host", "Config status"}, [][]string{{"datacenter1", "cluster1", "4", "2", "host-001", "16384", "ESXi", "VMware", "esxi-host-1", "green"}})
	sheet("vDatastore", []string{"Hosts", "Address", "Name", "Object ID", "Free MiB", "MHA", "Capacity MiB", "Type"}, [][]string{{"esxi-host-1", "10.0.0.1", "datastore1", "datastore-001", "524288", "false", "1048576", "VMFS"}})
	_ = f.DeleteSheet("Sheet1")
	var b bytes.Buffer
	if _, err := f.WriteTo(&b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func excelExtension(t *testing.T, dataDir string) {
	t.Helper()
	src := os.Getenv("DUCKDB_EXCEL_EXTENSION")
	if src == "" {
		src = filepath.Join("..", "..", "local-data", "v1.4.3", "linux_amd64", "excel.duckdb_extension")
	}
	file, err := os.Open(src)
	if err != nil {
		t.Skip("set DUCKDB_EXCEL_EXTENSION to run RVTools ingestion")
	}
	defer file.Close()
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("the local Excel extension fixture is linux_amd64")
	}
	dir := filepath.Join(dataDir, "v1.4.3", "linux_amd64")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(filepath.Join(dir, "excel.duckdb_extension"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTwoRVToolsUploadsCanBeCompared(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dataDir := t.TempDir()
	excelExtension(t, dataDir)
	pool := store.NewPool(time.Minute)
	defer pool.Close()
	mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(dataDir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
	if err != nil {
		t.Fatal(err)
	}
	if err := mainDB.Migrate(context.Background(), migrations.RunMain); err != nil {
		t.Fatal(err)
	}
	pool.Add(mainDB)
	cfg := config.NewConfigurationWithOptionsAndDefaults(config.WithAgent(config.Agent{ID: uuid.NewString(), DataFolder: dataDir}))
	validator, err := opa.NewValidator(map[string]string{"test.rego": "package io.konveyor.forklift.vmware\nconcerns := []\n"})
	if err != nil {
		t.Fatal(err)
	}
	mgr := services.NewServiceManager(services.WithConfig(cfg), services.WithPool(pool), services.WithOpaValidator(validator))
	if err := mgr.Initialize(); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop(context.Background())
	router := gin.New()
	v2.RegisterHandlers(router.Group("/api/v2"), handlers.NewHandler(*cfg, mgr))
	call := func(method, path string, body io.Reader, ctype string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v2"+path, body)
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	upload := func(workbooks ...[]string) {
		t.Helper()
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		for i, ids := range workbooks {
			part, err := mw.CreateFormFile("files", fmt.Sprintf("generated-%d.xlsx", i))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write(workbook(t, ids)); err != nil {
				t.Fatal(err)
			}
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		w := call(http.MethodPost, "/collector/rvtools", &body, mw.FormDataContentType())
		if w.Code != http.StatusAccepted {
			t.Fatalf("upload status %d: %s", w.Code, w.Body.String())
		}
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			w = call(http.MethodGet, "/collector", nil, "")
			var status v2.CollectorStatus
			if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
				t.Fatal(err)
			}
			if status.Status == v2.CollectorStatusStatusCollected {
				return
			}
			if status.Status == v2.CollectorStatusStatusError {
				t.Fatalf("collection error: %s", w.Body.String())
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("collection timed out")
	}
	collections := func() []v2.Collection {
		t.Helper()
		w := call(http.MethodGet, "/collections", nil, "")
		if w.Code != http.StatusOK {
			t.Fatalf("collections status %d: %s", w.Code, w.Body.String())
		}
		var response v2.CollectionListResponse
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response.Collections
	}
	upload([]string{"vm-001", "vm-002"})
	first := collections()
	if len(first) != 1 {
		t.Fatalf("first upload produced %d collections", len(first))
	}
	upload([]string{"vm-001", "vm-002", "vm-003"})
	second := collections()
	if len(second) != 2 || second[0].Id == second[1].Id {
		t.Fatalf("uploads did not produce distinct collections: %+v", second)
	}
	vmList := call(http.MethodGet, "/virtualmachines", nil, "")
	if vmList.Code != http.StatusOK || !strings.Contains(vmList.Body.String(), "vm-003") {
		t.Fatalf("VM list: %d %s", vmList.Code, vmList.Body.String())
	}
	other := second[0].Id
	if other == first[0].Id {
		other = second[1].Id
	}
	comparison := call(http.MethodGet, fmt.Sprintf("/collections/compare/%s/%s", first[0].Id, other), nil, "")
	if comparison.Code != http.StatusOK {
		t.Fatalf("comparison: %d %s", comparison.Code, comparison.Body.String())
	}
	var summary v2.CollectionComparisonSummary
	if err := json.Unmarshal(comparison.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Diff.TotalVMs.Delta != 1 {
		t.Fatalf("unexpected comparison: %+v", summary)
	}
	diff := call(http.MethodGet, fmt.Sprintf("/collections/compare/%s/%s/total", first[0].Id, other), nil, "")
	if diff.Code != http.StatusOK {
		t.Fatalf("comparison diff: %d %s", diff.Code, diff.Body.String())
	}
	var totalDiff v2.CollectionComparisonDiff
	if err := json.Unmarshal(diff.Body.Bytes(), &totalDiff); err != nil {
		t.Fatal(err)
	}
	if totalDiff.OnlyInB.Total != 1 || len(totalDiff.OnlyInB.VmIds) != 1 || totalDiff.OnlyInB.VmIds[0] != "vm-003" {
		t.Fatalf("unexpected VM diff: %+v", totalDiff)
	}
	inventory := call(http.MethodGet, "/inventory", nil, "")
	if inventory.Code != http.StatusOK || !strings.Contains(inventory.Body.String(), `"totalMigratable":3`) {
		t.Fatalf("inventory: %d %s", inventory.Code, inventory.Body.String())
	}
	export := call(http.MethodGet, fmt.Sprintf("/collections/%s/export?scope=vms&format=zip", other), nil, "")
	if export.Code != http.StatusOK || !bytes.HasPrefix(export.Body.Bytes(), []byte("PK")) {
		t.Fatalf("VM export: %d %s", export.Code, export.Body.String())
	}
	upload([]string{"vm-004"}, []string{"vm-005"})
	if len(collections()) != 3 {
		t.Fatal("one multipart upload with two workbooks did not create one collection")
	}
	merged := call(http.MethodGet, "/virtualmachines", nil, "")
	if merged.Code != http.StatusOK || !strings.Contains(merged.Body.String(), "vm-004") || !strings.Contains(merged.Body.String(), "vm-005") {
		t.Fatalf("merged VM list: %d %s", merged.Code, merged.Body.String())
	}
	var invalidBody bytes.Buffer
	invalidForm := multipart.NewWriter(&invalidBody)
	invalidFile, err := invalidForm.CreateFormFile("files", "invalid.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invalidFile.Write([]byte("not an Excel workbook")); err != nil {
		t.Fatal(err)
	}
	if err := invalidForm.Close(); err != nil {
		t.Fatal(err)
	}
	if w := call(http.MethodPost, "/collector/rvtools", &invalidBody, invalidForm.FormDataContentType()); w.Code != http.StatusAccepted {
		t.Fatalf("invalid upload was not queued: %d %s", w.Code, w.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		w := call(http.MethodGet, "/collector", nil, "")
		var status v2.CollectorStatus
		if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
			t.Fatal(err)
		}
		if status.Status == v2.CollectorStatusStatusError {
			if status.Error == nil || *status.Error == "" {
				t.Fatal("invalid workbook produced no error message")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("invalid workbook did not fail: %s", w.Body.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(collections()) != 3 {
		t.Fatal("invalid workbook was published as a collection")
	}
}
