package model

import (
	"testing"
)

func TestParsedDocumentSetField(t *testing.T) {
	doc := &ParsedDocument{
		Lines: []Line{
			{Raw: "id: T-001", Key: "id", Value: "T-001"},
			{Raw: "status: queued", Key: "status", Value: "queued"},
			{Raw: "title: Test task", Key: "title", Value: "Test task"},
		},
	}

	// Update existing field
	found := doc.SetField("status", "active")
	if !found {
		t.Error("SetField should find existing 'status' field")
	}
	if doc.Lines[1].Raw != "status: active" {
		t.Errorf("updated line = %q, want %q", doc.Lines[1].Raw, "status: active")
	}
	if doc.Lines[1].Value != "active" {
		t.Errorf("updated value = %q, want %q", doc.Lines[1].Value, "active")
	}
}

func TestParsedDocumentSetFieldPreservesComment(t *testing.T) {
	doc := &ParsedDocument{
		Lines: []Line{
			{Raw: "stage: coding  # delivery stage", Key: "stage", Value: "coding", Comment: "delivery stage"},
		},
	}

	doc.SetField("stage", "pushed")
	if doc.Lines[0].Raw != "stage: pushed  # delivery stage" {
		t.Errorf("line = %q, want comment preserved", doc.Lines[0].Raw)
	}
}

func TestParsedDocumentSetFieldAppends(t *testing.T) {
	doc := &ParsedDocument{
		Lines: []Line{
			{Raw: "id: T-001", Key: "id", Value: "T-001"},
		},
	}

	found := doc.SetField("priority", "2")
	if found {
		t.Error("SetField should return false for new field")
	}
	if len(doc.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(doc.Lines))
	}
	if doc.Lines[1].Raw != "priority: 2" {
		t.Errorf("appended line = %q, want %q", doc.Lines[1].Raw, "priority: 2")
	}
}

func TestParsedDocumentGetField(t *testing.T) {
	doc := &ParsedDocument{
		Lines: []Line{
			{Raw: "id: T-001", Key: "id", Value: "T-001"},
			{Raw: "status: queued", Key: "status", Value: "queued"},
		},
	}

	val, ok := doc.GetField("status")
	if !ok {
		t.Fatal("GetField should find 'status'")
	}
	if val != "queued" {
		t.Errorf("GetField = %q, want %q", val, "queued")
	}

	_, ok = doc.GetField("nonexistent")
	if ok {
		t.Error("GetField should return false for missing field")
	}
}

func TestParsedDocumentString(t *testing.T) {
	doc := &ParsedDocument{
		Lines: []Line{
			{Raw: "id: T-001"},
			{Raw: "status: queued"},
			{Raw: ""},
			{Raw: "title: A task"},
		},
	}

	want := "id: T-001\nstatus: queued\n\ntitle: A task"
	got := doc.String()
	if got != want {
		t.Errorf("String() =\n%q\nwant:\n%q", got, want)
	}
}

func TestParsedDocumentStringEmpty(t *testing.T) {
	doc := &ParsedDocument{}
	if doc.String() != "" {
		t.Errorf("empty doc String() = %q, want empty", doc.String())
	}
}

func TestParsedDocumentSetFieldIgnoresNested(t *testing.T) {
	doc := &ParsedDocument{
		Lines: []Line{
			{Raw: "id: T-001", Key: "id", Value: "T-001", Indent: 0},
			{Raw: "  id: nested", Key: "id", Value: "nested", Indent: 2},
		},
	}

	// Should update the top-level one, not the nested one
	doc.SetField("id", "T-002")
	if doc.Lines[0].Value != "T-002" {
		t.Error("should update top-level id")
	}
	if doc.Lines[1].Value != "nested" {
		t.Error("should not modify nested id")
	}
}
