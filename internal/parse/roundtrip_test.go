package parse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRoundtripAllFiles is the critical gate: parse every agent-state file,
// serialize back, compare byte-for-byte. Zero diff = pass.
func TestRoundtripAllFiles(t *testing.T) {
	agentStateDir := os.Getenv("AS_TEST_DIR")
	if agentStateDir == "" {
		t.Skip("AS_TEST_DIR not set — set to agent-state directory for roundtrip test")
	}

	dirs := []string{
		filepath.Join(agentStateDir, "tasks"),
		filepath.Join(agentStateDir, "issues"),
		filepath.Join(agentStateDir, "archive"),
	}

	var total, passed, failed int

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Logf("skipping %s: %v", dir, err)
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			if entry.Name() == "index.md" || entry.Name() == "_template.md" {
				continue
			}

			path := filepath.Join(dir, entry.Name())
			total++

			t.Run(entry.Name(), func(t *testing.T) {
				original, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read: %v", err)
				}

				item, err := File(path)
				if err != nil {
					t.Fatalf("parse: %v", err)
				}

				got := item.Doc.String()
				want := string(original)

				// Normalize: strip trailing newline for comparison
				// (files may or may not end with newline)
				got = strings.TrimRight(got, "\n")
				want = strings.TrimRight(want, "\n")

				if got != want {
					failed++
					// Find first diff
					gotLines := strings.Split(got, "\n")
					wantLines := strings.Split(want, "\n")
					maxLines := len(gotLines)
					if len(wantLines) > maxLines {
						maxLines = len(wantLines)
					}

					for i := 0; i < maxLines; i++ {
						var gl, wl string
						if i < len(gotLines) {
							gl = gotLines[i]
						} else {
							gl = "<EOF>"
						}
						if i < len(wantLines) {
							wl = wantLines[i]
						} else {
							wl = "<EOF>"
						}
						if gl != wl {
							t.Errorf("first diff at line %d:\ngot:  %q\nwant: %q", i+1, gl, wl)
							// Show a few more lines of context
							for j := i + 1; j < i+4 && j < maxLines; j++ {
								if j < len(gotLines) {
									gl = gotLines[j]
								} else {
									gl = "<EOF>"
								}
								if j < len(wantLines) {
									wl = wantLines[j]
								} else {
									wl = "<EOF>"
								}
								if gl != wl {
									t.Errorf("  line %d: got=%q want=%q", j+1, gl, wl)
								}
							}
							break
						}
					}

					if len(gotLines) != len(wantLines) {
						t.Errorf("line count: got %d, want %d", len(gotLines), len(wantLines))
					}
				} else {
					passed++
				}
			})
		}
	}

	t.Logf("roundtrip: %d/%d passed, %d failed", passed, total, failed)
}
