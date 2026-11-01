package estimation

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTimeRequestDecodesSelectionAndOverrides(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"selection":{"include":{"vms":["vm-1"],"clusters":["cluster-a"]}},"estimationSchema":["storage-offload"],"params":{"work_hours_per_day":6}}`))
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	var body timeRequest
	if err := decode(context, &body); err != nil {
		t.Fatal(err)
	}
	selected := body.selection().Include
	if len(selected.VMIDs) != 1 || selected.VMIDs[0] != "vm-1" || len(selected.Clusters) != 1 || selected.Clusters[0] != "cluster-a" {
		t.Fatalf("selection = %+v", selected)
	}
	if len(body.options().Schemas) != 1 || body.options().Schemas[0] != "storage-offload" || body.options().Params["work_hours_per_day"] != float64(6) {
		t.Fatalf("options = %+v", body.options())
	}
}

func TestUnknownSelectorIsRejected(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"selection":{"include":{"all":true},"exclude":{"vms":["vm-1"]}}}`))
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	var body selectionRequest
	if err := decode(context, &body); err == nil {
		t.Fatal("unsupported exclude selector was accepted")
	}
}
