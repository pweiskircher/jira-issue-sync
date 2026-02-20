package converter

import "github.com/pat/jira-issue-sync/internal/contracts"

// RiskSignal flags potentially lossy or unsafe description conversion outcomes.
type RiskSignal struct {
	ReasonCode contracts.ReasonCode
	Message    string
}

// MarkdownResult is the adapter output when rendering Markdown from ADF.
type MarkdownResult struct {
	Markdown string
	Risks    []RiskSignal
}

// ADFResult is the adapter output when rendering ADF from Markdown.
type ADFResult struct {
	ADFJSON string
	Risks   []RiskSignal
}

// Adapter defines the conversion boundary so conversion engines can be swapped.
type Adapter interface {
	ToMarkdown(adfJSON string) (MarkdownResult, error)
	ToADF(markdown string) (ADFResult, error)
}
