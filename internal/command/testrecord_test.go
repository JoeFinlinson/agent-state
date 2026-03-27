package command

import (
	"strings"
	"testing"
)

func testRecordOpts() TestRecordOpts {
	return TestRecordOpts{
		GitHeadSHA: func(dir string) (string, error) {
			return "abc1234567890", nil
		},
	}
}

func TestTestRecordRequiredSuite(t *testing.T) {
	s, cfg := setupPRTestEnv(t)
	opts := testRecordOpts()

	code := TestRecord(s, cfg, "T-003", "api_unit", opts)
	if code != 0 {
		t.Fatalf("TestRecord returned %d, want 0", code)
	}

	// Verify evidence recorded
	item, _ := s.Get("T-003")
	ev, ok := getNestedField(item, "testing_evidence", "api_unit")
	if !ok || !strings.HasPrefix(ev, "pass abc1234") {
		t.Errorf("testing_evidence.api_unit = %q", ev)
	}
}

func TestTestRecordScopeSuite(t *testing.T) {
	s, cfg := setupPRTestEnv(t)
	opts := testRecordOpts()

	// First mark scope suite as required (as st pr would)
	item, _ := s.Get("T-003")
	setNestedField(item, "testing_evidence", "api_integration", "required")
	s.Write(item)

	code := TestRecord(s, cfg, "T-003", "api_integration", opts)
	if code != 0 {
		t.Fatalf("TestRecord returned %d, want 0", code)
	}

	// Verify evidence replaced "required"
	item, _ = s.Get("T-003")
	ev, ok := getNestedField(item, "testing_evidence", "api_integration")
	if !ok || !strings.HasPrefix(ev, "pass") {
		t.Errorf("testing_evidence.api_integration = %q, want pass ...", ev)
	}
}

func TestTestRecordInvalidSuite(t *testing.T) {
	s, cfg := setupPRTestEnv(t)
	opts := testRecordOpts()

	code := TestRecord(s, cfg, "T-003", "nonexistent_suite", opts)
	if code != 1 {
		t.Errorf("TestRecord returned %d, want 1", code)
	}
}

func TestTestRecordItemNotFound(t *testing.T) {
	s, cfg := setupPRTestEnv(t)
	opts := testRecordOpts()

	code := TestRecord(s, cfg, "T-999", "api_unit", opts)
	if code != 1 {
		t.Errorf("TestRecord returned %d, want 1", code)
	}
}

func TestTestRecordItemNotActive(t *testing.T) {
	s, cfg := setupPRTestEnv(t)
	opts := testRecordOpts()

	// T-001 is queued
	code := TestRecord(s, cfg, "T-001", "api_unit", opts)
	if code != 1 {
		t.Errorf("TestRecord returned %d, want 1", code)
	}
}

func TestTestRecordNoTestingConfig(t *testing.T) {
	s, cfg := setupPRTestEnv(t)
	cfg.Testing = nil
	opts := testRecordOpts()

	code := TestRecord(s, cfg, "T-003", "api_unit", opts)
	if code != 1 {
		t.Errorf("TestRecord returned %d, want 1", code)
	}
}

func TestTestRecordSHATruncation(t *testing.T) {
	s, cfg := setupPRTestEnv(t)
	opts := TestRecordOpts{
		GitHeadSHA: func(dir string) (string, error) {
			return "abcdef1234567890abcdef1234567890abcdef12\n", nil
		},
	}

	code := TestRecord(s, cfg, "T-003", "api_unit", opts)
	if code != 0 {
		t.Fatalf("TestRecord returned %d", code)
	}

	item, _ := s.Get("T-003")
	ev, _ := getNestedField(item, "testing_evidence", "api_unit")
	if !strings.Contains(ev, "abcdef1") {
		t.Errorf("evidence = %q, want truncated SHA", ev)
	}
	// Should not contain the full 40-char SHA
	if strings.Contains(ev, "abcdef1234567890") {
		t.Errorf("evidence = %q, SHA not truncated", ev)
	}
}
