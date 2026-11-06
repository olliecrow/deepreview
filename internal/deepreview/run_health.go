package deepreview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	runHealthJSONName    = "run-health.json"
	runHealthMDName      = "run-health.md"
	stderrReadChunkBytes = 32 * 1024
	stderrClassifyLimit  = 64 * 1024
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
	Diagnostics     []string                `json:"diagnostics,omitempty"`
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
	LongLines      int                   `json:"long_lines"`
	NonEmptyLogSet []runHealthStderrFile `json:"non_empty_logs,omitempty"`
}

type runHealthStderrFile struct {
	Path         string `json:"path"`
	Bytes        int64  `json:"bytes"`
	Lines        int    `json:"lines"`
	WarningLines int    `json:"warning_lines"`
	ErrorLines   int    `json:"error_lines"`
	LongLines    int    `json:"long_lines,omitempty"`
	Unreadable   bool   `json:"unreadable,omitempty"`
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
			report.Diagnostics = append(report.Diagnostics, "a run-health entry could not be read")
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
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
			fileSummary := summarizeStderrLog(rel, path)
			report.Stderr.TotalFiles++
			if fileSummary.Unreadable {
				report.Diagnostics = append(report.Diagnostics, "a stderr log could not be read")
			}
			report.Stderr.TotalBytes += fileSummary.Bytes
			report.Stderr.TotalLines += fileSummary.Lines
			report.Stderr.WarningLines += fileSummary.WarningLines
			report.Stderr.ErrorLines += fileSummary.ErrorLines
			report.Stderr.LongLines += fileSummary.LongLines
			if fileSummary.Bytes > 0 && !fileSummary.Unreadable {
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

func summarizeStderrLog(relPath, path string) runHealthStderrFile {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		summary := runHealthStderrFile{Path: relPath, Unreadable: true}
		if err == nil {
			summary.Bytes = info.Size()
		}
		return summary
	}
	summary := runHealthStderrFile{
		Path:  relPath,
		Bytes: info.Size(),
	}
	if info.Size() == 0 {
		return summary
	}

	f, err := os.Open(path)
	if err != nil {
		summary.Unreadable = true
		return summary
	}
	defer f.Close()

	buf := make([]byte, stderrReadChunkBytes)
	line := make([]byte, 0, 256)
	lineTooLong := false
	finishLine := func() {
		summary.Lines++
		if lineTooLong {
			summary.LongLines++
		} else {
			text := string(line)
			if looksLikeWarningLine(text) {
				summary.WarningLines++
			}
			if looksLikeErrorLine(text) {
				summary.ErrorLines++
			}
		}
		line = line[:0]
		lineTooLong = false
	}
	for {
		n, readErr := f.Read(buf)
		chunk := buf[:n]
		for len(chunk) > 0 {
			nl := bytes.IndexByte(chunk, '\n')
			if nl < 0 {
				if lineTooLong || len(line)+len(chunk) > stderrClassifyLimit {
					lineTooLong = true
					line = line[:0]
				} else {
					line = append(line, chunk...)
				}
				break
			}
			piece := chunk[:nl]
			chunk = chunk[nl+1:]
			if lineTooLong || len(line)+len(piece) > stderrClassifyLimit {
				lineTooLong = true
				line = line[:0]
			} else {
				line = append(line, piece...)
			}
			finishLine()
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			summary.Unreadable = true
			return summary
		}
	}
	if lineTooLong || len(line) > 0 {
		finishLine()
	}
	return summary
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
		fmt.Sprintf("- long lines: `%d`", report.Stderr.LongLines),
		"",
		"## non-empty stderr logs",
	)
	if len(report.Stderr.NonEmptyLogSet) == 0 {
		lines = append(lines, "- none")
	} else {
		for _, file := range report.Stderr.NonEmptyLogSet {
			lines = append(lines,
				fmt.Sprintf(
					"- `%s`: bytes=`%d`, lines=`%d`, warnings=`%d`, errors=`%d`, long_lines=`%d`",
					file.Path,
					file.Bytes,
					file.Lines,
					file.WarningLines,
					file.ErrorLines,
					file.LongLines,
				),
			)
		}
	}
	if len(report.Diagnostics) > 0 {
		lines = append(lines, "", "## diagnostics")
		for _, item := range report.Diagnostics {
			lines = append(lines, "- "+item)
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

func sanitizeRunHealthReport(report runHealthReport) runHealthReport {
	report.RunID = sanitizePublicText(report.RunID)
	report.Mode = sanitizePublicText(report.Mode)
	report.GeneratedAt = sanitizePublicText(report.GeneratedAt)
	if report.FinalStatus != nil {
		status := *report.FinalStatus
		status.Decision = sanitizePublicText(status.Decision)
		status.Reason = sanitizePublicText(status.Reason)
		if status.NextFocus != nil {
			nextFocus := sanitizePublicText(*status.NextFocus)
			status.NextFocus = &nextFocus
		}
		report.FinalStatus = &status
	}
	if len(report.Stderr.NonEmptyLogSet) > 0 {
		logs := make([]runHealthStderrFile, len(report.Stderr.NonEmptyLogSet))
		for i, file := range report.Stderr.NonEmptyLogSet {
			file.Path = sanitizePublicText(file.Path)
			logs[i] = file
		}
		report.Stderr.NonEmptyLogSet = logs
	}
	if len(report.Diagnostics) > 0 {
		diags := make([]string, len(report.Diagnostics))
		for i, item := range report.Diagnostics {
			diags[i] = sanitizePublicText(item)
		}
		report.Diagnostics = diags
	}
	return report
}

func writeRunHealthArtifacts(runRoot string, report runHealthReport) error {
	sanitized := sanitizeRunHealthReport(report)
	payload, err := json.MarshalIndent(sanitized, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if err := assertPublicTextSafe(string(payload), "run health"); err != nil {
		return err
	}
	markdown := buildRunHealthMarkdown(sanitized)
	if err := assertPublicTextSafe(markdown, "run health"); err != nil {
		return err
	}
	if err := writePrivateFile(filepath.Join(runRoot, runHealthJSONName), payload); err != nil {
		return err
	}
	return writePrivateFile(filepath.Join(runRoot, runHealthMDName), []byte(markdown))
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
