package issue

import "github.com/pat/jira-issue-sync/internal/contracts"

// FrontMatter captures the frozen issue file schema.
type FrontMatter struct {
	SchemaVersion string
	Key           string
	Summary       string
	IssueType     string
	Status        string
	Priority      string
	Assignee      string
	Labels        []string
	Reporter      string
	CreatedAt     string
	UpdatedAt     string
	SyncedAt      string
}

// Document is the deterministic in-memory issue model.
type Document struct {
	CanonicalKey string
	FrontMatter  FrontMatter
	MarkdownBody string
	RawADFJSON   string
}

// CanonicalFrontMatterOrder is the deterministic render order.
var CanonicalFrontMatterOrder = []contracts.FrontMatterKey{
	contracts.FrontMatterKeySchemaVersion,
	contracts.FrontMatterKeyKey,
	contracts.FrontMatterKeySummary,
	contracts.FrontMatterKeyIssueType,
	contracts.FrontMatterKeyStatus,
	contracts.FrontMatterKeyPriority,
	contracts.FrontMatterKeyAssignee,
	contracts.FrontMatterKeyLabels,
	contracts.FrontMatterKeyReporter,
	contracts.FrontMatterKeyCreatedAt,
	contracts.FrontMatterKeyUpdatedAt,
	contracts.FrontMatterKeySyncedAt,
}
