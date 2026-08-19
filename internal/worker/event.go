package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxEventBytes = 64 * 1024

var (
	endpointIDPattern = regexp.MustCompile(`^[a-f0-9]{24}$`)
	codePattern       = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	digitsPattern     = regexp.MustCompile(`^[0-9]+$`)
)

type Event struct {
	Version        int    `json:"version,omitempty"`
	EndpointID     string `json:"endpoint_id,omitempty"`
	LegacyEndpoint string `json:"endpoint,omitempty"`
	Code           string `json:"code"`
	AdID           string `json:"ad_id"`
	SubID          string `json:"subid"`
	Attempts       int    `json:"attempts"`
	NextAttemptAt  int64  `json:"next_attempt_at"`
	Diagnostics    bool   `json:"diagnostics,omitempty"`
}

func readEvent(path string) (Event, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Event{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxEventBytes {
		return Event{}, errors.New("queue item must be a bounded regular file")
	}

	file, err := os.Open(path)
	if err != nil {
		return Event{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxEventBytes+1))
	var event Event
	if err := decoder.Decode(&event); err != nil {
		return Event{}, fmt.Errorf("decode queue item: %w", err)
	}
	if err := event.validate(); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (event Event) validate() error {
	if event.Version != 0 && event.Version != 2 {
		return fmt.Errorf("unsupported event version %d", event.Version)
	}
	if event.EndpointID == "" && event.LegacyEndpoint == "" {
		return errors.New("event has no endpoint reference")
	}
	if event.EndpointID != "" && !endpointIDPattern.MatchString(event.EndpointID) {
		return errors.New("event endpoint ID is invalid")
	}
	if !codePattern.MatchString(event.Code) {
		return errors.New("event code is invalid")
	}
	if !digitsPattern.MatchString(event.AdID) || len(event.AdID) > 64 {
		return errors.New("event ad ID is invalid")
	}
	if event.SubID == "" || len(event.SubID) > 4096 || strings.ContainsRune(event.SubID, '\x00') {
		return errors.New("event sub ID is invalid")
	}
	if event.Attempts < 0 || event.Attempts > 1000 {
		return errors.New("event attempts value is invalid")
	}
	return nil
}

func resolveEndpoint(event Event, endpointsRoot string) (string, error) {
	if event.EndpointID == "" {
		return validateEndpoint(event.LegacyEndpoint)
	}
	path := filepath.Join(endpointsRoot, event.EndpointID+".url")
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("read endpoint configuration: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		return "", errors.New("endpoint configuration must be a bounded regular file")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read endpoint configuration: %w", err)
	}
	return validateEndpoint(strings.TrimSpace(string(payload)))
}

func validateEndpoint(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("endpoint URL is invalid")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("endpoint URL contains unsupported components")
	}
	if matched, _ := regexp.MatchString(`/postback/[^/]+$`, parsed.EscapedPath()); !matched {
		return "", errors.New("endpoint URL path is invalid")
	}
	return parsed.String(), nil
}
