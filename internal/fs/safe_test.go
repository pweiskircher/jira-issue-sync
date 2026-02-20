package fs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSafeFSRejectsEscapingPaths(t *testing.T) {
	t.Parallel()

	safe, err := NewSafeFS(filepath.Join(t.TempDir(), ".issues"))
	if err != nil {
		t.Fatalf("expected safe fs, got error: %v", err)
	}

	err = safe.WriteFileAtomic("../escape.txt", []byte("nope"), 0o644)
	if !errors.Is(err, ErrPathEscapes) {
		t.Fatalf("expected ErrPathEscapes, got: %v", err)
	}
}

func TestSafeFSWriteAndRenameInsideRoot(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".issues")
	safe, err := NewSafeFS(root)
	if err != nil {
		t.Fatalf("expected safe fs, got error: %v", err)
	}

	if err := safe.WriteFileAtomic(filepath.Join("open", "PROJ-1.md"), []byte("content\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := safe.Rename(filepath.Join("open", "PROJ-1.md"), filepath.Join("closed", "PROJ-1.md")); err != nil {
		t.Fatalf("rename failed: %v", err)
	}

	resolved, err := safe.Resolve(filepath.Join("closed", "PROJ-1.md"))
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != "content\n" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}
