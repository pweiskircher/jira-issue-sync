package contracts

import (
	"errors"
	"reflect"
	"testing"
)

func TestValidateConfigVersionMismatchIsTyped(t *testing.T) {
	config := Config{
		ConfigVersion: "999",
		Profiles: map[string]ProjectProfile{
			"core": {ProjectKey: "CORE"},
		},
	}

	err := ValidateConfig(config)
	if err == nil {
		t.Fatalf("expected error")
	}

	var mismatchErr ConfigVersionMismatchError
	if !errors.As(err, &mismatchErr) {
		t.Fatalf("expected ConfigVersionMismatchError, got %T", err)
	}

	if mismatchErr.Code() != ConfigErrorCodeVersionMismatch {
		t.Fatalf("unexpected code: %q", mismatchErr.Code())
	}

	if mismatchErr.Found != "999" {
		t.Fatalf("unexpected found version: %q", mismatchErr.Found)
	}

	if !reflect.DeepEqual(mismatchErr.Supported, []string{ConfigSchemaVersionV1}) {
		t.Fatalf("unexpected supported versions: %#v", mismatchErr.Supported)
	}
}

func TestValidateConfigReturnsDeterministicSortedIssues(t *testing.T) {
	config := Config{
		ConfigVersion:  "1",
		DefaultProfile: "missing",
		DefaultJQL:     "   ",
		Profiles: map[string]ProjectProfile{
			"": {
				TransitionOverrides: map[string]TransitionOverride{
					"Doing": {},
				},
			},
			"alpha": {
				ProjectKey: "  ",
				DefaultJQL: "  ",
				TransitionOverrides: map[string]TransitionOverride{
					"Review": {
						Dynamic: &DynamicTransitionSelector{
							Aliases: []string{"done", "", "Done"},
						},
					},
				},
			},
		},
	}

	err := ValidateConfig(config)
	if err == nil {
		t.Fatalf("expected error")
	}

	var validationErr ConfigValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ConfigValidationError, got %T", err)
	}

	if validationErr.Code() != ConfigErrorCodeValidationFailed {
		t.Fatalf("unexpected code: %q", validationErr.Code())
	}

	issues := validationErr.Issues
	got := make([]string, 0, len(issues))
	for _, issue := range issues {
		got = append(got, issue.Path+"|"+string(issue.Code))
	}

	want := []string{
		"default_jql|invalid_value",
		"default_profile|unknown_reference",
		"profiles.|invalid_value",
		"profiles..project_key|required",
		"profiles..transition_overrides.Doing|required",
		"profiles.alpha.default_jql|invalid_value",
		"profiles.alpha.project_key|required",
		"profiles.alpha.transition_overrides.Review.dynamic.aliases[1]|invalid_value",
		"profiles.alpha.transition_overrides.Review.dynamic.aliases[2]|duplicate_value",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected issues\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestResolveDefaultJQLPrecedence(t *testing.T) {
	config := Config{
		ConfigVersion: "1",
		DefaultJQL:    "project = GLOBAL",
		Profiles: map[string]ProjectProfile{
			"core": {
				ProjectKey: "CORE",
				DefaultJQL: "project = CORE",
			},
		},
	}

	jql, source, ok := ResolveDefaultJQL(config, "core")
	if !ok {
		t.Fatalf("expected JQL")
	}
	if jql != "project = CORE" || source != JQLSourceProfile {
		t.Fatalf("expected profile JQL, got %q from %q", jql, source)
	}

	jql, source, ok = ResolveDefaultJQL(config, "missing")
	if !ok {
		t.Fatalf("expected JQL")
	}
	if jql != "project = GLOBAL" || source != JQLSourceGlobal {
		t.Fatalf("expected global JQL, got %q from %q", jql, source)
	}

	jql, source, ok = ResolveDefaultJQL(Config{ConfigVersion: "1", Profiles: map[string]ProjectProfile{}}, "")
	if ok || jql != "" || source != "" {
		t.Fatalf("expected no JQL, got %q from %q (ok=%v)", jql, source, ok)
	}
}

func TestResolveTransitionSelectionPrecedence(t *testing.T) {
	selection := ResolveTransitionSelection(TransitionOverride{
		TransitionID:   " 31 ",
		TransitionName: "Done",
		Dynamic: &DynamicTransitionSelector{
			TargetStatus: "Closed",
			Aliases:      []string{"Done"},
		},
	}, "ignored")

	if selection.Kind != TransitionSelectionByID || selection.TransitionID != "31" {
		t.Fatalf("expected ID selection, got %#v", selection)
	}

	selection = ResolveTransitionSelection(TransitionOverride{
		TransitionName: " Done ",
		Dynamic: &DynamicTransitionSelector{
			TargetStatus: "Closed",
		},
	}, "ignored")

	if selection.Kind != TransitionSelectionByName || selection.TransitionName != "Done" {
		t.Fatalf("expected name selection, got %#v", selection)
	}

	selection = ResolveTransitionSelection(TransitionOverride{
		Dynamic: &DynamicTransitionSelector{
			TargetStatus: " Closed ",
			Aliases:      []string{"Done", "done", "", "QA"},
		},
	}, "Done")

	if selection.Kind != TransitionSelectionDynamic {
		t.Fatalf("expected dynamic selection, got %#v", selection)
	}

	wantCandidates := []string{"Closed", "Done", "QA"}
	if !reflect.DeepEqual(selection.DynamicStatusCandidates, wantCandidates) {
		t.Fatalf("unexpected dynamic candidates: want %#v got %#v", wantCandidates, selection.DynamicStatusCandidates)
	}
}

func TestResolveTransitionSelectionForStatusCaseInsensitiveLookup(t *testing.T) {
	profile := ProjectProfile{
		ProjectKey: "CORE",
		TransitionOverrides: map[string]TransitionOverride{
			"in progress": {TransitionName: "Start Progress"},
		},
	}

	selection := ResolveTransitionSelectionForStatus(profile, "In Progress")
	if selection.Kind != TransitionSelectionByName {
		t.Fatalf("expected name selection, got %#v", selection)
	}
	if selection.TransitionName != "Start Progress" {
		t.Fatalf("unexpected transition name: %q", selection.TransitionName)
	}
}

func TestResolveTransitionSelectionForStatusFallsBackToDynamicTarget(t *testing.T) {
	profile := ProjectProfile{ProjectKey: "CORE"}

	selection := ResolveTransitionSelectionForStatus(profile, "Done")
	if selection.Kind != TransitionSelectionDynamic {
		t.Fatalf("expected dynamic selection, got %#v", selection)
	}
	if !reflect.DeepEqual(selection.DynamicStatusCandidates, []string{"Done"}) {
		t.Fatalf("unexpected candidates: %#v", selection.DynamicStatusCandidates)
	}
}
