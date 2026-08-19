package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveEndpointFromRegistry(t *testing.T) {
	root := t.TempDir()
	id := strings.Repeat("a", 24)
	want := "https://app.lazyarb.com/postback/token"
	if err := os.WriteFile(filepath.Join(root, id+".url"), []byte(want+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveEndpoint(Event{EndpointID: id}, root)
	if err != nil {
		t.Fatalf("resolveEndpoint() error = %v", err)
	}
	if got != want {
		t.Fatalf("resolveEndpoint() = %q, want %q", got, want)
	}
}

func TestResolveEndpointRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	id := strings.Repeat("b", 24)
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("https://app.lazyarb.com/postback/token"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, id+".url")); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveEndpoint(Event{EndpointID: id}, root); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("resolveEndpoint() error = %v", err)
	}
}

func TestEventValidationRejectsUnknownVersion(t *testing.T) {
	event := Event{
		Version:    1,
		EndpointID: strings.Repeat("c", 24),
		Code:       "click_install",
		AdID:       "123",
		SubID:      "abc",
	}
	if err := event.validate(); err == nil || !strings.Contains(err.Error(), "unsupported event version") {
		t.Fatalf("validate() error = %v", err)
	}
}
