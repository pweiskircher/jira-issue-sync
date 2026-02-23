// pattern: Imperative Shell
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
)

func Read(path string) (contracts.Config, error) {
	resolvedPath := resolvePath(path)
	raw, err := os.ReadFile(resolvedPath)
	if err != nil {
		return contracts.Config{}, &Error{Code: ErrorCodeReadFailed, Path: resolvedPath, Err: err}
	}

	config, err := decode(raw)
	if err != nil {
		return contracts.Config{}, &Error{Code: ErrorCodeParseFailed, Path: resolvedPath, Err: err}
	}

	if err := contracts.ValidateConfig(config); err != nil {
		return contracts.Config{}, &Error{Code: ErrorCodeValidationFailed, Path: resolvedPath, Err: err}
	}

	return config, nil
}

func Write(path string, config contracts.Config) error {
	resolvedPath := resolvePath(path)
	if err := contracts.ValidateConfig(config); err != nil {
		return &Error{Code: ErrorCodeValidationFailed, Path: resolvedPath, Err: err}
	}

	dir := filepath.Dir(resolvedPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &Error{Code: ErrorCodeWriteFailed, Path: resolvedPath, Err: fmt.Errorf("failed to create parent directory: %w", err)}
	}

	encoded, err := encode(config)
	if err != nil {
		return &Error{Code: ErrorCodeWriteFailed, Path: resolvedPath, Err: err}
	}

	if err := os.WriteFile(resolvedPath, encoded, 0o644); err != nil {
		return &Error{Code: ErrorCodeWriteFailed, Path: resolvedPath, Err: err}
	}

	return nil
}

func decode(raw []byte) (contracts.Config, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var config contracts.Config
	if err := decoder.Decode(&config); err != nil {
		return contracts.Config{}, fmt.Errorf("failed to decode config JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return contracts.Config{}, errors.New("unexpected trailing JSON content")
		}
		return contracts.Config{}, fmt.Errorf("failed to decode trailing config JSON content: %w", err)
	}

	return config, nil
}

func encode(config contracts.Config) ([]byte, error) {
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to encode config JSON: %w", err)
	}
	return append(encoded, '\n'), nil
}

func resolvePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return contracts.ConfigFilePath
	}
	return trimmed
}
