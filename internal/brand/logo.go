package brand

import (
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

const ansiReset = "\033[0m"

// Subtle top-to-bottom monochrome silver gradient matching ANSI Shadow block banners.
var bannerPalette = []string{
	"\033[38;5;255m",
	"\033[38;5;251m",
	"\033[38;5;247m",
	"\033[38;5;244m",
	"\033[38;5;241m",
	"\033[38;5;238m",
}

var logoLines = []string{
	" ██████╗ ██████╗ ██████╗ ███████╗ ██████╗ ███████╗███╗   ██╗",
	"██╔════╝██╔═══██╗██╔══██╗██╔════╝██╔════╝ ██╔════╝████╗  ██║",
	"██║     ██║   ██║██║  ██║█████╗  ██║  ███╗█████╗  ██╔██╗ ██║",
	"██║     ██║   ██║██║  ██║██╔══╝  ██║   ██║██╔══╝  ██║╚██╗██║",
	"╚██████╗╚██████╔╝██████╔╝███████╗╚██████╔╝███████╗██║ ╚████║",
	" ╚═════╝ ╚═════╝ ╚═════╝ ╚══════╝ ╚═════╝ ╚══════╝╚═╝  ╚═══╝",
}

// LogoLines returns the 6-row ANSI Shadow ASCII art for "CODEGEN".
func LogoLines() []string {
	out := make([]string, len(logoLines))
	copy(out, logoLines)
	return out
}

// Banner renders the CODEGEN banner for w, applying monochrome shading when w is a color-capable TTY.
func Banner(w io.Writer) string {
	lines := LogoLines()
	if !supportsColor(w) {
		return strings.Join(lines, "\n")
	}
	for i, line := range lines {
		lines[i] = bannerPalette[i%len(bannerPalette)] + line + ansiReset
	}
	return strings.Join(lines, "\n")
}

func supportsColor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
