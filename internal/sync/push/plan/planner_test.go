package plan

import (
	"reflect"
	"testing"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	"github.com/pweiskircher/jira-issue-sync/internal/converter"
	"github.com/pweiskircher/jira-issue-sync/internal/issue"
)

func TestBuildIssuePlanReportsMissingBaseSnapshot(t *testing.T) {
	local := testDocument("PROJ-1", "Summary", "Body", "To Do", nil, "", "", "")
	remote := testDocument("PROJ-1", "Summary", "Body", "To Do", nil, "", "", "")

	plan := BuildIssuePlan(IssueInput{Local: local, Remote: remote})

	if plan.Action != ActionBlocked {
		t.Fatalf("unexpected action: got=%s want=%s", plan.Action, ActionBlocked)
	}
	if len(plan.Conflicts) != 1 {
		t.Fatalf("expected one conflict, got %d", len(plan.Conflicts))
	}
	if plan.Conflicts[0].ReasonCode != contracts.ReasonCodeConflictBaseSnapshotMissing {
		t.Fatalf("unexpected reason: got=%s", plan.Conflicts[0].ReasonCode)
	}
}

func TestBuildIssuePlanBuildsSafeUpdatesAndTransition(t *testing.T) {
	base := testDocument("PROJ-1", "Old", "Old body", "To Do", []string{"a"}, "", "Low", "")
	local := testDocument("PROJ-1", " New summary ", "Old body", "Done", []string{"b", "a"}, "", " high ", "")
	remote := testDocument("PROJ-1", "Old", "Old body", "To Do", []string{"a"}, "", "Low", "")

	plan := BuildIssuePlan(IssueInput{Local: local, Original: &base, Remote: remote})

	if plan.Action != ActionUpdate {
		t.Fatalf("unexpected action: got=%s want=%s", plan.Action, ActionUpdate)
	}
	if plan.Updates.Summary == nil || *plan.Updates.Summary != "New summary" {
		t.Fatalf("expected summary update, got %#v", plan.Updates.Summary)
	}
	if plan.Updates.Priority == nil || *plan.Updates.Priority != "High" {
		t.Fatalf("expected priority update, got %#v", plan.Updates.Priority)
	}
	if plan.Updates.Labels == nil || !reflect.DeepEqual(*plan.Updates.Labels, []string{"a", "b"}) {
		t.Fatalf("expected normalized label update, got %#v", plan.Updates.Labels)
	}
	if plan.Transition == nil || plan.Transition.TargetStatus != "Done" {
		t.Fatalf("expected status transition to Done, got %#v", plan.Transition)
	}
}

func TestBuildIssuePlanDetectsConflictAndKeepsSafeChanges(t *testing.T) {
	base := testDocument("PROJ-1", "Old", "Body", "To Do", []string{"a"}, "", "", "")
	local := testDocument("PROJ-1", "Mine", "Body", "To Do", []string{"a", "b"}, "", "", "")
	remote := testDocument("PROJ-1", "Theirs", "Body", "To Do", []string{"a"}, "", "", "")

	plan := BuildIssuePlan(IssueInput{Local: local, Original: &base, Remote: remote})

	if plan.Action != ActionUpdatePartial {
		t.Fatalf("unexpected action: got=%s want=%s", plan.Action, ActionUpdatePartial)
	}
	if len(plan.Conflicts) != 1 || plan.Conflicts[0].Field != contracts.JiraFieldSummary {
		t.Fatalf("expected summary conflict, got %#v", plan.Conflicts)
	}
	if plan.Updates.Labels == nil || !reflect.DeepEqual(*plan.Updates.Labels, []string{"a", "b"}) {
		t.Fatalf("expected label update to remain executable, got %#v", plan.Updates.Labels)
	}
}

func TestBuildIssuePlanBlocksRiskyDescriptionWhenRawADFIsMissing(t *testing.T) {
	base := testDocument("PROJ-1", "Summary", "Old", "To Do", nil, "", "", `{"version":1,"type":"doc","content":[]}`)
	local := testDocument("PROJ-1", "Summary", "New", "To Do", nil, "", "", "")
	remote := testDocument("PROJ-1", "Summary", "Old", "To Do", nil, "", "", `{"version":1,"type":"doc","content":[]}`)

	plan := BuildIssuePlan(IssueInput{
		Local:    local,
		Original: &base,
		Remote:   remote,
		DescriptionRisk: DescriptionRiskInput{
			LocalRawADF: RawADFStateMissing,
		},
	})

	if plan.Updates.Description != nil {
		t.Fatalf("description update should be blocked when raw ADF is missing")
	}
	if plan.Action != ActionBlocked {
		t.Fatalf("unexpected action: got=%s want=%s", plan.Action, ActionBlocked)
	}
	if len(plan.Blocked) != 1 {
		t.Fatalf("expected one blocked field, got %d", len(plan.Blocked))
	}
	expectedReasonCodes := []contracts.ReasonCode{
		contracts.ReasonCodeDescriptionRiskyBlocked,
		contracts.ReasonCodeDescriptionADFBlockMissing,
	}
	if !reflect.DeepEqual(plan.Blocked[0].ReasonCodes, expectedReasonCodes) {
		t.Fatalf("unexpected reason codes: got=%v want=%v", plan.Blocked[0].ReasonCodes, expectedReasonCodes)
	}
}

func TestBuildIssuePlanBlocksRiskyDescriptionFromConverterSignals(t *testing.T) {
	base := testDocument("PROJ-1", "Summary", "Old", "To Do", nil, "", "", "")
	local := testDocument("PROJ-1", "Summary", "New", "To Do", nil, "", "", "")
	remote := testDocument("PROJ-1", "Summary", "Old", "To Do", nil, "", "", "")

	plan := BuildIssuePlan(IssueInput{
		Local:    local,
		Original: &base,
		Remote:   remote,
		DescriptionRisk: DescriptionRiskInput{
			LocalRawADF: RawADFStateValid,
			ConverterRisks: []converter.RiskSignal{
				{ReasonCode: contracts.ReasonCodeDescriptionADFBlockMalformed, Message: "lossy conversion"},
				{ReasonCode: contracts.ReasonCodeDescriptionADFBlockMalformed, Message: "dedupe"},
			},
		},
	})

	if plan.Updates.Description != nil {
		t.Fatalf("description update should be blocked by converter risk")
	}
	expectedReasonCodes := []contracts.ReasonCode{
		contracts.ReasonCodeDescriptionRiskyBlocked,
		contracts.ReasonCodeDescriptionADFBlockMalformed,
	}
	if !reflect.DeepEqual(plan.Blocked[0].ReasonCodes, expectedReasonCodes) {
		t.Fatalf("unexpected reason codes: got=%v want=%v", plan.Blocked[0].ReasonCodes, expectedReasonCodes)
	}
}

func TestBuildIssuePlanAllowsSafeDescriptionUpdate(t *testing.T) {
	base := testDocument("PROJ-1", "Summary", "Old", "To Do", nil, "", "", "")
	local := testDocument("PROJ-1", "Summary", "New", "To Do", nil, "", "", "")
	remote := testDocument("PROJ-1", "Summary", "Old", "To Do", nil, "", "", "")

	plan := BuildIssuePlan(IssueInput{
		Local:    local,
		Original: &base,
		Remote:   remote,
		DescriptionRisk: DescriptionRiskInput{
			LocalRawADF: RawADFStateValid,
		},
	})

	if plan.Action != ActionUpdate {
		t.Fatalf("unexpected action: got=%s want=%s", plan.Action, ActionUpdate)
	}
	if plan.Updates.Description == nil || *plan.Updates.Description != "New" {
		t.Fatalf("expected safe description update, got %#v", plan.Updates.Description)
	}
}

func TestBuildIssuePlanValidatesConsistentIssueKeys(t *testing.T) {
	base := testDocument("PROJ-1", "Summary", "Body", "To Do", nil, "", "", "")
	local := testDocument("PROJ-1", "Summary", "Body", "To Do", nil, "", "", "")
	remote := testDocument("PROJ-2", "Summary", "Body", "To Do", nil, "", "", "")

	plan := BuildIssuePlan(IssueInput{Local: local, Original: &base, Remote: remote})

	if plan.Action != ActionBlocked {
		t.Fatalf("unexpected action: got=%s want=%s", plan.Action, ActionBlocked)
	}
	if len(plan.Blocked) != 1 {
		t.Fatalf("expected one blocked entry, got %d", len(plan.Blocked))
	}
	if plan.Blocked[0].ReasonCodes[0] != contracts.ReasonCodeValidationFailed {
		t.Fatalf("unexpected validation reason: %#v", plan.Blocked[0])
	}
}

func testDocument(
	key string,
	summary string,
	description string,
	status string,
	labels []string,
	assignee string,
	priority string,
	rawADF string,
) issue.Document {
	return issue.Document{
		CanonicalKey: key,
		FrontMatter: issue.FrontMatter{
			Key:      key,
			Summary:  summary,
			Status:   status,
			Labels:   append([]string(nil), labels...),
			Assignee: assignee,
			Priority: priority,
		},
		MarkdownBody: description,
		RawADFJSON:   rawADF,
	}
}
