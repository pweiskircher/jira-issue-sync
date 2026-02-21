package transitionsintegration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/pat/jira-issue-sync/internal/commands"
	"github.com/pat/jira-issue-sync/internal/config"
	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/jira"
)

func TestPushTransitionAmbiguityUsesTypedReasonCodeAndOverridePrecedence(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTransitionConfig(t, workspace)
	writeTransitionIssueFixtures(t, workspace)

	adapter := &transitionAdapterStub{
		issues: map[string]jira.Issue{
			"PROJ-9": {
				Key: "PROJ-9",
				Fields: jira.IssueFields{
					Summary:     "Remote summary",
					Description: json.RawMessage(`{"version":1,"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"body"}]}]}`),
					Status:      &jira.StatusRef{Name: "To Do"},
					IssueType:   &jira.NamedRef{Name: "Task"},
				},
			},
		},
		resolutions: map[string]jira.TransitionResolution{
			"PROJ-9": {
				Kind:       jira.TransitionResolutionAmbiguous,
				ReasonCode: contracts.ReasonCodeTransitionAmbiguous,
			},
		},
	}

	report, err := commands.RunPush(context.Background(), workspace, commands.PushOptions{
		Profile:     "team",
		Adapter:     adapter,
		Environment: config.Environment{JiraAPIToken: "token"},
	})
	if err != nil {
		t.Fatalf("run push failed: %v", err)
	}

	selection, ok := adapter.selectionByIssue["PROJ-9"]
	if !ok {
		t.Fatalf("expected transition selection to be passed to adapter")
	}
	if selection.Kind != contracts.TransitionSelectionByID || selection.TransitionID != "42" {
		t.Fatalf("expected override precedence id > name > dynamic, got %#v", selection)
	}

	if len(report.Issues) != 1 {
		t.Fatalf("expected one per-issue result, got %#v", report.Issues)
	}
	result := report.Issues[0]
	if result.Status != contracts.PerIssueStatusWarning {
		t.Fatalf("expected warning status when transition is ambiguous, got %#v", result)
	}
	if !containsReason(result, contracts.ReasonCodeTransitionAmbiguous) {
		t.Fatalf("expected typed transition_ambiguous reason code, got %#v", result.Messages)
	}
}

func containsReason(result contracts.PerIssueResult, code contracts.ReasonCode) bool {
	for _, message := range result.Messages {
		if message.ReasonCode == code {
			return true
		}
	}
	return false
}
