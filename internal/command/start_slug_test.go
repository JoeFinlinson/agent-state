package command

import (
	"strings"
	"testing"
)

// TestNormalizeSlug covers the I-579 acceptance criteria: a user-supplied
// --slug must produce the same canonical branch whether they typed the bare
// slug or the full prefixed path. Slashes left over after normalization are
// a user error (they create the broken-directory illusion that motivated
// the fix).
func TestNormalizeSlug(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		slug    string
		want    string
		wantErr string // substring; empty = no error expected
	}{
		{
			name: "bare slug passes through unchanged",
			id:   "I-579",
			slug: "cost-ground-truth",
			want: "cost-ground-truth",
		},
		{
			name: "full prefixed path is idempotent",
			id:   "I-579",
			slug: "fix/I-579-cost-ground-truth",
			want: "cost-ground-truth",
		},
		{
			name: "type-only prefix (id missing) is stripped and id is re-applied by caller",
			id:   "I-579",
			slug: "fix/cost-ground-truth",
			want: "cost-ground-truth",
		},
		{
			name: "feat prefix on a task id is stripped",
			id:   "T-001",
			slug: "feat/T-001-add-thing",
			want: "add-thing",
		},
		{
			name: "feat prefix used on an issue id is still stripped (operator typo)",
			id:   "I-579",
			slug: "feat/I-579-cost-ground-truth",
			want: "cost-ground-truth",
		},
		{
			name: "chore prefix is recognized",
			id:   "T-100",
			slug: "chore/T-100-cleanup",
			want: "cleanup",
		},
		{
			name: "case-insensitive type prefix",
			id:   "I-579",
			slug: "FIX/I-579-foo",
			want: "foo",
		},
		{
			name: "case-insensitive id segment",
			id:   "I-579",
			slug: "fix/i-579-foo",
			want: "foo",
		},
		{
			name:    "slash inside slug after normalization is rejected",
			id:      "I-579",
			slug:    "cost/ground/truth",
			wantErr: "single path segment",
		},
		{
			name:    "trailing slash on otherwise valid slug is rejected",
			id:      "I-579",
			slug:    "fix/I-579-cost/truth",
			wantErr: "single path segment",
		},
		{
			name:    "slug that is exactly the prefix is rejected as empty",
			id:      "I-579",
			slug:    "fix/I-579-",
			wantErr: "empty",
		},
		{
			name: "unknown leading segment is left intact (caller's slash check rejects)",
			id:   "I-579",
			slug: "weird/I-579-foo",
			// "weird" is not a known prefix, so the slug is unchanged and the
			// remaining slash trips the single-segment guard.
			wantErr: "single path segment",
		},
		{
			name: "id appears in slug body but not as leading prefix is preserved",
			id:   "I-579",
			slug: "refers-to-I-579-elsewhere",
			want: "refers-to-I-579-elsewhere",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeSlug(tc.id, tc.slug)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("normalizeSlug(%q, %q) = %q, nil; want error containing %q",
						tc.id, tc.slug, got, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("normalizeSlug(%q, %q) error = %v; want substring %q",
						tc.id, tc.slug, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeSlug(%q, %q) unexpected error: %v",
					tc.id, tc.slug, err)
			}
			if got != tc.want {
				t.Errorf("normalizeSlug(%q, %q) = %q; want %q",
					tc.id, tc.slug, got, tc.want)
			}
		})
	}
}

// TestNormalizeSlug_BranchComposition demonstrates the end-to-end invariant
// that motivated I-579: composing the branch name from the normalized slug
// always yields a single canonical form, regardless of which equivalent
// input the user supplied.
func TestNormalizeSlug_BranchComposition(t *testing.T) {
	id := "I-579"
	prefix := "fix"
	want := "fix/I-579-cost-ground-truth"

	inputs := []string{
		"cost-ground-truth",
		"fix/I-579-cost-ground-truth",
		"fix/cost-ground-truth",
		"FIX/I-579-cost-ground-truth",
	}

	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			s, err := normalizeSlug(id, in)
			if err != nil {
				t.Fatalf("normalizeSlug(%q, %q): %v", id, in, err)
			}
			got := prefix + "/" + id + "-" + s
			if got != want {
				t.Errorf("composed branch = %q; want %q", got, want)
			}
		})
	}
}
