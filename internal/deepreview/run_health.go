package deepreview

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	runHealthJSONName = "run-health.json"
	runHealthMDName   = "run-health.md"
)

type runHealthReport struct {
	RunID           string                  `json:"run_id"`
	Mode            string                  `json:"mode"`
	GeneratedAt     string                  `json:"generated_at"`
	CompletedRounds int                     `json:"completed_rounds"`
	HasFinalStatus  bool                    `json:"has_final_status"`
	FinalStatus     *RoundStatus            `json:"final_status,omitempty"`
	Artifacts       runHealthArtifactCounts `json:"artifacts"`
	Stderr          runHealthStderrSummary  `json:"stderr"`
}

type runHealthArtifactCounts struct {
	ReviewReports         int  `json:"review_reports"`
	RoundSummaries        int  `json:"round_summaries"`
	RoundStatuses         int  `json:"round_statuses"`
	RoundRecords          int  `json:"round_records"`
	StdoutLogs            int  `json:"stdout_logs"`
	StderrLogs            int  `json:"stderr_logs"`
	DeliveryResultPresent bool `json:"delivery_result_present"`
	PRTitlePresent        bool `json:"pr_title_present"`
	PRBodyPresent         bool `json:"pr_body_present"`
}

type runHealthStderrSummary struct {
	TotalFiles     int                   `json:"total_files"`
	NonEmptyFiles  int                   `json:"non_empty_files"`
	TotalBytes     int64                 `json:"total_bytes"`
	TotalLines     int                   `json:"total_lines"`
	WarningLines   int                   `json:"warning_lines"`
	ErrorLines     int                   `json:"error_lines"`
	NonEmptyLogSet []runHealthStderrFile `json:"non_empty_logs,omitempty"`
}

type runHealthStderrFile struct {
	Path         string `json:"path"`
	Bytes        int64  `json:"bytes"`
	Lines        int    `json:"lines"`
	WarningLines int    `json:"warning_lines"`
	ErrorLines   int    `json:"error_lines"`
}

func collectRunHealth(runRoot, runID, mode string) (runHealthReport, error) {
	report := runHealthReport{
		RunID:       strings.TrimSpace(runID),
		Mode:        strings.TrimSpace(mode),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if strings.TrimSpace(runRoot) == "" {
		return report, NewDeepReviewError("run root is required for run health")
	}

	reviewSnapshot := readCompletionReviewSnapshot(runRoot)
	report.CompletedRounds = reviewSnapshot.CompletedRounds
	report.HasFinalStatus = reviewSnapshot.HasFinalStatus
	if reviewSnapshot.HasFinalStatus {
		statusCopy := reviewSnapshot.FinalStatus
		report.FinalStatus = &statusCopy
	}

	var stderrFiles []runHealthStderrFile
	err := filepath.WalkDir(runRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		base := filepath.Base(path)
		rel := relativeRunPath(runRoot, path)
		switch {
		case matchedRoundReview(base):
			report.Artifacts.ReviewReports++
		case base == "round-summary.md":
			report.Artifacts.RoundSummaries++
		case base == "round-status.json":
			report.Artifacts.RoundStatuses++
		case base == "round.json":
			report.Artifacts.RoundRecords++
		case strings.HasSuffix(base, ".stdout.jsonl"):
			report.Artifacts.StdoutLogs++
		case strings.HasSuffix(base, ".stderr.log"):
			report.Artifacts.StderrLogs++
			fileSummary, err := summarizeStderrLog(rel, path)
			if err != nil {
				return err
			}
			report.Stderr.TotalFiles++
			report.Stderr.TotalBytes += fileSummary.Bytes
			report.Stderr.TotalLines += fileSummary.Lines
			report.Stderr.WarningLines += fileSummary.WarningLines
			report.Stderr.ErrorLines += fileSummary.ErrorLines
			if fileSummary.Bytes > 0 {
				report.Stderr.NonEmptyFiles++
				stderrFiles = append(stderrFiles, fileSummary)
			}
		case rel == filepath.ToSlash(filepath.Join("delivery", "delivery-result.json")):
			report.Artifacts.DeliveryResultPresent = true
		case rel == "pr-title.txt":
			report.Artifacts.PRTitlePresent = true
		case rel == "pr-body.md":
			report.Artifacts.PRBodyPresent = true
		}
		return nil
	})
	if err != nil {
		return report, err
	}

	sort.Slice(stderrFiles, func(i, j int) bool {
		if stderrFiles[i].Path == stderrFiles[j].Path {
			return stderrFiles[i].Bytes < stderrFiles[j].Bytes
		}
		return stderrFiles[i].Path < stderrFiles[j].Path
	})
	report.Stderr.NonEmptyLogSet = stderrFiles
	return report, nil
}

func matchedRoundReview(base string) bool {
	ok, err := filepath.Match("review-*.md", base)
	return err == nil && ok
}

func relativeRunPath(runRoot, path string) string {
	rel, err := filepath.Rel(runRoot, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func summarizeStderrLog(relPath, path string) (runHealthStderrFile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return runHealthStderrFile{}, err
	}
	summary := runHealthStderrFile{
		Path:  relPath,
		Bytes: info.Size(),
	}
	if info.Size() == 0 {
		return summary, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return runHealthStderrFile{}, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		summary.Lines++
		if looksLikeWarningLine(line) {
			summary.WarningLines++
		}
		if looksLikeErrorLine(line) {
			summary.ErrorLines++
		}
	}
	if err := scanner.Err(); err != nil {
		return runHealthStderrFile{}, err
	}
	return summary, nil
}

func looksLikeWarningLine(line string) bool {
	return logLineContainsLevel(line, "WARN") || strings.Contains(strings.ToLower(strings.TrimSpace(line)), " warning:")
}

func looksLikeErrorLine(line string) bool {
	return logLineContainsLevel(line, "ERROR") || strings.Contains(strings.ToLower(strings.TrimSpace(line)), " error:")
}

func logLineContainsLevel(line, level string) bool {
	trimmed := strings.ToUpper(strings.TrimSpace(line))
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, level+" ") {
		return true
	}
	return strings.Contains(trimmed, " "+level+" ")
}

func buildRunHealthMarkdown(report runHealthReport) string {
	lines := []string{
		"# deepreview run health",
		"",
		fmt.Sprintf("- run id: `%s`", report.RunID),
		fmt.Sprintf("- mode: `%s`", report.Mode),
		fmt.Sprintf("- generated at: `%s`", report.GeneratedAt),
		fmt.Sprintf("- completed rounds: `%d`", report.CompletedRounds),
	}
	if report.FinalStatus != nil {
		lines = append(lines, fmt.Sprintf("- final review status: %s", sanitizePublicText(formatFinalReviewStatus(*report.FinalStatus))))
		if reason := formatFinalReviewReason(*report.FinalStatus); reason != "" {
			lines = append(lines, fmt.Sprintf("- final review summary: %s", sanitizePublicText(reason)))
		}
	}

	lines = append(lines,
		"",
		"## artifact coverage",
		fmt.Sprintf("- review reports: `%d`", report.Artifacts.ReviewReports),
		fmt.Sprintf("- round summaries: `%d`", report.Artifacts.RoundSummaries),
		fmt.Sprintf("- round status files: `%d`", report.Artifacts.RoundStatuses),
		fmt.Sprintf("- round records: `%d`", report.Artifacts.RoundRecords),
		fmt.Sprintf("- stdout logs: `%d`", report.Artifacts.StdoutLogs),
		fmt.Sprintf("- stderr logs: `%d` total, `%d` non-empty", report.Artifacts.StderrLogs, report.Stderr.NonEmptyFiles),
		fmt.Sprintf("- delivery result present: `%s`", yesNo(report.Artifacts.DeliveryResultPresent)),
		fmt.Sprintf("- PR title present: `%s`", yesNo(report.Artifacts.PRTitlePresent)),
		fmt.Sprintf("- PR body present: `%s`", yesNo(report.Artifacts.PRBodyPresent)),
		"",
		"## stderr overview",
		fmt.Sprintf("- total stderr bytes: `%d`", report.Stderr.TotalBytes),
		fmt.Sprintf("- total stderr lines: `%d`", report.Stderr.TotalLines),
		fmt.Sprintf("- warning lines: `%d`", report.Stderr.WarningLines),
		fmt.Sprintf("- error lines: `%d`", report.Stderr.ErrorLines),
		"",
		"## non-empty stderr logs",
	)
	if len(report.Stderr.NonEmptyLogSet) == 0 {
		lines = append(lines, "- none")
	} else {
		for _, file := range report.Stderr.NonEmptyLogSet {
			lines = append(lines,
				fmt.Sprintf(
					"- `%s`: bytes=`%d`, lines=`%d`, warnings=`%d`, errors=`%d`",
					file.Path,
					file.Bytes,
					file.Lines,
					file.WarningLines,
					file.ErrorLines,
				),
			)
		}
	}

	return sanitizePublicText(strings.Join(lines, "\n") + "\n")
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func writeRunHealthArtifacts(runRoot string, report runHealthReport) error {
	jsonPath := filepath.Join(runRoot, runHealthJSONName)
	mdPath := filepath.Join(runRoot, runHealthMDName)

	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(jsonPath, payload, 0o644); err != nil {
		return err
	}

	markdown := buildRunHealthMarkdown(report)
	if err := assertPublicTextSafe(markdown, "run health"); err != nil {
		return err
	}
	return os.WriteFile(mdPath, []byte(markdown), 0o644)
}

func runHealthArtifactsExist(runRoot string) bool {
	if strings.TrimSpace(runRoot) == "" {
		return false
	}
	for _, path := range []string{
		filepath.Join(runRoot, runHealthJSONName),
		filepath.Join(runRoot, runHealthMDName),
	} {
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	return true
}
