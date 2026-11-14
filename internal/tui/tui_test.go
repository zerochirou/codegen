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
}
