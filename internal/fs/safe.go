package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrEmptyPath   = errors.New("path must not be empty")
	ErrAbsolute    = errors.New("absolute paths are not allowed")
	ErrPathEscapes = errors.New("path escapes root")
)

// SafeFS constrains all file operations to a single root directory.
type SafeFS struct {
	root string
}

func NewSafeFS(root string) (*SafeFS, error) {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		return nil, fmt.Errorf("invalid root: %w", ErrEmptyPath)
	}

	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return nil, err
	}

	return &SafeFS{root: abs}, nil
}

func (s *SafeFS) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

func (s *SafeFS) Resolve(path string) (string, error) {
	if s == nil {
		return "", errors.New("safe filesystem is nil")
	}

	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", ErrEmptyPath
	}
	if filepath.IsAbs(trimmed) {
		return "", ErrAbsolute
	}

	cleaned := filepath.Clean(trimmed)
	if cleaned == "." {
		return s.root, nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", ErrPathEscapes
	}

	target := filepath.Join(s.root, cleaned)
	rel, err := filepath.Rel(s.root, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrPathEscapes
	}

	return target, nil
}

func (s *SafeFS) EnsureDir(path string, perm os.FileMode) error {
	resolved, err := s.Resolve(path)
	if err != nil {
		return err
	}
	return os.MkdirAll(resolved, perm)
}

func (s *SafeFS) WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	resolved, err := s.Resolve(path)
	if err != nil {
		return err
	}

	dir := filepath.Dir(resolved)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	temp, err := os.CreateTemp(dir, "."+filepath.Base(resolved)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temp.Name()) }()

	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Chmod(perm); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}

	return os.Rename(temp.Name(), resolved)
}

func (s *SafeFS) Rename(fromPath, toPath string) error {
	fromResolved, err := s.Resolve(fromPath)
	if err != nil {
		return err
	}
	toResolved, err := s.Resolve(toPath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(toResolved), 0o755); err != nil {
		return err
	}

	return os.Rename(fromResolved, toResolved)
}

func (s *SafeFS) ReadFile(path string) ([]byte, error) {
	resolved, err := s.Resolve(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(resolved)
}

func (s *SafeFS) Remove(path string) error {
	resolved, err := s.Resolve(path)
	if err != nil {
		return err
	}
	if err := os.Remove(resolved); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
