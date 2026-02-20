package issue

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pat/jira-issue-sync/internal/contracts"
	"github.com/pat/jira-issue-sync/internal/converter"
)

// ParseDocument parses a markdown issue file into a deterministic model.
func ParseDocument(path, content string) (Document, error) {
	normalized := contracts.NormalizeSingleValue(contracts.NormalizationNormalizeLineEndings, content)
	frontMatterLines, body, err := splitFrontMatter(normalized)
	if err != nil {
		return Document{}, err
	}

	parsed, err := parseFrontMatter(frontMatterLines)
	if err != nil {
		return Document{}, err
	}

	frontMatter, err := buildFrontMatter(parsed)
	if err != nil {
		return Document{}, err
	}

	filenameKey, _ := ParseFilenameKey(path)
	canonicalKey := resolveCanonicalKey(frontMatter.Key, filenameKey)
	if canonicalKey == "" {
		return Document{}, &ParseError{
			Code:       ParseErrorCodeMissingRequiredField,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Field:      contracts.FrontMatterKeyKey,
			Message:    "issue key is required in front matter or filename",
		}
	}
	if !contracts.JiraIssueKeyPattern.MatchString(canonicalKey) && !contracts.LocalDraftKeyPattern.MatchString(canonicalKey) {
		return Document{}, &ParseError{
			Code:       ParseErrorCodeInvalidIssueKey,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Field:      contracts.FrontMatterKeyKey,
			Message:    "issue key does not match supported key formats",
		}
	}
	frontMatter.Key = canonicalKey

	markdownBody, rawADFJSON, err := extractAndValidateRawADF(body)
	if err != nil {
		return Document{}, err
	}

	return Document{
		CanonicalKey: canonicalKey,
		FrontMatter:  frontMatter,
		MarkdownBody: markdownBody,
		RawADFJSON:   rawADFJSON,
	}, nil
}

// RenderDocument renders the deterministic canonical markdown issue format.
func RenderDocument(doc Document) (string, error) {
	canonical, err := canonicalizeDocument(doc)
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	builder.WriteString(contracts.FrontMatterDelimiter)
	builder.WriteString("\n")

	for _, key := range CanonicalFrontMatterOrder {
		if line, ok := renderFrontMatterLine(canonical.FrontMatter, key); ok {
			builder.WriteString(line)
			builder.WriteString("\n")
		}
	}

	builder.WriteString(contracts.FrontMatterDelimiter)
	builder.WriteString("\n")

	if canonical.MarkdownBody != "" {
		builder.WriteString("\n")
		builder.WriteString(canonical.MarkdownBody)
		builder.WriteString("\n")
	}

	if canonical.RawADFJSON != "" {
		if canonical.MarkdownBody == "" {
			builder.WriteString("\n")
		} else {
			builder.WriteString("\n")
		}
		builder.WriteString("```")
		builder.WriteString(contracts.RawADFFenceLanguage)
		builder.WriteString("\n")
		builder.WriteString(canonical.RawADFJSON)
		builder.WriteString("\n```")
		builder.WriteString("\n")
	}

	return builder.String(), nil
}

func canonicalizeDocument(doc Document) (Document, error) {
	key := resolveCanonicalKey(strings.TrimSpace(doc.FrontMatter.Key), strings.TrimSpace(doc.CanonicalKey))
	if key == "" {
		return Document{}, &ParseError{
			Code:       ParseErrorCodeMissingRequiredField,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Field:      contracts.FrontMatterKeyKey,
			Message:    "issue key is required",
		}
	}
	if !contracts.JiraIssueKeyPattern.MatchString(key) && !contracts.LocalDraftKeyPattern.MatchString(key) {
		return Document{}, &ParseError{
			Code:       ParseErrorCodeInvalidIssueKey,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Field:      contracts.FrontMatterKeyKey,
			Message:    "issue key does not match supported key formats",
		}
	}

	frontMatter := doc.FrontMatter
	frontMatter.Key = key
	if strings.TrimSpace(frontMatter.SchemaVersion) == "" {
		frontMatter.SchemaVersion = contracts.IssueFileSchemaVersionV1
	}

	normalizedFrontMatter, err := normalizeFrontMatter(frontMatter)
	if err != nil {
		return Document{}, err
	}

	normalizedMarkdown := strings.TrimSpace(
		contracts.NormalizeSingleValue(contracts.NormalizationNormalizeLineEndings, doc.MarkdownBody),
	)
	canonicalRawADF := ""
	if strings.TrimSpace(doc.RawADFJSON) != "" {
		validated, validateErr := converter.ValidateAndCanonicalizeRawADF(doc.RawADFJSON)
		if validateErr != nil {
			return Document{}, mapRawADFError(validateErr)
		}
		canonicalRawADF = validated
	}

	return Document{
		CanonicalKey: key,
		FrontMatter:  normalizedFrontMatter,
		MarkdownBody: normalizedMarkdown,
		RawADFJSON:   canonicalRawADF,
	}, nil
}

func splitFrontMatter(content string) ([]string, string, error) {
	lines := strings.Split(content, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != contracts.FrontMatterDelimiter {
		return nil, "", &ParseError{
			Code:       ParseErrorCodeMalformedDocument,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Message:    "document must begin with front matter delimiter",
		}
	}

	closing := -1
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == contracts.FrontMatterDelimiter {
			closing = index
			break
		}
	}
	if closing == -1 {
		return nil, "", &ParseError{
			Code:       ParseErrorCodeMalformedDocument,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Message:    "front matter closing delimiter is missing",
		}
	}

	frontMatterLines := lines[1:closing]
	body := strings.Join(lines[closing+1:], "\n")
	body = strings.TrimPrefix(body, "\n")
	return frontMatterLines, body, nil
}

func parseFrontMatter(lines []string) (map[contracts.FrontMatterKey]interface{}, error) {
	values := make(map[contracts.FrontMatterKey]interface{})
	for index := 0; index < len(lines); index++ {
		line := strings.TrimSpace(lines[index])
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return nil, &ParseError{
				Code:       ParseErrorCodeMalformedFrontMatter,
				ReasonCode: contracts.ReasonCodeValidationFailed,
				Message:    fmt.Sprintf("invalid front matter line: %q", line),
			}
		}

		key := contracts.FrontMatterKey(strings.TrimSpace(parts[0]))
		if !contracts.SupportedFrontMatterKey(key) {
			return nil, &ParseError{
				Code:       ParseErrorCodeUnsupportedField,
				ReasonCode: contracts.ReasonCodeValidationFailed,
				Field:      key,
				Message:    "unsupported front matter key",
			}
		}
		if _, exists := values[key]; exists {
			return nil, &ParseError{
				Code:       ParseErrorCodeMalformedFrontMatter,
				ReasonCode: contracts.ReasonCodeValidationFailed,
				Field:      key,
				Message:    "duplicate front matter key",
			}
		}

		rawValue := strings.TrimSpace(parts[1])
		if key == contracts.FrontMatterKeyLabels {
			if rawValue == "" {
				labels := make([]string, 0)
				for index+1 < len(lines) {
					next := strings.TrimSpace(lines[index+1])
					if !strings.HasPrefix(next, "- ") {
						break
					}
					labels = append(labels, unquote(strings.TrimSpace(strings.TrimPrefix(next, "- "))))
					index++
				}
				values[key] = labels
				continue
			}
			values[key] = parseInlineLabels(rawValue)
			continue
		}

		values[key] = unquote(rawValue)
	}

	return values, nil
}

func buildFrontMatter(values map[contracts.FrontMatterKey]interface{}) (FrontMatter, error) {
	for _, key := range contracts.RequiredFrontMatterKeys {
		if _, exists := values[key]; !exists {
			return FrontMatter{}, &ParseError{
				Code:       ParseErrorCodeMissingRequiredField,
				ReasonCode: contracts.ReasonCodeValidationFailed,
				Field:      key,
				Message:    "required front matter key is missing",
			}
		}
	}

	frontMatter := FrontMatter{
		SchemaVersion: toString(values[contracts.FrontMatterKeySchemaVersion]),
		Key:           toString(values[contracts.FrontMatterKeyKey]),
		Summary:       toString(values[contracts.FrontMatterKeySummary]),
		IssueType:     toString(values[contracts.FrontMatterKeyIssueType]),
		Status:        toString(values[contracts.FrontMatterKeyStatus]),
		Priority:      toString(values[contracts.FrontMatterKeyPriority]),
		Assignee:      toString(values[contracts.FrontMatterKeyAssignee]),
		Labels:        toStringSlice(values[contracts.FrontMatterKeyLabels]),
		Reporter:      toString(values[contracts.FrontMatterKeyReporter]),
		CreatedAt:     toString(values[contracts.FrontMatterKeyCreatedAt]),
		UpdatedAt:     toString(values[contracts.FrontMatterKeyUpdatedAt]),
		SyncedAt:      toString(values[contracts.FrontMatterKeySyncedAt]),
	}

	return normalizeFrontMatter(frontMatter)
}

func normalizeFrontMatter(frontMatter FrontMatter) (FrontMatter, error) {
	frontMatter.SchemaVersion = strings.TrimSpace(frontMatter.SchemaVersion)
	if frontMatter.SchemaVersion != contracts.IssueFileSchemaVersionV1 {
		return FrontMatter{}, &ParseError{
			Code:       ParseErrorCodeInvalidSchemaVersion,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Field:      contracts.FrontMatterKeySchemaVersion,
			Message:    "schema version is unsupported",
		}
	}

	frontMatter.Key = strings.TrimSpace(frontMatter.Key)
	if frontMatter.Key == "" {
		return FrontMatter{}, &ParseError{
			Code:       ParseErrorCodeMissingRequiredField,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Field:      contracts.FrontMatterKeyKey,
			Message:    "issue key is required",
		}
	}

	frontMatter.Summary = strings.TrimSpace(frontMatter.Summary)
	if frontMatter.Summary == "" {
		return FrontMatter{}, &ParseError{
			Code:       ParseErrorCodeInvalidRequiredValue,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Field:      contracts.FrontMatterKeySummary,
			Message:    "summary must not be empty",
		}
	}

	frontMatter.IssueType = strings.TrimSpace(frontMatter.IssueType)
	if frontMatter.IssueType == "" {
		return FrontMatter{}, &ParseError{
			Code:       ParseErrorCodeInvalidRequiredValue,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Field:      contracts.FrontMatterKeyIssueType,
			Message:    "issue type must not be empty",
		}
	}

	frontMatter.Status = strings.TrimSpace(frontMatter.Status)
	if frontMatter.Status == "" {
		return FrontMatter{}, &ParseError{
			Code:       ParseErrorCodeInvalidRequiredValue,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Field:      contracts.FrontMatterKeyStatus,
			Message:    "status must not be empty",
		}
	}

	frontMatter.Priority = contracts.NormalizeSingleValue(contracts.NormalizationTrimAndTitleCase, frontMatter.Priority)
	frontMatter.Assignee = contracts.NormalizeSingleValue(contracts.NormalizationTrimEmptyToNull, frontMatter.Assignee)
	frontMatter.Reporter = contracts.NormalizeSingleValue(contracts.NormalizationTrimEmptyToNull, frontMatter.Reporter)
	frontMatter.CreatedAt = strings.TrimSpace(frontMatter.CreatedAt)
	frontMatter.UpdatedAt = strings.TrimSpace(frontMatter.UpdatedAt)
	frontMatter.SyncedAt = strings.TrimSpace(frontMatter.SyncedAt)
	frontMatter.Labels = contracts.NormalizeLabels(frontMatter.Labels)

	return frontMatter, nil
}

func resolveCanonicalKey(frontMatterKey string, filenameKey string) string {
	if strings.TrimSpace(frontMatterKey) != "" {
		return strings.TrimSpace(frontMatterKey)
	}
	return strings.TrimSpace(filenameKey)
}

func extractAndValidateRawADF(body string) (string, string, error) {
	normalized := contracts.NormalizeSingleValue(contracts.NormalizationNormalizeLineEndings, body)
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return "", "", nil
	}

	fenceCount := strings.Count(normalized, "```"+contracts.RawADFFenceLanguage)
	if fenceCount > 1 {
		return "", "", &ParseError{
			Code:       ParseErrorCodeMalformedRawADF,
			ReasonCode: contracts.ReasonCodeDescriptionADFBlockMalformed,
			Message:    "multiple embedded raw ADF fenced blocks are not supported",
		}
	}
	if fenceCount == 0 {
		return normalized, "", nil
	}

	match := contracts.RawADFFencedBlockPattern.FindStringSubmatch(normalized)
	if len(match) != 2 {
		return "", "", &ParseError{
			Code:       ParseErrorCodeMalformedRawADF,
			ReasonCode: contracts.ReasonCodeDescriptionADFBlockMalformed,
			Message:    "embedded raw ADF fenced block is malformed",
		}
	}

	canonicalRawADF, err := converter.ValidateAndCanonicalizeRawADF(match[1])
	if err != nil {
		return "", "", mapRawADFError(err)
	}

	markdown := contracts.RawADFFencedBlockPattern.ReplaceAllString(normalized, "")
	markdown = strings.TrimSpace(markdown)
	return markdown, canonicalRawADF, nil
}

func mapRawADFError(err error) error {
	if err == nil {
		return nil
	}
	return &ParseError{
		Code:       ParseErrorCodeMalformedRawADF,
		ReasonCode: contracts.ReasonCodeDescriptionADFBlockMalformed,
		Message:    "embedded raw ADF payload is invalid",
		Err:        err,
	}
}

func renderFrontMatterLine(frontMatter FrontMatter, key contracts.FrontMatterKey) (string, bool) {
	switch key {
	case contracts.FrontMatterKeySchemaVersion:
		return string(key) + ": " + quote(frontMatter.SchemaVersion), true
	case contracts.FrontMatterKeyKey:
		return string(key) + ": " + quote(frontMatter.Key), true
	case contracts.FrontMatterKeySummary:
		return string(key) + ": " + quote(frontMatter.Summary), true
	case contracts.FrontMatterKeyIssueType:
		return string(key) + ": " + quote(frontMatter.IssueType), true
	case contracts.FrontMatterKeyStatus:
		return string(key) + ": " + quote(frontMatter.Status), true
	case contracts.FrontMatterKeyPriority:
		if frontMatter.Priority == "" {
			return "", false
		}
		return string(key) + ": " + quote(frontMatter.Priority), true
	case contracts.FrontMatterKeyAssignee:
		if frontMatter.Assignee == "" {
			return "", false
		}
		return string(key) + ": " + quote(frontMatter.Assignee), true
	case contracts.FrontMatterKeyLabels:
		if len(frontMatter.Labels) == 0 {
			return "", false
		}
		var builder strings.Builder
		builder.WriteString(string(key))
		builder.WriteString(":")
		for _, label := range frontMatter.Labels {
			builder.WriteString("\n- ")
			builder.WriteString(quote(label))
		}
		return builder.String(), true
	case contracts.FrontMatterKeyReporter:
		if frontMatter.Reporter == "" {
			return "", false
		}
		return string(key) + ": " + quote(frontMatter.Reporter), true
	case contracts.FrontMatterKeyCreatedAt:
		if frontMatter.CreatedAt == "" {
			return "", false
		}
		return string(key) + ": " + quote(frontMatter.CreatedAt), true
	case contracts.FrontMatterKeyUpdatedAt:
		if frontMatter.UpdatedAt == "" {
			return "", false
		}
		return string(key) + ": " + quote(frontMatter.UpdatedAt), true
	case contracts.FrontMatterKeySyncedAt:
		if frontMatter.SyncedAt == "" {
			return "", false
		}
		return string(key) + ": " + quote(frontMatter.SyncedAt), true
	default:
		return "", false
	}
}

func quote(value string) string {
	return strconv.Quote(value)
}

func unquote(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if unquoted, err := strconv.Unquote(trimmed); err == nil {
		return unquoted
	}
	return trimmed
}

func parseInlineLabels(rawValue string) []string {
	trimmed := strings.TrimSpace(rawValue)
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]"))
		if inner == "" {
			return []string{}
		}
		parts := strings.Split(inner, ",")
		labels := make([]string, 0, len(parts))
		for _, part := range parts {
			labels = append(labels, unquote(part))
		}
		return labels
	}
	if trimmed == "" {
		return []string{}
	}
	return []string{unquote(trimmed)}
}

func toString(value interface{}) string {
	if value == nil {
		return ""
	}
	scalar, ok := value.(string)
	if !ok {
		return ""
	}
	return scalar
}

func toStringSlice(value interface{}) []string {
	if value == nil {
		return nil
	}
	slice, ok := value.([]string)
	if !ok {
		return nil
	}
	return slice
}
