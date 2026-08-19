package worker

import (
	"strings"
	"testing"
	"time"
)

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("LAZYARB_QUEUE_ROOT", "/tmp/queue")
	t.Setenv("LAZYARB_ENDPOINTS_ROOT", "/tmp/config")
	t.Setenv("LAZYARB_LOG_FILE", "/tmp/worker.log")
	t.Setenv("LAZYARB_BATCH_SIZE", "12")
	t.Setenv("LAZYARB_CONNECT_TIMEOUT", "750ms")

	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv() error = %v", err)
	}
	if config.BatchSize != 12 || config.ConnectTimeout != 750*time.Millisecond {
		t.Fatalf("ConfigFromEnv() = %#v", config)
	}
}

func TestConfigFromEnvRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{name: "relative path", key: "LAZYARB_QUEUE_ROOT", value: "queue", want: "absolute path"},
		{name: "oversized batch", key: "LAZYARB_BATCH_SIZE", value: "101", want: "between 1 and 100"},
		{name: "short timeout", key: "LAZYARB_CONNECT_TIMEOUT", value: "20ms", want: "between 100ms"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(test.key, test.value)
			_, err := ConfigFromEnv()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ConfigFromEnv() error = %v, want containing %q", err, test.want)
			}
		})
	}
}
