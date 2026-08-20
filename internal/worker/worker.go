package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var responseReasonPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type BuildInfo struct {
	Version string
	Commit  string
}

type runtime struct {
	config Config
	build  BuildInfo
	queue  *queue
	log    *logger
	client *http.Client
}

type deliveryResult struct {
	item        claimedEvent
	ok          bool
	retryable   bool
	interrupted bool
	httpCode    int
	errorText   string
	durationMS  float64
}

func Run(ctx context.Context, config Config, build BuildInfo) error {
	runtime, lock, err := newRuntime(config, build)
	if err != nil {
		return err
	}
	defer lock.Close()

	recovered, err := runtime.queue.recover()
	if err != nil {
		return err
	}
	runtime.log.write("info", "worker_started", map[string]any{"version": build.Version, "commit": build.Commit, "recovered": recovered})
	if err := runtime.writeHeartbeat(); err != nil {
		return err
	}

	ticker := time.NewTicker(config.PollInterval)
	heartbeat := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			runtime.log.write("info", "worker_stopped", map[string]any{"reason": "signal"})
			return nil
		case <-heartbeat.C:
			if err := runtime.writeHeartbeat(); err != nil {
				runtime.log.write("error", "heartbeat_write_failed", map[string]any{"error": err.Error()})
			}
		case <-ticker.C:
			if _, err := runtime.processBatch(ctx); err != nil {
				runtime.log.write("error", "batch_failed", map[string]any{"error": err.Error()})
			}
		}
	}
}

func RunOnce(ctx context.Context, config Config, build BuildInfo) error {
	runtime, lock, err := newRuntime(config, build)
	if err != nil {
		return err
	}
	defer lock.Close()
	if _, err := runtime.queue.recover(); err != nil {
		return err
	}
	if err := runtime.writeHeartbeat(); err != nil {
		return err
	}
	_, err = runtime.processBatch(ctx)
	return err
}

func newRuntime(config Config, build BuildInfo) (*runtime, *os.File, error) {
	log := &logger{path: config.LogFile}
	queue := &queue{root: config.QueueRoot, log: log}
	if err := queue.validate(); err != nil {
		return nil, nil, err
	}
	lock, err := queue.lock()
	if err != nil {
		return nil, nil, err
	}
	dialer := &net.Dialer{Timeout: config.ConnectTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          config.BatchSize * 2,
		MaxIdleConnsPerHost:   config.BatchSize,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   config.ConnectTimeout,
		ResponseHeaderTimeout: config.RequestTimeout,
	}
	return &runtime{
		config: config,
		build:  build,
		queue:  queue,
		log:    log,
		client: &http.Client{Transport: transport, Timeout: config.RequestTimeout},
	}, lock, nil
}

func (runtime *runtime) processBatch(ctx context.Context) (int, error) {
	items, err := runtime.queue.claim(runtime.config.BatchSize, time.Now())
	if err != nil || len(items) == 0 {
		return len(items), err
	}

	results := make(chan deliveryResult, len(items))
	var wait sync.WaitGroup
	for _, item := range items {
		wait.Add(1)
		go func(item claimedEvent) {
			defer wait.Done()
			results <- runtime.deliver(ctx, item)
		}(item)
	}
	wait.Wait()
	close(results)

	for result := range results {
		contextFields := map[string]any{
			"code":        result.item.event.Code,
			"http_code":   result.httpCode,
			"duration_ms": result.durationMS,
		}
		if result.errorText != "" {
			contextFields["error"] = result.errorText
		}
		switch {
		case result.ok:
			if result.item.event.Diagnostics {
				runtime.log.write("info", "delivery_profile", contextFields)
			}
			if err := runtime.queue.acknowledge(result.item.path); err != nil {
				return len(items), err
			}
		case result.retryable:
			runtime.log.write("warning", "delivery_retry_scheduled", contextFields)
			if err := runtime.queue.retry(result.item, runtime.config.MaxAttempts); err != nil {
				return len(items), err
			}
		case result.interrupted:
			if err := runtime.queue.release(result.item.path); err != nil {
				return len(items), err
			}
		default:
			runtime.log.write("error", "delivery_rejected", contextFields)
			runtime.queue.deadLetter(result.item.path, "permanent_delivery_failure", errors.New(result.errorText))
		}
	}
	return len(items), nil
}

func (runtime *runtime) deliver(ctx context.Context, item claimedEvent) deliveryResult {
	startedAt := time.Now()
	result := deliveryResult{item: item}
	endpoint, err := resolveEndpoint(item.event, runtime.config.EndpointsRoot)
	if err != nil {
		result.errorText = err.Error()
		result.durationMS = milliseconds(time.Since(startedAt))
		return result
	}

	query := url.Values{}
	query.Set("code", item.event.Code)
	query.Set("ad_id", item.event.AdID)
	query.Set("subid", item.event.SubID)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil {
		result.errorText = err.Error()
		result.durationMS = milliseconds(time.Since(startedAt))
		return result
	}
	request.Header.Set("User-Agent", "LazyArb-Keitaro-Worker/2")
	response, err := runtime.client.Do(request)
	result.durationMS = milliseconds(time.Since(startedAt))
	if err != nil {
		result.interrupted = errors.Is(err, context.Canceled)
		result.retryable = !result.interrupted
		result.errorText = err.Error()
		return result
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	result.httpCode = response.StatusCode
	result.ok = response.StatusCode >= 200 && response.StatusCode < 300
	result.retryable = response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooEarly || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
	if !result.ok {
		result.errorText = responseErrorText(response.Status, responseBody)
	}
	return result
}

func responseErrorText(status string, body []byte) string {
	payload := struct {
		Reason string `json:"reason"`
		Error  string `json:"error"`
	}{}
	if json.Unmarshal(body, &payload) != nil {
		return status
	}
	reason := payload.Reason
	if reason == "" {
		reason = payload.Error
	}
	if !responseReasonPattern.MatchString(reason) {
		return status
	}
	return status + ": " + reason
}

func (runtime *runtime) writeHeartbeat() error {
	payload, err := json.Marshal(map[string]any{
		"time":    time.Now().UTC().Format(time.RFC3339Nano),
		"version": runtime.build.Version,
		"commit":  runtime.build.Commit,
	})
	if err != nil {
		return err
	}
	path := filepath.Join(runtime.config.QueueRoot, "state", "heartbeat.json")
	temporary, err := os.CreateTemp(filepath.Dir(path), ".heartbeat-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o640); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func CheckHealth(config Config) error {
	path := filepath.Join(config.QueueRoot, "state", "heartbeat.json")
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("worker heartbeat is unavailable: %w", err)
	}
	if age := time.Since(info.ModTime()); age < 0 || age > config.HeartbeatTTL {
		return fmt.Errorf("worker heartbeat is stale: %s", age.Round(time.Second))
	}
	return nil
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration.Round(time.Microsecond)) / float64(time.Millisecond)
}
