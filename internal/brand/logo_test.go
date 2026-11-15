package brand

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLogoLinesUniformWidth(t *testing.T) {
	lines := LogoLines()
	if len(lines) != 6 {
		t.Fatalf("expected 6 lines, got %d", len(lines))
	}
	wantWidth := utf8.RuneCountInString(lines[0])
	if wantWidth != 60 {
		t.Fatalf("expected width 60, got %d", wantWidth)
	}
	for i, line := range lines {
		if got := utf8.RuneCountInString(line); got != wantWidth {
			t.Fatalf("line %d rune width = %d, want %d: %q", i, got, wantWidth, line)
		}
	}
}

func TestBannerPlainWhenNotTTY(t *testing.T) {
	var buf bytes.Buffer
	got := Banner(&buf)
	if strings.Contains(got, "\033[") {
		t.Fatalf("expected no ANSI escapes for non-TTY buffer, got %q", got)
	}
	if !strings.Contains(got, "██████╗") {
		t.Fatalf("expected CODEGEN block glyphs in banner:\n%s", got)
	}
}
