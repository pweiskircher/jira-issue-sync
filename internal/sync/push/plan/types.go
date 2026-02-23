package plan

import (
	"strings"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	"github.com/pweiskircher/jira-issue-sync/internal/converter"
	"github.com/pweiskircher/jira-issue-sync/internal/issue"
)

// Action classifies deterministic push planning outcomes.
type Action string

const (
	ActionNoop          Action = "noop"
	ActionUpdate        Action = "update"
	ActionUpdatePartial Action = "update_partial"
	ActionBlocked       Action = "blocked"
)

// RawADFState captures the local raw-ADF block state needed for risk gating.
type RawADFState string

const (
	RawADFStateValid     RawADFState = "valid"
	RawADFStateMissing   RawADFState = "missing"
	RawADFStateMalformed RawADFState = "malformed"
)

// DescriptionRiskInput carries converter and raw-ADF risk signals.
type DescriptionRiskInput struct {
	ConverterRisks []converter.RiskSignal
	LocalRawADF    RawADFState
}

// IssueInput is the data required for deterministic three-way planning.
type IssueInput struct {
	Local           issue.Document
	Original        *issue.Document
	Remote          issue.Document
	DescriptionRisk DescriptionRiskInput
}

// UpdateSet contains safe, conflict-free writable field updates.
type UpdateSet struct {
	Summary     *string
	Description *string
	Labels      *[]string
	Assignee    *string
	Priority    *string
}

// TransitionPlan captures a desired status transition.
type TransitionPlan struct {
	TargetStatus string
}

// FieldConflict captures a field-level three-way conflict.
type FieldConflict struct {
	Field      contracts.JiraField
	ReasonCode contracts.ReasonCode
	Message    string
}

// BlockedField captures a gated (not executable) field update.
type BlockedField struct {
	Field       contracts.JiraField
	ReasonCodes []contracts.ReasonCode
	Message     string
}

// IssuePlan is an actionable deterministic plan for one issue.
type IssuePlan struct {
	Key        string
	Action     Action
	Updates    UpdateSet
	Transition *TransitionPlan
	Conflicts  []FieldConflict
	Blocked    []BlockedField
	Reasons    []contracts.ReasonCode
}

func (plan IssuePlan) HasExecutableChanges() bool {
	if plan.Transition != nil {
		return true
	}
	return plan.Updates.Summary != nil ||
		plan.Updates.Description != nil ||
		plan.Updates.Labels != nil ||
		plan.Updates.Assignee != nil ||
		plan.Updates.Priority != nil
}

func (plan IssuePlan) HasConflictsOrBlocks() bool {
	return len(plan.Conflicts) > 0 || len(plan.Blocked) > 0
}

func resolveAction(plan IssuePlan) Action {
	hasChanges := plan.HasExecutableChanges()
	hasBlocks := plan.HasConflictsOrBlocks()

	switch {
	case hasChanges && hasBlocks:
		return ActionUpdatePartial
	case hasChanges:
		return ActionUpdate
	case hasBlocks:
		return ActionBlocked
	default:
		return ActionNoop
	}
}

func appendUniqueReasonCode(reasonCodes []contracts.ReasonCode, reasonCode contracts.ReasonCode) []contracts.ReasonCode {
	if strings.TrimSpace(string(reasonCode)) == "" {
		return reasonCodes
	}
	for _, existing := range reasonCodes {
		if existing == reasonCode {
			return reasonCodes
		}
	}
	return append(reasonCodes, reasonCode)
}
