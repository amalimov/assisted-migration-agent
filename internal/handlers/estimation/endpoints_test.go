package estimation_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	agentapi "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	agenthandlers "github.com/kubev2v/assisted-migration-agent/agent/handlers/v2"
	analyzerapi "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
	analyzerhandlers "github.com/kubev2v/assisted-migration-agent/analyzer/handlers/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services/estimations"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/internal/vmselection"
	"github.com/kubev2v/assisted-migration-agent/test"
)

func addCollection(t *testing.T, pool *store.Pool, id string) *store.Store {
	t.Helper()
	db, err := pool.NewDatabase(id, filepath.Join(t.TempDir(), id+".duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background(), func(ctx context.Context, sqlDB *sql.DB) error {
		st, err := db.Store()
		if err != nil {
			return err
		}
		if err := duckdb_parser.New(st.Querier(), test.NewMockValidator()).Init(); err != nil {
			return err
		}
		return migrations.RunCollection(ctx, sqlDB, id)
	}); err != nil {
		t.Fatal(err)
	}
	pool.Add(db)
	st, err := db.Store()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func insert(t *testing.T, st *store.Store, id, osName string, excluded bool, diskMiB int) {
	t.Helper()
	_, err := st.Querier().ExecContext(context.Background(), `INSERT INTO vinfo ("VM ID", "VM", "Cluster", "migration_excluded", "OS according to the VMware Tools", "OsDiskComplexity") VALUES (?, ?, 'cluster-a', ?, ?, 2)`, id, id, excluded, osName)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.Querier().ExecContext(context.Background(), `INSERT INTO vdisk ("VM ID", "Capacity MiB") VALUES (?, ?)`, id, diskMiB)
	if err != nil {
		t.Fatal(err)
	}
}

func routers(pool *store.Pool) (*gin.Engine, *gin.Engine) {
	resolver := vmselection.NewResolver(pool)
	service := estimations.NewService()
	agent := gin.New()
	agentapi.RegisterHandlers(agent.Group("/api/v2"), agenthandlers.NewHandler(config.Configuration{}, nil,
		agenthandlers.EstimationDependencies{Resolver: resolver, Service: service}))
	analyzer := gin.New()
	analyzerapi.RegisterHandlers(analyzer.Group("/api/v1"), analyzerhandlers.NewHandler(config.Configuration{}, nil,
		analyzerhandlers.EstimationDependencies{Resolver: resolver, Service: service}))
	return agent, analyzer
}

func post(t *testing.T, router *gin.Engine, path, body string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, request)
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("%s: invalid JSON response (%d): %s: %v", path, w.Code, w.Body.String(), err)
	}
	return w.Code, response
}

func TestAgentAndAnalyzerEstimationEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pool := store.NewPool(time.Minute)
	t.Cleanup(pool.Close)
	first := addCollection(t, pool, "first")
	insert(t, first, "vm-first", "Red Hat Enterprise Linux 8", false, 400)
	insert(t, first, "vm-excluded", "Windows Server 2019", true, 1024)
	group, err := first.Group().Create(context.Background(), models.Group{Name: "first-group", Filter: "name = 'vm-first'"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Querier().ExecContext(context.Background(), `INSERT INTO group_matches (group_id, vm_ids) VALUES (?, ['vm-first', 'vm-excluded'])`, group.ID); err != nil {
		t.Fatal(err)
	}
	second := addCollection(t, pool, "second")
	insert(t, second, "vm-latest", "Windows Server 2022", false, 800)
	agent, analyzer := routers(pool)
	all := `{"selection":{"include":{"all":true}}}`
	timeBody := `{"selection":{"include":{"all":true}},"estimationSchema":["storage-offload"],"params":{"post_migration_engineers":2}}`
	for _, tc := range []struct {
		path string
		body string
		key  string
	}{
		{"complexity-estimation", all, "complexityByDisk"},
		{"migration-estimation", timeBody, "estimation"},
		{"migration-estimation/by-complexity", timeBody, "complexityByOsDisk"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			for _, suffix := range []string{"collections/first/", "collections/second/", ""} {
				agentCode, agentResponse := post(t, agent, "/api/v2/"+suffix+tc.path, tc.body)
				analyzerCode, analyzerResponse := post(t, analyzer, "/api/v1/"+suffix+tc.path, tc.body)
				if agentCode != 200 || analyzerCode != 200 {
					t.Fatalf("%s: Agent %d %v; Analyzer %d %v", suffix, agentCode, agentResponse, analyzerCode, analyzerResponse)
				}
				if !reflect.DeepEqual(agentResponse, analyzerResponse) {
					t.Fatalf("Agent/Analyzer differ for %s: %v / %v", suffix, agentResponse, analyzerResponse)
				}
				if agentResponse[tc.key] == nil {
					t.Fatalf("missing %s in %v", tc.key, agentResponse)
				}
				if suffix == "collections/second/" {
					latestCode, latestResponse := post(t, agent, "/api/v2/"+tc.path, tc.body)
					if latestCode != 200 || !reflect.DeepEqual(latestResponse, agentResponse) {
						t.Fatalf("latest route did not delegate to second collection: %d %v / %v", latestCode, latestResponse, agentResponse)
					}
				}
			}
		})
	}
	for _, tc := range []struct {
		path string
		body string
		want int
	}{
		{"/api/v2/collections/missing/complexity-estimation", all, 404},
		{"/api/v2/collections/first/complexity-estimation", `{"selection":{"include":{"vms":["vm-excluded"]}}}`, 400},
		{"/api/v2/collections/first/complexity-estimation", `{"selection":{"include":{}}}`, 400},
		{"/api/v2/collections/first/complexity-estimation", `{"selection":{"include":{"all":true},"exclude":{"vms":["vm-first"]}}}`, 400},
		{"/api/v2/collections/first/complexity-estimation", `{"selection":{"include":{"vcenters":["vc-1"]}}}`, 400},
		{"/api/v2/collections/first/complexity-estimation", `{"selection":{"include":{"all":true,"vms":["vm-first"]}}}`, 400},
		{"/api/v2/collections/first/migration-estimation", `{"selection":{"include":{"all":true}},"params":{"post_migration_engineers":0}}`, 400},
		{"/api/v2/collections/first/migration-estimation", `{"selection":{"include":{"all":true}},"estimationSchema":["invalid"]}`, 400},
		{"/api/v2/collections/first/complexity-estimation", `{"selection":{"include":{"groups":["00000000-0000-0000-0000-000000000000"]}}}`, 404},
	} {
		code, response := post(t, agent, tc.path, tc.body)
		if code != tc.want {
			t.Fatalf("%s: got %d %v, want %d", tc.path, code, response, tc.want)
		}
	}
	for _, body := range []string{
		`{"selection":{"include":{"vms":["vm-first"]}}}`,
		fmt.Sprintf(`{"selection":{"include":{"groups":[%q]}}}`, group.ID.String()),
		`{"selection":{"include":{"clusters":["cluster-a"]}}}`,
		fmt.Sprintf(`{"selection":{"include":{"vms":["vm-first","vm-first"],"groups":[%q],"clusters":["cluster-a"]}}}`, group.ID.String()),
	} {
		for _, product := range []struct {
			prefix string
			router *gin.Engine
		}{{"/api/v2", agent}, {"/api/v1", analyzer}} {
			code, response := post(t, product.router, product.prefix+"/collections/first/complexity-estimation", body)
			if code != 200 {
				t.Fatalf("selection %s: %d %v", body, code, response)
			}
			var selected float64
			for _, item := range response["complexityByDisk"].([]any) {
				selected += item.(map[string]any)["vmCount"].(float64)
			}
			if selected != 1 {
				t.Fatalf("selection %s: counted %.0f VMs, want one eligible VM", body, selected)
			}
		}
	}
}

func TestLatestEstimationRoutesWithoutCollection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pool := store.NewPool(time.Minute)
	t.Cleanup(pool.Close)
	agent, analyzer := routers(pool)
	for _, product := range []struct {
		prefix string
		router *gin.Engine
	}{{"/api/v2", agent}, {"/api/v1", analyzer}} {
		for _, operation := range []string{"complexity-estimation", "migration-estimation", "migration-estimation/by-complexity"} {
			code, response := post(t, product.router, product.prefix+"/"+operation, `{"selection":{"include":{"all":true}}}`)
			if code != http.StatusNotFound {
				t.Fatalf("%s: got %d %v, want 404", operation, code, response)
			}
		}
	}
}
