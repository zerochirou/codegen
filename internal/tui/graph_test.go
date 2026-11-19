package tui

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zerochirou/codegen/internal/chroma"
)

func containsBraille(s string) bool {
	for _, r := range s {
		if r >= 0x2800 && r <= 0x28FF {
			return true
		}
	}
	return false
}

func TestNoteGraphBuildLinksBacklinksTagsAndOrphans(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "chroma_data")
	store, err := chroma.Open(dir)
	if err != nil {
		t.Fatalf("chroma.Open failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	entries := []struct {
		id, content string
		meta        map[string]string
	}{
		{
			id:      "mem-arch",
			content: "# Architecture Core\nUses Cobra and ChromaDB.",
			meta: map[string]string{
				"tags":      "architecture,go",
				"node_type": "architecture",
				"links":     "mem-cli",
			},
		},
		{
			id:      "mem-cli",
			content: "CLI commands wire [[mem-tui]] cleanly.",
			meta: map[string]string{
				"tags":      "cli,go",
				"node_type": "workflow",
			},
		},
		{
			id:      "mem-tui",
			content: "MemoryTUI renders Obsidian-style graph.",
			meta: map[string]string{
				"tags":      "tui",
				"node_type": "architecture",
			},
		},
		{
			id:      "mem-orphan",
			content: "Standalone note with no tags or links.",
			meta:    map[string]string{},
		},
	}
	for _, e := range entries {
		if _, err := store.Save(ctx, "default", e.id, e.content, e.meta); err != nil {
			t.Fatal(err)
		}
	}

	m := NewModel(store, Options{PersistDir: dir})
	updated, _ := m.Update(m.Init()())
	m = updated.(Model)
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 115, Height: 34})
	m = updated.(Model)

	// Switch to Tab 3 (`3 Note Graph`) via key "3".
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m = updated.(Model)
	if m.activeTab != 2 {
		t.Fatalf("expected activeTab=2, got %d", m.activeTab)
	}

	// Verify graph nodes (4 memory nodes + 4 unique tag hubs: architecture, cli, go, tui).
	if len(m.graph.Nodes) != 8 {
		t.Fatalf("expected 8 graph nodes (4 memories + 4 tags), got %d", len(m.graph.Nodes))
	}

	archIdx, ok := m.graph.NodeByID["mem-arch"]
	if !ok {
		t.Fatalf("missing mem-arch node")
	}
	cliIdx := m.graph.NodeByID["mem-cli"]
	tuiIdx := m.graph.NodeByID["mem-tui"]

	if len(m.graph.Nodes[archIdx].Outgoing) != 1 || m.graph.Nodes[archIdx].Outgoing[0] != cliIdx {
		t.Fatalf("mem-arch Outgoing = %v, want [%d]", m.graph.Nodes[archIdx].Outgoing, cliIdx)
	}
	if len(m.graph.Nodes[cliIdx].Incoming) != 1 || m.graph.Nodes[cliIdx].Incoming[0] != archIdx {
		t.Fatalf("mem-cli Incoming (backlinks) = %v, want [%d]", m.graph.Nodes[cliIdx].Incoming, archIdx)
	}
	if len(m.graph.Nodes[cliIdx].Outgoing) != 1 || m.graph.Nodes[cliIdx].Outgoing[0] != tuiIdx {
		t.Fatalf("mem-cli wiki-link Outgoing = %v, want [%d]", m.graph.Nodes[cliIdx].Outgoing, tuiIdx)
	}

	view := m.View()
	if !strings.Contains(view, "3 Note Graph") || !strings.Contains(view, "[t #Tags:ON]") || !containsBraille(view) {
		t.Fatalf("expected Note Graph view with toolbar and Braille edges, got:\n%s", view)
	}

	// Toggle #Tags OFF (`t`) -> only 4 memory nodes remain.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = updated.(Model)
	if m.graph.ShowTags || len(m.graph.Nodes) != 4 {
		t.Fatalf("expected ShowTags=false and 4 nodes, got ShowTags=%v nodes=%d", m.graph.ShowTags, len(m.graph.Nodes))
	}

	// Toggle Orphans OFF (`o`) -> mem-orphan hidden, 3 connected nodes remain.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = updated.(Model)
	if m.graph.ShowOrphans || len(m.graph.Nodes) != 3 {
		t.Fatalf("expected ShowOrphans=false and 3 connected nodes, got ShowOrphans=%v nodes=%d", m.graph.ShowOrphans, len(m.graph.Nodes))
	}
	if _, hasOrphan := m.graph.NodeByID["mem-orphan"]; hasOrphan {
		t.Fatalf("mem-orphan should be hidden when ShowOrphans=false")
	}

	// Turn Orphans and #Tags back ON via toolbar mouse click.
	for _, btn := range m.graph.ToolbarHits {
		if btn.Action == "tags" || btn.Action == "orphans" {
			updated, _ = m.Update(tea.MouseMsg{X: btn.StartX + 1, Y: 7, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			m = updated.(Model)
		}
	}
	if !m.graph.ShowTags || !m.graph.ShowOrphans || len(m.graph.Nodes) != 8 {
		t.Fatalf("toolbar clicks should restore tags & orphans, got tags=%v orphans=%v nodes=%d", m.graph.ShowTags, m.graph.ShowOrphans, len(m.graph.Nodes))
	}
}

func TestNoteGraphNavigationZoomMouseAndEmptyStates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "chroma_data")
	store, err := chroma.Open(dir)
	if err != nil {
		t.Fatalf("chroma.Open failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	// 1. Empty store should render safe empty message and ignore navigation cleanly.
	emptyM := NewModel(store, Options{PersistDir: dir})
	updated, _ := emptyM.Update(emptyM.Init()())
	emptyM = updated.(Model)
	updated, _ = emptyM.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	emptyM = updated.(Model)
	updated, _ = emptyM.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	emptyM = updated.(Model)
	for _, k := range []string{"n", "p", "enter", "t", "o", "+", "-", "0", "f", "c", "d"} {
		updated, _ = emptyM.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		emptyM = updated.(Model)
	}
	if !strings.Contains(emptyM.View(), "No memories stored") {
		t.Fatalf("expected helpful empty state on empty graph:\n%s", emptyM.View())
	}

	// 2. Populated graph: test zoom, pan, click node, click neighbor, and Enter -> Inspector.
	ctx := context.Background()
	if _, err := store.Save(ctx, "default", "n1", "First connected node", map[string]string{"links": "n2", "tags": "core"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(ctx, "default", "n2", "Second connected node", map[string]string{"tags": "core"}); err != nil {
		t.Fatal(err)
	}

	m := NewModel(store, Options{PersistDir: dir})
	updated, _ = m.Update(m.Init()())
	m = updated.(Model)
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 110, Height: 32})
	m = updated.(Model)

	// Click "3 Note Graph" tab on Y = 5.
	_, w0, w1, w2 := m.renderTabs()
	updated, _ = m.Update(tea.MouseMsg{X: w0 + w1 + w2/2, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if m.activeTab != 2 {
		t.Fatalf("expected click on Tab 3 to activate Note Graph, got activeTab=%d", m.activeTab)
	}

	// Zoom in (+), pan (H), center (f), reset (0).
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	m = updated.(Model)
	if m.graph.Zoom <= 1.0 {
		t.Fatalf("expected zoom > 1.0 after '+', got %v", m.graph.Zoom)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	m = updated.(Model)
	if m.graph.Zoom != 1.0 || m.graph.PanX != 0 || m.graph.PanY != 0 {
		t.Fatalf("expected '0' to reset zoom/pan, got zoom=%v pan=(%v,%v)", m.graph.Zoom, m.graph.PanX, m.graph.PanY)
	}

	// Click node n2 directly on the canvas.
	n2Idx := m.graph.NodeByID["n2"]
	n2 := m.graph.Nodes[n2Idx]
	updated, _ = m.Update(tea.MouseMsg{
		X:      2 + n2.ScreenX,
		Y:      9 + n2.ScreenY,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	m = updated.(Model)
	if m.graph.SelectedID != "n2" {
		t.Fatalf("expected clicking n2 on canvas to select n2, got %q", m.graph.SelectedID)
	}

	// Click neighbor link (backlink -> n1) in right Graph Inspector pane.
	if len(m.graph.NeighborHits) == 0 {
		t.Fatalf("expected neighbor hitboxes for n2")
	}
	targetHit := m.graph.NeighborHits[0]
	wantID := m.graph.Nodes[targetHit.NodeIdx].ID
	updated, _ = m.Update(tea.MouseMsg{
		X:      m.width - 10,
		Y:      targetHit.ScreenY,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	m = updated.(Model)
	if m.graph.SelectedID != wantID {
		t.Fatalf("expected clicking neighbor row to select %q, got %q", wantID, m.graph.SelectedID)
	}

	// Ensure a memory node is selected, then press Enter to jump back to Inspector.
	m.selectGraphNode(m.graph.NodeByID["n1"])
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.activeTab != 0 {
		t.Fatalf("expected Enter on memory node to switch to Inspector (tab 0), got %d", m.activeTab)
	}
	rec, ok := m.selectedRecord()
	if !ok || rec.Record.ID != "n1" {
		t.Fatalf("expected Inspector cursor synced to n1, got %+v", rec)
	}
}

func TestNoteGraphDragNodeLabelAndCanvasPan(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "chroma_data")
	store, err := chroma.Open(dir)
	if err != nil {
		t.Fatalf("chroma.Open failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	if _, err := store.Save(ctx, "default", "n1", "Alpha Node", map[string]string{"links": "n2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(ctx, "default", "n2", "Beta Node", map[string]string{}); err != nil {
		t.Fatal(err)
	}

	m := NewModel(store, Options{PersistDir: dir})
	updated, _ := m.Update(m.Init()())
	m = updated.(Model)
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 110, Height: 32})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m = updated.(Model)

	// 1. Drag node n1 by its marker glyph and verify it moves & pins.
	n1Idx := m.graph.NodeByID["n1"]
	origScreenX := m.graph.Nodes[n1Idx].ScreenX
	origScreenY := m.graph.Nodes[n1Idx].ScreenY
	startMouseX := 2 + origScreenX
	startMouseY := 9 + origScreenY

	updated, _ = m.Update(tea.MouseMsg{X: startMouseX, Y: startMouseY, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if m.graph.DragMode != dragNode || m.graph.SelectedID != "n1" {
		t.Fatalf("expected dragNode for n1, got mode=%v selected=%q", m.graph.DragMode, m.graph.SelectedID)
	}

	deltaX, deltaY := 8, 3
	if startMouseX+deltaX >= 2+m.graph.CanvasW {
		deltaX = -8
	}
	if startMouseY+deltaY >= 9+m.graph.CanvasH {
		deltaY = -3
	}
	updated, _ = m.Update(tea.MouseMsg{X: startMouseX + deltaX, Y: startMouseY + deltaY, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	n1 := m.graph.Nodes[m.graph.NodeByID["n1"]]
	if n1.ScreenX != origScreenX+deltaX || n1.ScreenY != origScreenY+deltaY {
		t.Fatalf("expected dragged n1 at (%d,%d), got (%d,%d)", origScreenX+deltaX, origScreenY+deltaY, n1.ScreenX, n1.ScreenY)
	}

	// Release drop and confirm position is pinned across rebuild (toggling #tags).
	updated, _ = m.Update(tea.MouseMsg{X: startMouseX + deltaX, Y: startMouseY + deltaY, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if m.graph.DragMode != dragNone {
		t.Fatalf("expected dragNone after release, got %v", m.graph.DragMode)
	}
	pinnedPos, pinned := m.graph.PinnedPositions["n1"]
	if !pinned {
		t.Fatalf("expected n1 position to be pinned after drop")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = updated.(Model)
	n1After := m.graph.Nodes[m.graph.NodeByID["n1"]]
	if math.Abs(n1After.X-pinnedPos[0]) > 1e-9 || math.Abs(n1After.Y-pinnedPos[1]) > 1e-9 {
		t.Fatalf("expected pinned n1 position preserved across rebuild, got (%v,%v) want (%v,%v)", n1After.X, n1After.Y, pinnedPos[0], pinnedPos[1])
	}

	// 2. Drag node n2 via its label hitbox without jumping.
	if len(m.graph.LabelHits) == 0 {
		t.Fatalf("expected label hits on canvas")
	}
	var n2Label graphLabelHit
	foundLabel := false
	n2Idx := m.graph.NodeByID["n2"]
	for _, lh := range m.graph.LabelHits {
		if lh.NodeIdx == n2Idx {
			n2Label = lh
			foundLabel = true
			break
		}
	}
	if !foundLabel {
		t.Fatalf("expected label hit for n2")
	}
	n2OrigSX := m.graph.Nodes[n2Idx].ScreenX
	grabX := 2 + n2Label.StartX + 1
	grabY := 9 + n2Label.Y
	updated, _ = m.Update(tea.MouseMsg{X: grabX, Y: grabY, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	shiftX := 4
	if grabX+shiftX >= 2+m.graph.CanvasW {
		shiftX = -4
	}
	updated, _ = m.Update(tea.MouseMsg{X: grabX + shiftX, Y: grabY, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if m.graph.Nodes[m.graph.NodeByID["n2"]].ScreenX != n2OrigSX+shiftX {
		t.Fatalf("expected label drag to move n2 smoothly by %d cols, got %d -> %d", shiftX, n2OrigSX, m.graph.Nodes[m.graph.NodeByID["n2"]].ScreenX)
	}
	updated, _ = m.Update(tea.MouseMsg{X: grabX + shiftX, Y: grabY, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	m = updated.(Model)

	// 3. Reset layout via 'R' clears pinned positions.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	m = updated.(Model)
	if len(m.graph.PinnedPositions) != 0 {
		t.Fatalf("expected 'R' to clear pinned positions, got %v", m.graph.PinnedPositions)
	}

	// 4. Drag empty canvas background to pan, and release with X10 MouseButtonNone outside canvas.
	emptyX, emptyY := -1, -1
	for cy := 0; cy < m.graph.CanvasH && emptyX == -1; cy++ {
		for cx := 0; cx < m.graph.CanvasW; cx++ {
			if m.hitTestGraphCanvas(cx, cy) == -1 {
				emptyX = 2 + cx
				emptyY = 9 + cy
				break
			}
		}
	}
	if emptyX == -1 {
		t.Fatalf("failed to locate empty canvas cell")
	}
	beforePanX := m.graph.PanX
	beforeNodeSX := m.graph.Nodes[0].ScreenX
	updated, _ = m.Update(tea.MouseMsg{X: emptyX, Y: emptyY, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if m.graph.DragMode != dragPan {
		t.Fatalf("expected dragPan on empty canvas press, got %v", m.graph.DragMode)
	}
	updated, _ = m.Update(tea.MouseMsg{X: emptyX + 6, Y: emptyY, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if m.graph.PanX >= beforePanX || m.graph.Nodes[0].ScreenX <= beforeNodeSX {
		t.Fatalf("expected dragging canvas right to decrease PanX and shift nodes right: pan %v->%v, sx %d->%d",
			beforePanX, m.graph.PanX, beforeNodeSX, m.graph.Nodes[0].ScreenX)
	}
	// Release outside canvas (Y=3, top bar area) with MouseButtonNone (X10 style).
	updated, _ = m.Update(tea.MouseMsg{X: emptyX + 6, Y: 3, Action: tea.MouseActionRelease, Button: tea.MouseButtonNone})
	m = updated.(Model)
	if m.graph.DragMode != dragNone {
		t.Fatalf("expected X10 release outside canvas to end drag, got %v", m.graph.DragMode)
	}
}

func TestNoteGraphDenseLayoutSpacingAndFit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "chroma_data")
	store, err := chroma.Open(dir)
	if err != nil {
		t.Fatalf("chroma.Open failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	tags := []string{"skills-page", "architecture", "drizzle", "better-auth", "swr", "ui", "d1", "editor"}
	for i := 0; i < 20; i++ {
		t1 := tags[i%len(tags)]
		t2 := tags[(i+3)%len(tags)]
		meta := map[string]string{
			"tags":      t1 + "," + t2,
			"node_type": "architecture",
		}
		if i > 0 {
			meta["links"] = fmt.Sprintf("mem-%02d", i-1)
		}
		if _, err := store.Save(ctx, "default", fmt.Sprintf("mem-%02d", i), fmt.Sprintf("Project Node %02d for spacing test", i), meta); err != nil {
			t.Fatal(err)
		}
	}

	m := NewModel(store, Options{PersistDir: dir})
	updated, _ := m.Update(m.Init()())
	m = updated.(Model)
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 128, Height: 38})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m = updated.(Model)

	if len(m.graph.Nodes) != 28 {
		t.Fatalf("expected 28 nodes (20 memories + 8 tags), got %d", len(m.graph.Nodes))
	}

	// Verify world spread expands beyond [0, 1] and no two nodes collapse together.
	minX, maxX := m.graph.Nodes[0].X, m.graph.Nodes[0].X
	minY, maxY := m.graph.Nodes[0].Y, m.graph.Nodes[0].Y
	for i := range m.graph.Nodes {
		a := m.graph.Nodes[i]
		minX = math.Min(minX, a.X)
		maxX = math.Max(maxX, a.X)
		minY = math.Min(minY, a.Y)
		maxY = math.Max(maxY, a.Y)
		for j := i + 1; j < len(m.graph.Nodes); j++ {
			b := m.graph.Nodes[j]
			ellip := math.Hypot((a.X-b.X)/0.24, (a.Y-b.Y)/0.22)
			if ellip < 0.85 {
				t.Fatalf("nodes %s and %s are too close in world space (ellip=%.3f)", a.ID, b.ID, ellip)
			}
			if a.Visible && b.Visible && a.ScreenX == b.ScreenX && a.ScreenY == b.ScreenY {
				t.Fatalf("visible nodes %s and %s overlap at (%d,%d)", a.ID, b.ID, a.ScreenX, a.ScreenY)
			}
		}
	}
	if maxX-minX <= 1.2 || maxY-minY <= 1.2 {
		t.Fatalf("expected expanded world span for 28 nodes, got spanX=%.2f spanY=%.2f", maxX-minX, maxY-minY)
	}

	// Press 'F' to fit all 28 nodes inside the canvas.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	m = updated.(Model)
	for _, n := range m.graph.Nodes {
		if !n.Visible {
			t.Fatalf("expected node %s to be visible after 'F' fit, got screen=(%d,%d) canvas=(%d,%d)",
				n.ID, n.ScreenX, n.ScreenY, m.graph.CanvasW, m.graph.CanvasH)
		}
	}

	// Press '0' to restore 1.0x camera, then click toolbar [F Fit] and [0 1x].
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	m = updated.(Model)
	if m.graph.Zoom != 1.0 || m.graph.PanX != 0 || m.graph.PanY != 0 {
		t.Fatalf("expected '0' to reset zoom=1.0 and pan=(0,0), got zoom=%v pan=(%v,%v)", m.graph.Zoom, m.graph.PanX, m.graph.PanY)
	}
	for _, action := range []string{"fit", "reset"} {
		clicked := false
		for _, btn := range m.graph.ToolbarHits {
			if btn.Action == action {
				updated, _ = m.Update(tea.MouseMsg{X: btn.StartX + 1, Y: 7, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
				m = updated.(Model)
				clicked = true
				break
			}
		}
		if !clicked {
			t.Fatalf("missing toolbar hitbox for %q", action)
		}
	}
	if m.graph.Zoom != 1.0 || m.graph.PanX != 0 || m.graph.PanY != 0 {
		t.Fatalf("expected toolbar [0 1x] to restore default camera, got zoom=%v pan=(%v,%v)", m.graph.Zoom, m.graph.PanX, m.graph.PanY)
	}
}
