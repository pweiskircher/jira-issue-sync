package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	internalfs "github.com/pweiskircher/jira-issue-sync/internal/fs"
	"github.com/pweiskircher/jira-issue-sync/internal/issue"
)

const CacheSchemaVersionV1 = "1"

type IssueState string

const (
	IssueStateOpen   IssueState = "open"
	IssueStateClosed IssueState = "closed"
)

type Cache struct {
	Version string                `json:"version"`
	Issues  map[string]CacheEntry `json:"issues"`
}

type CacheEntry struct {
	Path            string `json:"path,omitempty"`
	Status          string `json:"status,omitempty"`
	RemoteUpdatedAt string `json:"remote_updated_at,omitempty"`
}

type Store struct {
	fs *internalfs.SafeFS
}

func New(root string) (*Store, error) {
	safe, err := internalfs.NewSafeFS(root)
	if err != nil {
		return nil, err
	}

	return &Store{fs: safe}, nil
}

func NewDefault() (*Store, error) {
	return New(contracts.DefaultIssuesRootDir)
}

func (s *Store) Root() string {
	if s == nil || s.fs == nil {
		return ""
	}
	return s.fs.Root()
}

func (s *Store) EnsureLayout() error {
	if s == nil || s.fs == nil {
		return fmt.Errorf("store is not initialized")
	}

	dirs := []string{"open", "closed", ".sync", filepath.Join(".sync", "originals")}
	for _, dir := range dirs {
		if err := s.fs.EnsureDir(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) WriteIssue(state IssueState, key, summary, markdown string) (string, error) {
	if err := s.EnsureLayout(); err != nil {
		return "", err
	}

	dir, err := issueDir(state)
	if err != nil {
		return "", err
	}

	filename, err := issue.BuildFilename(strings.TrimSpace(key), summary)
	if err != nil {
		return "", err
	}

	relativePath := filepath.Join(dir, filename)
	if err := s.fs.WriteFileAtomic(relativePath, normalizeText(markdown), 0o644); err != nil {
		return "", err
	}

	return relativePath, nil
}

func (s *Store) WriteOriginalSnapshot(key string, markdown string) (string, error) {
	if err := s.EnsureLayout(); err != nil {
		return "", err
	}

	trimmedKey := strings.TrimSpace(key)
	if !contracts.JiraIssueKeyPattern.MatchString(trimmedKey) && !contracts.LocalDraftKeyPattern.MatchString(trimmedKey) {
		return "", fmt.Errorf("invalid issue key %q", key)
	}

	relativePath := filepath.Join(".sync", "originals", trimmedKey+".md")
	if err := s.fs.WriteFileAtomic(relativePath, normalizeText(markdown), 0o644); err != nil {
		return "", err
	}

	return relativePath, nil
}

func (s *Store) SaveCache(cache Cache) error {
	if err := s.EnsureLayout(); err != nil {
		return err
	}

	canonical := canonicalizeCache(cache)
	encoded, err := json.MarshalIndent(canonical, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')

	return s.fs.WriteFileAtomic(filepath.Join(".sync", "cache.json"), encoded, 0o644)
}

func (s *Store) LoadCache() (Cache, error) {
	if s == nil || s.fs == nil {
		return Cache{}, fmt.Errorf("store is not initialized")
	}

	encoded, err := s.fs.ReadFile(filepath.Join(".sync", "cache.json"))
	if err != nil {
		if errorsIsNotExist(err) {
			return canonicalizeCache(Cache{}), nil
		}
		return Cache{}, err
	}

	var cache Cache
	if err := json.Unmarshal(encoded, &cache); err != nil {
		return Cache{}, err
	}

	return canonicalizeCache(cache), nil
}

func (s *Store) WriteFile(relativePath string, data []byte) error {
	if err := s.EnsureLayout(); err != nil {
		return err
	}
	return s.fs.WriteFileAtomic(relativePath, data, 0o644)
}

func (s *Store) Rename(fromRelativePath, toRelativePath string) error {
	if s == nil || s.fs == nil {
		return fmt.Errorf("store is not initialized")
	}
	return s.fs.Rename(fromRelativePath, toRelativePath)
}

func (s *Store) ReadFile(relativePath string) ([]byte, error) {
	if s == nil || s.fs == nil {
		return nil, fmt.Errorf("store is not initialized")
	}
	return s.fs.ReadFile(relativePath)
}

func (s *Store) Remove(relativePath string) error {
	if s == nil || s.fs == nil {
		return fmt.Errorf("store is not initialized")
	}
	return s.fs.Remove(relativePath)
}

func issueDir(state IssueState) (string, error) {
	switch state {
	case IssueStateOpen:
		return "open", nil
	case IssueStateClosed:
		return "closed", nil
	default:
		return "", fmt.Errorf("unsupported issue state %q", state)
	}
}

func normalizeText(input string) []byte {
	normalized := contracts.NormalizeSingleValue(contracts.NormalizationNormalizeLineEndings, input)
	if normalized == "" {
		return []byte{}
	}
	if !strings.HasSuffix(normalized, "\n") {
		normalized += "\n"
	}
	return []byte(normalized)
}

func canonicalizeCache(cache Cache) Cache {
	canonical := cache
	if strings.TrimSpace(canonical.Version) == "" {
		canonical.Version = CacheSchemaVersionV1
	}
	if canonical.Issues == nil {
		canonical.Issues = map[string]CacheEntry{}
	}
	return canonical
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}
