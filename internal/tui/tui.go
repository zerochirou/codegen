package tui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/zerochirou/codegen/internal/chroma"
)

const (
	contextBudgetTokens = 1_000_000
	gridCols            = 37
	gridRows            = 10
	gridCells           = gridCols * gridRows
)

var categoryColors = []lipgloss.Color{
	lipgloss.Color("#eeeeee"),
	lipgloss.Color("#ffe08a"),
	lipgloss.Color("#d0d0d0"),
	lipgloss.Color("#a3a3a3"),
	lipgloss.Color("#8a8a8a"),
	lipgloss.Color("#525252"),
}

// fx.sh monochrome + warm-amber terminal palette
const (
	fxBgBase       = lipgloss.Color("#000000")
	fxBgCard       = lipgloss.Color("#0a0a0a")
	fxBgElevated   = lipgloss.Color("#171717")
	fxBorderRail   = lipgloss.Color("#262626")
	fxBorderFocus  = lipgloss.Color("#d0d0d0")
	fxFgBright     = lipgloss.Color("#eeeeee")
	fxFgAuto       = lipgloss.Color("#d0d0d0")
	fxFgDim        = lipgloss.Color("#8a8a8a")
	fxAccentInvBg  = lipgloss.Color("#ededed")
	fxAccentInvFg  = lipgloss.Color("#000000")
	fxAmber        = lipgloss.Color("#ffe08a")
	fxDestructive  = lipgloss.Color("#f36356")
)

type recordView struct {
	Record chroma.Record
	Match  string
}

type loadedMsg struct {
	records []recordView
	err     error
}

type deletedMsg struct {
	id  string
	err error
}

// Options configures the MemoryTUI.
type Options struct {
	PersistDir     string
	CollectionName string
	IsGlobal       bool
}

// Model is the Bubble Tea model for inspecting ChromaDB memories.
type Model struct {
	opts       Options
	store      *chroma.Store
	records    []recordView
	cursor     int
	activeTab  int // 0 = Inspector, 1 = Session Context
	searching  bool
	search     textinput.Model
	viewport   viewport.Model
	statusText string
	errText    string
	width      int
	height     int
}

// NewModel constructs a MemoryTUI Model.
func NewModel(store *chroma.Store, opts Options) Model {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(fxAmber).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(fxFgBright)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(fxFgDim)
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(fxFgBright)
	ti.Placeholder = "Search context semantically by meaning, keyword, or #tag..."
	ti.CharLimit = 256
	ti.Width = 50

	vp := viewport.New(40, 12)
	return Model{
		opts:     opts,
		store:    store,
		search:   ti,
		viewport: vp,
		width:    100,
		height:   30,
	}
}

// Run opens the ChromaDB store and starts the Bubble Tea TUI.
func Run(opts Options) error {
	persistDir := opts.PersistDir
	if strings.TrimSpace(persistDir) == "" {
		persistDir = chroma.ResolvePersistDir(opts.IsGlobal)
	}
	opts.PersistDir = persistDir

	store, err := chroma.Open(persistDir)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	m := NewModel(store, opts)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return m.loadMemoriesCmd("")
}

func (m Model) loadMemoriesCmd(query string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		cleaned := strings.TrimSpace(query)
		if cleaned == "" {
			recs, err := m.store.List(ctx, m.opts.CollectionName)
			if err != nil {
				return loadedMsg{err: err}
			}
			views := make([]recordView, 0, len(recs))
			for _, r := range recs {
				views = append(views, recordView{Record: r, Match: "—"})
			}
			return loadedMsg{records: views}
		}

		recs, err := m.store.Query(ctx, m.opts.CollectionName, cleaned, 30)
		if err != nil {
			return loadedMsg{err: err}
		}
		views := make([]recordView, 0, len(recs))
		for _, r := range recs {
			matchStr := "—"
			if r.Distance != nil {
				score := int((1.0-*r.Distance)*100 + 0.5)
				if score < 0 {
					score = 0
				} else if score > 100 {
					score = 100
				}
				matchStr = fmt.Sprintf("%d%%", score)
			}
			views = append(views, recordView{Record: r, Match: matchStr})
		}
		return loadedMsg{records: views}
	}
}

func (m Model) deleteMemoryCmd(id string) tea.Cmd {
	return func() tea.Msg {
		err := m.store.Delete(context.Background(), m.opts.CollectionName, id)
		return deletedMsg{id: id, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncLayout()
		return m, nil

	case loadedMsg:
		if msg.err != nil {
			m.errText = msg.err.Error()
			return m, nil
		}
		m.errText = ""
		m.records = msg.records
		if m.cursor >= len(m.records) {
			m.cursor = max(0, len(m.records)-1)
		}
		m.syncDetailViewport()
		return m, nil

	case deletedMsg:
		if msg.err != nil {
			m.errText = msg.err.Error()
			return m, nil
		}
		m.statusText = fmt.Sprintf("Deleted %s", truncate(msg.id, 12))
		return m, m.loadMemoriesCmd(m.search.Value())

	case tea.KeyMsg:
		if m.searching {
			switch msg.String() {
			case "esc":
				m.searching = false
				m.search.Blur()
				return m, nil
			case "enter":
				m.searching = false
				m.search.Blur()
				return m, m.loadMemoriesCmd(m.search.Value())
			}
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			if strings.TrimSpace(m.search.Value()) == "" && msg.String() == "backspace" {
				return m, tea.Batch(cmd, m.loadMemoriesCmd(""))
			}
			return m, cmd
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "/":
			m.searching = true
			m.search.Focus()
			return m, textinput.Blink
		case "tab":
			m.activeTab = (m.activeTab + 1) % 2
			return m, nil
		case "1":
			m.activeTab = 0
			return m, nil
		case "2":
			m.activeTab = 1
			return m, nil
		case "r":
			m.statusText = "Memories reloaded."
			return m, m.loadMemoriesCmd(m.search.Value())
		case "up", "k":
			if m.activeTab == 0 && m.cursor > 0 {
				m.cursor--
				m.syncDetailViewport()
			} else if m.activeTab == 1 {
				m.viewport.ScrollUp(1)
			}
			return m, nil
		case "down", "j":
			if m.activeTab == 0 && m.cursor < len(m.records)-1 {
				m.cursor++
				m.syncDetailViewport()
			} else if m.activeTab == 1 {
				m.viewport.ScrollDown(1)
			}
			return m, nil
		case "pgup", "ctrl+u":
			m.viewport.HalfPageUp()
			return m, nil
		case "pgdown", "ctrl+d":
			m.viewport.HalfPageDown()
			return m, nil
		case "c":
			if rec, ok := m.selectedRecord(); ok {
				if err := copyToClipboard(rec.Record.Content); err == nil {
					m.statusText = "Copied memory content to clipboard."
				} else {
					m.statusText = "Clipboard unavailable: " + err.Error()
				}
			}
			return m, nil
		case "d":
			if rec, ok := m.selectedRecord(); ok {
				return m, m.deleteMemoryCmd(rec.Record.ID)
			}
			return m, nil
		}
	}
	return m, nil
}

func (m *Model) syncLayout() {
	searchW := max(20, m.width-34)
	m.search.Width = searchW

	bodyH := max(10, m.height-12)
	rightW := max(24, m.width-m.leftPaneWidth()-8)
	m.viewport.Width = rightW
	m.viewport.Height = max(6, bodyH-5)
	m.syncDetailViewport()
}

func (m Model) leftPaneWidth() int {
	w := (m.width * 46) / 100
	if w < 38 {
		w = 38
	}
	if w > m.width-30 {
		w = max(24, m.width/2)
	}
	return w
}

func (m *Model) syncDetailViewport() {
	rec, ok := m.selectedRecord()
	if !ok {
		m.viewport.SetContent("No memories stored in this ChromaDB directory.")
		return
	}
	m.viewport.SetContent(wrapText(rec.Record.Content, max(20, m.viewport.Width-2)))
	m.viewport.GotoTop()
}

func (m Model) selectedRecord() (recordView, bool) {
	if len(m.records) == 0 || m.cursor < 0 || m.cursor >= len(m.records) {
		return recordView{}, false
	}
	return m.records[m.cursor], true
}

func (m Model) View() string {
	scopeBadge := lipgloss.NewStyle().
		Bold(true).
		Padding(0, 1).
		Background(fxAccentInvBg).
		Foreground(fxAccentInvFg).
		Render("PROJECT")
	if m.opts.IsGlobal {
		scopeBadge = lipgloss.NewStyle().
			Bold(true).
			Padding(0, 1).
			Background(fxAmber).
			Foreground(fxAccentInvFg).
			Render("GLOBAL")
	}

	titleText := lipgloss.NewStyle().Foreground(fxFgBright).Bold(true).Render("▸ codegen")
	pathText := lipgloss.NewStyle().Foreground(fxFgDim).Render(m.opts.PersistDir)
	header := lipgloss.NewStyle().
		Background(fxBgCard).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(fxBorderRail).
		Padding(0, 1).
		Width(m.width).
		Render(fmt.Sprintf("%s  %s  %s", titleText, scopeBadge, pathText))

	countLabel := fmt.Sprintf("%d memories", len(m.records))
	if len(m.records) == 1 {
		countLabel = "1 memory"
	}
	if strings.TrimSpace(m.search.Value()) != "" {
		countLabel = fmt.Sprintf("%d matches", len(m.records))
	}

	searchBorder := fxBorderRail
	if m.searching {
		searchBorder = fxBorderFocus
	}
	searchBox := lipgloss.NewStyle().
		Background(fxBgCard).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(searchBorder).
		Padding(0, 1).
		Width(max(24, m.width-24)).
		Render(m.search.View())

	badgeBox := lipgloss.NewStyle().
		Background(fxBgCard).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(fxBorderRail).
		Foreground(fxFgAuto).
		Padding(0, 2).
		Render(countLabel)

	topBar := lipgloss.JoinHorizontal(lipgloss.Center, searchBox, " ", badgeBox)

	tab0Style := lipgloss.NewStyle().Padding(0, 2).Foreground(fxFgDim)
	tab1Style := lipgloss.NewStyle().Padding(0, 2).Foreground(fxFgDim)
	if m.activeTab == 0 {
		tab0Style = tab0Style.Foreground(fxFgBright).Bold(true).Underline(true)
	} else {
		tab1Style = tab1Style.Foreground(fxFgBright).Bold(true).Underline(true)
	}
	tabs := lipgloss.JoinHorizontal(lipgloss.Top, tab0Style.Render("1 Inspector"), tab1Style.Render("2 Session Context"))

	var body string
	if m.activeTab == 0 {
		body = m.renderInspector()
	} else {
		body = m.renderContextMap()
	}

	statusLine := ""
	if m.errText != "" {
		statusLine = lipgloss.NewStyle().Foreground(fxDestructive).Padding(0, 1).Render("Error: " + m.errText)
	} else if m.statusText != "" {
		statusLine = lipgloss.NewStyle().Foreground(fxAmber).Padding(0, 1).Render("▸ " + m.statusText)
	}

	footer := lipgloss.NewStyle().
		Background(fxBgCard).
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(fxBorderRail).
		Foreground(fxFgDim).
		Padding(0, 1).
		Width(m.width).
		Render("q quit  ·  / search  ·  ↑/↓ navigate  ·  Tab switch view  ·  r reload  ·  c copy  ·  d delete")

	parts := []string{header, topBar, tabs, body}
	if statusLine != "" {
		parts = append(parts, statusLine)
	}
	parts = append(parts, footer)
	return lipgloss.NewStyle().
		Background(fxBgBase).
		Foreground(fxFgBright).
		Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func (m Model) renderInspector() string {
	bodyH := max(10, m.height-12)
	leftW := m.leftPaneWidth()
	rightW := max(24, m.width-leftW-5)

	var rows []string
	hdr := lipgloss.NewStyle().Bold(true).Foreground(fxFgDim).Render(
		fmt.Sprintf("  %-10s %-5s %-13s %s", "ID", "Match", "Tags", "Preview"),
	)
	rows = append(rows, hdr)

	maxRows := max(3, bodyH-4)
	start := 0
	if m.cursor >= maxRows {
		start = m.cursor - maxRows + 1
	}
	end := min(len(m.records), start+maxRows)

	for i := start; i < end; i++ {
		rec := m.records[i]
		shortID := truncate(rec.Record.ID, 9)
		tags := truncate(rec.Record.Metadata["tags"], 12)
		previewW := max(10, leftW-36)
		preview := truncate(strings.ReplaceAll(rec.Record.Content, "\n", " "), previewW)
		if i == m.cursor {
			marker := lipgloss.NewStyle().Foreground(fxAmber).Bold(true).Render("▸")
			line := fmt.Sprintf("%s %-10s %-5s %-13s %s", marker, shortID, rec.Match, tags, preview)
			line = lipgloss.NewStyle().
				Background(fxBgElevated).
				Foreground(fxFgBright).
				Bold(true).
				Width(leftW - 4).
				Render(line)
			rows = append(rows, line)
		} else {
			line := fmt.Sprintf("  %-10s %-5s %-13s %s", shortID, rec.Match, tags, preview)
			line = lipgloss.NewStyle().Foreground(fxFgAuto).Render(line)
			rows = append(rows, line)
		}
	}
	if len(m.records) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(fxFgDim).Render("  (empty — no memories found)"))
	}

	leftPane := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(fxBorderRail).
		Background(fxBgCard).
		Padding(0, 1).
		Width(leftW).
		Height(bodyH).
		Render(strings.Join(rows, "\n"))

	var detailMeta string
	if rec, ok := m.selectedRecord(); ok {
		tags := rec.Record.Metadata["tags"]
		if tags == "" {
			tags = "none"
		}
		nodeType := rec.Record.Metadata["node_type"]
		metaLine := fmt.Sprintf("ID: %s", rec.Record.ID)
		if rec.Match != "—" {
			metaLine += fmt.Sprintf("   Match: %s", rec.Match)
		}
		if nodeType != "" {
			metaLine += fmt.Sprintf("   Type: %s", nodeType)
		}
		detailMeta = lipgloss.NewStyle().Foreground(fxFgBright).Bold(true).Render("Context Inspector") + "\n" +
			lipgloss.NewStyle().Foreground(fxFgDim).Render(metaLine+"\nTags: ") +
			lipgloss.NewStyle().Foreground(fxAmber).Render(tags)
	} else {
		detailMeta = lipgloss.NewStyle().Foreground(fxFgBright).Bold(true).Render("Context Inspector") + "\n" +
			lipgloss.NewStyle().Foreground(fxFgDim).Render("Select a memory to inspect.")
	}

	sep := lipgloss.NewStyle().Foreground(fxBorderRail).Render(strings.Repeat("─", max(10, rightW-4)))
	rightPane := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(fxBorderRail).
		Background(fxBgCard).
		Foreground(fxFgBright).
		Padding(0, 1).
		Width(rightW).
		Height(bodyH).
		Render(detailMeta + "\n" + sep + "\n" + m.viewport.View())

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, " ", rightPane)
}

func (m Model) renderContextMap() string {
	catTokens := map[string]int{}
	totalTokens := 0
	for _, rec := range m.records {
		tok := estimateTokens(rec.Record.Content)
		totalTokens += tok
		tags := strings.TrimSpace(rec.Record.Metadata["tags"])
		cat := "untagged"
		if tags != "" {
			cat = strings.TrimSpace(strings.Split(tags, ",")[0])
		}
		catTokens[cat] += tok
	}

	type catEntry struct {
		name   string
		tokens int
	}
	var sorted []catEntry
	for k, v := range catTokens {
		sorted = append(sorted, catEntry{name: k, tokens: v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].tokens == sorted[j].tokens {
			return sorted[i].name < sorted[j].name
		}
		return sorted[i].tokens > sorted[j].tokens
	})

	cells := make([]string, 0, gridCells)
	for i, c := range sorted {
		col := categoryColors[i%len(categoryColors)]
		cnt := max(1, int(float64(c.tokens)/float64(contextBudgetTokens)*gridCells+0.5))
		styled := lipgloss.NewStyle().Foreground(col).Render("◉")
		for k := 0; k < cnt && len(cells) < gridCells; k++ {
			cells = append(cells, styled)
		}
		if len(cells) >= gridCells {
			break
		}
	}
	emptyCell := lipgloss.NewStyle().Foreground(fxBorderRail).Render("□")
	for len(cells) < gridCells {
		cells = append(cells, emptyCell)
	}

	gridLines := make([]string, 0, gridRows)
	for r := 0; r < gridRows; r++ {
		gridLines = append(gridLines, strings.Join(cells[r*gridCols:(r+1)*gridCols], " "))
	}

	usedPct := float64(totalTokens) / float64(contextBudgetTokens) * 100
	freeTokens := max(0, contextBudgetTokens-totalTokens)
	freePct := max(0.0, 100.0-usedPct)

	legend := []string{
		lipgloss.NewStyle().Bold(true).Foreground(fxFgBright).Render(
			fmt.Sprintf("ChromaDB Context · %s/%s tokens (%.1f%%)", formatTokens(totalTokens), formatTokens(contextBudgetTokens), usedPct),
		),
		lipgloss.NewStyle().Foreground(fxFgDim).Render("Token usage by category"),
	}
	for i, c := range sorted {
		col := categoryColors[i%len(categoryColors)]
		pct := float64(c.tokens) / float64(contextBudgetTokens) * 100
		dot := lipgloss.NewStyle().Foreground(col).Render("◉")
		legend = append(legend, fmt.Sprintf("%s %s: %s tokens (%.1f%%)", dot, c.name, formatTokens(c.tokens), pct))
	}
	legend = append(legend, fmt.Sprintf("%s Free space: %s (%.1f%%)", emptyCell, formatTokens(freeTokens), freePct))

	bodyH := max(10, m.height-12)
	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		strings.Join(gridLines, "\n"),
		"   ",
		strings.Join(legend, "\n"),
	)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(fxBorderRail).
		Background(fxBgCard).
		Foreground(fxFgAuto).
		Padding(1, 2).
		Width(max(40, m.width-2)).
		Height(bodyH).
		Render(content)
}

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return max(1, len(text)/4)
}

func formatTokens(tokens int) string {
	if tokens >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	}
	if tokens >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(tokens)/1_000)
	}
	return fmt.Sprintf("%d", tokens)
}

func truncate(s string, maxLen int) string {
	if maxLen <= 1 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := words[0]
		for _, w := range words[1:] {
			if len([]rune(line))+1+len([]rune(w)) > width {
				out = append(out, line)
				line = w
			} else {
				line += " " + w
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		} else {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		}
	default:
		return fmt.Errorf("unsupported OS %s", runtime.GOOS)
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
