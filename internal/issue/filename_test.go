package issue

import "testing"

func TestBuildFilenameUsesStableSlug(t *testing.T) {
	filename, err := BuildFilename("PROJ-123", " Fix Login: Flow! ")
	if err != nil {
		t.Fatalf("expected filename build success, got: %v", err)
	}

	if filename != "PROJ-123-fix-login-flow.md" {
		t.Fatalf("unexpected filename: %s", filename)
	}
}

func TestBuildFilenameRejectsInvalidKey(t *testing.T) {
	_, err := BuildFilename("123", "summary")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !IsParseErrorCode(err, ParseErrorCodeInvalidIssueKey) {
		t.Fatalf("expected invalid key parse error, got: %v", err)
	}
}

func TestParseFilenameKey(t *testing.T) {
	key, ok := ParseFilenameKey("/tmp/L-1a2b3c-new-idea.md")
	if !ok {
		t.Fatalf("expected parse success")
	}
	if key != "L-1a2b3c" {
		t.Fatalf("unexpected key: %s", key)
	}
}
