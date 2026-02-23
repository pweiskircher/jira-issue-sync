package contracts

import (
	"sort"
	"strings"
)

type JiraField string

const (
	JiraFieldSummary     JiraField = "summary"
	JiraFieldDescription JiraField = "description"
	JiraFieldLabels      JiraField = "labels"
	JiraFieldAssignee    JiraField = "assignee"
	JiraFieldPriority    JiraField = "priority"
	JiraFieldStatus      JiraField = "status"

	JiraFieldKey          JiraField = "key"
	JiraFieldIssueType    JiraField = "issue_type"
	JiraFieldReporter     JiraField = "reporter"
	JiraFieldCreatedAt    JiraField = "created_at"
	JiraFieldUpdatedAt    JiraField = "updated_at"
	JiraFieldSyncedAt     JiraField = "synced_at"
	JiraFieldCustomFields JiraField = "custom_fields"
)

type SyncDirection string

const (
	SyncDirectionBidirectional SyncDirection = "bidirectional"
	SyncDirectionReadOnly      SyncDirection = "read_only"
)

type NormalizationRule string

const (
	NormalizationIdentity             NormalizationRule = "identity"
	NormalizationTrimOuterWhitespace  NormalizationRule = "trim_outer_whitespace"
	NormalizationNormalizeLineEndings NormalizationRule = "normalize_line_endings"
	NormalizationLabelsCanonicalSet   NormalizationRule = "labels_canonical_set"
	NormalizationTrimEmptyToNull      NormalizationRule = "trim_empty_to_null"
	NormalizationTrimAndTitleCase     NormalizationRule = "trim_and_title_case"
)

type UnsupportedFieldPolicy string

const (
	UnsupportedFieldPolicyWarnAndIgnore UnsupportedFieldPolicy = "warn_and_ignore"
)

const DefaultUnsupportedFieldReasonCode = ReasonCodeUnsupportedFieldIgnored

// UnsupportedJiraFieldsMVP are explicit non-goals for writable syncing in MVP.
var UnsupportedJiraFieldsMVP = []string{
	"comments",
	"attachments",
	"worklogs",
	"sprint",
	"epic_link",
}

type FieldContract struct {
	Field             JiraField
	Direction         SyncDirection
	Normalization     NormalizationRule
	UnsupportedPolicy UnsupportedFieldPolicy
}

var WritableFieldContracts = []FieldContract{
	{Field: JiraFieldSummary, Direction: SyncDirectionBidirectional, Normalization: NormalizationTrimOuterWhitespace, UnsupportedPolicy: UnsupportedFieldPolicyWarnAndIgnore},
	{Field: JiraFieldDescription, Direction: SyncDirectionBidirectional, Normalization: NormalizationNormalizeLineEndings, UnsupportedPolicy: UnsupportedFieldPolicyWarnAndIgnore},
	{Field: JiraFieldLabels, Direction: SyncDirectionBidirectional, Normalization: NormalizationLabelsCanonicalSet, UnsupportedPolicy: UnsupportedFieldPolicyWarnAndIgnore},
	{Field: JiraFieldAssignee, Direction: SyncDirectionBidirectional, Normalization: NormalizationTrimEmptyToNull, UnsupportedPolicy: UnsupportedFieldPolicyWarnAndIgnore},
	{Field: JiraFieldPriority, Direction: SyncDirectionBidirectional, Normalization: NormalizationTrimAndTitleCase, UnsupportedPolicy: UnsupportedFieldPolicyWarnAndIgnore},
	{Field: JiraFieldStatus, Direction: SyncDirectionBidirectional, Normalization: NormalizationTrimOuterWhitespace, UnsupportedPolicy: UnsupportedFieldPolicyWarnAndIgnore},
}

var ReadOnlyFieldContracts = []FieldContract{
	{Field: JiraFieldKey, Direction: SyncDirectionReadOnly, Normalization: NormalizationTrimOuterWhitespace, UnsupportedPolicy: UnsupportedFieldPolicyWarnAndIgnore},
	{Field: JiraFieldIssueType, Direction: SyncDirectionReadOnly, Normalization: NormalizationTrimOuterWhitespace, UnsupportedPolicy: UnsupportedFieldPolicyWarnAndIgnore},
	{Field: JiraFieldReporter, Direction: SyncDirectionReadOnly, Normalization: NormalizationTrimOuterWhitespace, UnsupportedPolicy: UnsupportedFieldPolicyWarnAndIgnore},
	{Field: JiraFieldCreatedAt, Direction: SyncDirectionReadOnly, Normalization: NormalizationIdentity, UnsupportedPolicy: UnsupportedFieldPolicyWarnAndIgnore},
	{Field: JiraFieldUpdatedAt, Direction: SyncDirectionReadOnly, Normalization: NormalizationIdentity, UnsupportedPolicy: UnsupportedFieldPolicyWarnAndIgnore},
	{Field: JiraFieldSyncedAt, Direction: SyncDirectionReadOnly, Normalization: NormalizationIdentity, UnsupportedPolicy: UnsupportedFieldPolicyWarnAndIgnore},
	{Field: JiraFieldCustomFields, Direction: SyncDirectionReadOnly, Normalization: NormalizationIdentity, UnsupportedPolicy: UnsupportedFieldPolicyWarnAndIgnore},
}

func SupportedWritableField(field JiraField) bool {
	for _, contract := range WritableFieldContracts {
		if contract.Field == field {
			return true
		}
	}
	return false
}

func SupportedReadOnlyField(field JiraField) bool {
	for _, contract := range ReadOnlyFieldContracts {
		if contract.Field == field {
			return true
		}
	}
	return false
}

func NormalizeSingleValue(rule NormalizationRule, value string) string {
	switch rule {
	case NormalizationTrimOuterWhitespace:
		return strings.TrimSpace(value)
	case NormalizationNormalizeLineEndings:
		normalized := strings.ReplaceAll(value, "\r\n", "\n")
		normalized = strings.ReplaceAll(normalized, "\r", "\n")
		return normalized
	case NormalizationTrimEmptyToNull:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return ""
		}
		return trimmed
	case NormalizationTrimAndTitleCase:
		trimmed := strings.TrimSpace(strings.ToLower(value))
		if trimmed == "" {
			return ""
		}
		return strings.ToUpper(trimmed[:1]) + trimmed[1:]
	default:
		return value
	}
}

func NormalizeLabels(values []string) []string {
	canonical := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		label := strings.ToLower(strings.TrimSpace(value))
		if label == "" {
			continue
		}
		if _, exists := seen[label]; exists {
			continue
		}
		seen[label] = struct{}{}
		canonical = append(canonical, label)
	}
	sort.Strings(canonical)
	return canonical
}
