package issue

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
)

func TestParseRenderRoundTripIsDeterministic(t *testing.T) {
	input := `---
schema_version: "1"
key: "PROJ-123"
summary: "Fix Login: flow"
issue_type: "Task"
status: "In Progress"
priority: "  HIGH "
assignee: "  jdoe  "
labels: ["Bug", "p1", "bug"]
---

User-facing markdown description.

` + "```jira-adf" + `
{
  "version": 1,
  "type": "doc",
  "content": []
}
` + "```" + `
`

	doc, err := ParseDocument("/tmp/PROJ-999-wrong-slug.md", input)
	if err != nil {
		t.Fatalf("expected parse success, got: %v", err)
	}

	if doc.CanonicalKey != "PROJ-123" {
		t.Fatalf("expected canonical key from front matter, got %s", doc.CanonicalKey)
	}

	rendered, err := RenderDocument(doc)
	if err != nil {
		t.Fatalf("expected render success, got: %v", err)
	}

	reparsed, err := ParseDocument("/tmp/PROJ-123-fix-login-flow.md", rendered)
	if err != nil {
		t.Fatalf("expected reparse success, got: %v", err)
	}

	rerendered, err := RenderDocument(reparsed)
	if err != nil {
		t.Fatalf("expected rerender success, got: %v", err)
	}

	if rendered != rerendered {
		t.Fatalf("expected deterministic round-trip render\nfirst:\n%s\nsecond:\n%s", rendered, rerendered)
	}
}

func TestRenderDocumentUsesCanonicalFieldOrder(t *testing.T) {
	doc := Document{
		CanonicalKey: "PROJ-42",
		FrontMatter: FrontMatter{
			SchemaVersion: contracts.IssueFileSchemaVersionV1,
			Key:           "PROJ-42",
			Summary:       "Order fields",
			IssueType:     "Task",
			Status:        "Open",
			Priority:      "high",
			Labels:        []string{"z", "a"},
			CustomFields: map[string]json.RawMessage{
				"customfield_10011": json.RawMessage(`{"value":"Gold"}`),
			},
		},
	}

	rendered, err := RenderDocument(doc)
	if err != nil {
		t.Fatalf("expected render success, got: %v", err)
	}

	order := []string{
		"schema_version:",
		"key:",
		"summary:",
		"issue_type:",
		"status:",
		"priority:",
		"labels:",
		"custom_fields:",
	}
	lastIndex := -1
	for _, token := range order {
		index := strings.Index(rendered, token)
		if index == -1 {
			t.Fatalf("expected token in rendered output: %s", token)
		}
		if index <= lastIndex {
			t.Fatalf("expected canonical order for %s", token)
		}
		lastIndex = index
	}
}

func TestParseDocumentReturnsTypedErrorForMissingRequiredField(t *testing.T) {
	input := `---
schema_version: "1"
key: "PROJ-1"
issue_type: "Task"
status: "Open"
---

Body
`

	_, err := ParseDocument("/tmp/PROJ-1-missing-summary.md", input)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !IsParseErrorCode(err, ParseErrorCodeMissingRequiredField) {
		t.Fatalf("expected missing required field parse error, got: %v", err)
	}
}

func TestParseDocumentReturnsTypedErrorForInvalidSchemaVersion(t *testing.T) {
	input := `---
schema_version: "2"
key: "PROJ-1"
summary: "Summary"
issue_type: "Task"
status: "Open"
---
`

	_, err := ParseDocument("/tmp/PROJ-1.md", input)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !IsParseErrorCode(err, ParseErrorCodeInvalidSchemaVersion) {
		t.Fatalf("expected schema version parse error, got: %v", err)
	}
}

func TestParseDocumentParsesCustomFieldsJSON(t *testing.T) {
	input := `---
schema_version: "1"
key: "PROJ-1"
summary: "Summary"
issue_type: "Task"
status: "Open"
custom_fields: {"customfield_10010":"Enterprise","customfield_10011":{"id":"20000","value":"Gold"}}
---
`

	doc, err := ParseDocument("/tmp/PROJ-1.md", input)
	if err != nil {
		t.Fatalf("expected parse success, got: %v", err)
	}
	if got := string(doc.FrontMatter.CustomFields["customfield_10010"]); got != "\"Enterprise\"" {
		t.Fatalf("expected custom field value, got %q", got)
	}
}

func TestParseDocumentAllowsAliasedCustomFieldKey(t *testing.T) {
	input := `---
schema_version: "1"
key: "PROJ-1"
summary: "Summary"
issue_type: "Task"
status: "Open"
custom_fields: {"customer":"Enterprise"}
---
`

	doc, err := ParseDocument("/tmp/PROJ-1.md", input)
	if err != nil {
		t.Fatalf("expected parse success, got: %v", err)
	}
	if got := string(doc.FrontMatter.CustomFields["customer"]); got != "\"Enterprise\"" {
		t.Fatalf("unexpected custom field value: %q", got)
	}
}

func TestParseDocumentReturnsTypedErrorForMalformedRawADF(t *testing.T) {
	input := `---
schema_version: "1"
key: "PROJ-1"
summary: "Summary"
issue_type: "Task"
status: "Open"
---

Body

` + "```jira-adf" + `
{bad-json}
` + "```" + `
`

	_, err := ParseDocument("/tmp/PROJ-1.md", input)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !IsParseErrorCode(err, ParseErrorCodeMalformedRawADF) {
		t.Fatalf("expected malformed raw ADF parse error, got: %v", err)
	}
}
