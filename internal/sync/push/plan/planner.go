package plan

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/pweiskircher/jira-issue-sync/internal/conflict"
	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	"github.com/pweiskircher/jira-issue-sync/internal/issue"
)

var writableFieldOrder = []contracts.JiraField{
	contracts.JiraFieldSummary,
	contracts.JiraFieldDescription,
	contracts.JiraFieldLabels,
	contracts.JiraFieldAssignee,
	contracts.JiraFieldPriority,
	contracts.JiraFieldStatus,
}

type normalizedWritableFields struct {
	Summary     string
	Description string
	Labels      []string
	Assignee    string
	Priority    string
	Status      string
}

// BuildIssuePlan creates a deterministic per-issue push plan.
func BuildIssuePlan(input IssueInput) IssuePlan {
	plan := IssuePlan{Key: canonicalKey(input)}

	if input.Original == nil {
		plan.Conflicts = append(plan.Conflicts, FieldConflict{
			ReasonCode: contracts.ReasonCodeConflictBaseSnapshotMissing,
			Message:    "original snapshot is required for three-way planning",
		})
		plan.Reasons = appendUniqueReasonCode(plan.Reasons, contracts.ReasonCodeConflictBaseSnapshotMissing)
		plan.Action = resolveAction(plan)
		return plan
	}

	if reasonCode, message, ok := validateIssueKeys(input); ok {
		plan.Blocked = append(plan.Blocked, BlockedField{
			ReasonCodes: []contracts.ReasonCode{reasonCode},
			Message:     message,
		})
		plan.Reasons = appendUniqueReasonCode(plan.Reasons, reasonCode)
		plan.Action = resolveAction(plan)
		return plan
	}

	local := normalizeWritableFields(input.Local)
	base := normalizeWritableFields(*input.Original)
	remote := normalizeWritableFields(input.Remote)

	for _, field := range writableFieldOrder {
		switch field {
		case contracts.JiraFieldSummary:
			comparison := conflict.CompareComparable(base.Summary, local.Summary, remote.Summary)
			applyFieldComparison(&plan, field, comparison, func() {
				value := local.Summary
				plan.Updates.Summary = &value
			})
		case contracts.JiraFieldDescription:
			comparison := conflict.CompareComparable(base.Description, local.Description, remote.Description)
			applyDescriptionComparison(&plan, comparison, local.Description, strings.TrimSpace(input.Original.RawADFJSON) != "", input.DescriptionRisk)
		case contracts.JiraFieldLabels:
			comparison := conflict.Compare(base.Labels, local.Labels, remote.Labels, func(left, right []string) bool {
				return reflect.DeepEqual(left, right)
			})
			applyFieldComparison(&plan, field, comparison, func() {
				value := append([]string(nil), local.Labels...)
				plan.Updates.Labels = &value
			})
		case contracts.JiraFieldAssignee:
			comparison := conflict.CompareComparable(base.Assignee, local.Assignee, remote.Assignee)
			applyFieldComparison(&plan, field, comparison, func() {
				value := local.Assignee
				plan.Updates.Assignee = &value
			})
		case contracts.JiraFieldPriority:
			comparison := conflict.CompareComparable(base.Priority, local.Priority, remote.Priority)
			applyFieldComparison(&plan, field, comparison, func() {
				value := local.Priority
				plan.Updates.Priority = &value
			})
		case contracts.JiraFieldStatus:
			comparison := conflict.CompareComparable(base.Status, local.Status, remote.Status)
			applyFieldComparison(&plan, field, comparison, func() {
				plan.Transition = &TransitionPlan{TargetStatus: local.Status}
			})
		}
	}

	plan.Action = resolveAction(plan)
	return plan
}

func applyFieldComparison[T any](plan *IssuePlan, field contracts.JiraField, comparison conflict.Comparison[T], applyLocalChange func()) {
	if plan == nil {
		return
	}

	switch comparison.Outcome {
	case conflict.OutcomeLocalChanged:
		applyLocalChange()
	case conflict.OutcomeConflict:
		plan.Conflicts = append(plan.Conflicts, FieldConflict{
			Field:      field,
			ReasonCode: contracts.ReasonCodeConflictFieldChangedBoth,
			Message:    fmt.Sprintf("field %q changed both locally and remotely", field),
		})
		plan.Reasons = appendUniqueReasonCode(plan.Reasons, contracts.ReasonCodeConflictFieldChangedBoth)
	}
}

func applyDescriptionComparison(
	plan *IssuePlan,
	comparison conflict.Comparison[string],
	localDescription string,
	hadBaselineRawADF bool,
	descriptionRiskInput DescriptionRiskInput,
) {
	if plan == nil {
		return
	}

	switch comparison.Outcome {
	case conflict.OutcomeLocalChanged:
		riskReasonCodes := classifyDescriptionRisk(hadBaselineRawADF, descriptionRiskInput)
		if len(riskReasonCodes) > 0 {
			reasonCodes := make([]contracts.ReasonCode, 0, len(riskReasonCodes)+1)
			reasonCodes = append(reasonCodes, contracts.ReasonCodeDescriptionRiskyBlocked)
			reasonCodes = append(reasonCodes, riskReasonCodes...)
			plan.Blocked = append(plan.Blocked, BlockedField{
				Field:       contracts.JiraFieldDescription,
				ReasonCodes: reasonCodes,
				Message:     "description update was blocked because conversion risk was detected",
			})
			for _, reasonCode := range reasonCodes {
				plan.Reasons = appendUniqueReasonCode(plan.Reasons, reasonCode)
			}
			return
		}

		value := localDescription
		plan.Updates.Description = &value
	case conflict.OutcomeConflict:
		plan.Conflicts = append(plan.Conflicts, FieldConflict{
			Field:      contracts.JiraFieldDescription,
			ReasonCode: contracts.ReasonCodeConflictFieldChangedBoth,
			Message:    "field \"description\" changed both locally and remotely",
		})
		plan.Reasons = appendUniqueReasonCode(plan.Reasons, contracts.ReasonCodeConflictFieldChangedBoth)
	}
}

func classifyDescriptionRisk(hadBaselineRawADF bool, input DescriptionRiskInput) []contracts.ReasonCode {
	reasonCodes := make([]contracts.ReasonCode, 0)

	if hadBaselineRawADF {
		switch input.LocalRawADF {
		case RawADFStateMissing:
			reasonCodes = append(reasonCodes, contracts.ReasonCodeDescriptionADFBlockMissing)
		case RawADFStateMalformed:
			reasonCodes = append(reasonCodes, contracts.ReasonCodeDescriptionADFBlockMalformed)
		}
	}

	for _, riskSignal := range input.ConverterRisks {
		reasonCodes = appendUniqueReasonCode(reasonCodes, riskSignal.ReasonCode)
	}

	return reasonCodes
}

func canonicalKey(input IssueInput) string {
	if key := strings.TrimSpace(input.Local.CanonicalKey); key != "" {
		return key
	}
	if key := strings.TrimSpace(input.Local.FrontMatter.Key); key != "" {
		return key
	}
	if input.Original != nil {
		if key := strings.TrimSpace(input.Original.CanonicalKey); key != "" {
			return key
		}
		if key := strings.TrimSpace(input.Original.FrontMatter.Key); key != "" {
			return key
		}
	}
	if key := strings.TrimSpace(input.Remote.CanonicalKey); key != "" {
		return key
	}
	return strings.TrimSpace(input.Remote.FrontMatter.Key)
}

func validateIssueKeys(input IssueInput) (contracts.ReasonCode, string, bool) {
	if input.Original == nil {
		return "", "", false
	}

	localKey := strings.TrimSpace(canonicalKey(input))
	originalKey := strings.TrimSpace(input.Original.CanonicalKey)
	if originalKey == "" {
		originalKey = strings.TrimSpace(input.Original.FrontMatter.Key)
	}
	remoteKey := strings.TrimSpace(input.Remote.CanonicalKey)
	if remoteKey == "" {
		remoteKey = strings.TrimSpace(input.Remote.FrontMatter.Key)
	}

	if localKey == "" || originalKey == "" || remoteKey == "" {
		return contracts.ReasonCodeValidationFailed, "issue key is required for local/original/remote documents", true
	}
	if localKey != originalKey || localKey != remoteKey {
		return contracts.ReasonCodeValidationFailed, "issue keys must match across local/original/remote documents", true
	}
	return "", "", false
}

func normalizeWritableFields(document issue.Document) normalizedWritableFields {
	return normalizedWritableFields{
		Summary:     contracts.NormalizeSingleValue(contracts.NormalizationTrimOuterWhitespace, document.FrontMatter.Summary),
		Description: contracts.NormalizeSingleValue(contracts.NormalizationNormalizeLineEndings, document.MarkdownBody),
		Labels:      contracts.NormalizeLabels(document.FrontMatter.Labels),
		Assignee:    contracts.NormalizeSingleValue(contracts.NormalizationTrimEmptyToNull, document.FrontMatter.Assignee),
		Priority:    contracts.NormalizeSingleValue(contracts.NormalizationTrimAndTitleCase, document.FrontMatter.Priority),
		Status:      contracts.NormalizeSingleValue(contracts.NormalizationTrimOuterWhitespace, document.FrontMatter.Status),
	}
}
