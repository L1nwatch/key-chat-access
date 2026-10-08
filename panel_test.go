package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPanelInstallAndRestore(t *testing.T) {
	// Exercise the filesystem contract with a small fixture; the integration
	// test additionally uses the exact pinned, released upstream HTML.
	original := []byte("<script>I=async t=>{if(D||C||T)return;original()}</script>")
	prior := bytes.Clone(panelSourceJSON)
	panelSourceJSON, _ = json.Marshal(panelSource{SHA256: digest(original), Anchor: "I=async t=>{if(D||C||T)return;"})
	t.Cleanup(func() { panelSourceJSON = prior })
	dir := t.TempDir()
	filename := filepath.Join(dir, "management.html")
	if err := os.WriteFile(filename, original, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := updatePanel(filename, "install", "stale"); err == nil {
		t.Fatal("stale digest accepted")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("stale request created files")
	}
	state, err := updatePanel(filename, "install", digest(original))
	if err != nil || !state.Installed {
		t.Fatalf("install failed: %v", err)
	}
	patched, _, _ := readPanel(filename)
	if !bytes.Contains(patched, []byte(panelMarker)) || bytes.Count(patched, []byte(panelMarker)) != 1 {
		t.Fatal("missing or repeated shortcut")
	}
	backup := filepath.Join(dir, ".key-chat-access-panel-backup-"+digest(original)+".html")
	saved, mode, err := readPanel(backup)
	if err != nil || !bytes.Equal(saved, original) || mode != 0600 {
		t.Fatal("original panel was not backed up privately")
	}
	if _, err := updatePanel(filename, "install", state.SHA256); err != nil {
		t.Fatal("repeated install was not idempotent")
	}
	if err := os.WriteFile(backup, []byte("corrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := updatePanel(filename, "restore", state.SHA256); err == nil {
		t.Fatal("corrupted backup accepted")
	}
	current, _, _ := readPanel(filename)
	if !bytes.Equal(current, patched) {
		t.Fatal("failed restore changed the panel")
	}
	_ = os.WriteFile(backup, original, 0600)
	restored, err := updatePanel(filename, "restore", state.SHA256)
	if err != nil || restored.SHA256 != digest(original) || restored.Installed {
		t.Fatalf("restore failed: %v", err)
	}
	current, _, _ = readPanel(filename)
	if !bytes.Equal(current, original) {
		t.Fatal("restore did not preserve the original bytes")
	}
}

func TestPanelRejectsUnsupportedFiles(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "management.html")
	data := []byte("unrecognized panel version")
	_ = os.WriteFile(filename, data, 0644)
	if _, err := updatePanel(filename, "install", digest(data)); err == nil {
		t.Fatal("unsupported panel accepted")
	}
	current, _, _ := readPanel(filename)
	if !bytes.Equal(current, data) {
		t.Fatal("unsupported panel changed")
	}
	link := filepath.Join(dir, "link.html")
	if err := os.Symlink(filename, link); err != nil {
		t.Fatal(err)
	}
	if _, err := updatePanel(link, "install", digest(data)); err == nil {
		t.Fatal("symbolic link accepted")
	}
}

func TestPanelLocationMatchesHostOverrides(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MANAGEMENT_STATIC_PATH", dir)
	got, err := managementPanelPath()
	if err != nil || got != filepath.Join(dir, "management.html") {
		t.Fatalf("static directory override: %q %v", got, err)
	}
	t.Setenv("MANAGEMENT_STATIC_PATH", filepath.Join(dir, "management.html"))
	got, _ = managementPanelPath()
	if got != filepath.Join(dir, "management.html") {
		t.Fatal("static file override was not respected")
	}
	t.Setenv("MANAGEMENT_STATIC_PATH", "")
	t.Setenv("WRITABLE_PATH", dir)
	got, _ = managementPanelPath()
	if got != filepath.Join(dir, "static", "management.html") {
		t.Fatal("writable directory override was not respected")
	}
}
