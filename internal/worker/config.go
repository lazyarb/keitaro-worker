package worker

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	QueueRoot      string
	EndpointsRoot  string
	LogFile        string
	BatchSize      int
	MaxAttempts    int
	PollInterval   time.Duration
	ConnectTimeout time.Duration
	RequestTimeout time.Duration
	HeartbeatTTL   time.Duration
}

func ConfigFromEnv() (Config, error) {
	config := Config{
		QueueRoot:      envOrDefault("LAZYARB_QUEUE_ROOT", "/data/queue"),
		EndpointsRoot:  envOrDefault("LAZYARB_ENDPOINTS_ROOT", "/data/config/endpoints.d"),
		LogFile:        envOrDefault("LAZYARB_LOG_FILE", "/data/log/worker.log"),
		BatchSize:      25,
		MaxAttempts:    8,
		PollInterval:   200 * time.Millisecond,
		ConnectTimeout: time.Second,
		RequestTimeout: 5 * time.Second,
		HeartbeatTTL:   30 * time.Second,
	}

	var err error
	if config.BatchSize, err = envInt("LAZYARB_BATCH_SIZE", config.BatchSize, 1, 100); err != nil {
		return Config{}, err
	}
	if config.MaxAttempts, err = envInt("LAZYARB_MAX_ATTEMPTS", config.MaxAttempts, 1, 32); err != nil {
		return Config{}, err
	}
	if config.PollInterval, err = envDuration("LAZYARB_POLL_INTERVAL", config.PollInterval, 25*time.Millisecond, 10*time.Second); err != nil {
		return Config{}, err
	}
	if config.ConnectTimeout, err = envDuration("LAZYARB_CONNECT_TIMEOUT", config.ConnectTimeout, 100*time.Millisecond, 30*time.Second); err != nil {
		return Config{}, err
	}
	if config.RequestTimeout, err = envDuration("LAZYARB_REQUEST_TIMEOUT", config.RequestTimeout, config.ConnectTimeout, time.Minute); err != nil {
		return Config{}, err
	}
	if config.HeartbeatTTL, err = envDuration("LAZYARB_HEARTBEAT_TTL", config.HeartbeatTTL, 5*time.Second, 5*time.Minute); err != nil {
		return Config{}, err
	}

	for name, value := range map[string]string{
		"LAZYARB_QUEUE_ROOT":     config.QueueRoot,
		"LAZYARB_ENDPOINTS_ROOT": config.EndpointsRoot,
		"LAZYARB_LOG_FILE":       config.LogFile,
	} {
		if !strings.HasPrefix(value, "/") || strings.ContainsRune(value, '\x00') {
			return Config{}, fmt.Errorf("%s must be an absolute path", name)
		}
	}
	return config, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback, minimum, maximum int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minimum, maximum)
	}
	return parsed, nil
}

func envDuration(name string, fallback, minimum, maximum time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %s and %s", name, minimum, maximum)
	}
	return parsed, nil
}
