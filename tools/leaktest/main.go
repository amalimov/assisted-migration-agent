package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"
)

var (
	addr         = flag.String("addr", "http://localhost:8000", "agent address")
	xlsx         = flag.String("xlsx", "", "RVTools workbook to upload in each round")
	apiPrefix    = flag.String("api-prefix", "/api/v2", "API path prefix")
	pollInterval = flag.Duration("poll-interval", 5*time.Second, "polling interval")
	timeout      = flag.Duration("timeout", 10*time.Minute, "max wait per collection")
)

func main() {
	flag.Parse()
	if *xlsx == "" {
		log("usage: go run ./tools/leaktest -xlsx /path/to/export.xlsx")
		os.Exit(2)
	}

	log("=== DB Connection Leak Test ===")
	log("agent: %s", *addr)

	log("")
	log("--- Round 1: first collection ---")
	if !startCollection() {
		return
	}
	if !pollUntilDone("round-1") {
		return
	}
	go func() {
		for {
			if err := probeReadEndpoints("after round-1"); err != nil {
				os.Exit(1)
			}
			<-time.After(100 * time.Microsecond)
		}
	}()

	log("")
	log("--- Round 2: second collection ---")
	if !startCollection() {
		return
	}

	if !pollUntilDone("round-2") {
		return
	}
	_ = probeReadEndpoints("after round-2")

	log("")
	log("=== Done ===")
}

func startCollection() bool {
	file, err := os.Open(*xlsx)
	if err != nil {
		log("FAIL: opening workbook: %v", err)
		return false
	}
	defer file.Close()
	var bodyBuf bytes.Buffer
	writer := multipart.NewWriter(&bodyBuf)
	part, err := writer.CreateFormFile("files", "leaktest.xlsx")
	if err != nil {
		log("FAIL: multipart form: %v", err)
		return false
	}
	if _, err := io.Copy(part, file); err != nil {
		log("FAIL: reading workbook: %v", err)
		return false
	}
	if err := writer.Close(); err != nil {
		log("FAIL: multipart form: %v", err)
		return false
	}
	req, err := http.NewRequest(http.MethodPost, *addr+*apiPrefix+"/collector/rvtools", &bodyBuf)
	if err != nil {
		log("FAIL: request: %v", err)
		return false
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log("FAIL: upload: %v", err)
		return false
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(resp.Body)
	status, body := resp.StatusCode, string(response)
	if status != http.StatusAccepted && status != http.StatusOK {
		log("FAIL: POST /collector/rvtools → %d: %s", status, body)
		return false
	}
	log("POST /collector/rvtools → %d", status)
	return true
}

func pollUntilDone(label string) bool {
	deadline := time.Now().Add(*timeout)
	lastState := ""

	for time.Now().Before(deadline) {
		status, body := doRequest("GET", "/collector", "")
		if status != http.StatusOK {
			log("FAIL: GET /collector → %d: %s", status, body)
			return false
		}

		var resp map[string]any
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			log("FAIL: bad JSON from GET /collector: %s", body)
			return false
		}

		state, _ := resp["status"].(string)
		errMsg, _ := resp["error"].(string)

		if state != lastState {
			if errMsg != "" {
				log("[%s] GET /collector → state=%s error=%q", label, state, errMsg)
			} else {
				log("[%s] GET /collector → state=%s", label, state)
			}
			lastState = state
		}

		switch state {
		case "collected":
			return true
		case "error":
			log("[%s] collection ended with error: %s", label, errMsg)
			return false
		}

		time.Sleep(*pollInterval)
	}

	log("FAIL: [%s] timed out after %s", label, *timeout)
	return false
}

func probeReadEndpoints(label string) error {
	endpoints := []string{
		"/inventory",
		"/groups?page=1&pageSize=20",
	}

	log("[%s] probing read endpoints:", label)
	for _, ep := range endpoints {
		status, body := doRequest("GET", ep, "")
		if status != http.StatusOK {
			log("  GET %s → %d ← FAILURE: %s", ep, status, truncate(body, 200))
			return errors.New("inventory")
		} else {
			log("  GET %s → %d OK", ep, status)
		}
	}

	log("[%s] probing group create/delete:", label)
	createBody := `{"name":"_leaktest_probe","filter":"name = '_leaktest_'"}`
	status, body := doRequest("POST", "/groups", createBody)
	if status != http.StatusCreated {
		log("  POST /groups → %d ← FAILURE: %s", status, truncate(body, 200))
		return errors.New("create group")
	}
	log("  POST /groups → %d OK", status)

	var created map[string]any
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		log("  cannot parse created group: %s", truncate(body, 200))
		return err
	}
	groupID, _ := created["id"].(string)
	if groupID == "" {
		log("  created group has no id: %s", truncate(body, 200))
		return errors.New("group has no id")
	}

	status, body = doRequest("DELETE", "/groups/"+groupID, "")
	if status != http.StatusNoContent {
		log("  DELETE /groups/%s → %d ← FAILURE: %s", groupID, status, truncate(body, 200))
		return errors.New("delete group failed")
	}
	log("  DELETE /groups/%s → %d OK", groupID, status)
	return nil
}

func doRequest(method, path, body string) (int, string) {
	url := *addr + *apiPrefix + path

	var reqBody io.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return 0, fmt.Sprintf("request error: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Sprintf("connection error: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(respBody)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func log(format string, args ...any) {
	ts := time.Now().Format("15:04:05")
	fmt.Printf("[%s] %s\n", ts, fmt.Sprintf(format, args...))
}
