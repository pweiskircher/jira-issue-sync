// pattern: Functional Core
package config

import (
	"os"
	"sort"
	"strings"

	"github.com/pat/jira-issue-sync/internal/contracts"
)

const (
	EnvJiraAPIToken = "JIRA_API_TOKEN"
	EnvJiraBaseURL  = "JIRA_BASE_URL"
	EnvJiraEmail    = "JIRA_EMAIL"
)

type RuntimeFlags struct {
	Profile     string
	JQL         string
	JiraBaseURL string
	JiraEmail   string
}

type Environment struct {
	JiraAPIToken string
	JiraBaseURL  string
	JiraEmail    string
}

type ResolveOptions struct {
	RequireToken bool
}

type JQLSource string

const (
	JQLSourceFlag    JQLSource = "flag_jql"
	JQLSourceProfile JQLSource = JQLSource(contracts.JQLSourceProfile)
	JQLSourceGlobal  JQLSource = JQLSource(contracts.JQLSourceGlobal)
)

type RuntimeSettings struct {
	ProfileName         string
	Profile             contracts.ProjectProfile
	JiraAPIToken        string
	JiraBaseURL         string
	JiraEmail           string
	DefaultJQL          string
	DefaultJQLSource    JQLSource
	TransitionOverrides map[string]contracts.TransitionOverride
}

func (settings RuntimeSettings) ResolveTransitionSelection(targetStatus string) contracts.TransitionSelection {
	return contracts.ResolveTransitionSelectionForStatus(settings.Profile, targetStatus)
}

func Resolve(config contracts.Config, flags RuntimeFlags, env Environment, options ResolveOptions) (RuntimeSettings, error) {
	if err := contracts.ValidateConfig(config); err != nil {
		return RuntimeSettings{}, &ResolveError{
			Code:    ResolveErrorCodeInvalidConfig,
			Message: "configuration is invalid",
			Err:     err,
		}
	}

	profileName, profile, err := resolveProfile(config, flags.Profile)
	if err != nil {
		return RuntimeSettings{}, err
	}

	flagJQL := strings.TrimSpace(flags.JQL)
	if flags.JQL != "" && flagJQL == "" {
		return RuntimeSettings{}, &ResolveError{
			Code:    ResolveErrorCodeInvalidFlag,
			Message: "--jql must not be only whitespace",
		}
	}

	token := strings.TrimSpace(env.JiraAPIToken)
	if options.RequireToken && token == "" {
		return RuntimeSettings{}, &ResolveError{
			Code:    ResolveErrorCodeMissingToken,
			Message: EnvJiraAPIToken + " is required",
		}
	}

	settings := RuntimeSettings{
		ProfileName:         profileName,
		Profile:             cloneProfile(profile),
		TransitionOverrides: cloneTransitionOverrides(profile.TransitionOverrides),
		JiraAPIToken:        token,
		JiraBaseURL:         firstNonEmpty(strings.TrimSpace(flags.JiraBaseURL), strings.TrimSpace(env.JiraBaseURL), strings.TrimSpace(config.Jira.BaseURL)),
		JiraEmail:           firstNonEmpty(strings.TrimSpace(flags.JiraEmail), strings.TrimSpace(env.JiraEmail), strings.TrimSpace(config.Jira.Email)),
	}

	if flagJQL != "" {
		settings.DefaultJQL = flagJQL
		settings.DefaultJQLSource = JQLSourceFlag
		return settings, nil
	}

	jql, source, ok := contracts.ResolveDefaultJQL(config, profileName)
	if ok {
		settings.DefaultJQL = jql
		settings.DefaultJQLSource = JQLSource(source)
	}

	return settings, nil
}

func EnvironmentFromOS() Environment {
	return EnvironmentFromLookup(os.LookupEnv)
}

func EnvironmentFromLookup(lookup func(string) (string, bool)) Environment {
	if lookup == nil {
		return Environment{}
	}

	return Environment{
		JiraAPIToken: lookupTrimmed(lookup, EnvJiraAPIToken),
		JiraBaseURL:  lookupTrimmed(lookup, EnvJiraBaseURL),
		JiraEmail:    lookupTrimmed(lookup, EnvJiraEmail),
	}
}

func resolveProfile(config contracts.Config, profileFlag string) (string, contracts.ProjectProfile, error) {
	flagValue := strings.TrimSpace(profileFlag)
	if profileFlag != "" && flagValue == "" {
		return "", contracts.ProjectProfile{}, &ResolveError{
			Code:    ResolveErrorCodeInvalidFlag,
			Message: "--profile must not be only whitespace",
		}
	}

	if flagValue != "" {
		profile, ok := config.Profiles[flagValue]
		if !ok {
			return "", contracts.ProjectProfile{}, &ResolveError{
				Code:    ResolveErrorCodeUnknownProfile,
				Message: "--profile references unknown profile " + flagValue,
			}
		}
		return flagValue, profile, nil
	}

	defaultProfile := strings.TrimSpace(config.DefaultProfile)
	if defaultProfile != "" {
		profile, ok := config.Profiles[defaultProfile]
		if !ok {
			return "", contracts.ProjectProfile{}, &ResolveError{
				Code:    ResolveErrorCodeUnknownProfile,
				Message: "default_profile references unknown profile " + defaultProfile,
			}
		}
		return defaultProfile, profile, nil
	}

	if len(config.Profiles) == 1 {
		for name, profile := range config.Profiles {
			return name, profile, nil
		}
	}

	available := sortedProfileNames(config.Profiles)
	return "", contracts.ProjectProfile{}, &ResolveError{
		Code:    ResolveErrorCodeMissingProfile,
		Message: "profile is required when config defines multiple profiles: " + strings.Join(available, ", "),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func sortedProfileNames(profiles map[string]contracts.ProjectProfile) []string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func lookupTrimmed(lookup func(string) (string, bool), key string) string {
	value, ok := lookup(key)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func cloneProfile(profile contracts.ProjectProfile) contracts.ProjectProfile {
	cloned := profile
	cloned.TransitionOverrides = cloneTransitionOverrides(profile.TransitionOverrides)
	return cloned
}

func cloneTransitionOverrides(overrides map[string]contracts.TransitionOverride) map[string]contracts.TransitionOverride {
	if len(overrides) == 0 {
		return nil
	}

	cloned := make(map[string]contracts.TransitionOverride, len(overrides))
	for key, override := range overrides {
		clonedOverride := override
		if override.Dynamic != nil {
			clonedDynamic := *override.Dynamic
			clonedDynamic.Aliases = append([]string(nil), override.Dynamic.Aliases...)
			clonedOverride.Dynamic = &clonedDynamic
		}
		cloned[key] = clonedOverride
	}

	return cloned
}
