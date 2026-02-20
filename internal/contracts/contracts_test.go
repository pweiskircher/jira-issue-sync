package contracts

import (
	"reflect"
	"testing"
)

func TestFileFormatFrontMatterContract(t *testing.T) {
	expectedRequired := []FrontMatterKey{
		FrontMatterKeySchemaVersion,
		FrontMatterKeyKey,
		FrontMatterKeySummary,
		FrontMatterKeyIssueType,
		FrontMatterKeyStatus,
	}

	if !reflect.DeepEqual(RequiredFrontMatterKeys, expectedRequired) {
		t.Fatalf("required front matter keys changed: got=%v want=%v", RequiredFrontMatterKeys, expectedRequired)
	}

	if !SupportedFrontMatterKey(FrontMatterKeyLabels) {
		t.Fatalf("expected labels to be supported")
	}
	if SupportedFrontMatterKey(FrontMatterKey("unknown")) {
		t.Fatalf("expected unknown key to be unsupported")
	}

	if len(AllFrontMatterKeys()) != len(RequiredFrontMatterKeys)+len(OptionalFrontMatterKeys) {
		t.Fatalf("all front matter keys length mismatch")
	}
}

func TestRawADFContract(t *testing.T) {
	markdown := "# Title\n\n```jira-adf\n{\"version\":1,\"type\":\"doc\",\"content\":[]}\n```\n"

	payload, ok := ExtractRawADFJSON(markdown)
	if !ok {
		t.Fatalf("expected to extract raw ADF fenced block")
	}

	doc, err := ParseRawADFDoc(payload)
	if err != nil {
		t.Fatalf("expected valid ADF payload, got error: %v", err)
	}

	if !IsValidRawADFDoc(doc) {
		t.Fatalf("expected valid ADF doc contract")
	}

	if _, ok := ExtractRawADFJSON("no fenced block"); ok {
		t.Fatalf("did not expect raw ADF block")
	}
}

func TestCLIOutputContract(t *testing.T) {
	if OutputStreamContracts[OutputModeJSON].StdoutRule == "" {
		t.Fatalf("json stdout rule must be defined")
	}
	if OutputStreamContracts[OutputModeHuman].StderrRule == "" {
		t.Fatalf("human stderr rule must be defined")
	}

	if code := ResolveExitCode(AggregateCounts{}, false); code != ExitCodeSuccess {
		t.Fatalf("expected success exit code, got %d", code)
	}
	if code := ResolveExitCode(AggregateCounts{Warnings: 1}, false); code != ExitCodePartial {
		t.Fatalf("expected partial exit code for warnings, got %d", code)
	}
	if code := ResolveExitCode(AggregateCounts{}, true); code != ExitCodeFatal {
		t.Fatalf("expected fatal exit code, got %d", code)
	}

	env := CommandEnvelope{
		EnvelopeVersion: JSONEnvelopeVersionV1,
		Command:         CommandMeta{Name: "push"},
	}
	if err := ValidateEnvelopeBasics(env); err != nil {
		t.Fatalf("expected envelope validation success, got %v", err)
	}
}

func TestRuntimeDefaultsAndLockPolicy(t *testing.T) {
	if DefaultIssuesRootDir != ".issues" {
		t.Fatalf("unexpected issues root: %s", DefaultIssuesRootDir)
	}
	if !RequiresLock(CommandPush) {
		t.Fatalf("push must require lock")
	}
	if RequiresLock(CommandStatus) {
		t.Fatalf("status must not require lock")
	}
	if CommandLockPolicy[CommandSync] != LockRequirementExclusive {
		t.Fatalf("sync lock requirement changed")
	}
}

func TestFieldMappingAndNormalization(t *testing.T) {
	if !SupportedWritableField(JiraFieldSummary) || !SupportedWritableField(JiraFieldDescription) {
		t.Fatalf("expected summary and description to be writable")
	}
	if SupportedWritableField(JiraFieldReporter) {
		t.Fatalf("reporter must not be writable")
	}
	if !SupportedReadOnlyField(JiraFieldReporter) {
		t.Fatalf("reporter should be read-only")
	}

	if got := NormalizeSingleValue(NormalizationNormalizeLineEndings, "a\r\nb\rc"); got != "a\nb\nc" {
		t.Fatalf("line ending normalization mismatch: %q", got)
	}
	if got := NormalizeSingleValue(NormalizationTrimAndTitleCase, "  HIGH "); got != "High" {
		t.Fatalf("priority normalization mismatch: %q", got)
	}

	labels := NormalizeLabels([]string{"Bug", "  bug", "P1", "p1", ""})
	expected := []string{"bug", "p1"}
	if !reflect.DeepEqual(labels, expected) {
		t.Fatalf("label normalization mismatch: got=%v want=%v", labels, expected)
	}
}

func TestReasonCodesStableAndUnique(t *testing.T) {
	if len(StableReasonCodes) == 0 {
		t.Fatalf("stable reason-code taxonomy must not be empty")
	}

	seen := make(map[ReasonCode]struct{})
	for _, code := range StableReasonCodes {
		if _, exists := seen[code]; exists {
			t.Fatalf("duplicate reason code: %s", code)
		}
		seen[code] = struct{}{}
	}

	if !IsStableReasonCode(ReasonCodeUnsupportedFieldIgnored) {
		t.Fatalf("unsupported-field reason code must be stable")
	}
	if DefaultUnsupportedFieldReasonCode != ReasonCodeUnsupportedFieldIgnored {
		t.Fatalf("unsupported field default reason code changed")
	}
}
