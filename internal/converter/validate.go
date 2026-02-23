package converter

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
)

// ValidateAndCanonicalizeRawADF validates the raw ADF fenced payload contract and
// returns a compact canonical JSON form for deterministic persistence.
func ValidateAndCanonicalizeRawADF(payload string) (string, error) {
	trimmed := strings.TrimSpace(payload)

	doc, err := contracts.ParseRawADFDoc(trimmed)
	if err != nil {
		return "", &Error{
			Code:       ErrorCodeMalformedADFJSON,
			ReasonCode: contracts.ReasonCodeDescriptionADFBlockMalformed,
			Message:    "failed to parse embedded raw ADF JSON",
			Err:        err,
		}
	}

	if !contracts.IsValidRawADFDoc(doc) {
		return "", &Error{
			Code:       ErrorCodeInvalidADFEnvelope,
			ReasonCode: contracts.ReasonCodeDescriptionADFBlockMalformed,
			Message:    "embedded raw ADF payload does not match required envelope",
		}
	}

	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(trimmed)); err != nil {
		return "", &Error{
			Code:       ErrorCodeMalformedADFJSON,
			ReasonCode: contracts.ReasonCodeDescriptionADFBlockMalformed,
			Message:    "failed to canonicalize embedded raw ADF JSON",
			Err:        err,
		}
	}

	return compact.String(), nil
}
