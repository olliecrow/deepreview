package deepreview

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

type workerResultMsg struct {
	err error
}

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func waitWorkerCmd(doneCh <-chan error) tea.Cmd {
	return func() tea.Msg {
		err := <-doneCh
		return workerResultMsg{err: err}
	}
}

type tuiModel struct {
	state        *SharedProgressState
	doneCh       <-chan error
	workerErr    error
	done         bool
	finalShownAt *time.Time
	width        int
	height       int
	tick         int
}

func newTUIModel(state *SharedProgressState, doneCh <-chan error, width, height int) tuiModel {
	return tuiModel{state: state, doneCh: doneCh, width: width, height: height}
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(tickCmd(), waitWorkerCmd(m.doneCh))
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, tea.ClearScreen
	case workerResultMsg:
		now := time.Now()
		m.done = true
		m.workerErr = msg.err
		m.finalShownAt = &now
		return m, tickCmd()
	case tickMsg:
		m.tick++
		if m.done && m.finalShownAt != nil {
			if time.Since(*m.finalShownAt) >= 600*time.Millisecond {
				return m, tea.Quit
			}
		}
		return m, tickCmd()
	}
	return m, nil
}

var (
	spinnerFrames = []string{"|", "/", "-", "\\"}

	headerStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("24")).Padding(0, 1)
	accentStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("45"))
	subtleStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	valueStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	runningStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	successStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	errorStyle         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	borderStyle        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 1)
	summaryBorderStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("69")).Padding(0, 1)
	tableBorderStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("33")).Padding(0, 1)
	footerBorderStyle  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	highlightStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51"))
	tableHeaderStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("117"))
	zebraStyle         = lipgloss.NewStyle().Background(lipgloss.Color("235"))
	chipBaseStyle      = lipgloss.NewStyle().Bold(true).Padding(0, 1)
)

const (
	passiveDisplayLine  = "display: passive live stream (auto-refresh 100ms)"
	stageLegendLine     = "legend: > active, + done, x failed, ~ running"
	ansiReset           = "\x1b[0m"
	viewportRightGutter = 1
	defaultContentWidth = 72
)

func chip(style lipgloss.Style, text string) string {
	return style.Render(text)
}

func joinChipsWithinWidth(chips []string, width int) string {
	if width <= 0 || len(chips) == 0 {
		return ""
	}
	line := ""
	for _, c := range chips {
		candidate := c
		if line != "" {
			candidate = line + " " + c
		}
		if lipgloss.Width(candidate) > width {
			break
		}
		line = candidate
	}
	if line == "" {
		return chips[0]
	}
	return line
}

func lineCount(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

func renderPanelTitle(label string) string {
	return accentStyle.Render(strings.ToUpper(label))
}

func panelInnerWidth(style lipgloss.Style, totalWidth int) int {
	if totalWidth < 1 {
		totalWidth = 1
	}
	inner := totalWidth - style.GetHorizontalBorderSize() - style.GetHorizontalPadding()
	if inner < 1 {
		inner = 1
	}
	return inner
}

func renderPanel(style lipgloss.Style, title string, bodyLines []string, totalWidth int) string {
	widthWithoutBorder := totalWidth - style.GetHorizontalBorderSize()
	if widthWithoutBorder < 1 {
		widthWithoutBorder = 1
	}
	inner := widthWithoutBorder - style.GetHorizontalPadding()
	if inner < 1 {
		inner = 1
	}
	lines := make([]string, 0, len(bodyLines)+1)
	lines = append(lines, renderPanelTitle(title))
	for _, line := range bodyLines {
		lines = append(lines, fit(line, inner))
	}
	return style.Width(widthWithoutBorder).Render(strings.Join(lines, "\n"))
}

func timelineColumns(innerWidth int) (int, int, int, int) {
	statusCol := 8
	timeCol := 8
	stageCol := clamp(innerWidth/3, 12, 34)
	detailsCol := innerWidth - (6 + 2 + stageCol + 2 + statusCol + 1 + timeCol + 2)
	if detailsCol < 12 {
		stageCol -= (12 - detailsCol)
		if stageCol < 12 {
			stageCol = 12
		}
		detailsCol = innerWidth - (6 + 2 + stageCol + 2 + statusCol + 1 + timeCol + 2)
	}
	if detailsCol < 8 {
		detailsCol = 8
	}
	return stageCol, statusCol, timeCol, detailsCol
}

type stageActivity struct {
	round   string
	name    string
	status  string
	elapsed string
	message string
}

func latestActivity(snapshot ProgressSnapshot) stageActivity {
	if len(snapshot.Stages) == 0 {
		return stageActivity{
			round:   "-",
			name:    "none",
			status:  "idle",
			elapsed: "00:00",
			message: "waiting for first stage",
		}
	}
	for i := len(snapshot.Stages) - 1; i >= 0; i-- {
		row := snapshot.Stages[i]
		if row.Status == "running" {
			return stageActivity{
				round:   stageRound(row.RoundNumber),
				name:    normalizeDisplayText(row.Name),
				status:  normalizeDisplayText(row.Status),
				elapsed: fmtDuration(row.Elapsed),
				message: normalizeDisplayText(row.Message),
			}
		}
	}
	row := snapshot.Stages[len(snapshot.Stages)-1]
	return stageActivity{
		round:   stageRound(row.RoundNumber),
		name:    normalizeDisplayText(row.Name),
		status:  normalizeDisplayText(row.Status),
		elapsed: fmtDuration(row.Elapsed),
		message: normalizeDisplayText(row.Message),
	}
}

func stageRound(roundNumber *int) string {
	if roundNumber == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *roundNumber)
}

func maxSeenRound(snapshot ProgressSnapshot) string {
	maxRound := 0
	for _, row := range snapshot.Stages {
		if row.RoundNumber != nil && *row.RoundNumber > maxRound {
			maxRound = *row.RoundNumber
		}
	}
	if maxRound == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", maxRound)
}

func statusMarker(status string, isActive bool) string {
	if isActive && status == "running" {
		return ">"
	}
	switch status {
	case "running":
		return "~"
	case "done":
		return "+"
	case "failed":
		return "x"
	default:
		return "-"
	}
}

func (m tuiModel) View() string {
	if m.width > 0 && m.width <= 1 {
		return ""
	}
	snapshot := m.state.Snapshot()
	spinner := spinnerFrames[m.tick%len(spinnerFrames)]
	now := time.Now()
	end := now
	if snapshot.RunFinishedAt != nil {
		end = *snapshot.RunFinishedAt
	}
	elapsed := end.Sub(snapshot.RunStartedAt)

	doneCount := 0
	runningCount := 0
	failedCount := 0
	for _, row := range snapshot.Stages {
		switch row.Status {
		case "done":
			doneCount++
		case "running":
			runningCount++
		case "failed":
			failedCount++
		}
	}
	completedCount := doneCount + failedCount

	contentWidth := effectiveContentWidth(m.width)
	if m.width > 0 && contentWidth < 28 {
		compactTitle := fit(fmt.Sprintf("deepreview %s %s", spinner, fmtDuration(elapsed)), contentWidth)
		latest := fit("latest: "+latestStageLine(snapshot), contentWidth)
		return clampViewHeight(strings.Join([]string{compactTitle, latest}, "\n"), m.width, m.height)
	}

	lines := make([]string, 0, 12)
	topPlain := fmt.Sprintf("deepreview %s  elapsed %s", spinner, fmtDuration(elapsed))
	lines = append(lines, headerStyle.Render(fit(topPlain, panelInnerWidth(headerStyle, contentWidth))))

	runChipStyle := chipBaseStyle.Foreground(lipgloss.Color("231")).Background(lipgloss.Color("24"))
	runState := "RUNNING"
	if snapshot.Success != nil && *snapshot.Success {
		runState = "SUCCESS"
		runChipStyle = chipBaseStyle.Foreground(lipgloss.Color("22")).Background(lipgloss.Color("120"))
	} else if snapshot.Success != nil && !*snapshot.Success {
		runState = "FAILED"
		runChipStyle = chipBaseStyle.Foreground(lipgloss.Color("231")).Background(lipgloss.Color("160"))
	}
	stageChip := chip(chipBaseStyle.Foreground(lipgloss.Color("231")).Background(lipgloss.Color("62")), fmt.Sprintf("STAGES %d", len(snapshot.Stages)))
	runningChip := chip(chipBaseStyle.Foreground(lipgloss.Color("16")).Background(lipgloss.Color("228")), fmt.Sprintf("RUNNING %d", runningCount))
	doneChip := chip(chipBaseStyle.Foreground(lipgloss.Color("22")).Background(lipgloss.Color("120")), fmt.Sprintf("DONE %d", doneCount))
	failedChipStyle := chipBaseStyle.Foreground(lipgloss.Color("52")).Background(lipgloss.Color("215"))
	if failedCount > 0 {
		failedChipStyle = chipBaseStyle.Foreground(lipgloss.Color("231")).Background(lipgloss.Color("160"))
	}
	failedChip := chip(failedChipStyle, fmt.Sprintf("FAILED %d", failedCount))
	badgeLine := joinChipsWithinWidth([]string{
		chip(runChipStyle, runState),
		stageChip,
		runningChip,
		doneChip,
		failedChip,
	}, contentWidth)
	lines = append(lines, badgeLine+ansiReset)

	barWidth := clamp(contentWidth-30, 12, 54)
	progressBar := renderProgressBar(completedCount, len(snapshot.Stages), barWidth)
	progressPercent := 0
	if len(snapshot.Stages) > 0 {
		progressPercent = int((float64(completedCount) / float64(len(snapshot.Stages))) * 100)
	}
	progressPlain := fmt.Sprintf("progress %s %d/%d complete (%d%%)", progressBar, completedCount, len(snapshot.Stages), progressPercent)
	lines = append(lines, accentStyle.Render(fit(progressPlain, contentWidth)))

	activity := latestActivity(snapshot)
	metaLines := []string{
		"run: " + orFallback(snapshot.RunID, "starting..."),
		"repo: " + orFallback(snapshot.Repo, "-"),
		"branch: " + orFallback(snapshot.SourceBranch, "-") + "    mode: " + orFallback(snapshot.Mode, "-"),
		"artifacts: " + orFallback(snapshot.RunRoot, "-"),
		"latest: " + latestStageLine(snapshot),
	}
	summaryLines := []string{
		"round: " + maxSeenRound(snapshot),
		"active stage: " + activity.name,
		"active round: " + activity.round,
		"active status: " + activity.status,
		"active elapsed: " + activity.elapsed,
	}
	if strings.TrimSpace(activity.message) != "" {
		summaryLines = append(summaryLines, "details: "+activity.message)
	}

	if contentWidth >= 110 {
		leftW := clamp((contentWidth-1)*62/100, 52, contentWidth-34)
		rightW := contentWidth - 1 - leftW
		if rightW < 32 {
			rightW = 32
			leftW = contentWidth - 1 - rightW
		}
		contextBox := renderPanel(borderStyle, "run context", metaLines, leftW)
		summaryBox := renderPanel(summaryBorderStyle, "live summary", summaryLines, rightW)
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, valueStyle.Render(contextBox), " ", valueStyle.Render(summaryBox)))
	} else {
		contextBox := renderPanel(borderStyle, "run context", metaLines, contentWidth)
		summaryBox := renderPanel(summaryBorderStyle, "live summary", summaryLines, contentWidth)
		lines = append(lines, valueStyle.Render(contextBox))
		lines = append(lines, valueStyle.Render(summaryBox))
	}

	if m.width < 60 || m.height < 12 {
		compactRows := stageRowsLimit(lines, m.width, m.height)
		rows, hiddenOlder := compactWindow(snapshot.Stages, compactRows)
		compactLines := make([]string, 0, len(rows)+2)
		compactInner := panelInnerWidth(tableBorderStyle, contentWidth)
		if len(rows) == 0 {
			compactLines = append(compactLines, subtleStyle.Render(fit("- waiting for first stage...", compactInner)))
		}
		for _, row := range rows {
			compactLines = append(compactLines, renderCompactRow(row, compactInner))
		}
		if hiddenOlder > 0 {
			compactLines = append(compactLines, subtleStyle.Render(fit(fmt.Sprintf("history: %d older stage(s) hidden", hiddenOlder), compactInner)))
		}
		lines = append(lines, renderPanel(tableBorderStyle, fmt.Sprintf("stage timeline (compact %d/%d)", len(rows), len(snapshot.Stages)), compactLines, contentWidth))
		lines = append(lines, renderStatusPanel(snapshot, contentWidth))
		return clampViewHeight(strings.Join(lines, "\n\n"), m.width, m.height)
	}

	tablePanelWidth := contentWidth
	tableInner := panelInnerWidth(tableBorderStyle, tablePanelWidth)
	stageCol, statusCol, timeCol, detailsCol := timelineColumns(tableInner)

	head := fmt.Sprintf("%5s  %-*s  %-*s %-*s  %s", "rnd", stageCol, "stage", statusCol, "status", timeCol, "time", "details")
	sep := fmt.Sprintf("%s  %s  %s %s  %s", strings.Repeat("-", 5), strings.Repeat("-", stageCol), strings.Repeat("-", statusCol), strings.Repeat("-", timeCol), strings.Repeat("-", detailsCol))

	availableRows := stageRowsLimit(lines, m.width, m.height)
	rows, hiddenOlder := compactWindow(snapshot.Stages, availableRows)
	tableLines := []string{
		tableHeaderStyle.Render(fit(head, tableInner)),
		subtleStyle.Render(fit(sep, tableInner)),
	}
	for idx, row := range rows {
		roundLabel := "-"
		if row.RoundNumber != nil {
			roundLabel = fmt.Sprintf("%d", *row.RoundNumber)
		}
		stageName := normalizeDisplayText(row.Name)
		statusRaw := normalizeDisplayText(row.Status)
		message := normalizeDisplayText(row.Message)
		status := fit(statusRaw, statusCol)
		statusStyle := statusTextStyle(statusRaw)
		stageStyle := valueStyle
		isActive := idx == len(rows)-1 && statusRaw == "running"
		if idx == len(rows)-1 && statusRaw == "running" {
			stageStyle = highlightStyle
		}
		line := subtleStyle.Render(fmt.Sprintf("%s%4s  ", statusMarker(statusRaw, isActive), roundLabel)) +
			stageStyle.Render(fmt.Sprintf("%-*s", stageCol, fit(stageName, stageCol))) + "  " +
			statusStyle.Render(fmt.Sprintf("%-*s", statusCol, status)) + " " +
			valueStyle.Render(fmt.Sprintf("%-*s", timeCol, fmtDuration(row.Elapsed))) + "  " +
			valueStyle.Render(fit(message, detailsCol))
		if idx%2 == 1 {
			line = zebraStyle.Render(line)
		}
		tableLines = append(tableLines, line)
	}
	if hiddenOlder > 0 {
		tableLines = append(tableLines, subtleStyle.Render(fit(fmt.Sprintf("history: %d older stage(s) hidden", hiddenOlder), tableInner)))
	}
	lines = append(lines, renderPanel(tableBorderStyle, fmt.Sprintf("stage timeline (%d/%d visible)", len(rows), len(snapshot.Stages)), tableLines, tablePanelWidth))

	lines = append(lines, renderStatusPanel(snapshot, contentWidth))

	return clampViewHeight(strings.Join(lines, "\n\n"), m.width, m.height)
}

func RunTUIWithWorker(state *SharedProgressState, initialWidth, initialHeight int, worker func() error) error {
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- worker()
	}()

	p := tea.NewProgram(newTUIModel(state, doneCh, initialWidth, initialHeight), tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return err
	}
	m, ok := finalModel.(tuiModel)
	if !ok {
		return nil
	}
	return m.workerErr
}

func fmtDuration(d time.Duration) string {
	seconds := int(d.Seconds())
	if seconds < 0 {
		seconds = 0
	}
	minutes := seconds / 60
	sec := seconds % 60
	hours := minutes / 60
	minutes = minutes % 60
	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, sec)
	}
	return fmt.Sprintf("%02d:%02d", minutes, sec)
}

func effectiveContentWidth(viewportWidth int) int {
	if viewportWidth <= 0 {
		return defaultContentWidth
	}
	// Avoid writing into the terminal's last column; exact-width lines can
	// auto-wrap and cause renderers that rewind by logical lines to drift/scroll.
	content := viewportWidth - viewportRightGutter
	if content < 1 {
		content = 1
	}
	return content
}

func renderedRowsForLine(line string, viewportWidth int) int {
	if viewportWidth <= 0 {
		return 1
	}
	lineWidth := lipgloss.Width(line)
	return 1 + (lineWidth / viewportWidth)
}

func renderedRowsForView(view string, viewportWidth int) int {
	if strings.TrimSpace(view) == "" {
		return 0
	}
	rows := 0
	for _, line := range strings.Split(view, "\n") {
		rows += renderedRowsForLine(line, viewportWidth)
	}
	return rows
}

func clampViewHeight(view string, viewportWidth, viewportHeight int) string {
	if viewportWidth > 0 && viewportWidth <= 1 {
		return ""
	}
	if viewportHeight <= 0 || strings.TrimSpace(view) == "" {
		return view
	}
	lines := strings.Split(view, "\n")
	out := make([]string, 0, len(lines))
	usedRows := 0
	for _, line := range lines {
		lineRows := renderedRowsForLine(line, viewportWidth)
		if usedRows+lineRows > viewportHeight {
			break
		}
		out = append(out, line)
		usedRows += lineRows
	}
	if len(out) == len(lines) {
		return view
	}
	if len(out) == 0 {
		return subtleStyle.Render(fit("deepreview", effectiveContentWidth(viewportWidth)))
	}
	marker := subtleStyle.Render(fit("... output clipped to terminal height ...", effectiveContentWidth(viewportWidth)))
	if usedRows >= viewportHeight {
		out[len(out)-1] = marker
	} else {
		out = append(out, marker)
	}
	return strings.Join(out, "\n")
}

func fit(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(text) <= width {
		return text
	}
	if width <= 3 {
		return runewidth.Truncate(text, width, "")
	}
	return runewidth.Truncate(text, width, "...")
}

func clamp(v, minV, maxV int) int {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func orFallback(s, fallback string) string {
	s = normalizeDisplayText(s)
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func normalizeDisplayText(s string) string {
	if s == "" {
		return ""
	}
	s = ansi.Strip(s)
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		default:
			return r
		}
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func renderProgressBar(done, total, width int) string {
	if width < 3 {
		width = 3
	}
	if total <= 0 {
		return "[" + strings.Repeat("-", width) + "]"
	}
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}
	filled := int(float64(done) / float64(total) * float64(width))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("=", filled) + strings.Repeat("-", width-filled) + "]"
}

func latestStageLine(snapshot ProgressSnapshot) string {
	if len(snapshot.Stages) == 0 {
		return "none"
	}
	latest := snapshot.Stages[len(snapshot.Stages)-1]
	round := "-"
	if latest.RoundNumber != nil {
		round = fmt.Sprintf("%d", *latest.RoundNumber)
	}
	return fmt.Sprintf("round %s / %s / %s", round, normalizeDisplayText(latest.Name), normalizeDisplayText(latest.Status))
}

func renderCompactRow(row StageSnapshot, width int) string {
	name := normalizeDisplayText(row.Name)
	status := normalizeDisplayText(row.Status)
	message := normalizeDisplayText(row.Message)
	base := fmt.Sprintf("%s r%s %s %s %s", statusMarker(status, status == "running"), stageRound(row.RoundNumber), status, fmtDuration(row.Elapsed), name)
	if strings.TrimSpace(message) != "" {
		base += " | " + message
	}
	base = fit(base, width)
	return statusTextStyle(status).Render(base)
}

func compactWindow(rows []StageSnapshot, limit int) ([]StageSnapshot, int) {
	if limit <= 0 || len(rows) == 0 {
		return nil, len(rows)
	}
	start := len(rows) - limit
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > len(rows) {
		end = len(rows)
	}
	window := rows[start:end]
	return window, start
}

func renderFooter(snapshot ProgressSnapshot) (string, lipgloss.Style) {
	footer := "status: running"
	footerStyle := runningStyle
	failedCount := 0
	for _, stage := range snapshot.Stages {
		if stage.Status == "failed" {
			failedCount++
		}
	}
	if failedCount > 0 && snapshot.Success == nil {
		footer = fmt.Sprintf("status: running with %d failed stage(s)", failedCount)
		footerStyle = errorStyle
	}
	if snapshot.Success != nil {
		if *snapshot.Success {
			footer = "status: success"
			footerStyle = successStyle
		} else {
			footer = "status: failed"
			footerStyle = errorStyle
		}
		if snapshot.FinalMessage != "" {
			footer += " | " + normalizeDisplayText(snapshot.FinalMessage)
		}
	}
	return footer, footerStyle
}

func stageRowsLimit(lines []string, viewportWidth, viewportHeight int) int {
	frameRows := renderedRowsForView(strings.Join(lines, "\n\n"), viewportWidth)
	rows := viewportHeight - (frameRows + 8)
	if rows < 1 {
		return 1
	}
	return rows
}

func statusTextStyle(status string) lipgloss.Style {
	switch status {
	case "running":
		return runningStyle
	case "done":
		return successStyle
	case "failed":
		return errorStyle
	default:
		return valueStyle
	}
}

func renderStatusPanel(snapshot ProgressSnapshot, contentWidth int) string {
	panelWidth := contentWidth
	footer, footerStyle := renderFooter(snapshot)
	footerInner := panelInnerWidth(footerBorderStyle, panelWidth)
	footerLines := []string{
		subtleStyle.Render(fit(passiveDisplayLine, footerInner)),
		subtleStyle.Render(fit(stageLegendLine, footerInner)),
		footerStyle.Render(fit(footer, footerInner)),
	}
	return renderPanel(footerBorderStyle, "status", footerLines, panelWidth)
}
