package deepreview

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestRenderProgressBarBounds(t *testing.T) {
	bar := renderProgressBar(5, 10, 10)
	if bar != "[=====-----]" {
		t.Fatalf("unexpected bar: %s", bar)
	}
	bar = renderProgressBar(15, 10, 10)
	if bar != "[==========]" {
		t.Fatalf("expected capped full bar, got: %s", bar)
	}
	bar = renderProgressBar(0, 0, 5)
	if bar != "[-----]" {
		t.Fatalf("expected empty bar when total=0, got: %s", bar)
	}
}

func TestCompactWindowTailsWithinBounds(t *testing.T) {
	rows := []StageSnapshot{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
		{Name: "d"},
	}
	window, hidden := compactWindow(rows, 2)
	if len(window) != 2 || window[0].Name != "c" || window[1].Name != "d" {
		t.Fatalf("unexpected tail window: %+v", window)
	}
	if hidden != 2 {
		t.Fatalf("expected hidden=2, got %d", hidden)
	}
}

func TestRenderCompactRowIncludesCoreFields(t *testing.T) {
	rn := 2
	row := StageSnapshot{
		RoundNumber: &rn,
		Name:        "execute stage",
		Status:      "running",
		Message:     "processing",
		Elapsed:     95 * time.Second,
	}
	line := renderCompactRow(row, 120)
	for _, expected := range []string{"r2", "running", "01:35", "execute stage", "processing"} {
		if !strings.Contains(line, expected) {
			t.Fatalf("compact row missing %q: %s", expected, line)
		}
	}
}

func TestLatestActivityPrefersRunningStage(t *testing.T) {
	r1 := 1
	r2 := 2
	snapshot := ProgressSnapshot{
		Stages: []StageSnapshot{
			{RoundNumber: &r1, Name: "prepare", Status: "done", Elapsed: 10 * time.Second},
			{RoundNumber: &r2, Name: "execute", Status: "running", Elapsed: 25 * time.Second, Message: "planning"},
		},
	}
	activity := latestActivity(snapshot)
	if activity.round != "2" || activity.name != "execute" || activity.status != "running" {
		t.Fatalf("unexpected activity: %+v", activity)
	}
	if activity.elapsed != "00:25" {
		t.Fatalf("unexpected elapsed: %s", activity.elapsed)
	}
}

func TestStatusMarker(t *testing.T) {
	if statusMarker("running", true) != ">" {
		t.Fatalf("expected active running marker")
	}
	if statusMarker("done", false) != "+" {
		t.Fatalf("expected done marker")
	}
	if statusMarker("failed", false) != "x" {
		t.Fatalf("expected failed marker")
	}
}

func TestRenderPanelHonorsWidth(t *testing.T) {
	panel := renderPanel(tableBorderStyle, "test", []string{"line one", "line two"}, 48)
	if lipgloss.Width(panel) != 48 {
		t.Fatalf("expected panel width 48, got %d", lipgloss.Width(panel))
	}
}

func TestTimelineColumnsHaveUsableDetails(t *testing.T) {
	stageCol, statusCol, timeCol, detailsCol := timelineColumns(56)
	if stageCol < 12 {
		t.Fatalf("expected stage column >= 12, got %d", stageCol)
	}
	if statusCol != 8 || timeCol != 8 {
		t.Fatalf("unexpected status/time columns: %d %d", statusCol, timeCol)
	}
	if detailsCol < 8 {
		t.Fatalf("expected details column >= 8, got %d", detailsCol)
	}
}

func TestRenderFooterShowsDegradedDuringRunningFailures(t *testing.T) {
	snapshot := ProgressSnapshot{
		Stages: []StageSnapshot{
			{Name: "prepare", Status: "done"},
			{Name: "execute", Status: "failed"},
		},
	}
	footer, _ := renderFooter(snapshot)
	if !strings.Contains(footer, "running with 1 failed stage") {
		t.Fatalf("unexpected footer: %s", footer)
	}
}

func TestStageRowsLimitMinimumOne(t *testing.T) {
	lines := []string{"a", "b", "c"}
	if got := stageRowsLimit(lines, 80, 2); got != 1 {
		t.Fatalf("stageRowsLimit should clamp to 1, got %d", got)
	}
}

func TestStatusTextStyleFallback(t *testing.T) {
	got := statusTextStyle("unknown").Render("x")
	want := valueStyle.Render("x")
	if got != want {
		t.Fatalf("unexpected fallback style")
	}
}

func TestJoinChipsWithinWidthBounds(t *testing.T) {
	chips := []string{
		chip(chipBaseStyle.Foreground(lipgloss.Color("231")).Background(lipgloss.Color("24")), "RUNNING"),
		chip(chipBaseStyle.Foreground(lipgloss.Color("231")).Background(lipgloss.Color("62")), "STAGES 12"),
		chip(chipBaseStyle.Foreground(lipgloss.Color("16")).Background(lipgloss.Color("228")), "RUNNING 1"),
	}
	line := joinChipsWithinWidth(chips, 20)
	if line == "" {
		t.Fatalf("expected non-empty chip line")
	}
	if lipgloss.Width(line) > 20 {
		t.Fatalf("chip line should fit width: got %d", lipgloss.Width(line))
	}
}

func TestTUIViewRespectsWidthInStandardLayout(t *testing.T) {
	model := seededTUIModelForViewTests(120, 40)
	view := model.View()
	for i, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > model.width {
			t.Fatalf("line %d width exceeds viewport: got=%d want<=%d line=%q", i+1, got, model.width, line)
		}
	}
}

func TestTUIViewRespectsWidthInCompactLayout(t *testing.T) {
	model := seededTUIModelForViewTests(52, 10)
	view := model.View()
	for i, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > model.width {
			t.Fatalf("line %d width exceeds viewport: got=%d want<=%d line=%q", i+1, got, model.width, line)
		}
	}
}

func TestTUIWindowResizeRequestsClearScreen(t *testing.T) {
	model := newTUIModel(NewSharedProgressState(), make(chan error), 0, 0)
	updated, cmd := model.Update(tea.WindowSizeMsg{Width: 88, Height: 25})
	if cmd == nil {
		t.Fatalf("expected clear-screen command on resize")
	}
	next := updated.(tuiModel)
	if next.width != 88 || next.height != 25 {
		t.Fatalf("unexpected model size after resize: width=%d height=%d", next.width, next.height)
	}
}

func TestNewTUIModelUsesInitialViewport(t *testing.T) {
	model := newTUIModel(NewSharedProgressState(), make(chan error), 91, 27)
	if model.width != 91 || model.height != 27 {
		t.Fatalf("expected initial viewport 91x27, got %dx%d", model.width, model.height)
	}
}

func TestNormalizeDisplayTextStripsANSIAndControls(t *testing.T) {
	in := "\x1b[31mline-1\nline-2\tok\x07\x1b[0m"
	got := normalizeDisplayText(in)
	if got != "line-1 line-2 ok" {
		t.Fatalf("unexpected normalized text: %q", got)
	}
}

func TestFitUsesDisplayWidth(t *testing.T) {
	text := "AB界CD"
	got := fit(text, 5)
	if lipgloss.Width(got) > 5 {
		t.Fatalf("fit exceeded width: got=%q width=%d", got, lipgloss.Width(got))
	}
}

func TestTUIViewNoisyStageTextStillFitsViewport(t *testing.T) {
	state := NewSharedProgressState()
	reporter := NewTUIProgressReporter(state)
	reporter.RunStarted("run-123", "owner/repo", "feature/resize", "pr", "/tmp/deepreview/runs/run-123")
	r1 := 1
	reporter.StageStarted("independent review stage", &r1, "\x1b[32mspawning\nindependent\treviewers\x1b[0m")
	reporter.StageProgress("independent review stage", "completed\nworkers:\t1/4", &r1)
	reporter.RunFinished(false, "failed:\nline-1\tline-2")

	model := newTUIModel(state, make(chan error), 0, 0)
	model.width = 72
	model.height = 18
	view := model.View()
	for i, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > model.width {
			t.Fatalf("line %d width exceeds viewport: got=%d want<=%d line=%q", i+1, got, model.width, line)
		}
	}
}

func TestTUIViewUltraNarrowFallbackFitsViewport(t *testing.T) {
	model := seededTUIModelForViewTests(12, 8)
	view := model.View()
	for i, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > model.width {
			t.Fatalf("line %d width exceeds viewport: got=%d want<=%d line=%q", i+1, got, model.width, line)
		}
	}
}

func TestTUIViewRespectsWidthAcrossRange(t *testing.T) {
	for width := 1; width <= 200; width++ {
		for _, height := range []int{1, 2, 4, 8, 14, 40, 60} {
			model := seededTUIModelForViewTests(width, height)
			view := model.View()
			for i, line := range strings.Split(view, "\n") {
				if got := lipgloss.Width(line); got > model.width {
					t.Fatalf("width=%d height=%d line=%d exceeds viewport: got=%d want<=%d line=%q", width, height, i+1, got, model.width, line)
				}
			}
		}
	}
}

func TestTUIViewLeavesRightEdgeGutterAcrossRange(t *testing.T) {
	for width := 2; width <= 200; width++ {
		for _, height := range []int{1, 2, 4, 8, 14, 40, 60} {
			model := seededTUIModelForViewTests(width, height)
			view := model.View()
			for i, line := range strings.Split(view, "\n") {
				if got := lipgloss.Width(line); got >= model.width {
					t.Fatalf("width=%d height=%d line=%d should leave right edge free: got=%d want<%d line=%q", width, height, i+1, got, model.width, line)
				}
			}
		}
	}
}

func TestEffectiveContentWidth(t *testing.T) {
	if got := effectiveContentWidth(120); got != 114 {
		t.Fatalf("expected 114 content width for viewport 120, got %d", got)
	}
	if got := effectiveContentWidth(1); got != 1 {
		t.Fatalf("expected minimum width 1, got %d", got)
	}
	if got := effectiveContentWidth(0); got != defaultContentWidth {
		t.Fatalf("expected default width %d for unknown viewport, got %d", defaultContentWidth, got)
	}
}

func TestNoBorderCollisionArtifactAcrossRange(t *testing.T) {
	for width := 2; width <= 200; width++ {
		for _, height := range []int{1, 2, 4, 8, 14, 40, 60} {
			model := seededTUIModelForViewTests(width, height)
			view := model.View()
			if strings.Contains(view, "││") {
				t.Fatalf("unexpected border collision artifact at width=%d height=%d", width, height)
			}
		}
	}
}

func TestTopPanelsBottomBordersAlignWhenSideBySide(t *testing.T) {
	model := seededTUIModelForViewTests(140, 40)
	view := model.View()
	foundAlignedBottom := false
	for _, line := range strings.Split(view, "\n") {
		first := strings.Index(line, "╰")
		last := strings.LastIndex(line, "╰")
		if first >= 0 && last > first && strings.Contains(line[first:], "╯") && strings.Contains(line[last:], "╯") {
			foundAlignedBottom = true
			break
		}
	}
	if !foundAlignedBottom {
		t.Fatalf("expected side-by-side top panels to have aligned bottom borders in one row")
	}
}

func TestTUIViewLeavesRightEdgeUnderRandomizedInputs(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for iter := 0; iter < 250; iter++ {
		width := 2 + rng.Intn(199)
		height := 1 + rng.Intn(60)
		state := NewSharedProgressState()
		reporter := NewTUIProgressReporter(state)
		reporter.RunStarted(
			fmt.Sprintf("run-%03d", iter),
			randomDisplayText(rng, 24),
			randomDisplayText(rng, 18),
			"pr",
			"/tmp/deepreview/runs/random",
		)

		stageCount := 1 + rng.Intn(24)
		for i := 0; i < stageCount; i++ {
			stageName := randomDisplayText(rng, 48)
			msg := randomDisplayText(rng, 72)
			var round *int
			if rng.Intn(10) != 0 {
				r := 1 + rng.Intn(4)
				round = &r
			}
			reporter.StageStarted(stageName, round, msg)
			if rng.Intn(3) != 0 {
				reporter.StageProgress(stageName, randomDisplayText(rng, 80), round)
			}
			if rng.Intn(4) != 0 {
				reporter.StageFinished(stageName, round, rng.Intn(5) != 0, randomDisplayText(rng, 84))
			}
		}
		if rng.Intn(2) == 0 {
			reporter.RunFinished(rng.Intn(4) != 0, randomDisplayText(rng, 64))
		}

		model := newTUIModel(state, make(chan error), width, height)
		model.tick = iter
		view := model.View()
		for i, line := range strings.Split(view, "\n") {
			if got := lipgloss.Width(line); got >= model.width {
				t.Fatalf(
					"iter=%d width=%d height=%d line=%d should leave right edge free: got=%d want<%d line=%q",
					iter, width, height, i+1, got, model.width, line,
				)
			}
		}
	}
}

func TestStandaloneTerminalWrapDriftModel(t *testing.T) {
	termWidth := 80
	// Exact-width writes trigger terminal auto-wrap before newline,
	// so each logical line consumes an extra terminal row.
	exactLines := []string{
		strings.Repeat("x", termWidth),
		strings.Repeat("y", termWidth),
	}
	if drift := rendererDriftRows(exactLines, termWidth); drift <= 0 {
		t.Fatalf("expected positive drift for exact-width lines, got %d", drift)
	}

	// Staying one column below terminal width avoids auto-wrap drift.
	safeLines := []string{
		strings.Repeat("x", termWidth-1),
		strings.Repeat("y", termWidth-1),
	}
	if drift := rendererDriftRows(safeLines, termWidth); drift != 0 {
		t.Fatalf("expected zero drift for sub-width lines, got %d", drift)
	}
}

func TestRenderedTUIViewHasNoWrapDriftAcrossRange(t *testing.T) {
	for width := 1; width <= 200; width++ {
		for _, height := range []int{1, 2, 4, 8, 14, 40, 60} {
			model := seededTUIModelForViewTests(width, height)
			view := model.View()
			lines := strings.Split(view, "\n")
			if drift := rendererDriftRows(lines, model.width); drift != 0 {
				t.Fatalf("width=%d height=%d expected zero wrap drift, got %d", width, height, drift)
			}
		}
	}
}

func TestRenderedRowsForView(t *testing.T) {
	view := strings.Join([]string{
		strings.Repeat("x", 79),
		strings.Repeat("y", 80),
	}, "\n")
	if got := renderedRowsForView(view, 80); got != 3 {
		t.Fatalf("expected 3 rendered rows, got %d", got)
	}
}

func TestClampViewWidthUsesSafeGutter(t *testing.T) {
	view := strings.Join([]string{
		strings.Repeat("a", 200),
		strings.Repeat("b", 200),
	}, "\n")
	out := clampViewWidth(view, 80)
	for i, line := range strings.Split(out, "\n") {
		if got := lipgloss.Width(line); got > effectiveContentWidth(80) {
			t.Fatalf("line %d exceeds safe width after clamp: got=%d want<=%d", i+1, got, effectiveContentWidth(80))
		}
	}
}

func TestTUIViewRespectsHeightAcrossRange(t *testing.T) {
	for width := 1; width <= 200; width++ {
		for _, height := range []int{1, 2, 4, 8, 10, 12, 14, 16, 18, 20, 24, 30, 40, 60} {
			model := seededTUIModelForViewTests(width, height)
			view := model.View()
			if got := renderedViewRows(view, width); got > height {
				t.Fatalf("width=%d height=%d rows=%d exceeds viewport", width, height, got)
			}
		}
	}
}

func TestTUIViewRespectsHeightUnderRandomizedInputs(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for iter := 0; iter < 250; iter++ {
		width := 1 + rng.Intn(200)
		height := 1 + rng.Intn(60)
		state := NewSharedProgressState()
		reporter := NewTUIProgressReporter(state)
		reporter.RunStarted(
			fmt.Sprintf("run-height-%03d", iter),
			randomDisplayText(rng, 24),
			randomDisplayText(rng, 18),
			"pr",
			"/tmp/deepreview/runs/random",
		)

		stageCount := 1 + rng.Intn(24)
		for i := 0; i < stageCount; i++ {
			stageName := randomDisplayText(rng, 48)
			msg := randomDisplayText(rng, 72)
			var round *int
			if rng.Intn(10) != 0 {
				r := 1 + rng.Intn(4)
				round = &r
			}
			reporter.StageStarted(stageName, round, msg)
			if rng.Intn(3) != 0 {
				reporter.StageProgress(stageName, randomDisplayText(rng, 80), round)
			}
			if rng.Intn(4) != 0 {
				reporter.StageFinished(stageName, round, rng.Intn(5) != 0, randomDisplayText(rng, 84))
			}
		}
		if rng.Intn(2) == 0 {
			reporter.RunFinished(rng.Intn(4) != 0, randomDisplayText(rng, 64))
		}

		model := newTUIModel(state, make(chan error), width, height)
		model.tick = iter
		view := model.View()
		if got := renderedViewRows(view, width); got > height {
			t.Fatalf("iter=%d width=%d height=%d rows=%d exceeds viewport", iter, width, height, got)
		}
	}
}

func seededTUIModelForViewTests(width, height int) tuiModel {
	state := NewSharedProgressState()
	reporter := NewTUIProgressReporter(state)
	reporter.RunStarted("run-123", "owner/repo", "feature/resize", "pr", "/tmp/deepreview/runs/run-123")

	r1 := 1
	r2 := 2
	reporter.StageStarted("prepare", nil, "cloning/fetching managed repository")
	reporter.StageFinished("prepare", nil, true, "done")
	reporter.StageStarted("independent review stage", &r1, "spawning independent reviewers")
	reporter.StageProgress("independent review stage", "completed workers: 1/4", &r1)
	reporter.StageFinished("independent review stage", &r1, true, "reports: 4")
	reporter.StageStarted("execute stage", &r1, "running execute prompt queue")
	reporter.StageFinished("execute stage", &r1, true, "decision=continue")
	reporter.StageStarted("independent review stage", &r2, "spawning independent reviewers")
	reporter.StageProgress("independent review stage", "completed workers: 2/4", &r2)

	model := newTUIModel(state, make(chan error), 0, 0)
	model.width = width
	model.height = height
	model.tick = 7
	return model
}

func randomDisplayText(rng *rand.Rand, maxLen int) string {
	if maxLen < 1 {
		maxLen = 1
	}
	length := 1 + rng.Intn(maxLen)
	var b strings.Builder
	tokens := []string{
		"a", "b", "c", "1", "2", "-", "_", " ", ".", ":", "/", "界",
		"\n", "\t", "\r", "\x07", "\x1b[31m", "\x1b[0m",
	}
	for i := 0; i < length; i++ {
		b.WriteString(tokens[rng.Intn(len(tokens))])
	}
	return b.String()
}

func rendererDriftRows(lines []string, termWidth int) int {
	if termWidth <= 0 {
		return 0
	}
	consumed := 0
	for _, line := range lines {
		consumed += terminalRowsForLine(line, termWidth)
	}
	return consumed - len(lines)
}

func renderedViewRows(view string, termWidth int) int {
	if strings.TrimSpace(view) == "" {
		return 0
	}
	rows := 0
	for _, line := range strings.Split(view, "\n") {
		rows += terminalRowsForLine(line, termWidth)
	}
	return rows
}

func terminalRowsForLine(line string, termWidth int) int {
	if termWidth <= 0 {
		return 1
	}
	width := lipgloss.Width(line)
	return 1 + (width / termWidth)
}
