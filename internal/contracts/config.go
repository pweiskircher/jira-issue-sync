// pattern: Functional Core
package contracts

import (
	"fmt"
	"sort"
	"strings"
)

const (
	// ConfigFilePath is the canonical config location under the project root.
	ConfigFilePath = ".issues/.sync/config.json"

	// ConfigSchemaVersionV1 is the current supported config schema version.
	ConfigSchemaVersionV1 = "1"
)

// SupportedConfigSchemaVersions is ordered for deterministic mismatch messaging.
var SupportedConfigSchemaVersions = []string{ConfigSchemaVersionV1}

// Config models .issues/.sync/config.json.
type Config struct {
	ConfigVersion  string                    `json:"config_version"`
	Jira           JiraConfig                `json:"jira"`
	DefaultProfile string                    `json:"default_profile,omitempty"`
	DefaultJQL     string                    `json:"default_jql,omitempty"`
	Profiles       map[string]ProjectProfile `json:"profiles"`
}

// JiraConfig contains non-secret Jira defaults; token is env-only by contract.
type JiraConfig struct {
	BaseURL string `json:"base_url,omitempty"`
	Email   string `json:"email,omitempty"`
}

// ProjectProfile scopes config to a project/workstream.
type ProjectProfile struct {
	ProjectKey          string                        `json:"project_key"`
	DefaultJQL          string                        `json:"default_jql,omitempty"`
	TransitionOverrides map[string]TransitionOverride `json:"transition_overrides,omitempty"`
	FieldConfig         FieldConfig                   `json:"field_config,omitempty"`
}

// FieldConfig controls pull field selection and custom-field labeling.
type FieldConfig struct {
	FetchMode       string            `json:"fetch_mode,omitempty"`
	IncludeFields   []string          `json:"include_fields,omitempty"`
	ExcludeFields   []string          `json:"exclude_fields,omitempty"`
	Aliases         map[string]string `json:"aliases,omitempty"`
	IncludeMetadata bool              `json:"include_metadata,omitempty"`
}

// TransitionOverride defines transition disambiguation selectors.
// Precedence is: TransitionID > TransitionName > Dynamic.
type TransitionOverride struct {
	TransitionID   string                     `json:"transition_id,omitempty"`
	TransitionName string                     `json:"transition_name,omitempty"`
	Dynamic        *DynamicTransitionSelector `json:"dynamic,omitempty"`
}

// DynamicTransitionSelector drives dynamic transition discovery.
type DynamicTransitionSelector struct {
	TargetStatus string   `json:"target_status,omitempty"`
	Aliases      []string `json:"aliases,omitempty"`
}

// TransitionSelectionKind captures resolved selector precedence.
type TransitionSelectionKind string

const (
	TransitionSelectionByID    TransitionSelectionKind = "id"
	TransitionSelectionByName  TransitionSelectionKind = "name"
	TransitionSelectionDynamic TransitionSelectionKind = "dynamic"
)

// TransitionSelection is a precedence-resolved transition lookup plan.
type TransitionSelection struct {
	Kind                    TransitionSelectionKind
	TransitionID            string
	TransitionName          string
	DynamicStatusCandidates []string
}

// JQLSource tracks where default JQL was resolved from.
type JQLSource string

const (
	JQLSourceProfile JQLSource = "profile_default_jql"
	JQLSourceGlobal  JQLSource = "global_default_jql"
)

// ConfigErrorCode classifies typed config contract failures.
type ConfigErrorCode string

const (
	ConfigErrorCodeVersionMismatch  ConfigErrorCode = "config_version_mismatch"
	ConfigErrorCodeValidationFailed ConfigErrorCode = "config_validation_failed"
)

// ConfigContractError is implemented by all typed config contract errors.
type ConfigContractError interface {
	error
	Code() ConfigErrorCode
}

// ConfigValidationCode classifies deterministic validation failures.
type ConfigValidationCode string

const (
	ConfigValidationCodeRequired         ConfigValidationCode = "required"
	ConfigValidationCodeInvalidValue     ConfigValidationCode = "invalid_value"
	ConfigValidationCodeUnknownReference ConfigValidationCode = "unknown_reference"
	ConfigValidationCodeDuplicateValue   ConfigValidationCode = "duplicate_value"
)

// ConfigValidationIssue identifies one validation failure.
type ConfigValidationIssue struct {
	Path    string
	Code    ConfigValidationCode
	Message string
}

// ConfigValidationError is returned when schema/content validation fails.
type ConfigValidationError struct {
	Issues []ConfigValidationIssue
}

func (e ConfigValidationError) Error() string {
	if len(e.Issues) == 0 {
		return "invalid configuration"
	}

	first := e.Issues[0]
	return fmt.Sprintf("invalid configuration: %s (%s: %s)", first.Path, first.Code, first.Message)
}

// Code returns a stable typed error code.
func (ConfigValidationError) Code() ConfigErrorCode {
	return ConfigErrorCodeValidationFailed
}

// ConfigVersionMismatchError is returned when config_version is unsupported.
type ConfigVersionMismatchError struct {
	Found     string
	Supported []string
}

func (e ConfigVersionMismatchError) Error() string {
	return fmt.Sprintf(
		"invalid configuration: unsupported config_version %q; supported versions: %s",
		e.Found,
		strings.Join(e.Supported, ", "),
	)
}

// Code returns a stable typed error code.
func (ConfigVersionMismatchError) Code() ConfigErrorCode {
	return ConfigErrorCodeVersionMismatch
}

// ValidateConfig enforces the schema contract with deterministic issue ordering.
func ValidateConfig(config Config) error {
	issues := make([]ConfigValidationIssue, 0)

	version := strings.TrimSpace(config.ConfigVersion)
	if version == "" {
		issues = appendIssue(issues, "config_version", ConfigValidationCodeRequired, "must be set")
	} else if !isSupportedVersion(version) {
		return ConfigVersionMismatchError{
			Found:     version,
			Supported: append([]string(nil), SupportedConfigSchemaVersions...),
		}
	}

	if config.DefaultJQL != "" && strings.TrimSpace(config.DefaultJQL) == "" {
		issues = appendIssue(issues, "default_jql", ConfigValidationCodeInvalidValue, "must not be only whitespace")
	}

	if len(config.Profiles) == 0 {
		issues = appendIssue(issues, "profiles", ConfigValidationCodeRequired, "must include at least one profile")
	}

	if profileName := strings.TrimSpace(config.DefaultProfile); profileName != "" {
		if _, ok := config.Profiles[profileName]; !ok {
			issues = appendIssue(issues, "default_profile", ConfigValidationCodeUnknownReference, "must reference a configured profile")
		}
	}

	for _, profileName := range sortedKeys(config.Profiles) {
		profilePath := "profiles." + profileName
		profile := config.Profiles[profileName]

		if strings.TrimSpace(profileName) == "" {
			issues = appendIssue(issues, profilePath, ConfigValidationCodeInvalidValue, "profile name must not be empty")
		}

		if strings.TrimSpace(profile.ProjectKey) == "" {
			issues = appendIssue(issues, profilePath+".project_key", ConfigValidationCodeRequired, "must be set")
		}

		if profile.DefaultJQL != "" && strings.TrimSpace(profile.DefaultJQL) == "" {
			issues = appendIssue(issues, profilePath+".default_jql", ConfigValidationCodeInvalidValue, "must not be only whitespace")
		}

		for _, targetStatus := range sortedKeys(profile.TransitionOverrides) {
			override := profile.TransitionOverrides[targetStatus]
			overridePath := profilePath + ".transition_overrides." + targetStatus
			issues = append(issues, validateTransitionOverride(overridePath, targetStatus, override)...)
		}

		issues = append(issues, validateFieldConfig(profilePath+".field_config", profile.FieldConfig)...)
	}

	if len(issues) == 0 {
		return nil
	}

	sortValidationIssues(issues)
	return ConfigValidationError{Issues: issues}
}

// ResolveDefaultJQL returns default JQL using profile-over-global precedence.
func ResolveDefaultJQL(config Config, profileName string) (string, JQLSource, bool) {
	if profileName != "" {
		if profile, ok := config.Profiles[profileName]; ok {
			if jql := strings.TrimSpace(profile.DefaultJQL); jql != "" {
				return jql, JQLSourceProfile, true
			}
		}
	}

	if jql := strings.TrimSpace(config.DefaultJQL); jql != "" {
		return jql, JQLSourceGlobal, true
	}

	return "", "", false
}

// ResolveTransitionSelectionForStatus resolves an override by target status key.
// Lookup is case-insensitive, then precedence is applied.
func ResolveTransitionSelectionForStatus(profile ProjectProfile, targetStatus string) TransitionSelection {
	if override, ok := findTransitionOverride(profile.TransitionOverrides, targetStatus); ok {
		return ResolveTransitionSelection(override, targetStatus)
	}

	return ResolveTransitionSelection(TransitionOverride{}, targetStatus)
}

// ResolveTransitionSelection applies selector precedence: ID > name > dynamic.
func ResolveTransitionSelection(override TransitionOverride, targetStatus string) TransitionSelection {
	if id := strings.TrimSpace(override.TransitionID); id != "" {
		return TransitionSelection{Kind: TransitionSelectionByID, TransitionID: id}
	}

	if name := strings.TrimSpace(override.TransitionName); name != "" {
		return TransitionSelection{Kind: TransitionSelectionByName, TransitionName: name}
	}

	candidates := make([]string, 0)
	if override.Dynamic != nil {
		if v := strings.TrimSpace(override.Dynamic.TargetStatus); v != "" {
			candidates = append(candidates, v)
		}
		for _, alias := range override.Dynamic.Aliases {
			if v := strings.TrimSpace(alias); v != "" {
				candidates = append(candidates, v)
			}
		}
	}

	if v := strings.TrimSpace(targetStatus); v != "" {
		candidates = append(candidates, v)
	}

	return TransitionSelection{
		Kind:                    TransitionSelectionDynamic,
		DynamicStatusCandidates: uniqueFold(candidates),
	}
}

func validateFieldConfig(path string, fieldConfig FieldConfig) []ConfigValidationIssue {
	issues := make([]ConfigValidationIssue, 0)

	fetchMode := strings.TrimSpace(fieldConfig.FetchMode)
	if fetchMode != "" {
		switch fetchMode {
		case "navigable", "all", "explicit":
		default:
			issues = appendIssue(issues, path+".fetch_mode", ConfigValidationCodeInvalidValue, "must be one of: navigable, all, explicit")
		}
	}

	for i, field := range fieldConfig.IncludeFields {
		trimmed := strings.TrimSpace(field)
		if trimmed == "" {
			issues = appendIssue(issues, fmt.Sprintf("%s.include_fields[%d]", path, i), ConfigValidationCodeInvalidValue, "must not be empty")
		}
	}
	for i, field := range fieldConfig.ExcludeFields {
		trimmed := strings.TrimSpace(field)
		if trimmed == "" {
			issues = appendIssue(issues, fmt.Sprintf("%s.exclude_fields[%d]", path, i), ConfigValidationCodeInvalidValue, "must not be empty")
		}
	}

	for key, alias := range fieldConfig.Aliases {
		if strings.TrimSpace(key) == "" {
			issues = appendIssue(issues, path+".aliases", ConfigValidationCodeInvalidValue, "alias keys must not be empty")
			continue
		}
		if strings.TrimSpace(alias) == "" {
			issues = appendIssue(issues, path+".aliases."+key, ConfigValidationCodeInvalidValue, "alias values must not be empty")
		}
	}

	return issues
}

func validateTransitionOverride(path string, targetStatus string, override TransitionOverride) []ConfigValidationIssue {
	issues := make([]ConfigValidationIssue, 0)

	hasID := false
	if override.TransitionID != "" {
		if strings.TrimSpace(override.TransitionID) == "" {
			issues = appendIssue(issues, path+".transition_id", ConfigValidationCodeInvalidValue, "must not be only whitespace")
		} else {
			hasID = true
		}
	}

	hasName := false
	if override.TransitionName != "" {
		if strings.TrimSpace(override.TransitionName) == "" {
			issues = appendIssue(issues, path+".transition_name", ConfigValidationCodeInvalidValue, "must not be only whitespace")
		} else {
			hasName = true
		}
	}

	hasDynamic := override.Dynamic != nil
	if override.Dynamic != nil {
		dynamicPath := path + ".dynamic"
		target := strings.TrimSpace(override.Dynamic.TargetStatus)
		if override.Dynamic.TargetStatus != "" && target == "" {
			issues = appendIssue(issues, dynamicPath+".target_status", ConfigValidationCodeInvalidValue, "must not be only whitespace")
		}
		if target == "" && strings.TrimSpace(targetStatus) == "" && len(override.Dynamic.Aliases) == 0 {
			issues = appendIssue(issues, dynamicPath+".target_status", ConfigValidationCodeRequired, "must be set when override key and aliases are empty")
		}

		seen := make(map[string]struct{})
		for i, alias := range override.Dynamic.Aliases {
			aliasPath := fmt.Sprintf("%s.aliases[%d]", dynamicPath, i)
			trimmed := strings.TrimSpace(alias)
			if trimmed == "" {
				issues = appendIssue(issues, aliasPath, ConfigValidationCodeInvalidValue, "must not be empty")
				continue
			}

			key := strings.ToLower(trimmed)
			if _, exists := seen[key]; exists {
				issues = appendIssue(issues, aliasPath, ConfigValidationCodeDuplicateValue, "must be unique (case-insensitive)")
				continue
			}
			seen[key] = struct{}{}
		}
	}

	if !hasID && !hasName && !hasDynamic {
		issues = appendIssue(
			issues,
			path,
			ConfigValidationCodeRequired,
			"must define at least one selector: transition_id, transition_name, or dynamic",
		)
	}

	return issues
}

func findTransitionOverride(overrides map[string]TransitionOverride, targetStatus string) (TransitionOverride, bool) {
	if len(overrides) == 0 {
		return TransitionOverride{}, false
	}

	if override, ok := overrides[targetStatus]; ok {
		return override, true
	}

	normalizedTarget := strings.ToLower(strings.TrimSpace(targetStatus))
	for _, key := range sortedKeys(overrides) {
		if strings.ToLower(strings.TrimSpace(key)) == normalizedTarget {
			return overrides[key], true
		}
	}

	return TransitionOverride{}, false
}

func appendIssue(issues []ConfigValidationIssue, path string, code ConfigValidationCode, message string) []ConfigValidationIssue {
	return append(issues, ConfigValidationIssue{Path: path, Code: code, Message: message})
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortValidationIssues(issues []ConfigValidationIssue) {
	sort.SliceStable(issues, func(i int, j int) bool {
		if issues[i].Path != issues[j].Path {
			return issues[i].Path < issues[j].Path
		}
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		return issues[i].Message < issues[j].Message
	})
}

func uniqueFold(values []string) []string {
	if len(values) == 0 {
		return values
	}

	result := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}

		seen[normalized] = struct{}{}
		result = append(result, strings.TrimSpace(value))
	}
	return result
}

func isSupportedVersion(version string) bool {
	for _, supported := range SupportedConfigSchemaVersions {
		if version == supported {
			return true
		}
	}
	return false
}
