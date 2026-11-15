package tui

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type graphNodeKind int

const (
	nodeMemory graphNodeKind = iota
	nodeTag
)

type graphEdgeKind int

const (
	edgeLink graphEdgeKind = iota
	edgeTag
)

const (
	graphEdgeMutedColor = lipgloss.Color("#666666") // 3.12:1 against #171717 (WCAG non-text >= 3:1)
	graphTagColor       = lipgloss.Color("#3fb950")
	graphLinkHighlight  = fxAmber
	graphTagHighlight   = lipgloss.Color("#58a6ff")
)

var wikiLinkPattern = regexp.MustCompile(`\[\[([^\[\]]+)\]\]`)

type graphNode struct {
	ID          string
	Kind        graphNodeKind
	RecordIndex int
	Title       string
	NodeType    string
	Tags        []string
	Content     string
	Color       lipgloss.Color

	// World coordinates in normalized [0, 1] x [0, 1] space.
	X float64
	Y float64

	// Projected screen coordinates relative to the graph canvas (0..canvasW-1, 0..canvasH-1).
	ScreenX int
	ScreenY int
	Visible bool

	Outgoing []int
	Incoming []int
	TagLinks []int
}

type graphEdge struct {
	From int
	To   int
	Kind graphEdgeKind
}

type graphNeighborHit struct {
	NodeIdx int
	ScreenY int
}

type graphToolbarButton struct {
	Action string
	StartX int
	EndX   int
}

type graphState struct {
	ShowTags     bool
	ShowOrphans  bool
	Zoom         float64
	PanX         float64
	PanY         float64
	SelectedID   string
	SelectedIdx  int
	Nodes        []graphNode
	Edges        []graphEdge
	NodeByID     map[string]int
	ToolbarHits  []graphToolbarButton
	NeighborHits []graphNeighborHit
	LabelHits    []graphLabelHit
	CanvasW      int
	CanvasH      int
}

type graphLabelHit struct {
	NodeIdx int
	Y       int
	StartX  int
	EndX    int
}

func newGraphState() graphState {
	return graphState{
		ShowTags:    true,
		ShowOrphans: true,
		Zoom:        1.0,
		SelectedIdx: -1,
		NodeByID:    map[string]int{},
	}
}

var nodeTypeColorMap = map[string]lipgloss.Color{
	"architecture": lipgloss.Color("#58a6ff"),
	"bugfix":       fxDestructive,
	"preference":   fxAmber,
	"workflow":     lipgloss.Color("#38bdf8"),
	"entity":       lipgloss.Color("#a371f7"),
	"checkpoint":   lipgloss.Color("#f778ba"),
	"convention":   lipgloss.Color("#3fb950"),
}

func colorForMemoryNode(nodeType string, tags []string) lipgloss.Color {
	if c, ok := nodeTypeColorMap[strings.ToLower(strings.TrimSpace(nodeType))]; ok {
		return c
	}
	if len(tags) > 0 {
		h := 0
		for _, r := range tags[0] {
			h = (h*31 + int(r))
		}
		if h < 0 {
			h = -h
		}
		return categoryColors[h%len(categoryColors)]
	}
	return fxFgAuto
}

func (m *Model) rebuildGraph() {
	prevID := m.graph.SelectedID
	if prevID == "" {
		if rec, ok := m.selectedRecord(); ok {
			prevID = rec.Record.ID
		}
	}

	g := &m.graph
	if g.Zoom <= 0 {
		g.Zoom = 1.0
	}
	g.Nodes = nil
	g.Edges = nil
	g.NodeByID = map[string]int{}

	type rawMem struct {
		recIdx   int
		id       string
		title    string
		nodeType string
		tags     []string
		links    []string
		content  string
	}
	mems := make([]rawMem, 0, len(m.records))
	titleToID := map[string]string{}
	for idx, rv := range m.records {
		tags := parseCommaValues(rv.Record.Metadata["tags"])
		title := deriveNodeTitle(rv.Record.ID, rv.Record.Content)
		nt := strings.TrimSpace(rv.Record.Metadata["node_type"])
		mems = append(mems, rawMem{
			recIdx:   idx,
			id:       rv.Record.ID,
			title:    title,
			nodeType: nt,
			tags:     tags,
			links:    parseCommaValues(rv.Record.Metadata["links"]),
			content:  rv.Record.Content,
		})
		titleToID[strings.ToLower(title)] = rv.Record.ID
	}

	// Map memory IDs and title references before filtering orphans.
	memIndexByID := make(map[string]int, len(mems))
	for i, rm := range mems {
		memIndexByID[rm.id] = i
	}
	resolveLinkTarget := func(ref string) (int, bool) {
		trimmed := strings.TrimSpace(ref)
		if trimmed == "" {
			return -1, false
		}
		if idx, ok := memIndexByID[trimmed]; ok {
			return idx, true
		}
		if idx, ok := memIndexByID[titleToID[strings.ToLower(trimmed)]]; ok {
			return idx, true
		}
		if len(trimmed) >= 6 {
			match := -1
			for i, rm := range mems {
				if strings.HasPrefix(rm.id, trimmed) {
					if match != -1 {
						return -1, false
					}
					match = i
				}
			}
			if match != -1 {
				return match, true
			}
		}
		return -1, false
	}

	explicitAdj := make([][]int, len(mems))
	for i, rm := range mems {
		seen := map[int]bool{i: true}
		addTarget := func(raw string) {
			if targetIdx, ok := resolveLinkTarget(raw); ok && !seen[targetIdx] {
				seen[targetIdx] = true
				explicitAdj[i] = append(explicitAdj[i], targetIdx)
			}
		}
		for _, l := range rm.links {
			addTarget(l)
		}
		for _, m := range wikiLinkPattern.FindAllStringSubmatch(rm.content, -1) {
			if len(m) > 1 {
				addTarget(m[1])
			}
		}
	}

	// Determine degree of each memory so ShowOrphans=false filters consistently.
	memDegree := make([]int, len(mems))
	for i, targets := range explicitAdj {
		for _, t := range targets {
			memDegree[i]++
			memDegree[t]++
		}
	}
	if g.ShowTags {
		for i, rm := range mems {
			memDegree[i] += len(rm.tags)
		}
	}

	keptMemToNode := make(map[int]int, len(mems))
	for i, rm := range mems {
		if !g.ShowOrphans && memDegree[i] == 0 {
			continue
		}
		nodeIdx := len(g.Nodes)
		keptMemToNode[i] = nodeIdx
		g.NodeByID[rm.id] = nodeIdx
		g.Nodes = append(g.Nodes, graphNode{
			ID:          rm.id,
			Kind:        nodeMemory,
			RecordIndex: rm.recIdx,
			Title:       rm.title,
			NodeType:    rm.nodeType,
			Tags:        rm.tags,
			Content:     rm.content,
			Color:       colorForMemoryNode(rm.nodeType, rm.tags),
		})
	}

	edgeSeen := map[[2]int]bool{}
	for srcMem, targets := range explicitAdj {
		u, okU := keptMemToNode[srcMem]
		if !okU {
			continue
		}
		for _, dstMem := range targets {
			v, okV := keptMemToNode[dstMem]
			if !okV || u == v {
				continue
			}
			g.Nodes[u].Outgoing = append(g.Nodes[u].Outgoing, v)
			g.Nodes[v].Incoming = append(g.Nodes[v].Incoming, u)
			pair := orderedPair(u, v)
			if !edgeSeen[pair] {
				edgeSeen[pair] = true
				g.Edges = append(g.Edges, graphEdge{From: pair[0], To: pair[1], Kind: edgeLink})
			}
		}
	}

	if g.ShowTags {
		tagToNode := map[string]int{}
		var tagNames []string
		for i, rm := range mems {
			if _, ok := keptMemToNode[i]; !ok {
				continue
			}
			for _, tag := range rm.tags {
				if _, exists := tagToNode[tag]; !exists {
					tagToNode[tag] = -1
					tagNames = append(tagNames, tag)
				}
			}
		}
		sort.Strings(tagNames)
		for _, tag := range tagNames {
			tagID := "tag:" + tag
			nodeIdx := len(g.Nodes)
			tagToNode[tag] = nodeIdx
			g.NodeByID[tagID] = nodeIdx
			g.Nodes = append(g.Nodes, graphNode{
				ID:          tagID,
				Kind:        nodeTag,
				RecordIndex: -1,
				Title:       "#" + tag,
				NodeType:    "tag",
				Color:       graphTagColor,
			})
		}

		for i, rm := range mems {
			u, okU := keptMemToNode[i]
			if !okU {
				continue
			}
			for _, tag := range rm.tags {
				v := tagToNode[tag]
				g.Nodes[u].TagLinks = append(g.Nodes[u].TagLinks, v)
				g.Nodes[v].TagLinks = append(g.Nodes[v].TagLinks, u)
				pair := orderedPair(u, v)
				if !edgeSeen[pair] {
					edgeSeen[pair] = true
					g.Edges = append(g.Edges, graphEdge{From: pair[0], To: pair[1], Kind: edgeTag})
				}
			}
		}
	}

	layoutGraph(g.Nodes, g.Edges)

	g.SelectedIdx = -1
	if prevID != "" {
		if idx, ok := g.NodeByID[prevID]; ok {
			g.SelectedIdx = idx
			g.SelectedID = prevID
		}
	}
	if g.SelectedIdx == -1 && len(g.Nodes) > 0 {
		g.SelectedIdx = 0
		g.SelectedID = g.Nodes[0].ID
	} else if g.SelectedIdx == -1 {
		g.SelectedID = ""
	}

	m.projectGraph()
	m.syncGraphViewport()
}

func orderedPair(a, b int) [2]int {
	if a < b {
		return [2]int{a, b}
	}
	return [2]int{b, a}
}

func parseCommaValues(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	seen := map[string]bool{}
	var out []string
	for _, p := range parts {
		clean := strings.TrimSpace(p)
		if clean == "" || seen[clean] {
			continue
		}
		seen[clean] = true
		out = append(out, clean)
	}
	return out
}

func deriveNodeTitle(id, content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		trimmed = strings.TrimLeft(trimmed, "#*-• ")
		trimmed = strings.ReplaceAll(trimmed, "**", "")
		trimmed = strings.ReplaceAll(trimmed, "`", "")
		trimmed = strings.Join(strings.Fields(trimmed), " ")
		if trimmed != "" {
			return truncate(trimmed, 18)
		}
	}
	return truncate(id, 12)
}

func layoutGraph(nodes []graphNode, edges []graphEdge) {
	n := len(nodes)
	if n == 0 {
		return
	}
	if n == 1 {
		nodes[0].X = 0.5
		nodes[0].Y = 0.5
		return
	}

	deg := make([]int, n)
	for _, e := range edges {
		deg[e.From]++
		deg[e.To]++
	}

	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool {
		di, dj := deg[order[i]], deg[order[j]]
		if di != dj {
			return di > dj
		}
		if nodes[order[i]].Kind != nodes[order[j]].Kind {
			return nodes[order[i]].Kind < nodes[order[j]].Kind
		}
		return nodes[order[i]].ID < nodes[order[j]].ID
	})

	// Place nodes on an aspect-balanced golden-angle spiral for deterministic convergence.
	const goldenAngle = 2.399963229728653
	const aspectX = 1.65
	posX := make([]float64, n)
	posY := make([]float64, n)
	for rank, idx := range order {
		r := 0.25 * math.Sqrt(float64(rank+1))
		theta := float64(rank) * goldenAngle
		posX[idx] = r * math.Cos(theta) * aspectX
		posY[idx] = r * math.Sin(theta)
	}

	area := float64(n) * 0.9
	k := math.Sqrt(area / float64(n))
	if k < 0.35 {
		k = 0.35
	}

	const iterations = 130
	dispX := make([]float64, n)
	dispY := make([]float64, n)
	for iter := 0; iter < iterations; iter++ {
		for i := 0; i < n; i++ {
			dispX[i] = 0
			dispY[i] = 0
		}

		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				dx := (posX[i] - posX[j]) / aspectX
				dy := posY[i] - posY[j]
				dist := math.Hypot(dx, dy)
				if dist < 0.01 {
					dist = 0.01
					dx = 0.01
				}
				rep := (k * k) / dist
				ux := (dx / dist) * rep * aspectX
				uy := (dy / dist) * rep
				dispX[i] += ux
				dispY[i] += uy
				dispX[j] -= ux
				dispY[j] -= uy
			}
		}

		for _, e := range edges {
			dx := (posX[e.From] - posX[e.To]) / aspectX
			dy := posY[e.From] - posY[e.To]
			dist := math.Hypot(dx, dy)
			if dist < 0.001 {
				continue
			}
			weight := 1.0
			if e.Kind == edgeLink {
				weight = 1.35
			} else {
				weight = 0.85
			}
			attr := (dist * dist / k) * weight
			ux := (dx / dist) * attr * aspectX
			uy := (dy / dist) * attr
			dispX[e.From] -= ux
			dispY[e.From] -= uy
			dispX[e.To] += ux
			dispY[e.To] += uy
		}

		// Center gravity prevents disconnected nodes from drifting away.
		for i := 0; i < n; i++ {
			grav := 0.045
			if deg[i] == 0 {
				grav = 0.09
			}
			dispX[i] -= posX[i] * grav
			dispY[i] -= posY[i] * grav
		}

		temp := 0.35 * (1.0 - float64(iter)/float64(iterations))
		if temp < 0.01 {
			temp = 0.01
		}
		for i := 0; i < n; i++ {
			d := math.Hypot(dispX[i], dispY[i])
			if d > 0 {
				step := math.Min(d, temp)
				posX[i] += (dispX[i] / d) * step
				posY[i] += (dispY[i] / d) * step
			}
		}
	}

	minX, maxX := posX[0], posX[0]
	minY, maxY := posY[0], posY[0]
	for i := 1; i < n; i++ {
		minX = math.Min(minX, posX[i])
		maxX = math.Max(maxX, posX[i])
		minY = math.Min(minY, posY[i])
		maxY = math.Max(maxY, posY[i])
	}
	spanX := math.Max(maxX-minX, 1e-6)
	spanY := math.Max(maxY-minY, 1e-6)
	for i := 0; i < n; i++ {
		nodes[i].X = (posX[i] - minX) / spanX
		nodes[i].Y = (posY[i] - minY) / spanY
	}
}

func (m Model) graphPaneWidths() (leftPaneW, rightPaneW, canvasW, canvasH int) {
	bodyH := max(10, m.height-12)
	leftPaneW = (m.width * 64) / 100
	if leftPaneW < 42 {
		leftPaneW = max(24, m.width/2)
	}
	if leftPaneW > m.width-34 {
		leftPaneW = max(24, m.width-34)
	}
	rightPaneW = max(24, m.width-leftPaneW-5)
	canvasW = max(18, leftPaneW-2)
	canvasH = max(6, bodyH-2)
	return leftPaneW, rightPaneW, canvasW, canvasH
}

func (m *Model) projectGraph() {
	_, _, canvasW, canvasH := m.graphPaneWidths()
	g := &m.graph
	g.CanvasW = canvasW
	g.CanvasH = canvasH

	padX := 3.0
	padY := 1.0
	usableW := math.Max(4.0, float64(canvasW)-padX*2-1)
	usableH := math.Max(3.0, float64(canvasH)-padY*2-1)
	centerX := float64(canvasW) / 2.0
	centerY := float64(canvasH) / 2.0

	for i := range g.Nodes {
		wx := (g.Nodes[i].X - 0.5 - g.PanX) * g.Zoom
		wy := (g.Nodes[i].Y - 0.5 - g.PanY) * g.Zoom
		sx := int(math.Round(centerX + wx*usableW))
		sy := int(math.Round(centerY + wy*usableH))
		g.Nodes[i].ScreenX = sx
		g.Nodes[i].ScreenY = sy
		g.Nodes[i].Visible = sx >= 1 && sx < canvasW-1 && sy >= 0 && sy < canvasH
	}
}

func (m *Model) selectGraphNode(idx int) {
	if idx < 0 || idx >= len(m.graph.Nodes) {
		return
	}
	m.graph.SelectedIdx = idx
	m.graph.SelectedID = m.graph.Nodes[idx].ID
	if m.graph.Nodes[idx].Kind == nodeMemory && m.graph.Nodes[idx].RecordIndex >= 0 && m.graph.Nodes[idx].RecordIndex < len(m.records) {
		m.cursor = m.graph.Nodes[idx].RecordIndex
		m.syncDetailViewport()
	}
	m.syncGraphViewport()
}

func (m *Model) centerGraphOnSelected() {
	if m.graph.SelectedIdx < 0 || m.graph.SelectedIdx >= len(m.graph.Nodes) {
		return
	}
	n := m.graph.Nodes[m.graph.SelectedIdx]
	m.graph.PanX = n.X - 0.5
	m.graph.PanY = n.Y - 0.5
	m.projectGraph()
}

func (m *Model) moveGraphSelectionDir(dirX, dirY int) {
	n := len(m.graph.Nodes)
	if n == 0 {
		return
	}
	if m.graph.SelectedIdx < 0 || m.graph.SelectedIdx >= n {
		m.selectGraphNode(0)
		return
	}
	cur := m.graph.Nodes[m.graph.SelectedIdx]
	bestIdx := -1
	bestScore := math.Inf(1)
	for i, cand := range m.graph.Nodes {
		if i == m.graph.SelectedIdx {
			continue
		}
		dx := cand.X - cur.X
		dy := cand.Y - cur.Y
		dot := dx*float64(dirX) + dy*float64(dirY)
		if dot <= 1e-4 {
			continue
		}
		perp := math.Abs(dx*float64(dirY) - dy*float64(dirX))
		score := dot + perp*2.2
		if score < bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	if bestIdx != -1 {
		m.selectGraphNode(bestIdx)
	}
}

func (m *Model) stepGraphSelection(delta int) {
	n := len(m.graph.Nodes)
	if n == 0 {
		return
	}
	if m.graph.SelectedIdx < 0 {
		m.selectGraphNode(0)
		return
	}
	next := (m.graph.SelectedIdx + delta + n) % n
	m.selectGraphNode(next)
}

func (m *Model) adjustGraphZoom(factor float64) {
	next := m.graph.Zoom * factor
	if next < 0.5 {
		next = 0.5
	} else if next > 3.0 {
		next = 3.0
	}
	m.graph.Zoom = next
	m.projectGraph()
}

func (m *Model) resetGraphCamera() {
	m.graph.Zoom = 1.0
	m.graph.PanX = 0
	m.graph.PanY = 0
	m.projectGraph()
}

func (m *Model) syncGraphViewport() {
	_, rightPaneW, _, _ := m.graphPaneWidths()
	bodyH := max(10, m.height-12)
	m.graphViewport.Width = max(20, rightPaneW-4)

	if m.graph.SelectedIdx < 0 || m.graph.SelectedIdx >= len(m.graph.Nodes) {
		m.graphViewport.Height = max(3, bodyH-6)
		m.graphViewport.SetContent(m.graphEmptyMessage())
		return
	}

	n := m.graph.Nodes[m.graph.SelectedIdx]
	headerLines, _ := m.buildGraphInspectorHeader(n, rightPaneW, bodyH)
	m.graphViewport.Height = max(2, bodyH-len(headerLines)-2)
	if n.Kind == nodeTag {
		m.graphViewport.SetContent(fmt.Sprintf("Tag cluster %s connects %d memories.", n.Title, len(n.TagLinks)))
	} else {
		m.graphViewport.SetContent(wrapText(n.Content, max(16, m.graphViewport.Width-1)))
	}
	m.graphViewport.GotoTop()
}

func (m Model) graphEmptyMessage() string {
	if len(m.records) == 0 {
		if strings.TrimSpace(m.search.Value()) != "" {
			return fmt.Sprintf("No matching memories for %q. Press / to refine or clear search.", m.search.Value())
		}
		return "No memories stored in this ChromaDB directory yet."
	}
	if !m.graph.ShowOrphans && len(m.graph.Nodes) == 0 {
		return "No connected nodes under current filters. Press t to show #tags or o to show orphans."
	}
	return "Select a node on the graph to inspect its connections."
}

type canvasCell struct {
	braille uint8
	ch      rune
	fg      lipgloss.Color
	bg      lipgloss.Color
	bold    bool
	prio    int
}

var brailleBits = [2][4]uint8{
	{0x01, 0x02, 0x04, 0x40},
	{0x08, 0x10, 0x20, 0x80},
}

func (m Model) renderNoteGraph() string {
	leftPaneW, rightPaneW, canvasW, canvasH := m.graphPaneWidths()
	bodyH := max(10, m.height-12)
	g := m.graph

	toolbarStr, _ := m.buildGraphToolbar(canvasW)
	sep := lipgloss.NewStyle().Foreground(fxBorderRail).Render(strings.Repeat("─", canvasW))

	var canvasRender string
	if len(g.Nodes) == 0 {
		emptyBox := lipgloss.NewStyle().
			Foreground(fxFgDim).
			Width(canvasW).
			Height(canvasH).
			Align(lipgloss.Center, lipgloss.Center).
			Render(m.graphEmptyMessage())
		canvasRender = emptyBox
	} else {
		canvasRender = renderGraphCanvas(g, canvasW, canvasH)
	}

	leftContent := toolbarStr + "\n" + sep + "\n" + canvasRender
	leftPane := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(fxBorderRail).
		Padding(0, 1).
		Width(leftPaneW).
		Height(bodyH).
		Render(leftContent)

	rightPane := m.renderGraphInspectorPane(rightPaneW, bodyH)
	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, " ", rightPane)
}

func (m Model) buildGraphToolbar(canvasW int) (string, []graphToolbarButton) {
	tagsState := "OFF"
	if m.graph.ShowTags {
		tagsState = "ON"
	}
	orphansState := "OFF"
	if m.graph.ShowOrphans {
		orphansState = "ON"
	}

	pillStyle := func(active bool) lipgloss.Style {
		if active {
			return lipgloss.NewStyle().Foreground(fxAmber).Bold(true)
		}
		return lipgloss.NewStyle().Foreground(fxFgDim)
	}

	btnTags := pillStyle(m.graph.ShowTags).Render(fmt.Sprintf("[t #Tags:%s]", tagsState))
	btnOrphans := pillStyle(m.graph.ShowOrphans).Render(fmt.Sprintf("[o Orphans:%s]", orphansState))
	btnZoomOut := lipgloss.NewStyle().Foreground(fxFgAuto).Render("[-]")
	zoomLabel := lipgloss.NewStyle().Foreground(fxFgDim).Render(fmt.Sprintf("%.1fx", m.graph.Zoom))
	btnZoomIn := lipgloss.NewStyle().Foreground(fxFgAuto).Render("[+]")
	btnFit := lipgloss.NewStyle().Foreground(fxFgAuto).Render("[0 Fit]")
	stats := lipgloss.NewStyle().Foreground(fxFgDim).Render(fmt.Sprintf("%dN·%dE", len(m.graph.Nodes), len(m.graph.Edges)))

	parts := []struct {
		action string
		text   string
	}{
		{"tags", btnTags},
		{"orphans", btnOrphans},
		{"zoom_out", btnZoomOut},
		{"", zoomLabel},
		{"zoom_in", btnZoomIn},
		{"fit", btnFit},
		{"", stats},
	}

	var rendered strings.Builder
	var hits []graphToolbarButton
	x := 2 // pane border (1) + left padding (1)
	for i, p := range parts {
		if i > 0 {
			rendered.WriteByte(' ')
			x++
		}
		w := lipgloss.Width(p.text)
		if p.action != "" {
			hits = append(hits, graphToolbarButton{
				Action: p.action,
				StartX: x,
				EndX:   x + w,
			})
		}
		rendered.WriteString(p.text)
		x += w
	}
	return truncateRenderedLine(rendered.String(), canvasW), hits
}

func truncateRenderedLine(s string, maxW int) string {
	if lipgloss.Width(s) <= maxW {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(maxW).Render(s)
}

func (m *Model) refreshGraphHitboxes() {
	_, rightPaneW, canvasW, _ := m.graphPaneWidths()
	bodyH := max(10, m.height-12)
	_, hits := m.buildGraphToolbar(canvasW)
	m.graph.ToolbarHits = hits
	m.graph.LabelHits = computeLabelHits(m.graph)
	if m.graph.SelectedIdx >= 0 && m.graph.SelectedIdx < len(m.graph.Nodes) {
		_, neighborHits := m.buildGraphInspectorHeader(m.graph.Nodes[m.graph.SelectedIdx], rightPaneW, bodyH)
		m.graph.NeighborHits = neighborHits
	} else {
		m.graph.NeighborHits = nil
	}
}

func computeLabelHits(g graphState) []graphLabelHit {
	if len(g.Nodes) == 0 || g.CanvasW <= 0 || g.CanvasH <= 0 {
		return nil
	}
	placements := planLabelPlacements(g, g.CanvasW, g.CanvasH)
	hits := make([]graphLabelHit, 0, len(placements))
	for _, p := range placements {
		hits = append(hits, graphLabelHit{
			NodeIdx: p.nodeIdx,
			Y:       p.y,
			StartX:  p.x,
			EndX:    p.x + len([]rune(p.text)),
		})
	}
	return hits
}

type labelPlacement struct {
	nodeIdx int
	x       int
	y       int
	text    string
	active  bool
}

func planLabelPlacements(g graphState, canvasW, canvasH int) []labelPlacement {
	isNeighbor := neighborSet(g, g.SelectedIdx)
	order := make([]int, len(g.Nodes))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		pi := labelPriority(order[i], g.SelectedIdx, isNeighbor)
		pj := labelPriority(order[j], g.SelectedIdx, isNeighbor)
		if pi != pj {
			return pi > pj
		}
		return g.Nodes[order[i]].Title < g.Nodes[order[j]].Title
	})

	occupied := make([][]bool, canvasH)
	for y := 0; y < canvasH; y++ {
		occupied[y] = make([]bool, canvasW)
	}
	// Reserve node markers so labels never overwrite node glyphs.
	for _, n := range g.Nodes {
		if n.Visible {
			occupied[n.ScreenY][n.ScreenX] = true
		}
	}

	var placed []labelPlacement
	for _, idx := range order {
		n := g.Nodes[idx]
		if !n.Visible {
			continue
		}
		label := n.Title
		if idx != g.SelectedIdx && !isNeighbor[idx] && len(g.Nodes) > 14 {
			label = truncate(label, 12)
		}
		runes := []rune(label)
		lw := len(runes)
		if lw == 0 {
			continue
		}

		// Try right of node, then left of node.
		candidates := []int{n.ScreenX + 2, n.ScreenX - lw - 1}
		for _, startX := range candidates {
			if startX < 0 || startX+lw > canvasW {
				continue
			}
			clear := true
			for x := startX; x < startX+lw; x++ {
				if occupied[n.ScreenY][x] {
					clear = false
					break
				}
			}
			if clear {
				for x := max(0, startX-1); x < min(canvasW, startX+lw+1); x++ {
					occupied[n.ScreenY][x] = true
				}
				placed = append(placed, labelPlacement{
					nodeIdx: idx,
					x:       startX,
					y:       n.ScreenY,
					text:    label,
					active:  idx == g.SelectedIdx || isNeighbor[idx],
				})
				break
			}
		}
	}
	return placed
}

func labelPriority(idx, selected int, neighbors map[int]bool) int {
	if idx == selected {
		return 3
	}
	if neighbors[idx] {
		return 2
	}
	return 1
}

func neighborSet(g graphState, idx int) map[int]bool {
	out := map[int]bool{}
	if idx < 0 || idx >= len(g.Nodes) {
		return out
	}
	for _, e := range g.Edges {
		if e.From == idx {
			out[e.To] = true
		} else if e.To == idx {
			out[e.From] = true
		}
	}
	return out
}

func renderGraphCanvas(g graphState, canvasW, canvasH int) string {
	grid := make([][]canvasCell, canvasH)
	for y := 0; y < canvasH; y++ {
		grid[y] = make([]canvasCell, canvasW)
	}

	neighbors := neighborSet(g, g.SelectedIdx)

	// Draw dimmed background edges first, then active edges incident to selected node.
	for pass := 0; pass < 2; pass++ {
		for _, e := range g.Edges {
			u := g.Nodes[e.From]
			v := g.Nodes[e.To]
			active := e.From == g.SelectedIdx || e.To == g.SelectedIdx
			if (pass == 0 && active) || (pass == 1 && !active) {
				continue
			}
			col := graphEdgeMutedColor
			prio := 1
			if active {
				prio = 2
				if e.Kind == edgeLink {
					col = graphLinkHighlight
				} else {
					col = graphTagHighlight
				}
			}
			drawBrailleLine(grid, canvasW, canvasH, u.ScreenX, u.ScreenY, v.ScreenX, v.ScreenY, col, prio)
		}
	}

	// Draw labels before node markers so markers stay crisp.
	for _, lp := range planLabelPlacements(g, canvasW, canvasH) {
		fg := fxFgDim
		var bg lipgloss.Color
		bold := false
		if lp.nodeIdx == g.SelectedIdx {
			fg = fxAccentInvFg
			bg = fxAmber
			bold = true
		} else if lp.active {
			fg = fxFgBright
			bold = true
		}
		for i, r := range []rune(lp.text) {
			x := lp.x + i
			if x >= 0 && x < canvasW && lp.y >= 0 && lp.y < canvasH {
				grid[lp.y][x] = canvasCell{
					ch:   r,
					fg:   fg,
					bg:   bg,
					bold: bold,
					prio: 3,
				}
			}
		}
	}

	// Draw node markers on top.
	for i, n := range g.Nodes {
		if !n.Visible {
			continue
		}
		glyph := '○'
		fg := n.Color
		bold := false
		prio := 4
		if n.Kind == nodeTag {
			glyph = '◈'
		}
		if i == g.SelectedIdx {
			glyph = '◉'
			fg = fxAmber
			bold = true
			prio = 5
		} else if neighbors[i] {
			if n.Kind == nodeMemory {
				glyph = '●'
			}
			bold = true
		} else if g.SelectedIdx >= 0 && len(g.Nodes) > 6 {
			fg = fxFgDim
		}
		grid[n.ScreenY][n.ScreenX] = canvasCell{
			ch:   glyph,
			fg:   fg,
			bold: bold,
			prio: prio,
		}
	}

	lines := make([]string, canvasH)
	for y := 0; y < canvasH; y++ {
		lines[y] = renderCanvasRow(grid[y])
	}
	return strings.Join(lines, "\n")
}

func drawBrailleLine(grid [][]canvasCell, w, h, x0, y0, x1, y1 int, col lipgloss.Color, prio int) {
	px0 := x0*2 + 1
	py0 := y0*4 + 2
	px1 := x1*2 + 1
	py1 := y1*4 + 2

	dx := px1 - px0
	if dx < 0 {
		dx = -dx
	}
	dy := py1 - py0
	if dy < 0 {
		dy = -dy
	}
	sx := -1
	if px0 < px1 {
		sx = 1
	}
	sy := -1
	if py0 < py1 {
		sy = 1
	}
	err := dx - dy

	px, py := px0, py0
	for {
		cx := px / 2
		cy := py / 4
		if px >= 0 && py >= 0 && cx >= 0 && cx < w && cy >= 0 && cy < h {
			cell := &grid[cy][cx]
			if prio >= cell.prio && cell.ch == 0 {
				cell.braille |= brailleBits[px%2][py%4]
				cell.fg = col
				cell.prio = prio
			}
		}
		if px == px1 && py == py1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			px += sx
		}
		if e2 < dx {
			err += dx
			py += sy
		}
	}
}

func renderCanvasRow(row []canvasCell) string {
	var b strings.Builder
	i := 0
	for i < len(row) {
		cur := row[i]
		curCh := cellRune(cur)
		if curCh == ' ' && cur.bg == "" {
			j := i + 1
			for j < len(row) && cellRune(row[j]) == ' ' && row[j].bg == "" {
				j++
			}
			b.WriteString(strings.Repeat(" ", j-i))
			i = j
			continue
		}

		var seg strings.Builder
		seg.WriteRune(curCh)
		j := i + 1
		for j < len(row) {
			nxt := row[j]
			nxtCh := cellRune(nxt)
			if nxtCh == ' ' && nxt.bg == "" {
				break
			}
			if nxt.fg != cur.fg || nxt.bg != cur.bg || nxt.bold != cur.bold {
				break
			}
			seg.WriteRune(nxtCh)
			j++
		}

		st := lipgloss.NewStyle()
		if cur.fg != "" {
			st = st.Foreground(cur.fg)
		}
		if cur.bg != "" {
			st = st.Background(cur.bg)
		}
		if cur.bold {
			st = st.Bold(true)
		}
		b.WriteString(st.Render(seg.String()))
		i = j
	}
	return b.String()
}

func cellRune(c canvasCell) rune {
	if c.ch != 0 {
		return c.ch
	}
	if c.braille != 0 {
		return rune(0x2800 | int(c.braille))
	}
	return ' '
}

func (m Model) buildGraphInspectorHeader(n graphNode, rightPaneW, bodyH int) ([]string, []graphNeighborHit) {
	innerW := max(18, rightPaneW-4)
	var lines []string
	var hits []graphNeighborHit

	badgeLabel := "MEMORY"
	if n.Kind == nodeTag {
		badgeLabel = "TAG HUB"
	} else if n.NodeType != "" {
		badgeLabel = strings.ToUpper(n.NodeType)
	}
	badge := lipgloss.NewStyle().
		Bold(true).
		Foreground(fxAccentInvFg).
		Background(n.Color).
		Padding(0, 1).
		Render(badgeLabel)
	title := lipgloss.NewStyle().Foreground(fxFgBright).Bold(true).Render(truncate(n.Title, max(8, innerW-lipgloss.Width(badge)-1)))
	lines = append(lines, badge+" "+title)

	if n.Kind == nodeMemory {
		idLine := lipgloss.NewStyle().Foreground(fxFgDim).Render("ID: " + truncate(n.ID, max(10, innerW-4)))
		lines = append(lines, idLine)
		if len(n.Tags) > 0 {
			lines = append(lines, lipgloss.NewStyle().Foreground(fxAmber).Render("Tags: "+truncate(strings.Join(n.Tags, ", "), max(8, innerW-6))))
		}
	}

	// Reserve at least 4 lines (sep + 2 viewport lines + hint) for content on compact terminals.
	budget := max(2, bodyH-len(lines)-4)
	addNeighbors := func(label string, idxs []int, prefix string, color lipgloss.Color) {
		if len(idxs) == 0 || budget <= 1 {
			return
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(fxFgDim).Bold(true).Render(fmt.Sprintf("%s (%d):", label, len(idxs))))
		budget--
		for i, targetIdx := range idxs {
			if budget <= 0 {
				break
			}
			if budget == 1 && i < len(idxs)-1 {
				lines = append(lines, lipgloss.NewStyle().Foreground(fxFgDim).Render(fmt.Sprintf("  +%d more", len(idxs)-i)))
				budget--
				break
			}
			target := m.graph.Nodes[targetIdx]
			rowY := 7 + len(lines) // pane top border at Y=6; content starts at Y=7
			hits = append(hits, graphNeighborHit{NodeIdx: targetIdx, ScreenY: rowY})
			rowText := fmt.Sprintf("  %s %s", prefix, truncate(target.Title, max(8, innerW-5)))
			lines = append(lines, lipgloss.NewStyle().Foreground(color).Render(rowText))
			budget--
		}
	}

	if n.Kind == nodeMemory {
		addNeighbors("Outgoing", n.Outgoing, "→", fxAmber)
		addNeighbors("Backlinks", n.Incoming, "←", fxAmber)
		addNeighbors("Tags", n.TagLinks, "#", graphTagHighlight)
		if len(n.Outgoing) == 0 && len(n.Incoming) == 0 && len(n.TagLinks) == 0 {
			lines = append(lines, lipgloss.NewStyle().Foreground(fxFgDim).Render("Orphan node (no links or tags)"))
		}
	} else {
		addNeighbors("Linked Memories", n.TagLinks, "•", fxFgBright)
	}

	return lines, hits
}

func (m Model) renderGraphInspectorPane(rightPaneW, bodyH int) string {
	innerW := max(18, rightPaneW-4)
	sep := lipgloss.NewStyle().Foreground(fxBorderRail).Render(strings.Repeat("─", innerW))

	var body string
	if m.graph.SelectedIdx < 0 || m.graph.SelectedIdx >= len(m.graph.Nodes) {
		header := lipgloss.NewStyle().Foreground(fxFgBright).Bold(true).Render("Note Graph Inspector")
		body = header + "\n" + sep + "\n" + m.graphViewport.View()
	} else {
		n := m.graph.Nodes[m.graph.SelectedIdx]
		headerLines, _ := m.buildGraphInspectorHeader(n, rightPaneW, bodyH)
		hint := "Enter inspect · c copy · d del"
		if n.Kind == nodeTag {
			hint = "Click memory or Enter to open"
		}
		hintLine := lipgloss.NewStyle().Foreground(fxFgDim).Render(truncate(hint, innerW))
		body = strings.Join(headerLines, "\n") + "\n" + sep + "\n" + m.graphViewport.View() + "\n" + hintLine
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(fxBorderRail).
		Foreground(fxFgBright).
		Padding(0, 1).
		Width(rightPaneW).
		Height(bodyH).
		Render(body)
}

func (m Model) handleGraphKey(key string) (Model, tea.Cmd) {
	switch key {
	case "t":
		m.graph.ShowTags = !m.graph.ShowTags
		m.rebuildGraph()
		m.refreshGraphHitboxes()
		return m, nil
	case "o":
		m.graph.ShowOrphans = !m.graph.ShowOrphans
		m.rebuildGraph()
		m.refreshGraphHitboxes()
		return m, nil
	case "+", "=":
		m.adjustGraphZoom(1.25)
		m.refreshGraphHitboxes()
		return m, nil
	case "-", "_":
		m.adjustGraphZoom(0.8)
		m.refreshGraphHitboxes()
		return m, nil
	case "0":
		m.resetGraphCamera()
		m.refreshGraphHitboxes()
		return m, nil
	case "f":
		m.centerGraphOnSelected()
		m.refreshGraphHitboxes()
		return m, nil
	case "n":
		m.stepGraphSelection(1)
		m.refreshGraphHitboxes()
		return m, nil
	case "p":
		m.stepGraphSelection(-1)
		m.refreshGraphHitboxes()
		return m, nil
	case "up", "k":
		m.moveGraphSelectionDir(0, -1)
		m.refreshGraphHitboxes()
		return m, nil
	case "down", "j":
		m.moveGraphSelectionDir(0, 1)
		m.refreshGraphHitboxes()
		return m, nil
	case "left", "h":
		m.moveGraphSelectionDir(-1, 0)
		m.refreshGraphHitboxes()
		return m, nil
	case "right", "l":
		m.moveGraphSelectionDir(1, 0)
		m.refreshGraphHitboxes()
		return m, nil
	case "K":
		m.graph.PanY -= 0.08 / m.graph.Zoom
		m.projectGraph()
		m.refreshGraphHitboxes()
		return m, nil
	case "J":
		m.graph.PanY += 0.08 / m.graph.Zoom
		m.projectGraph()
		m.refreshGraphHitboxes()
		return m, nil
	case "H":
		m.graph.PanX -= 0.08 / m.graph.Zoom
		m.projectGraph()
		m.refreshGraphHitboxes()
		return m, nil
	case "L":
		m.graph.PanX += 0.08 / m.graph.Zoom
		m.projectGraph()
		m.refreshGraphHitboxes()
		return m, nil
	case "enter":
		if m.graph.SelectedIdx >= 0 && m.graph.SelectedIdx < len(m.graph.Nodes) {
			n := m.graph.Nodes[m.graph.SelectedIdx]
			if n.Kind == nodeMemory && n.RecordIndex >= 0 && n.RecordIndex < len(m.records) {
				m.cursor = n.RecordIndex
				m.activeTab = 0
				m.syncDetailViewport()
				return m, nil
			}
			if n.Kind == nodeTag && len(n.TagLinks) > 0 {
				m.selectGraphNode(n.TagLinks[0])
				m.refreshGraphHitboxes()
				return m, nil
			}
		}
	}
	return m, nil
}

func (m Model) handleGraphMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	leftPaneW, _, canvasW, canvasH := m.graphPaneWidths()
	leftOuterEnd := leftPaneW + 2

	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		if msg.X < leftOuterEnd {
			if msg.Button == tea.MouseButtonWheelUp {
				m.adjustGraphZoom(1.15)
			} else {
				m.adjustGraphZoom(0.87)
			}
			m.refreshGraphHitboxes()
			return m, nil
		}
		var cmd tea.Cmd
		m.graphViewport, cmd = m.graphViewport.Update(msg)
		return m, cmd
	}

	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}

	// Toolbar sits on Y = 7 (pane border at Y=6).
	if msg.Y == 7 && msg.X < leftOuterEnd {
		for _, btn := range m.graph.ToolbarHits {
			if msg.X >= btn.StartX && msg.X < btn.EndX {
				switch btn.Action {
				case "tags":
					m.graph.ShowTags = !m.graph.ShowTags
					m.rebuildGraph()
				case "orphans":
					m.graph.ShowOrphans = !m.graph.ShowOrphans
					m.rebuildGraph()
				case "zoom_in":
					m.adjustGraphZoom(1.25)
				case "zoom_out":
					m.adjustGraphZoom(0.8)
				case "fit":
					m.resetGraphCamera()
				}
				m.refreshGraphHitboxes()
				return m, nil
			}
		}
		return m, nil
	}

	// Canvas spans X in [2, 2+canvasW), Y in [9, 9+canvasH).
	if msg.X >= 2 && msg.X < 2+canvasW && msg.Y >= 9 && msg.Y < 9+canvasH {
		cx := msg.X - 2
		cy := msg.Y - 9
		for _, lh := range m.graph.LabelHits {
			if cy == lh.Y && cx >= lh.StartX && cx < lh.EndX {
				m.selectGraphNode(lh.NodeIdx)
				m.refreshGraphHitboxes()
				return m, nil
			}
		}
		bestIdx := -1
		bestDist := 999.0
		for i, n := range m.graph.Nodes {
			if !n.Visible {
				continue
			}
			dx := float64(cx - n.ScreenX)
			dy := float64(cy-n.ScreenY) * 1.8
			d := math.Hypot(dx, dy)
			if d <= 2.3 && d < bestDist {
				bestDist = d
				bestIdx = i
			}
		}
		if bestIdx != -1 {
			m.selectGraphNode(bestIdx)
			m.refreshGraphHitboxes()
		}
		return m, nil
	}

	// Click neighbor link inside right Graph Inspector pane.
	if msg.X > leftOuterEnd {
		for _, nh := range m.graph.NeighborHits {
			if msg.Y == nh.ScreenY {
				m.selectGraphNode(nh.NodeIdx)
				m.refreshGraphHitboxes()
				return m, nil
			}
		}
	}

	return m, nil
}
