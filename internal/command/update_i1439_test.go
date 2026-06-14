package command

import (
	"strings"
	"testing"
)

// I-1439 write-side guard: `st update` must refuse an unknown/unmatched
// top-level field rather than silently appending a `field: value` block
// at EOF (which corrupts the file and stacks duplicate keys on repeat
// calls). The live incident was a bare SBAR sub-key (`situation` instead
// of `sbar.situation`) reported as "Updated" + exit 0 while appending a
// stray top-level key.

func TestUpdateRefusesBareSBARSubKey(t *testing.T) {
	s, cfg := setupTestEnv(t)

	for _, sub := range []string{"situation", "background", "assessment", "recommendation"} {
		if code := Update(s, cfg, "T-001", sub, "some text", UpdateModeValue); code != 2 {
			t.Errorf("Update bare %q: got exit %d, want 2 (refuse + suggest sbar.%s)", sub, code, sub)
		}
		item, _ := s.Get("T-001")
		for _, line := range strings.Split(item.Doc.String(), "\n") {
			if strings.HasPrefix(line, sub+":") {
				t.Errorf("bare %q was appended as a top-level key:\n%s", sub, item.Doc.String())
			}
		}
	}
}

func TestUpdateRefusesUnknownField(t *testing.T) {
	s, cfg := setupTestEnv(t)

	// A typo'd field name must fail loud, not append silently — otherwise
	// the operator believes they set `status` while the real field is
	// untouched and a stray `stauts:` key rides along forever.
	if code := Update(s, cfg, "T-001", "stauts", "active", UpdateModeValue); code != 2 {
		t.Fatalf("Update unknown field: got exit %d, want 2", code)
	}
	item, _ := s.Get("T-001")
	if strings.Contains(item.Doc.String(), "stauts:") {
		t.Errorf("typo'd field 'stauts:' was appended:\n%s", item.Doc.String())
	}
}

func TestUpdateAllowsCanonicalAndFreeformFields(t *testing.T) {
	s, cfg := setupTestEnv(t)

	// Canonical scalar field (present in template) — replace in place.
	if code := Update(s, cfg, "T-001", "title", "Renamed", UpdateModeValue); code != 0 {
		t.Errorf("Update canonical 'title': got exit %d, want 0", code)
	}
	// Documented free-form long-form body field — round-trips as a block
	// scalar even though the parser does not surface it as a typed field.
	if code := Update(s, cfg, "T-001", "notes", "a note", UpdateModeValue); code != 0 {
		t.Errorf("Update free-form 'notes': got exit %d, want 0", code)
	}
	// Dotted nested path must still route to the nested writer, not the guard.
	if code := Update(s, cfg, "T-001", "sbar.situation", "symptom", UpdateModeValue); code != 0 {
		t.Errorf("Update 'sbar.situation': got exit %d, want 0", code)
	}
}
