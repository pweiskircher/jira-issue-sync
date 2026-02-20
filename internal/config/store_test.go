package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pat/jira-issue-sync/internal/contracts"
)

func TestWriteThenReadRoundTrip(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".issues", ".sync", "config.json")

	input := contracts.Config{
		ConfigVersion:  contracts.ConfigSchemaVersionV1,
		DefaultProfile: "core",
		DefaultJQL:     "project = CORE",
		Jira: contracts.JiraConfig{
			BaseURL: "https://config.example",
			Email:   "config@example.com",
		},
		Profiles: map[string]contracts.ProjectProfile{
			"core": {
				ProjectKey: "CORE",
				DefaultJQL: "project = CORE AND statusCategory != Done",
				TransitionOverrides: map[string]contracts.TransitionOverride{
					"Done": {TransitionID: "31"},
				},
			},
		},
	}

	if err := Write(configPath, input); err != nil {
		t.Fatalf("expected write success, got %v", err)
	}

	loaded, err := Read(configPath)
	if err != nil {
		t.Fatalf("expected read success, got %v", err)
	}

	if loaded.ConfigVersion != input.ConfigVersion {
		t.Fatalf("unexpected config version: %q", loaded.ConfigVersion)
	}
	if loaded.DefaultProfile != "core" {
		t.Fatalf("unexpected default profile: %q", loaded.DefaultProfile)
	}
	if loaded.Jira.BaseURL != "https://config.example" {
		t.Fatalf("unexpected Jira base URL: %q", loaded.Jira.BaseURL)
	}
	if loaded.Profiles["core"].TransitionOverrides["Done"].TransitionID != "31" {
		t.Fatalf("unexpected transition override: %#v", loaded.Profiles["core"].TransitionOverrides)
	}
}

func TestReadRejectsUnknownTokenFields(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")

	raw := `{
  "config_version": "1",
  "jira": {
    "base_url": "https://config.example",
    "email": "config@example.com",
    "api_token": "should-never-be-read"
  },
  "profiles": {
    "core": {
      "project_key": "CORE"
    }
  }
}`

	if err := osWriteFile(configPath, []byte(raw)); err != nil {
		t.Fatalf("failed to seed config fixture: %v", err)
	}

	_, err := Read(configPath)
	if err == nil {
		t.Fatalf("expected parse error")
	}
	if !IsErrorCode(err, ErrorCodeParseFailed) {
		t.Fatalf("expected parse error code, got %v", err)
	}
	if !strings.Contains(err.Error(), "unknown field \"api_token\"") {
		t.Fatalf("expected unknown field diagnostic, got %q", err)
	}
}

func TestWriteRejectsInvalidConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")

	err := Write(configPath, contracts.Config{
		ConfigVersion: contracts.ConfigSchemaVersionV1,
		Profiles:      map[string]contracts.ProjectProfile{},
	})
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !IsErrorCode(err, ErrorCodeValidationFailed) {
		t.Fatalf("expected validation error code, got %v", err)
	}
}

func osWriteFile(path string, raw []byte) error {
	return os.WriteFile(path, raw, 0o644)
}
