package pull

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	"github.com/pweiskircher/jira-issue-sync/internal/converter"
)

// ADFMarkdownConverter provides a deterministic MVP ADF -> Markdown projection.
type ADFMarkdownConverter struct{}

func NewADFMarkdownConverter() ADFMarkdownConverter {
	return ADFMarkdownConverter{}
}

func (c ADFMarkdownConverter) ToMarkdown(adfJSON string) (converter.MarkdownResult, error) {
	trimmed := strings.TrimSpace(adfJSON)
	if trimmed == "" {
		return converter.MarkdownResult{}, nil
	}

	rawDoc, err := converter.ValidateAndCanonicalizeRawADF(trimmed)
	if err != nil {
		return converter.MarkdownResult{}, err
	}

	var envelope struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal([]byte(rawDoc), &envelope); err != nil {
		return converter.MarkdownResult{}, &converter.Error{
			Code:       converter.ErrorCodeMalformedADFJSON,
			ReasonCode: contracts.ReasonCodeDescriptionADFBlockMalformed,
			Message:    "failed to decode adf payload",
			Err:        err,
		}
	}

	lines := make([]string, 0, len(envelope.Content))
	for _, node := range envelope.Content {
		line := strings.TrimSpace(renderNode(node))
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}

	return converter.MarkdownResult{Markdown: strings.Join(lines, "\n\n")}, nil
}

func (c ADFMarkdownConverter) ToADF(markdown string) (converter.ADFResult, error) {
	trimmed := strings.TrimSpace(markdown)
	if trimmed == "" {
		return converter.ADFResult{ADFJSON: `{"version":1,"type":"doc","content":[]}`}, nil
	}

	payload := map[string]any{
		"version": 1,
		"type":    "doc",
		"content": []map[string]any{{
			"type": "paragraph",
			"content": []map[string]any{{
				"type": "text",
				"text": trimmed,
			}},
		}},
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return converter.ADFResult{}, fmt.Errorf("failed to encode adf payload: %w", err)
	}
	return converter.ADFResult{ADFJSON: string(encoded)}, nil
}

func renderNode(raw json.RawMessage) string {
	var node map[string]any
	if err := json.Unmarshal(raw, &node); err != nil {
		return ""
	}

	nodeType, _ := node["type"].(string)
	switch nodeType {
	case "text":
		text, _ := node["text"].(string)
		return text
	case "hardBreak":
		return "\n"
	case "bulletList":
		children := renderChildren(node)
		if len(children) == 0 {
			return ""
		}
		lines := make([]string, 0, len(children))
		for _, child := range children {
			child = strings.TrimSpace(child)
			if child == "" {
				continue
			}
			lines = append(lines, "- "+child)
		}
		return strings.Join(lines, "\n")
	case "orderedList":
		children := renderChildren(node)
		if len(children) == 0 {
			return ""
		}
		lines := make([]string, 0, len(children))
		for index, child := range children {
			child = strings.TrimSpace(child)
			if child == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("%d. %s", index+1, child))
		}
		return strings.Join(lines, "\n")
	default:
		return strings.TrimSpace(strings.Join(renderChildren(node), ""))
	}
}

func renderChildren(node map[string]any) []string {
	rawChildren, ok := node["content"].([]any)
	if !ok || len(rawChildren) == 0 {
		return nil
	}

	parts := make([]string, 0, len(rawChildren))
	for _, rawChild := range rawChildren {
		encoded, err := json.Marshal(rawChild)
		if err != nil {
			continue
		}
		value := renderNode(encoded)
		if value == "" {
			continue
		}
		parts = append(parts, value)
	}
	return parts
}
