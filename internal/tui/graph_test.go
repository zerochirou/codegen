package tui

import (
	"context"
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
