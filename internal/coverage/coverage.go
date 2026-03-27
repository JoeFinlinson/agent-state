// Package coverage parses Go coverprofile and vitest JSON-summary files
// to extract per-file coverage metrics. Zero external dependencies.
package coverage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// FileCoverage holds coverage metrics for a single file.
type FileCoverage struct {
	Path       string  `json:"path"`
	Lines      float64 `json:"lines"`      // percentage
	Branches   float64 `json:"branches"`   // percentage
	Functions  float64 `json:"functions"`  // percentage
	Statements float64 `json:"statements"` // percentage
}

// Thresholds defines minimum coverage requirements.
type Thresholds struct {
	Lines     float64
	Branches  float64
	Functions float64
}

// DefaultThresholds returns the default coverage thresholds.
func DefaultThresholds() Thresholds {
	return Thresholds{Lines: 90, Branches: 80, Functions: 100}
}

// Violation describes a file that failed to meet coverage thresholds.
type Violation struct {
	Path      string
	Metric    string  // "lines", "branches", "functions"
	Actual    float64
	Threshold float64
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s %.1f%% < %.1f%%", v.Path, v.Metric, v.Actual, v.Threshold)
}

// ParseGoCoverprofile parses a Go cover.out file into per-file coverage.
// modulePath is stripped from file paths (e.g., "github.com/user/repo/").
func ParseGoCoverprofile(r io.Reader, modulePath string) (map[string]FileCoverage, error) {
	type fileStats struct {
		totalStmts   int
		coveredStmts int
	}
	stats := map[string]*fileStats{}

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		// Skip mode line
		if strings.HasPrefix(line, "mode:") {
			continue
		}
		if line == "" {
			continue
		}

		// Format: filepath:startLine.startCol,endLine.endCol stmtCount execCount
		colonIdx := strings.LastIndex(line, ":")
		if colonIdx < 0 {
			continue
		}
		filePath := line[:colonIdx]

		rest := line[colonIdx+1:]
		spaceIdx := strings.Index(rest, " ")
		if spaceIdx < 0 {
			continue
		}
		afterCoords := strings.TrimSpace(rest[spaceIdx+1:])
		parts := strings.Fields(afterCoords)
		if len(parts) < 2 {
			continue
		}

		stmtCount, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		execCount, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}

		// Strip module path
		if modulePath != "" {
			filePath = strings.TrimPrefix(filePath, modulePath)
		}

		s, ok := stats[filePath]
		if !ok {
			s = &fileStats{}
			stats[filePath] = s
		}
		s.totalStmts += stmtCount
		if execCount > 0 {
			s.coveredStmts += stmtCount
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	result := make(map[string]FileCoverage, len(stats))
	for path, s := range stats {
		pct := 0.0
		if s.totalStmts > 0 {
			pct = float64(s.coveredStmts) / float64(s.totalStmts) * 100
		}
		result[path] = FileCoverage{
			Path:       path,
			Lines:      pct,
			Statements: pct,
			Branches:   100, // Go coverprofile doesn't track branches
			Functions:  100, // Go coverprofile doesn't track functions
		}
	}
	return result, nil
}

// ParseVitestSummary parses a vitest/istanbul coverage-summary.json file.
func ParseVitestSummary(r io.Reader) (map[string]FileCoverage, error) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding JSON: %w", err)
	}

	type metricBlock struct {
		Total   int     `json:"total"`
		Covered int     `json:"covered"`
		Pct     float64 `json:"pct"`
	}
	type fileSummary struct {
		Lines      metricBlock `json:"lines"`
		Statements metricBlock `json:"statements"`
		Functions  metricBlock `json:"functions"`
		Branches   metricBlock `json:"branches"`
	}

	result := make(map[string]FileCoverage)
	for path, data := range raw {
		if path == "total" {
			continue // skip aggregate
		}
		var fs fileSummary
		if err := json.Unmarshal(data, &fs); err != nil {
			continue // skip unparseable entries
		}
		result[path] = FileCoverage{
			Path:       path,
			Lines:      fs.Lines.Pct,
			Branches:   fs.Branches.Pct,
			Functions:  fs.Functions.Pct,
			Statements: fs.Statements.Pct,
		}
	}
	return result, nil
}

// CheckThresholds checks coverage for specific files against thresholds.
// Only files in the `files` list are checked. Returns violations.
func CheckThresholds(coverage map[string]FileCoverage, files []string, thresh Thresholds) []Violation {
	var violations []Violation
	for _, f := range files {
		cov, ok := coverage[f]
		if !ok {
			// File not in coverage report — might be excluded or not compiled
			continue
		}
		if cov.Lines < thresh.Lines {
			violations = append(violations, Violation{
				Path: f, Metric: "lines", Actual: cov.Lines, Threshold: thresh.Lines,
			})
		}
		if cov.Branches < thresh.Branches {
			violations = append(violations, Violation{
				Path: f, Metric: "branches", Actual: cov.Branches, Threshold: thresh.Branches,
			})
		}
		if cov.Functions < thresh.Functions {
			violations = append(violations, Violation{
				Path: f, Metric: "functions", Actual: cov.Functions, Threshold: thresh.Functions,
			})
		}
	}
	return violations
}
