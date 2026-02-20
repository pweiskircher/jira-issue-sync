package issue

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pat/jira-issue-sync/internal/contracts"
)

const (
	fallbackSlug = "issue"
	maxSlugLen   = 64
)

var keyPrefixInFilenamePattern = regexp.MustCompile(`^([A-Z][A-Z0-9]+-[0-9]+|L-[0-9a-f]+)(?:-.+)?\.md$`)

// StableSlug renders deterministic lowercase slugs for filenames.
func StableSlug(summary string) string {
	lower := strings.ToLower(strings.TrimSpace(summary))
	if lower == "" {
		return fallbackSlug
	}

	var builder strings.Builder
	lastHyphen := false
	for _, char := range lower {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
			lastHyphen = false
			continue
		}
		if !lastHyphen {
			builder.WriteByte('-')
			lastHyphen = true
		}
	}

	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		return fallbackSlug
	}
	if len(slug) > maxSlugLen {
		slug = strings.Trim(slug[:maxSlugLen], "-")
		if slug == "" {
			return fallbackSlug
		}
	}
	return slug
}

// BuildFilename renders stable issue filenames from key+summary.
func BuildFilename(key, summary string) (string, error) {
	if !contracts.JiraIssueKeyPattern.MatchString(key) && !contracts.LocalDraftKeyPattern.MatchString(key) {
		return "", &ParseError{
			Code:       ParseErrorCodeInvalidIssueKey,
			ReasonCode: contracts.ReasonCodeValidationFailed,
			Field:      contracts.FrontMatterKeyKey,
			Message:    "issue key does not match supported key formats",
		}
	}
	return fmt.Sprintf("%s-%s.md", key, StableSlug(summary)), nil
}

// ParseFilenameKey extracts a key prefix from a canonical issue filename.
func ParseFilenameKey(path string) (string, bool) {
	base := filepath.Base(path)
	match := keyPrefixInFilenamePattern.FindStringSubmatch(base)
	if len(match) != 2 {
		return "", false
	}
	return match[1], true
}
