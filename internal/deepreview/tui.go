package deepreview

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type workerResultMsg struct {
	err error
}

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func waitWorkerCmd(doneCh <-chan error) tea.Cmd {
	return func() tea.Msg {
		err := <-doneCh
		return workerResultMsg{err: err}
	}
}

type tuiModel struct {
	state           *SharedProgressState
	doneCh          <-chan error
	requestCancel   func()
	cancelRequested bool
	workerErr       error
	done            bool
	width           int
	height          int
	tick            int
}

func newTUIModel(state *SharedProgressState, doneCh <-chan error, requestCancel func(), width, height int) tuiModel {
	return tuiModel{
		state:         state,
		doneCh:        doneCh,
		requestCancel: requestCancel,
		width:         width,
		height:        height,
	}
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
	case tea.KeyMsg:
		if m.done {
			return m, tea.Quit
		}
		if msg.String() == "ctrl+c" {
			if !m.cancelRequested {
				m.cancelRequested = true
				return m, requestCancelCmd(m.requestCancel)
			}
			return m, nil
		}
	case workerResultMsg:
		m.done = true
		m.workerErr = msg.err
		return m, tea.Quit
	case tickMsg:
		if m.done {
			return m, nil
		}
		m.tick++
		return m, tickCmd()
	}
	return m, nil
}

var (
	spinnerFrames = []string{"|", "/", "-", "\\"}

	headerStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("24")).Padding(0, 1)
	accentStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("45"))
	subtleStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	valueStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	runningStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	successStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	errorStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	borderStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 1)
	tableBorderStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("33")).Padding(0, 1)
	highlightStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51"))
	tableHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("117"))
	zebraStyle       = lipgloss.NewStyle().Background(lipgloss.Color("235"))
	chipBaseStyle    = lipgloss.NewStyle().Bold(true).Padding(0, 1)
)

const (
	ansiReset            = "\x1b[0m"
	viewportRightGutter  = 1
	viewportBottomGutter = 0
	timelineSafetyGutter = 0
	defaultContentWidth  = 72
	sectionSeparator     = "\n"
	tuiWorkerExitTimeout = 30 * time.Second
)

func chip(style lipgloss.Style, text string) string {
	return style.Render(text)
}

func alignedPanelWidth(contentWidth int) int {
	panelWidth := contentWidth - timelineSafetyGutter
	if panelWidth < 28 {
		return contentWidth
	}
	return panelWidth
}

func joinSections(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, sectionSeparator)
}

func joinHeaderWithRightHint(left, right string, width int) string {
	if width <= 0 {
		return left
	}
	left = fit(left, width)
	right = fit(right, width)
	if right == "" {
		return left
	}
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	if rightWidth >= width {
		return right
	}
	if leftWidth+1+rightWidth > width {
		left = fit(left, width-rightWidth-1)
		leftWidth = lipgloss.Width(left)
	}
	spaces := width - leftWidth - rightWidth
	if spaces < 1 {
		spaces = 1
	}
	return left + strings.Repeat(" ", spaces) + right
}

func joinChipsWithinWidth(chips []string, width int) string {
	if width <= 0 || len(chips) == 0 {
		return ""
	}
	line := ""
	visibleCount := 0
	for _, c := range chips {
		candidate := c
		if line != "" {
			candidate = line + " " + c
		}
		if lipgloss.Width(candidate) > width {
			break
		}
		line = candidate
		visibleCount++
	}
	hiddenCount := len(chips) - visibleCount
	if hiddenCount <= 0 {
		if line == "" {
			return truncateANSIWithMarker(chips[0], width)
		}
		return line
	}

	overflowHint := fmt.Sprintf(" [+%d more]", hiddenCount)
	if line == "" {
		return truncateANSIWithMarker(overflowHint, width)
	}
	candidate := line + overflowHint
	if lipgloss.Width(candidate) <= width {
		return candidate
	}

	availableForLine := width - lipgloss.Width(overflowHint)
	if availableForLine <= 0 {
		return truncateANSIWithMarker(overflowHint, width)
	}
	trimmed := strings.TrimRight(truncateANSIWithMarker(line, availableForLine), " ")
	if trimmed == "" {
		return truncateANSIWithMarker(overflowHint, width)
	}
	return truncateANSIWithMarker(trimmed+overflowHint, width)
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

func copyRoundPtr(roundNumber *int) *int {
	if roundNumber == nil {
		return nil
	}
	v := *roundNumber
	return &v
}

func isPlannableStage(name string) bool {
	switch name {
	case "prepare", "independent review stage", "execute stage", "delivery":
		return true
	default:
		return false
	}
}

func plannedTimelineRows(snapshot ProgressSnapshot) ([]StageSnapshot, bool) {
	if snapshot.MaxRounds < 1 || snapshot.MaxRounds > maxReviewRounds {
		return nil, false
	}

	latestByKey := make(map[string]StageSnapshot)
	for _, row := range snapshot.Stages {
		if !isPlannableStage(row.Name) {
			continue
		}
		key := row.Name + "|" + stageRound(row.RoundNumber)
		latestByKey[key] = row
	}

	pendingRow := func(name string, round *int) StageSnapshot {
		message := "waiting"
		if round != nil {
			message = "waiting for round start"
		}
		return StageSnapshot{
			RoundNumber: copyRoundPtr(round),
			Name:        name,
			Status:      "pending",
			Message:     message,
			Elapsed:     0,
		}
	}

	rows := make([]StageSnapshot, 0, snapshot.MaxRounds*2+2)
	if row, ok := latestByKey["prepare|-"]; ok {
		rows = append(rows, row)
	} else {
		rows = append(rows, pendingRow("prepare", nil))
	}
	for round := 1; round <= snapshot.MaxRounds; round++ {
		r := round
		independentKey := "independent review stage|" + stageRound(&r)
		if row, ok := latestByKey[independentKey]; ok {
			rows = append(rows, row)
		} else {
			rows = append(rows, pendingRow("independent review stage", &r))
		}
		executeKey := "execute stage|" + stageRound(&r)
		if row, ok := latestByKey[executeKey]; ok {
			rows = append(rows, row)
		} else {
			rows = append(rows, pendingRow("execute stage", &r))
		}
	}
	if row, ok := latestByKey["delivery|-"]; ok {
		rows = append(rows, row)
	} else {
		rows = append(rows, pendingRow("delivery", nil))
	}
	return rows, true
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
	contentWidth := effectiveContentWidth(m.width)
	panelWidth := alignedPanelWidth(contentWidth)
	if m.width > 0 && contentWidth < 28 {
		label := runProgressLabel(snapshot)
		roundLabel := "round " + currentRoundLabel(snapshot)
		if m.cancelRequested && !m.done && ansi.StringWidth("canceling") <= contentWidth {
			label = "canceling"
		} else if ansi.StringWidth(label) > contentWidth {
			label = roundLabel
		}
		compactTitle := fit(label, contentWidth)
		latest := fit(latestStageLine(snapshot), contentWidth)
		return finalizeView(strings.Join([]string{compactTitle, latest}, "\n"), m.width, m.height)
	}

	lines := make([]string, 0, 10)
	headerInnerWidth := panelInnerWidth(headerStyle, panelWidth)
	rightHintText := "ctrl+c to cancel"
	if m.cancelRequested && !m.done {
		rightHintText = "canceling..."
	}
	if m.done {
		rightHintText = "finishing..."
	}
	rightHint := subtleStyle.Render(rightHintText)
	hintWidth := lipgloss.Width(rightHint)
	budget := headerInnerWidth - hintWidth - 1
	topPlain := fitHeaderProgress(snapshot, elapsed, spinner, m.cancelRequested && !m.done, budget)
	headerLine := joinHeaderWithRightHint(topPlain, rightHint, headerInnerWidth)
	styledHeader := headerStyle.Render(headerLine)
	lines = append(lines, styledHeader)

	runChipStyle := chipBaseStyle.Foreground(lipgloss.Color("231")).Background(lipgloss.Color("24"))
	runState := "RUNNING"
	if m.cancelRequested && snapshot.Success == nil {
		runState = "CANCELING"
		runChipStyle = chipBaseStyle.Foreground(lipgloss.Color("231")).Background(lipgloss.Color("172"))
	} else if snapshot.Success != nil && *snapshot.Success {
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
	}, panelWidth)
	lines = append(lines, badgeLine+ansiReset)

	activity := latestActivity(snapshot)
	maxRoundsLabel := "-"
	if snapshot.MaxRounds > 0 {
		maxRoundsLabel = fmt.Sprintf("%d", snapshot.MaxRounds)
	}
	metaLines := []string{
		"run: " + orFallback(snapshot.RunID, "starting..."),
		"repo: " + orFallback(snapshot.Repo, "-"),
		"branch: " + orFallback(snapshot.SourceBranch, "-") + "    mode: " + orFallback(snapshot.Mode, "-"),
		"max rounds: " + maxRoundsLabel,
		"artifacts: " + orFallback(snapshot.RunRoot, "-"),
		"latest: " + latestStageLine(snapshot),
		"active: r" + activity.round + " / " + activity.name + " / " + activity.status + " / " + activity.elapsed,
	}
	if strings.TrimSpace(activity.message) != "" {
		metaLines = append(metaLines, "details: "+activity.message)
	}
	contextBox := renderPanel(borderStyle, "run context", metaLines, panelWidth)
	lines = append(lines, valueStyle.Render(contextBox))

	if m.width < 60 || m.height < 12 {
		// The context panel is taller than a short terminal. Keep the fitted
		// header, which leads with the current round, and the active stage.
		compactView := []string{styledHeader}
		timelineRows := snapshot.Stages
		timelineTitle := "stage timeline"
		if plannedRows, ok := plannedTimelineRows(snapshot); ok {
			timelineRows = plannedRows
			timelineTitle = "planned stage timeline"
		}
		compactRows := stageRowsLimit(compactView, m.width, m.height)
		rows, hiddenOlder := focusTimelineWindow(timelineRows, compactRows)
		compactLines := make([]string, 0, len(rows)+2)
		compactInner := panelInnerWidth(tableBorderStyle, panelWidth)
		if len(rows) == 0 {
			compactLines = append(compactLines, subtleStyle.Render(fit("- waiting for first stage...", compactInner)))
		}
		for _, row := range rows {
			compactLines = append(compactLines, renderCompactRow(row, compactInner))
		}
		if hiddenOlder > 0 {
			compactLines = append(compactLines, subtleStyle.Render(fit(fmt.Sprintf("history: %d older stage(s) hidden", hiddenOlder), compactInner)))
		}
		compactView = append(compactView, renderPanel(tableBorderStyle, fmt.Sprintf("%s (compact %d/%d)", timelineTitle, len(rows), len(timelineRows)), compactLines, panelWidth))
		return finalizeView(joinSections(compactView), m.width, m.height)
	}

	tableInner := panelInnerWidth(tableBorderStyle, panelWidth)
	stageCol, statusCol, timeCol, detailsCol := timelineColumns(tableInner)

	head := fmt.Sprintf("%5s  %-*s  %-*s %-*s  %s", "rnd", stageCol, "stage", statusCol, "status", timeCol, "time", "details")
	sep := fmt.Sprintf("%s  %s  %s %s  %s", strings.Repeat("-", 5), strings.Repeat("-", stageCol), strings.Repeat("-", statusCol), strings.Repeat("-", timeCol), strings.Repeat("-", detailsCol))

	availableRows := stageRowsLimit(lines, m.width, m.height)
	timelineRows := snapshot.Stages
	timelineTitle := "stage timeline"
	if plannedRows, ok := plannedTimelineRows(snapshot); ok {
		timelineRows = plannedRows
		timelineTitle = "planned stage timeline"
	}
	rows, hiddenOlder := focusTimelineWindow(timelineRows, availableRows)
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
		isActive := statusRaw == "running"
		if isActive {
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
	lines = append(lines, renderPanel(tableBorderStyle, fmt.Sprintf("%s (%d/%d visible)", timelineTitle, len(rows), len(timelineRows)), tableLines, panelWidth))

	return finalizeView(joinSections(lines), m.width, m.height)
}

func requestCancelCmd(requestCancel func()) tea.Cmd {
	return func() tea.Msg {
		if requestCancel != nil {
			requestCancel()
		}
		return tuiCancelRequestedMsg{}
	}
}

type tuiCancelRequestedMsg struct{}

var runTUIProgram = func(model tea.Model, opts ...tea.ProgramOption) (tea.Model, error) {
	return tea.NewProgram(model, opts...).Run()
}

func RunTUIWithWorker(state *SharedProgressState, initialWidth, initialHeight int, requestCancel func(), worker func() error) error {
	doneCh := make(chan error, 1)
	workerDone := make(chan struct{})
	var workerErr error
	go func() {
		workerErr = worker()
		close(workerDone)
		doneCh <- workerErr
	}()

	finalModel, runErr := runTUIProgram(
		newTUIModel(state, doneCh, requestCancel, initialWidth, initialHeight),
		tea.WithAltScreen(),
	)
	if !workerHasFinished(workerDone) {
		if requestCancel != nil {
			requestCancel()
		}
		select {
		case <-workerDone:
		case <-time.After(tuiWorkerExitTimeout):
			return errors.Join(runErr, NewDeepReviewError("timed out waiting for TUI worker to exit"))
		}
	}
	<-workerDone
	if runErr != nil {
		return errors.Join(runErr, workerErr)
	}
	if m, ok := finalModel.(tuiModel); ok && m.workerErr != nil {
		return m.workerErr
	}
	return workerErr
}

func workerHasFinished(workerDone <-chan struct{}) bool {
	select {
	case <-workerDone:
		return true
	default:
		return false
	}
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

func effectiveFrameHeight(viewportHeight int) int {
	if viewportHeight <= 0 {
		return 0
	}
	if viewportHeight <= viewportBottomGutter+1 {
		return viewportHeight
	}
	content := viewportHeight - viewportBottomGutter
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

func clampViewWidth(view string, viewportWidth int) string {
	if strings.TrimSpace(view) == "" || viewportWidth <= 0 {
		return view
	}
	safeWidth := effectiveContentWidth(viewportWidth)
	lines := strings.Split(view, "\n")
	changed := false
	for i, line := range lines {
		if ansi.StringWidth(line) > safeWidth {
			lines[i] = truncateANSIWithMarker(line, safeWidth)
			changed = true
		}
	}
	if !changed {
		return view
	}
	return strings.Join(lines, "\n")
}

func finalizeView(view string, viewportWidth, viewportHeight int) string {
	return stabilizeFrame(clampViewHeight(clampViewWidth(view, viewportWidth), viewportWidth, viewportHeight), viewportWidth, viewportHeight)
}

func stabilizeFrame(view string, viewportWidth, viewportHeight int) string {
	if viewportWidth > 0 && viewportWidth <= 1 {
		return ""
	}
	if viewportWidth <= 0 {
		return view
	}
	frameHeight := effectiveFrameHeight(viewportHeight)
	// Keep one column free to avoid terminal auto-wrap drift.
	safeFrameWidth := viewportWidth - 1
	if safeFrameWidth < 1 {
		safeFrameWidth = 1
	}
	lines := []string{}
	if view != "" {
		lines = strings.Split(view, "\n")
	}
	if frameHeight > 0 && len(lines) > frameHeight {
		lines = lines[:frameHeight]
	}
	capHint := len(lines)
	if frameHeight > capHint {
		capHint = frameHeight
	}
	out := make([]string, 0, capHint)
	for _, line := range lines {
		out = append(out, padDisplayWidth(truncateANSIWithMarker(line, safeFrameWidth), safeFrameWidth))
	}
	if frameHeight > 0 {
		blank := strings.Repeat(" ", safeFrameWidth)
		for len(out) < frameHeight {
			out = append(out, blank)
		}
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n")
}

func padDisplayWidth(text string, width int) string {
	if width <= 0 {
		return ""
	}
	displayWidth := ansi.StringWidth(text)
	if displayWidth >= width {
		return text
	}
	return text + strings.Repeat(" ", width-displayWidth)
}

func clampViewHeight(view string, viewportWidth, viewportHeight int) string {
	if viewportWidth > 0 && viewportWidth <= 1 {
		return ""
	}
	frameHeight := effectiveFrameHeight(viewportHeight)
	if frameHeight <= 0 || strings.TrimSpace(view) == "" {
		return view
	}
	lines := strings.Split(view, "\n")
	out := make([]string, 0, len(lines))
	usedRows := 0
	for _, line := range lines {
		lineRows := renderedRowsForLine(line, viewportWidth)
		if usedRows+lineRows > frameHeight {
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
	if usedRows >= frameHeight {
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
	if ansi.StringWidth(text) <= width {
		return text
	}
	return truncateANSIWithMarker(text, width)
}

func truncateANSIWithMarker(text string, width int) string {
	if width <= 0 {
		return ""
	}
	tail := ""
	if width >= 4 {
		tail = "..."
	}
	return ansi.Truncate(text, width, tail)
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
	base := fmt.Sprintf("%s r%s %s %s %s", statusMarker(status, status == "running"), stageRound(row.RoundNumber), name, status, fmtDuration(row.Elapsed))
	if strings.TrimSpace(message) != "" {
		base += " | " + message
	}
	base = fit(base, width)
	return statusTextStyle(status).Render(base)
}

// focusTimelineWindow keeps the active stage on screen. A tail window would
// hide that stage behind later pending rounds. Delivery is pinned only after
// it starts, or when it is the next stage.
func focusTimelineWindow(rows []StageSnapshot, limit int) ([]StageSnapshot, int) {
	if limit <= 0 || len(rows) == 0 {
		return nil, len(rows)
	}
	if len(rows) <= limit {
		return rows, 0
	}
	focus := focusTimelineIndex(rows)
	end := focus + 2
	if end > len(rows) {
		end = len(rows)
	}
	if end-focus > limit {
		end = focus + limit
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	if end-start < limit {
		end = start + limit
		if end > len(rows) {
			end = len(rows)
			start = end - limit
			if start < 0 {
				start = 0
			}
		}
	}
	if focus < start {
		start = focus
		end = start + limit
		if end > len(rows) {
			end = len(rows)
		}
	}
	if deliveryIdx, include := deliveryAnchor(rows, focus); include && (deliveryIdx < start || deliveryIdx >= end) {
		if end-start >= limit && limit >= 2 {
			if end-1 != focus && end > start {
				end--
			} else if start != focus && start+1 < end {
				start++
			}
		}
		window := make([]StageSnapshot, 0, limit)
		if start < end {
			window = append(window, rows[start:end]...)
		}
		window = append(window, rows[deliveryIdx])
		if len(window) > limit {
			window = window[len(window)-limit:]
		}
		return window, start
	}
	return rows[start:end], start
}

func focusTimelineIndex(rows []StageSnapshot) int {
	lastRunning := -1
	lastSettled := -1
	for i, row := range rows {
		switch row.Status {
		case "running":
			lastRunning = i
		case "done", "failed":
			lastSettled = i
		}
	}
	if lastRunning >= 0 {
		return lastRunning
	}
	if lastSettled >= 0 {
		return lastSettled
	}
	return 0
}

func deliveryAnchor(rows []StageSnapshot, focus int) (int, bool) {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Name != "delivery" {
			continue
		}
		if rows[i].Status != "pending" {
			return i, true
		}
		return i, focus >= i-1 && focus <= i
	}
	return -1, false
}

func fitHeaderProgress(snapshot ProgressSnapshot, elapsed time.Duration, spinner string, canceling bool, budget int) string {
	round := "round " + currentRoundLabel(snapshot)
	progress := runProgressLabel(snapshot)
	if canceling {
		round = "canceling " + round
		progress = "canceling " + progress
	}
	candidates := []string{
		fmt.Sprintf("deepreview %s  %s  elapsed %s", spinner, progress, fmtDuration(elapsed)),
		fmt.Sprintf("%s  elapsed %s", progress, fmtDuration(elapsed)),
		fmt.Sprintf("%s  elapsed %s", round, fmtDuration(elapsed)),
		round,
	}
	if budget < 1 {
		return fit(round, 1)
	}
	for _, candidate := range candidates {
		if ansi.StringWidth(candidate) <= budget {
			return candidate
		}
	}
	return fit(round, budget)
}

func runProgressLabel(snapshot ProgressSnapshot) string {
	label := "round " + currentRoundLabel(snapshot)
	if completed, required, ok := reviewerCounts(snapshot); ok {
		label += fmt.Sprintf("  reviewers %d/%d", completed, required)
	}
	return label
}

func currentRoundLabel(snapshot ProgressSnapshot) string {
	var round *int
	for i := len(snapshot.Stages) - 1; i >= 0; i-- {
		row := snapshot.Stages[i]
		if row.Status == "running" && row.RoundNumber != nil {
			round = row.RoundNumber
			break
		}
	}
	if round == nil {
		for i := len(snapshot.Stages) - 1; i >= 0; i-- {
			if snapshot.Stages[i].RoundNumber != nil {
				round = snapshot.Stages[i].RoundNumber
				break
			}
		}
	}
	if round == nil {
		if snapshot.MaxRounds > 0 {
			return fmt.Sprintf("-/%d", snapshot.MaxRounds)
		}
		return "-"
	}
	if snapshot.MaxRounds > 0 {
		return fmt.Sprintf("%d/%d", *round, snapshot.MaxRounds)
	}
	return fmt.Sprintf("%d", *round)
}

func reviewerCounts(snapshot ProgressSnapshot) (int, int, bool) {
	fallback := ""
	for i := len(snapshot.Stages) - 1; i >= 0; i-- {
		row := snapshot.Stages[i]
		if row.Name != "independent review stage" {
			continue
		}
		if row.Status == "running" {
			return firstCountFraction(row.Message)
		}
		if fallback == "" {
			fallback = row.Message
		}
	}
	if fallback == "" {
		return 0, 0, false
	}
	return firstCountFraction(fallback)
}

func firstCountFraction(text string) (int, int, bool) {
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			continue
		}
		j := i
		for j < len(text) && text[j] >= '0' && text[j] <= '9' {
			j++
		}
		if j >= len(text) || text[j] != '/' || j == i {
			i = j
			continue
		}
		k := j + 1
		if k >= len(text) || text[k] < '0' || text[k] > '9' {
			i = j
			continue
		}
		end := k
		for end < len(text) && text[end] >= '0' && text[end] <= '9' {
			end++
		}
		completed, errCompleted := strconv.Atoi(text[i:j])
		required, errRequired := strconv.Atoi(text[k:end])
		if errCompleted == nil && errRequired == nil && required > 0 && completed >= 0 && completed <= required {
			return completed, required, true
		}
		if end > i {
			i = end - 1
		}
	}
	return 0, 0, false
}

func stageRowsLimit(lines []string, viewportWidth, viewportHeight int) int {
	frameRows := renderedRowsForView(joinSections(lines), viewportWidth)
	frameHeight := effectiveFrameHeight(viewportHeight)
	rows := frameHeight - (frameRows + 6)
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
	case "pending":
		return subtleStyle
	default:
		return valueStyle
	}
}
