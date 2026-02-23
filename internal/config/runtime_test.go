package config

import (
	"reflect"
	"testing"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
)

func TestResolveAppliesFlagsThenEnvThenConfigPrecedence(t *testing.T) {
	config := baseConfig()
	config.Jira.BaseURL = "https://config.example"
	config.Jira.Email = "config@example.com"

	settings, err := Resolve(
		config,
		RuntimeFlags{JiraBaseURL: " https://flag.example "},
		Environment{
			JiraBaseURL:  "https://env.example",
			JiraEmail:    "env@example.com",
			JiraAPIToken: "token-from-env",
		},
		ResolveOptions{RequireToken: true},
	)
	if err != nil {
		t.Fatalf("expected resolve success, got %v", err)
	}

	if settings.JiraBaseURL != "https://flag.example" {
		t.Fatalf("expected flag base URL, got %q", settings.JiraBaseURL)
	}
	if settings.JiraEmail != "env@example.com" {
		t.Fatalf("expected env email, got %q", settings.JiraEmail)
	}
	if settings.JiraAPIToken != "token-from-env" {
		t.Fatalf("expected env token, got %q", settings.JiraAPIToken)
	}
}

func TestResolveProfileSelectionAndJQLSources(t *testing.T) {
	config := baseConfig()
	config.DefaultProfile = "beta"
	config.DefaultJQL = "project = GLOBAL"
	config.Profiles["alpha"] = contracts.ProjectProfile{
		ProjectKey: "ALPHA",
		DefaultJQL: "project = ALPHA",
	}
	config.Profiles["beta"] = contracts.ProjectProfile{
		ProjectKey: "BETA",
		DefaultJQL: "project = BETA",
		TransitionOverrides: map[string]contracts.TransitionOverride{
			"Done": {
				TransitionID: "31",
			},
		},
	}

	settings, err := Resolve(config, RuntimeFlags{}, Environment{}, ResolveOptions{})
	if err != nil {
		t.Fatalf("expected resolve success, got %v", err)
	}
	if settings.ProfileName != "beta" {
		t.Fatalf("expected default profile beta, got %q", settings.ProfileName)
	}
	if settings.DefaultJQL != "project = BETA" || settings.DefaultJQLSource != JQLSourceProfile {
		t.Fatalf("expected profile default JQL, got %q (%q)", settings.DefaultJQL, settings.DefaultJQLSource)
	}

	selection := settings.ResolveTransitionSelection("Done")
	if selection.Kind != contracts.TransitionSelectionByID || selection.TransitionID != "31" {
		t.Fatalf("expected transition ID override, got %#v", selection)
	}

	flagSettings, err := Resolve(
		config,
		RuntimeFlags{Profile: "alpha", JQL: " project = FLAG "},
		Environment{},
		ResolveOptions{},
	)
	if err != nil {
		t.Fatalf("expected resolve success for flag overrides, got %v", err)
	}
	if flagSettings.ProfileName != "alpha" {
		t.Fatalf("expected selected profile alpha, got %q", flagSettings.ProfileName)
	}
	if flagSettings.DefaultJQL != "project = FLAG" || flagSettings.DefaultJQLSource != JQLSourceFlag {
		t.Fatalf("expected flag JQL override, got %q (%q)", flagSettings.DefaultJQL, flagSettings.DefaultJQLSource)
	}
}

func TestResolveReturnsMissingProfileWhenAmbiguous(t *testing.T) {
	config := contracts.Config{
		ConfigVersion: contracts.ConfigSchemaVersionV1,
		Profiles: map[string]contracts.ProjectProfile{
			"alpha": {ProjectKey: "ALPHA"},
			"beta":  {ProjectKey: "BETA"},
		},
	}

	_, err := Resolve(config, RuntimeFlags{}, Environment{}, ResolveOptions{})
	if err == nil {
		t.Fatalf("expected missing profile error")
	}
	if !IsResolveErrorCode(err, ResolveErrorCodeMissingProfile) {
		t.Fatalf("expected missing profile code, got %v", err)
	}
}

func TestResolveTokenRequirementIsEnvOnly(t *testing.T) {
	config := contracts.Config{
		ConfigVersion: contracts.ConfigSchemaVersionV1,
		Profiles: map[string]contracts.ProjectProfile{
			"core": {ProjectKey: "CORE"},
		},
	}

	_, err := Resolve(config, RuntimeFlags{}, Environment{}, ResolveOptions{RequireToken: true})
	if err == nil {
		t.Fatalf("expected missing token error")
	}
	if !IsResolveErrorCode(err, ResolveErrorCodeMissingToken) {
		t.Fatalf("expected missing token code, got %v", err)
	}

	settings, err := Resolve(
		config,
		RuntimeFlags{},
		Environment{JiraAPIToken: "token-from-env"},
		ResolveOptions{RequireToken: true},
	)
	if err != nil {
		t.Fatalf("expected resolve success with env token, got %v", err)
	}
	if settings.JiraAPIToken != "token-from-env" {
		t.Fatalf("unexpected token source: %q", settings.JiraAPIToken)
	}
}

func TestEnvironmentFromLookupTrimsValues(t *testing.T) {
	env := EnvironmentFromLookup(func(key string) (string, bool) {
		values := map[string]string{
			EnvJiraAPIToken: " token ",
			EnvJiraBaseURL:  " https://example ",
			EnvJiraEmail:    " user@example.com ",
		}
		value, ok := values[key]
		return value, ok
	})

	if !reflect.DeepEqual(env, Environment{
		JiraAPIToken: "token",
		JiraBaseURL:  "https://example",
		JiraEmail:    "user@example.com",
	}) {
		t.Fatalf("unexpected environment parsing: %#v", env)
	}
}

func baseConfig() contracts.Config {
	return contracts.Config{
		ConfigVersion: contracts.ConfigSchemaVersionV1,
		Profiles: map[string]contracts.ProjectProfile{
			"core": {
				ProjectKey: "CORE",
			},
		},
	}
}
