package worker

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type logger struct {
	path string
	mu   sync.Mutex
}

func (log *logger) write(level, message string, context map[string]any) {
	payload := map[string]any{
		"time":    time.Now().UTC().Format(time.RFC3339Nano),
		"level":   level,
		"message": message,
	}
	if len(context) > 0 {
		payload["context"] = context
	}
	line, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "LazyArb worker: %s\n", message)
		return
	}

	log.mu.Lock()
	defer log.mu.Unlock()
	file, err := os.OpenFile(log.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o640)
	if err != nil {
		fmt.Fprintln(os.Stderr, string(line))
		return
	}
	_, writeErr := file.Write(append(line, '\n'))
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		fmt.Fprintln(os.Stderr, string(line))
	}
}
