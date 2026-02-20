package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	internalfs "github.com/pat/jira-issue-sync/internal/fs"
)

func TestStoreEnsureLayoutCreatesContractDirectories(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".issues")
	store, err := New(root)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	if err := store.EnsureLayout(); err != nil {
		t.Fatalf("ensure layout failed: %v", err)
	}

	mustExist := []string{"open", "closed", filepath.Join(".sync", "originals")}
	for _, path := range mustExist {
		info, err := os.Stat(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("expected directory %q: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %q to be a directory", path)
		}
	}
}

func TestStorePersistsIssueAndSnapshotDeterministically(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".issues")
	store, err := New(root)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	issuePath, err := store.WriteIssue(IssueStateOpen, "PROJ-42", "Fix Login Flow", "line1\r\nline2")
	if err != nil {
		t.Fatalf("write issue failed: %v", err)
	}
	if issuePath != filepath.Join("open", "PROJ-42-fix-login-flow.md") {
		t.Fatalf("unexpected issue path: %q", issuePath)
	}

	data, err := store.ReadFile(issuePath)
	if err != nil {
		t.Fatalf("read issue failed: %v", err)
	}
	if string(data) != "line1\nline2\n" {
		t.Fatalf("unexpected issue content: %q", string(data))
	}

	snapshotPath, err := store.WriteOriginalSnapshot("PROJ-42", "snapshot")
	if err != nil {
		t.Fatalf("write snapshot failed: %v", err)
	}
	if snapshotPath != filepath.Join(".sync", "originals", "PROJ-42.md") {
		t.Fatalf("unexpected snapshot path: %q", snapshotPath)
	}
}

func TestStoreCacheRoundTripDeterministic(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".issues")
	store, err := New(root)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	if err := store.SaveCache(Cache{Issues: map[string]CacheEntry{
		"PROJ-2": {Path: filepath.Join("open", "PROJ-2.md"), Status: "open"},
		"PROJ-1": {Path: filepath.Join("closed", "PROJ-1.md"), Status: "closed"},
	}}); err != nil {
		t.Fatalf("save cache failed: %v", err)
	}

	loaded, err := store.LoadCache()
	if err != nil {
		t.Fatalf("load cache failed: %v", err)
	}
	if loaded.Version != CacheSchemaVersionV1 {
		t.Fatalf("unexpected cache version: %q", loaded.Version)
	}
	if len(loaded.Issues) != 2 {
		t.Fatalf("unexpected cache issue count: %d", len(loaded.Issues))
	}

	encoded, err := store.ReadFile(filepath.Join(".sync", "cache.json"))
	if err != nil {
		t.Fatalf("read cache file failed: %v", err)
	}
	expected := "{\n  \"version\": \"1\",\n  \"issues\": {\n    \"PROJ-1\": {\n      \"path\": \"closed/PROJ-1.md\",\n      \"status\": \"closed\"\n    },\n    \"PROJ-2\": {\n      \"path\": \"open/PROJ-2.md\",\n      \"status\": \"open\"\n    }\n  }\n}\n"
	if string(encoded) != expected {
		t.Fatalf("unexpected cache file:\n%s", string(encoded))
	}
}

func TestStoreWriteRejectsEscapingPath(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".issues")
	store, err := New(root)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	err = store.WriteFile("../escape", []byte("x"))
	if !errors.Is(err, internalfs.ErrPathEscapes) {
		t.Fatalf("expected path safety error, got: %v", err)
	}
}
