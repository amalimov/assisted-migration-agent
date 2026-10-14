package v2

import "testing"

func TestAgentAPIHasNoRVToolsUpload(t *testing.T) {
	spec, err := GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	if spec.Paths.Find("/collector/rvtools") != nil {
		t.Fatal("temporary RVTools upload is still in the Agent API")
	}
}
