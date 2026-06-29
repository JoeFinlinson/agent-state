package command

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/theraprac/agent-state/internal/config"
	"github.com/theraprac/agent-state/internal/store"
)

// Depth bucketing thresholds (see I-147 SBAR recommendation).
const (
	reviewDepthSmallLines = 50
	reviewDepthSmallFiles = 3
	reviewDepthHighLines  = 200
	reviewDepthHighFiles  = 6
)

// blastRadiusPaths contains path fragments that unconditionally route to "high"
// regardless of diff size (mirrors CLAUDE.md rule 5).
var blastRadiusPaths = []string{
	"internal/auth/",
	"internal/payment/",
	"internal/billing/",
	"db/changelog/",
	"claude-config/hooks/",
	".github/workflows/",
	"theraprac-infra/",
	"ansible/",
}

// statLineRe matches the trailing summary line of `git diff --stat`:
// " N files changed, M insertions(+), K deletions(-)"
var statLineRe = regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)

// ReviewDepthOpts holds injectable dependencies for unit testing.
type ReviewDepthOpts struct {
	// RunGit is injectable for tests; nil uses the real git binary.
	RunGit func(dir string, args ...string) (string, error)
}

// ReviewDepth computes a recommended /code-review depth for the item based on
// the combined diff across all its worktree repos. Prints one of "low",
// "medium", or "high" to stdout and returns 0 on success.
func ReviewDepth(s *store.Store, cfg *config.Config, id string, opts ReviewDepthOpts) int {
	if _, ok := s.Get(id); !ok {
		fmt.Fprintf(os.Stderr, "review-depth: %s not found\n", id)
		return 1
	}

	gitFn := opts.RunGit
	if gitFn == nil {
		gitFn = func(dir string, args ...string) (string, error) { return runGit(dir, args...) }
	}

	var totalFiles, totalLines int
	var changedPaths []string

	var repos []string
	if cfg != nil && cfg.Worktree != nil {
		repos = cfg.Worktree.Repos
	}
	for _, repo := range repos {
		dir := resolveRepoDirForItem(cfg, id, repo)
		if dir == "" || dir == repo {
			continue
		}
		if _, err := os.Stat(dir); err != nil {
			continue
		}

		// Determine which ref range to diff. Prefer origin/main...HEAD; fall back
		// to HEAD^...HEAD only for genuine orphan branches (commits exist but no
		// common ancestor with origin/main). When no commits are ahead of
		// origin/main the repo has no item-specific changes — skip it.
		statRef := "origin/main...HEAD"
		statOut, err := gitFn(dir, "diff", "--stat", statRef)
		if err != nil || strings.TrimSpace(statOut) == "" {
			commitsAhead, caErr := gitFn(dir, "log", "--oneline", "origin/main..HEAD")
			if caErr == nil && strings.TrimSpace(commitsAhead) == "" {
				continue
			}
			statRef = "HEAD^...HEAD"
			statOut, err = gitFn(dir, "diff", "--stat", statRef)
			if err != nil {
				continue
			}
		}

		files, lines := parseDiffStat(statOut)
		totalFiles += files
		totalLines += lines

		// Use the same ref range for path collection so blast-radius detection
		// is consistent with the stat (fixes the orphan-branch case).
		namesOut, err := gitFn(dir, "diff", "--name-only", statRef)
		if err == nil {
			for _, p := range strings.Split(strings.TrimSpace(namesOut), "\n") {
				if p != "" {
					changedPaths = append(changedPaths, p)
				}
			}
		}
	}

	if totalFiles == 0 && len(changedPaths) == 0 {
		fmt.Fprintf(os.Stderr, "review-depth: %s: no diff found across worktree repos — defaulting to 'high' (safe fallback)\n", id)
		fmt.Println("high")
		return 0
	}

	fmt.Println(computeDepth(totalFiles, totalLines, changedPaths))
	return 0
}

// computeDepth applies the bucketing logic. Pure function for testability.
func computeDepth(files, lines int, paths []string) string {
	if hasBlastRadiusPath(paths) {
		return "high"
	}
	if lines >= reviewDepthHighLines || files >= reviewDepthHighFiles {
		return "high"
	}
	if lines <= reviewDepthSmallLines && files <= reviewDepthSmallFiles {
		return "low"
	}
	return "medium"
}

// hasBlastRadiusPath reports whether any path in paths matches a
// blast-radius fragment (case-insensitive substring match).
func hasBlastRadiusPath(paths []string) bool {
	for _, p := range paths {
		pl := strings.ToLower(p)
		for _, frag := range blastRadiusPaths {
			if strings.Contains(pl, frag) {
				return true
			}
		}
	}
	return false
}

// parseDiffStat extracts total file count and total line delta from
// `git diff --stat` output. Returns (0, 0) when the summary line is absent.
func parseDiffStat(stat string) (files, lines int) {
	m := statLineRe.FindStringSubmatch(stat)
	if m == nil {
		return 0, 0
	}
	files, _ = strconv.Atoi(m[1])
	ins, _ := strconv.Atoi(m[2])
	del, _ := strconv.Atoi(m[3])
	return files, ins + del
}
