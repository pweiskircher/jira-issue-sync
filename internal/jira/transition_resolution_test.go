package jira

import (
	"reflect"
	"testing"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
)

func TestResolveTransitionSelectionByID(t *testing.T) {
	resolution := resolveTransitionSelection([]Transition{{ID: "11", Name: "Done", ToStatusName: "Done"}}, contracts.TransitionSelection{
		Kind:         contracts.TransitionSelectionByID,
		TransitionID: "11",
	})

	if resolution.Kind != TransitionResolutionSelected {
		t.Fatalf("expected selected resolution, got %#v", resolution)
	}
	if resolution.SelectionKind != contracts.TransitionSelectionByID {
		t.Fatalf("unexpected selection kind: %s", resolution.SelectionKind)
	}
	if resolution.Transition.ID != "11" {
		t.Fatalf("unexpected selected transition: %#v", resolution.Transition)
	}
	if !reflect.DeepEqual(resolution.TriedCandidates, []string{"11"}) {
		t.Fatalf("unexpected tried candidates: %#v", resolution.TriedCandidates)
	}
}

func TestResolveTransitionSelectionByNameAmbiguous(t *testing.T) {
	resolution := resolveTransitionSelection([]Transition{
		{ID: "21", Name: "Ship", ToStatusName: "Released"},
		{ID: "11", Name: "Ship", ToStatusName: "Delivered"},
	}, contracts.TransitionSelection{
		Kind:           contracts.TransitionSelectionByName,
		TransitionName: "ship",
	})

	if resolution.Kind != TransitionResolutionAmbiguous {
		t.Fatalf("expected ambiguous resolution, got %#v", resolution)
	}
	if resolution.ReasonCode != contracts.ReasonCodeTransitionAmbiguous {
		t.Fatalf("unexpected reason code: %s", resolution.ReasonCode)
	}
	if len(resolution.Matches) != 2 {
		t.Fatalf("expected 2 matches, got %#v", resolution.Matches)
	}
	if resolution.Matches[0].ID != "11" || resolution.Matches[1].ID != "21" {
		t.Fatalf("expected deterministic sorting in matches, got %#v", resolution.Matches)
	}
}

func TestResolveTransitionSelectionDynamicUsesCandidatesInOrder(t *testing.T) {
	resolution := resolveTransitionSelection([]Transition{
		{ID: "40", Name: "Close", ToStatusName: "Done"},
	}, contracts.TransitionSelection{
		Kind:                    contracts.TransitionSelectionDynamic,
		DynamicStatusCandidates: []string{"In Review", "done", "DONE"},
	})

	if resolution.Kind != TransitionResolutionSelected {
		t.Fatalf("expected selected resolution, got %#v", resolution)
	}
	if resolution.MatchedCandidate != "done" {
		t.Fatalf("expected first matching candidate to be used, got %q", resolution.MatchedCandidate)
	}
	if !reflect.DeepEqual(resolution.TriedCandidates, []string{"In Review", "done"}) {
		t.Fatalf("unexpected tried candidates: %#v", resolution.TriedCandidates)
	}
}

func TestResolveTransitionSelectionDynamicUnavailable(t *testing.T) {
	resolution := resolveTransitionSelection([]Transition{
		{ID: "40", Name: "Close", ToStatusName: "Done"},
	}, contracts.TransitionSelection{
		Kind:                    contracts.TransitionSelectionDynamic,
		DynamicStatusCandidates: []string{"In Progress", "Review"},
	})

	if resolution.Kind != TransitionResolutionUnavailable {
		t.Fatalf("expected unavailable resolution, got %#v", resolution)
	}
	if resolution.ReasonCode != contracts.ReasonCodeTransitionUnavailable {
		t.Fatalf("unexpected reason code: %s", resolution.ReasonCode)
	}
	if !reflect.DeepEqual(resolution.TriedCandidates, []string{"In Progress", "Review"}) {
		t.Fatalf("unexpected tried candidates: %#v", resolution.TriedCandidates)
	}
}
