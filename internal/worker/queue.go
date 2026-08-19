package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

var queueDirectories = []string{"tmp", "pending", "processing", "retry", "failed", "state"}

type queue struct {
	root string
	log  *logger
}

type claimedEvent struct {
	path  string
	event Event
}

func (q *queue) validate() error {
	for _, directory := range queueDirectories {
		info, err := os.Stat(filepath.Join(q.root, directory))
		if err != nil || !info.IsDir() {
			return fmt.Errorf("queue directory is unavailable: %s", directory)
		}
	}
	return nil
}

func (q *queue) lock() (*os.File, error) {
	path := filepath.Join(q.root, "state", "worker.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o640)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("another worker is already running")
	}
	return file, nil
}

func (q *queue) recover() (int, error) {
	recovered := 0
	for _, source := range []string{"processing"} {
		files, err := q.list(source, 0)
		if err != nil {
			return recovered, err
		}
		for _, path := range files {
			if _, err := q.move(path, "pending"); err != nil {
				return recovered, err
			}
			recovered++
		}
	}

	tmpEntries, err := os.ReadDir(filepath.Join(q.root, "tmp"))
	if err != nil {
		return recovered, err
	}
	for _, entry := range tmpEntries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(q.root, "tmp", entry.Name())
		if strings.HasPrefix(entry.Name(), "worker-") && strings.HasSuffix(entry.Name(), ".writing") {
			_ = os.Remove(path)
			continue
		}
		if strings.HasPrefix(entry.Name(), "enqueue-") && strings.HasSuffix(entry.Name(), ".json.writing") {
			name := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "enqueue-"), ".writing")
			if _, err := readEvent(path); err != nil {
				if _, moveErr := q.moveAs(path, "failed", "incomplete-"+name); moveErr != nil {
					return recovered, moveErr
				}
				continue
			}
			if _, err := q.moveAs(path, "pending", name); err != nil {
				return recovered, err
			}
			recovered++
			continue
		}
		if strings.HasSuffix(entry.Name(), ".json") {
			if _, err := q.move(path, "pending"); err != nil {
				return recovered, err
			}
			recovered++
		}
	}
	return recovered, nil
}

func (q *queue) claim(limit int, now time.Time) ([]claimedEvent, error) {
	paths := make([]string, 0, limit)
	retryPaths, err := q.list("retry", limit*20)
	if err != nil {
		return nil, err
	}
	for _, path := range retryPaths {
		event, readErr := readEvent(path)
		if readErr != nil {
			q.deadLetter(path, "invalid_event", readErr)
			continue
		}
		if event.NextAttemptAt <= now.Unix() {
			paths = append(paths, path)
			if len(paths) == limit {
				break
			}
		}
	}
	if len(paths) < limit {
		pendingPaths, listErr := q.list("pending", limit-len(paths))
		if listErr != nil {
			return nil, listErr
		}
		paths = append(paths, pendingPaths...)
	}

	claimed := make([]claimedEvent, 0, len(paths))
	for _, path := range paths {
		event, readErr := readEvent(path)
		if readErr != nil {
			q.deadLetter(path, "invalid_event", readErr)
			continue
		}
		claimedPath, moveErr := q.move(path, "processing")
		if moveErr != nil {
			return claimed, moveErr
		}
		claimed = append(claimed, claimedEvent{path: claimedPath, event: event})
	}
	return claimed, nil
}

func (q *queue) retry(item claimedEvent, maxAttempts int) error {
	item.event.Attempts++
	if item.event.Attempts >= maxAttempts {
		if err := q.writeAtomically(item.path, item.event); err != nil {
			return err
		}
		_, err := q.move(item.path, "failed")
		return err
	}
	delay := time.Duration(1<<min(item.event.Attempts, 8)) * time.Second
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	item.event.NextAttemptAt = time.Now().Add(delay).Unix()
	if err := q.writeAtomically(item.path, item.event); err != nil {
		return err
	}
	_, err := q.move(item.path, "retry")
	return err
}

func (q *queue) deadLetter(path, reason string, cause error) {
	if _, err := q.move(path, "failed"); err != nil {
		q.log.write("critical", "dead_letter_move_failed", map[string]any{"reason": reason, "error": err.Error()})
		return
	}
	context := map[string]any{"reason": reason}
	if cause != nil {
		context["error"] = cause.Error()
	}
	q.log.write("error", "event_moved_to_dead_letter", context)
}

func (q *queue) acknowledge(path string) error {
	return os.Remove(path)
}

func (q *queue) release(path string) error {
	_, err := q.move(path, "pending")
	return err
}

func (q *queue) list(directory string, limit int) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(q.root, directory))
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".json") {
			paths = append(paths, filepath.Join(q.root, directory, entry.Name()))
		}
	}
	sort.Strings(paths)
	if limit > 0 && len(paths) > limit {
		paths = paths[:limit]
	}
	return paths, nil
}

func (q *queue) move(source, directory string) (string, error) {
	return q.moveAs(source, directory, filepath.Base(source))
}

func (q *queue) moveAs(source, directory, name string) (string, error) {
	target := filepath.Join(q.root, directory, name)
	if err := os.Rename(source, target); err != nil {
		return "", fmt.Errorf("move queue item to %s: %w", directory, err)
	}
	return target, nil
}

func (q *queue) writeAtomically(path string, event Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Join(q.root, "tmp"), "worker-*.writing")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o660); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
