package contracts

import (
	"encoding/json"
	"regexp"
	"strings"
)

const (
	IssueFileSchemaVersionV1 = "1"

	FrontMatterDelimiter = "---"

	RawADFFenceLanguage = "jira-adf"
	RawADFDocType       = "doc"
	RawADFDocVersion    = 1
)

// Contracted key formats.
var (
	JiraIssueKeyPattern  = regexp.MustCompile(`^[A-Z][A-Z0-9]+-[0-9]+$`)
	LocalDraftKeyPattern = regexp.MustCompile(`^L-[0-9a-f]+$`)
)

// RawADFFencedBlockPattern matches exactly one embedded raw ADF fenced block payload.
var RawADFFencedBlockPattern = regexp.MustCompile("(?s)```jira-adf[ \\t]*\\n(\\{.*?\\})\\n```")

type FrontMatterKey string

const (
	FrontMatterKeySchemaVersion FrontMatterKey = "schema_version"
	FrontMatterKeyKey           FrontMatterKey = "key"
	FrontMatterKeySummary       FrontMatterKey = "summary"
	FrontMatterKeyIssueType     FrontMatterKey = "issue_type"
	FrontMatterKeyStatus        FrontMatterKey = "status"
	FrontMatterKeyPriority      FrontMatterKey = "priority"
	FrontMatterKeyAssignee      FrontMatterKey = "assignee"
	FrontMatterKeyLabels        FrontMatterKey = "labels"
	FrontMatterKeyReporter      FrontMatterKey = "reporter"
	FrontMatterKeyCreatedAt     FrontMatterKey = "created_at"
	FrontMatterKeyUpdatedAt     FrontMatterKey = "updated_at"
	FrontMatterKeySyncedAt      FrontMatterKey = "synced_at"
)

// RequiredFrontMatterKeys are mandatory for deterministic parsing.
var RequiredFrontMatterKeys = []FrontMatterKey{
	FrontMatterKeySchemaVersion,
	FrontMatterKeyKey,
	FrontMatterKeySummary,
	FrontMatterKeyIssueType,
	FrontMatterKeyStatus,
}

// OptionalFrontMatterKeys are part of the frozen schema and may be omitted.
var OptionalFrontMatterKeys = []FrontMatterKey{
	FrontMatterKeyPriority,
	FrontMatterKeyAssignee,
	FrontMatterKeyLabels,
	FrontMatterKeyReporter,
	FrontMatterKeyCreatedAt,
	FrontMatterKeyUpdatedAt,
	FrontMatterKeySyncedAt,
}

// RawADFDoc is the expected envelope inside the jira-adf fenced block.
type RawADFDoc struct {
	Version int               `json:"version"`
	Type    string            `json:"type"`
	Content []json.RawMessage `json:"content"`
}

func SupportedFrontMatterKey(key FrontMatterKey) bool {
	for _, required := range RequiredFrontMatterKeys {
		if required == key {
			return true
		}
	}
	for _, optional := range OptionalFrontMatterKeys {
		if optional == key {
			return true
		}
	}
	return false
}

func AllFrontMatterKeys() []FrontMatterKey {
	keys := make([]FrontMatterKey, 0, len(RequiredFrontMatterKeys)+len(OptionalFrontMatterKeys))
	keys = append(keys, RequiredFrontMatterKeys...)
	keys = append(keys, OptionalFrontMatterKeys...)
	return keys
}

func ExtractRawADFJSON(markdown string) (string, bool) {
	match := RawADFFencedBlockPattern.FindStringSubmatch(markdown)
	if len(match) != 2 {
		return "", false
	}
	return strings.TrimSpace(match[1]), true
}

func ParseRawADFDoc(payload string) (RawADFDoc, error) {
	var doc RawADFDoc
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		return RawADFDoc{}, err
	}
	return doc, nil
}

func IsValidRawADFDoc(doc RawADFDoc) bool {
	return doc.Type == RawADFDocType && doc.Version == RawADFDocVersion
}
