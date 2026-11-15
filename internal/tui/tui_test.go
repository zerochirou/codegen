package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zerochirou/codegen/internal/chroma"
)

func TestTUIModelLoadAndRender(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "chroma_data")
	store, err := chroma.Open(dir)
	if err != nil {
		t.Fatalf("chroma.Open failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	_, err = store.Save(context.Background(), "default", "doc-1", "Bubble Tea Go TUI inspector", map[string]string{
		"tags":      "ui,golang",
		"node_type": "architecture",
	})
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(store, Options{
		PersistDir: dir,
		IsGlobal:   false,
	})
	msg := m.Init()()
	updated, _ := m.Update(msg)
	m = updated.(Model)

	updated, _ = m.Update(tea.WindowSizeMsg{Width: 110, Height: 32})
	m = updated.(Model)

	view := m.View()
	if !strings.Contains(view, "PROJECT") || !strings.Contains(view, "1 memory") {
		t.Fatalf("unexpected inspector view:\n%s", view)
	}

	// Switch to Session Context tab
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	ctxView := m.View()
	if !strings.Contains(ctxView, "ChromaDB Context") || !strings.Contains(ctxView, "ui:") {
		t.Fatalf("unexpected context map view:\n%s", ctxView)
	}

	// Switch to Note Graph tab
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	graphView := m.View()
	if m.activeTab != 2 || !strings.Contains(graphView, "3 Note Graph") || !strings.Contains(graphView, "Bubble Tea Go TUI") {
		t.Fatalf("unexpected note graph view:\n%s", graphView)
	}
}

func TestTUIMouseInteractions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "chroma_data")
	store, err := chroma.Open(dir)
	if err != nil {
		t.Fatalf("chroma.Open failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	for _, item := range []struct {
		id, content, tags string
	}{
		{"doc-a", "First memory content for mouse test", "alpha"},
		{"doc-b", "Second memory content for mouse test", "beta"},
	} {
		if _, err := store.Save(context.Background(), "default", item.id, item.content, map[string]string{"tags": item.tags}); err != nil {
			t.Fatal(err)
		}
	}

	m := NewModel(store, Options{PersistDir: dir})
	updated, _ := m.Update(m.Init()())
	m = updated.(Model)
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 110, Height: 32})
	m = updated.(Model)

	// Verify rendered Y layout matches mouse hitboxes.
	lines := strings.Split(m.View(), "\n")
	if len(lines) <= 9 {
		t.Fatalf("expected at least 10 rendered lines, got %d", len(lines))
	}
	if !strings.Contains(lines[3], "↻ Reload") {
		t.Fatalf("expected Reload button on line 3, got %q", lines[3])
	}
	if !strings.Contains(lines[5], "1 Inspector") || !strings.Contains(lines[5], "2 Session Context") || !strings.Contains(lines[5], "3 Note Graph") {
		t.Fatalf("expected tab bar on line 5, got %q", lines[5])
	}

	// Click Tab 2 ("2 Session Context") and back to Tab 1 ("1 Inspector").
	updated, _ = m.Update(tea.MouseMsg{X: 20, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if m.activeTab != 1 || !strings.Contains(m.View(), "ChromaDB Context") {
		t.Fatalf("expected click on Tab 2 to activate Session Context, got activeTab=%d", m.activeTab)
	}

	updated, _ = m.Update(tea.MouseMsg{X: 4, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if m.activeTab != 0 {
		t.Fatalf("expected click on Tab 1 to activate Inspector, got activeTab=%d", m.activeTab)
	}

	// Click second row in left pane (Y = 9).
	initialID := m.records[m.cursor].Record.ID
	updated, _ = m.Update(tea.MouseMsg{X: 6, Y: 9, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if m.cursor != 1 || m.records[m.cursor].Record.ID == initialID {
		t.Fatalf("expected click on row 2 (Y=9) to select second memory, cursor=%d", m.cursor)
	}

	// Wheel up returns to first row; wheel down advances back.
	updated, _ = m.Update(tea.MouseMsg{X: 6, Y: 9, Button: tea.MouseButtonWheelUp})
	m = updated.(Model)
	if m.cursor != 0 {
		t.Fatalf("expected wheel up on left pane to select cursor=0, got %d", m.cursor)
	}
	updated, _ = m.Update(tea.MouseMsg{X: 6, Y: 9, Button: tea.MouseButtonWheelDown})
	m = updated.(Model)
	if m.cursor != 1 {
		t.Fatalf("expected wheel down on left pane to select cursor=1, got %d", m.cursor)
	}

	// Click search box to focus, then click Reload button to blur and reload.
	updated, _ = m.Update(tea.MouseMsg{X: 5, Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if !m.searching {
		t.Fatalf("expected click on search box to focus search")
	}

	top := m.renderTopBarParts()
	updated, cmd := m.Update(tea.MouseMsg{X: top.reloadStartX + 2, Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if m.searching {
		t.Fatalf("expected click outside search box to blur search")
	}
	if m.statusText != "Memories reloaded." || cmd == nil {
		t.Fatalf("expected reload button click to trigger reload command, status=%q cmd=%v", m.statusText, cmd != nil)
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if len(m.records) != 2 {
		t.Fatalf("expected 2 records after reload, got %d", len(m.records))
	}
}
