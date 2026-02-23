package execute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	"github.com/pweiskircher/jira-issue-sync/internal/converter"
	"github.com/pweiskircher/jira-issue-sync/internal/issue"
	"github.com/pweiskircher/jira-issue-sync/internal/jira"
	pushplan "github.com/pweiskircher/jira-issue-sync/internal/sync/push/plan"
)

type Options struct {
	Adapter             jira.Adapter
	Converter           converter.Adapter
	DryRun              bool
	TransitionSelection contracts.TransitionSelection
}

type Input struct {
	Key      string
	Local    issue.Document
	Original issue.Document
	Remote   issue.Document
}

type Outcome struct {
	Result        contracts.PerIssueResult
	RemoteUpdated bool
	FullyApplied  bool
}

func ExecuteIssue(ctx context.Context, options Options, input Input) Outcome {
	planInput, adfPayload, adfReason, adfErr := buildPlanInput(options.Converter, input)
	if adfErr != nil {
		return Outcome{Result: contracts.PerIssueResult{
			Key:    input.Key,
			Action: "push-error",
			Status: contracts.PerIssueStatusError,
			Messages: []contracts.IssueMessage{{
				Level:      "error",
				ReasonCode: adfReason,
				Text:       strings.TrimSpace(adfErr.Error()),
			}},
		}}
	}

	plan := pushplan.BuildIssuePlan(planInput)
	messages := messagesFromPlan(plan)
	result := contracts.PerIssueResult{Key: input.Key, Action: string(plan.Action)}

	if !plan.HasExecutableChanges() {
		if len(plan.Conflicts) > 0 {
			result.Status = contracts.PerIssueStatusConflict
		} else if len(plan.Blocked) > 0 {
			result.Status = contracts.PerIssueStatusWarning
		} else {
			result.Status = contracts.PerIssueStatusSkipped
		}
		result.Messages = messages
		return Outcome{Result: result, FullyApplied: result.Status == contracts.PerIssueStatusSkipped}
	}

	if options.DryRun {
		messages = append(messages, contracts.IssueMessage{Level: "info", ReasonCode: contracts.ReasonCodeDryRunNoWrite, Text: "dry-run: skipped remote mutations"})
		result.Status = statusFromPlan(plan)
		if result.Status == contracts.PerIssueStatusSuccess {
			result.Status = contracts.PerIssueStatusSkipped
		}
		result.Messages = messages
		return Outcome{Result: result}
	}

	remoteUpdated := false
	if request, hasUpdate := buildUpdateRequest(plan, adfPayload); hasUpdate {
		if err := options.Adapter.UpdateIssue(ctx, input.Key, request); err != nil {
			messages = append(messages, contracts.IssueMessage{Level: "error", ReasonCode: reasonFromError(err), Text: "failed to apply issue update: " + strings.TrimSpace(err.Error())})
			result.Status = contracts.PerIssueStatusError
			result.Action = "push-error"
			result.Messages = messages
			return Outcome{Result: result}
		}
		remoteUpdated = true
	}

	transitionSkipped := false
	if plan.Transition != nil {
		resolution, err := options.Adapter.ResolveTransition(ctx, input.Key, options.TransitionSelection)
		if err != nil {
			messages = append(messages, contracts.IssueMessage{Level: "error", ReasonCode: reasonFromError(err), Text: "failed to resolve transition: " + strings.TrimSpace(err.Error())})
			result.Status = contracts.PerIssueStatusError
			result.Action = "push-error"
			result.Messages = messages
			return Outcome{Result: result, RemoteUpdated: remoteUpdated}
		}

		switch resolution.Kind {
		case jira.TransitionResolutionSelected:
			if err := options.Adapter.ApplyTransition(ctx, input.Key, resolution.Transition.ID); err != nil {
				messages = append(messages, contracts.IssueMessage{Level: "error", ReasonCode: reasonFromError(err), Text: "failed to apply transition: " + strings.TrimSpace(err.Error())})
				result.Status = contracts.PerIssueStatusError
				result.Action = "push-error"
				result.Messages = messages
				return Outcome{Result: result, RemoteUpdated: remoteUpdated}
			}
			remoteUpdated = true
		case jira.TransitionResolutionAmbiguous, jira.TransitionResolutionUnavailable:
			reason := resolution.ReasonCode
			if reason == "" {
				reason = contracts.ReasonCodeTransitionUnavailable
			}
			messages = append(messages, contracts.IssueMessage{Level: "warning", ReasonCode: reason, Text: transitionSkipMessage(resolution, plan.Transition.TargetStatus)})
			transitionSkipped = true
		}
	}

	result.Status = statusFromPlan(plan)
	if transitionSkipped || result.Status == contracts.PerIssueStatusConflict {
		result.Status = contracts.PerIssueStatusWarning
	}
	result.Messages = messages
	if remoteUpdated {
		result.Action = "updated"
	}

	fullyApplied := result.Status == contracts.PerIssueStatusSuccess && plan.Action == pushplan.ActionUpdate
	return Outcome{Result: result, RemoteUpdated: remoteUpdated, FullyApplied: fullyApplied}
}

func buildPlanInput(markdownConverter converter.Adapter, input Input) (pushplan.IssueInput, *json.RawMessage, contracts.ReasonCode, error) {
	rawState := pushplan.RawADFStateValid
	if strings.TrimSpace(input.Local.RawADFJSON) == "" {
		rawState = pushplan.RawADFStateMissing
	} else if _, err := converter.ValidateAndCanonicalizeRawADF(input.Local.RawADFJSON); err != nil {
		rawState = pushplan.RawADFStateMalformed
	}

	adfResult, err := markdownConverter.ToADF(input.Local.MarkdownBody)
	if err != nil {
		reason := contracts.ReasonCodeValidationFailed
		if typed := asConverterError(err); typed != nil && typed.ReasonCode != "" {
			reason = typed.ReasonCode
		}
		return pushplan.IssueInput{}, nil, reason, fmt.Errorf("failed to convert markdown description to adf: %w", err)
	}

	trimmedADF := strings.TrimSpace(adfResult.ADFJSON)
	var payload *json.RawMessage
	if trimmedADF != "" {
		asRaw := json.RawMessage(trimmedADF)
		payload = &asRaw
	}

	planInput := pushplan.IssueInput{
		Local:    input.Local,
		Original: &input.Original,
		Remote:   input.Remote,
		DescriptionRisk: pushplan.DescriptionRiskInput{
			ConverterRisks: adfResult.Risks,
			LocalRawADF:    rawState,
		},
	}
	return planInput, payload, "", nil
}

func buildUpdateRequest(plan pushplan.IssuePlan, descriptionPayload *json.RawMessage) (jira.UpdateIssueRequest, bool) {
	request := jira.UpdateIssueRequest{
		Summary:      plan.Updates.Summary,
		Labels:       plan.Updates.Labels,
		PriorityName: plan.Updates.Priority,
	}
	if plan.Updates.Assignee != nil {
		request.AssigneeAccountID = plan.Updates.Assignee
	}
	if plan.Updates.Description != nil {
		request.Description = descriptionPayload
	}

	hasUpdate := request.Summary != nil || request.Description != nil || request.Labels != nil || request.AssigneeAccountID != nil || request.PriorityName != nil
	return request, hasUpdate
}

func statusFromPlan(plan pushplan.IssuePlan) contracts.PerIssueStatus {
	switch plan.Action {
	case pushplan.ActionBlocked:
		if len(plan.Conflicts) > 0 {
			return contracts.PerIssueStatusConflict
		}
		return contracts.PerIssueStatusWarning
	case pushplan.ActionUpdatePartial:
		return contracts.PerIssueStatusWarning
	case pushplan.ActionNoop:
		return contracts.PerIssueStatusSkipped
	default:
		return contracts.PerIssueStatusSuccess
	}
}

func messagesFromPlan(plan pushplan.IssuePlan) []contracts.IssueMessage {
	messages := make([]contracts.IssueMessage, 0, len(plan.Conflicts)+len(plan.Blocked))
	for _, conflict := range plan.Conflicts {
		messages = append(messages, contracts.IssueMessage{Level: "error", ReasonCode: conflict.ReasonCode, Text: strings.TrimSpace(conflict.Message)})
	}
	for _, blocked := range plan.Blocked {
		reasonCode := contracts.ReasonCodeValidationFailed
		if len(blocked.ReasonCodes) > 0 {
			reasonCode = blocked.ReasonCodes[0]
		}
		messages = append(messages, contracts.IssueMessage{Level: "warning", ReasonCode: reasonCode, Text: strings.TrimSpace(blocked.Message)})
	}
	return messages
}

func transitionSkipMessage(resolution jira.TransitionResolution, targetStatus string) string {
	candidate := strings.TrimSpace(resolution.MatchedCandidate)
	if candidate == "" {
		candidate = strings.TrimSpace(targetStatus)
	}
	if candidate == "" {
		candidate = "requested status"
	}

	switch resolution.Kind {
	case jira.TransitionResolutionAmbiguous:
		return "skipped ambiguous transition for " + candidate
	default:
		return "skipped unavailable transition for " + candidate
	}
}

func reasonFromError(err error) contracts.ReasonCode {
	if typed := asJiraError(err); typed != nil && typed.ReasonCode != "" {
		return typed.ReasonCode
	}
	if typed := asConverterError(err); typed != nil && typed.ReasonCode != "" {
		return typed.ReasonCode
	}
	return contracts.ReasonCodeValidationFailed
}

func asJiraError(err error) *jira.Error {
	var typed *jira.Error
	if errors.As(err, &typed) {
		return typed
	}
	return nil
}

func asConverterError(err error) *converter.Error {
	var typed *converter.Error
	if errors.As(err, &typed) {
		return typed
	}
	return nil
}
