package durablefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublishPreservesExistingUnlessReplaceRequested(t *testing.T) {
	dir := t.TempDir()
	target, source := filepath.Join(dir, "data"), filepath.Join(dir, "temp")
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Publish(source, target, false); err == nil {
		t.Fatal("overwrote existing file")
	}
	raw, _ := os.ReadFile(target)
	if string(raw) != "old" {
		t.Fatal("existing file changed")
	}
	if err := Publish(source, target, true); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(target)
	if string(raw) != "new" {
		t.Fatal("replacement lost")
	}
	if err := Publish(source, target, true); err == nil {
		t.Fatal("missing source error swallowed")
	}
}
func TestPublishNewFile(t *testing.T) {
	dir := t.TempDir()
	source, target := filepath.Join(dir, "temp"), filepath.Join(dir, "data")
	if err := os.WriteFile(source, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Publish(source, target, false); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(target)
	if err != nil || string(raw) != "new" {
		t.Fatalf("publish: %s %v", raw, err)
	}
}
