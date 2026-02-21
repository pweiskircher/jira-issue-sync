package contracts

import (
	"regexp"
	"strings"
)

// TempIDBodyReferencePattern matches markdown-local temp issue references that are eligible for rewrite.
var TempIDBodyReferencePattern = regexp.MustCompile(`#(L-[0-9a-f]+)\b`)

// RewriteTempIDReferences rewrites markdown-local #L-<hex> references to canonical Jira keys.
//
// Only reference-style tokens outside embedded raw ADF fenced blocks are rewritten.
func RewriteTempIDReferences(markdown string, replacements map[string]string) string {
	if markdown == "" || len(replacements) == 0 {
		return markdown
	}

	blockRanges := RawADFFencedBlockPattern.FindAllStringIndex(markdown, -1)
	if len(blockRanges) == 0 {
		return rewriteTempIDSegment(markdown, replacements)
	}

	var builder strings.Builder
	cursor := 0

	for _, block := range blockRanges {
		if block[0] > cursor {
			builder.WriteString(rewriteTempIDSegment(markdown[cursor:block[0]], replacements))
		}

		builder.WriteString(markdown[block[0]:block[1]])
		cursor = block[1]
	}

	if cursor < len(markdown) {
		builder.WriteString(rewriteTempIDSegment(markdown[cursor:], replacements))
	}

	return builder.String()
}

func rewriteTempIDSegment(segment string, replacements map[string]string) string {
	if segment == "" {
		return segment
	}

	return TempIDBodyReferencePattern.ReplaceAllStringFunc(segment, func(match string) string {
		localKey := strings.TrimPrefix(match, "#")
		replacement, ok := replacements[localKey]
		if !ok {
			return match
		}

		replacement = strings.TrimSpace(replacement)
		if !JiraIssueKeyPattern.MatchString(replacement) {
			return match
		}

		return "#" + replacement
	})
}
