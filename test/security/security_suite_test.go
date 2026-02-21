package security_test

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pat/jira-issue-sync/internal/contracts"
	internalfs "github.com/pat/jira-issue-sync/internal/fs"
	httpclient "github.com/pat/jira-issue-sync/internal/http"
	"github.com/pat/jira-issue-sync/internal/lock"
)

func TestTokenRedactionPrimitiveDoesNotLeakSecrets(t *testing.T) {
	redactor := httpclient.NewRedactor("token-123", "Bearer super-secret")
	input := "auth failed with token-123 and header Bearer super-secret"

	redacted := redactor.Redact(input)

	if strings.Contains(redacted, "token-123") || strings.Contains(redacted, "Bearer super-secret") {
		t.Fatalf("redaction leaked sensitive values: %q", redacted)
	}
	if strings.Count(redacted, httpclient.RedactedPlaceholder) != 2 {
		t.Fatalf("expected deterministic placeholder count, got %q", redacted)
	}
}

func TestSafeFSRejectsPathTraversalAndAbsoluteTargets(t *testing.T) {
	safe, err := internalfs.NewSafeFS(filepath.Join(t.TempDir(), contracts.DefaultIssuesRootDir))
	if err != nil {
		t.Fatalf("new safe fs failed: %v", err)
	}

	if err := safe.WriteFileAtomic("../escape.md", []byte("nope"), 0o644); !errors.Is(err, internalfs.ErrPathEscapes) {
		t.Fatalf("expected ErrPathEscapes for traversal write, got %v", err)
	}
	if _, err := safe.Resolve("/tmp/escape.md"); !errors.Is(err, internalfs.ErrAbsolute) {
		t.Fatalf("expected ErrAbsolute for absolute paths, got %v", err)
	}
}

func TestFileLockStaleRecoveryPolicy(t *testing.T) {
	t.Run("recovers stale lock", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), contracts.DefaultSyncDir, "lock")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		if err := os.WriteFile(path, []byte("stale\n"), 0o600); err != nil {
			t.Fatalf("write stale lock failed: %v", err)
		}
		staleTime := time.Now().Add(-10 * time.Minute)
		if err := os.Chtimes(path, staleTime, staleTime); err != nil {
			t.Fatalf("chtimes failed: %v", err)
		}

		locker := lock.NewFileLock(path, lock.Options{
			StaleAfter:     1 * time.Second,
			AcquireTimeout: 200 * time.Millisecond,
			PollInterval:   10 * time.Millisecond,
		})

		lease, err := locker.Acquire(nil)
		if err != nil {
			t.Fatalf("acquire failed: %v", err)
		}
		if !lease.RecoveredStale() {
			t.Fatalf("expected stale lock recovery signal")
		}
		if err := lease.Release(); err != nil {
			t.Fatalf("release failed: %v", err)
		}
	})

	t.Run("does not recover fresh lock", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), contracts.DefaultSyncDir, "lock")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		if err := os.WriteFile(path, []byte("fresh\n"), 0o600); err != nil {
			t.Fatalf("write fresh lock failed: %v", err)
		}

		locker := lock.NewFileLock(path, lock.Options{
			StaleAfter:     10 * time.Minute,
			AcquireTimeout: 80 * time.Millisecond,
			PollInterval:   10 * time.Millisecond,
		})

		_, err := locker.Acquire(nil)
		if !errors.Is(err, lock.ErrAcquireTimeout) {
			t.Fatalf("expected acquire timeout for fresh lock, got %v", err)
		}
	})
}

func TestCoreSyncPathsDoNotImportOSExec(t *testing.T) {
	repoRoot := mustFindRepoRoot(t)
	targets := []string{
		filepath.Join(repoRoot, "internal", "sync"),
		filepath.Join(repoRoot, "internal", "commands"),
	}

	for _, dir := range targets {
		err := filepath.WalkDir(dir, func(path string, entry iofs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}

			for _, imported := range file.Imports {
				if strings.Trim(imported.Path.Value, "\"") == "os/exec" {
					return fmt.Errorf("%s imports os/exec; core sync paths must not shell out to gh/jira CLIs", path)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func mustFindRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}

	current := wd
	for {
		candidate := filepath.Join(current, "go.mod")
		if _, err := os.Stat(candidate); err == nil {
			return current
		}

		next := filepath.Dir(current)
		if next == current {
			t.Fatalf("could not find repository root from %s", wd)
		}
		current = next
	}
}
