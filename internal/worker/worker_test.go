package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunOnceDeliversAndAcknowledgesEvent(t *testing.T) {
	requestQuery := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestQuery <- request.URL.RawQuery
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	config, endpointID := testRuntimeConfig(t, server.URL+"/postback/token")
	writeTestEvent(t, config.QueueRoot, "pending", "event.json", Event{
		Version: 2, EndpointID: endpointID, Code: "click_install", AdID: "42", SubID: "sub-1",
	})

	if err := RunOnce(context.Background(), config, BuildInfo{Version: "test", Commit: "abc"}); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	select {
	case query := <-requestQuery:
		if query != "ad_id=42&code=click_install&subid=sub-1" {
			t.Fatalf("request query = %q", query)
		}
	case <-time.After(time.Second):
		t.Fatal("postback request was not received")
	}
	if _, err := os.Stat(filepath.Join(config.QueueRoot, "pending", "event.json")); !os.IsNotExist(err) {
		t.Fatalf("delivered event still exists: %v", err)
	}
}

func TestRunOnceRetriesTemporaryFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	config, endpointID := testRuntimeConfig(t, server.URL+"/postback/token")
	writeTestEvent(t, config.QueueRoot, "pending", "retry.json", Event{
		Version: 2, EndpointID: endpointID, Code: "click_install", AdID: "42", SubID: "sub-1",
	})

	if err := RunOnce(context.Background(), config, BuildInfo{Version: "test"}); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	payload, err := os.ReadFile(filepath.Join(config.QueueRoot, "retry", "retry.json"))
	if err != nil {
		t.Fatal(err)
	}
	var event Event
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatal(err)
	}
	if event.Attempts != 1 || event.NextAttemptAt <= time.Now().Unix() {
		t.Fatalf("retried event = %#v", event)
	}
}

func TestRunOnceDeadLettersPermanentFailureWithoutTokenLeak(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = response.Write([]byte(`{"reason":"unknown_code","detail":"secret-token sub-1"}`))
	}))
	defer server.Close()
	config, endpointID := testRuntimeConfig(t, server.URL+"/postback/secret-token")
	writeTestEvent(t, config.QueueRoot, "pending", "failed.json", Event{
		Version: 2, EndpointID: endpointID, Code: "click_install", AdID: "42", SubID: "sub-1",
	})

	if err := RunOnce(context.Background(), config, BuildInfo{Version: "test"}); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(config.QueueRoot, "failed", "failed.json")); err != nil {
		t.Fatalf("dead-letter event: %v", err)
	}
	logPayload, err := os.ReadFile(config.LogFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logPayload), "secret-token") || strings.Contains(string(logPayload), "sub-1") {
		t.Fatalf("worker log leaks event credentials: %s", logPayload)
	}
	if !strings.Contains(string(logPayload), "401 Unauthorized: unknown_code") {
		t.Fatalf("worker log omits safe response reason: %s", logPayload)
	}
}

func TestResponseErrorTextRejectsUnsafeServerDetails(t *testing.T) {
	if got := responseErrorText("400 Bad Request", []byte(`{"reason":"token abc"}`)); got != "400 Bad Request" {
		t.Fatalf("responseErrorText() = %q", got)
	}
}

func TestRecoverMovesInterruptedEventBackToPending(t *testing.T) {
	config, endpointID := testRuntimeConfig(t, "https://app.lazyarb.com/postback/token")
	writeTestEvent(t, config.QueueRoot, "processing", "event.json", Event{
		Version: 2, EndpointID: endpointID, Code: "click_install", AdID: "42", SubID: "sub-1",
	})
	queue := &queue{root: config.QueueRoot, log: &logger{path: config.LogFile}}
	recovered, err := queue.recover()
	if err != nil {
		t.Fatalf("recover() error = %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recover() = %d, want 1", recovered)
	}
	if _, err := os.Stat(filepath.Join(config.QueueRoot, "pending", "event.json")); err != nil {
		t.Fatalf("pending event: %v", err)
	}
}

func testRuntimeConfig(t *testing.T, endpoint string) (Config, string) {
	t.Helper()
	root := t.TempDir()
	queueRoot := filepath.Join(root, "queue")
	for _, directory := range queueDirectories {
		if err := os.MkdirAll(filepath.Join(queueRoot, directory), 0o770); err != nil {
			t.Fatal(err)
		}
	}
	endpointsRoot := filepath.Join(root, "endpoints")
	if err := os.MkdirAll(endpointsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	endpointID := strings.Repeat("c", 24)
	if err := os.WriteFile(filepath.Join(endpointsRoot, endpointID+".url"), []byte(endpoint), 0o600); err != nil {
		t.Fatal(err)
	}
	return Config{
		QueueRoot: queueRoot, EndpointsRoot: endpointsRoot, LogFile: filepath.Join(root, "worker.log"),
		BatchSize: 5, MaxAttempts: 3, PollInterval: 10 * time.Millisecond,
		ConnectTimeout: time.Second, RequestTimeout: 2 * time.Second, HeartbeatTTL: 30 * time.Second,
	}, endpointID
}

func writeTestEvent(t *testing.T, root, directory, name string, event Event) {
	t.Helper()
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, directory, name), payload, 0o660); err != nil {
		t.Fatal(err)
	}
}
