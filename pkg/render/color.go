package render

import (
	"os"
	"strings"
	"sync"
)

// ANSI escape sequences.
const (
	ansiReset = "\033[0m"
	ansiRed   = "\033[31m"
	ansiGreen = "\033[32m"
	ansiBold  = "\033[1m"
	ansiDim   = "\033[2m"
)

var (
	colorEnabled bool
	colorOnce    sync.Once
	colorForced  *bool // nil = auto-detect, non-nil = forced value
)

// ForceColor overrides TTY auto-detection. Useful for testing or --color flags.
func ForceColor(enabled bool) {
	colorForced = &enabled
	// Reset the once so next call re-evaluates.
	colorOnce = sync.Once{}
}

// IsTTY reports whether stdout appears to be a terminal.
func IsTTY() bool {
	colorOnce.Do(detectColor)
	return colorEnabled
}

func detectColor() {
	if colorForced != nil {
		colorEnabled = *colorForced
		return
	}

	// Respect NO_COLOR (https://no-color.org).
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		colorEnabled = false
		return
	}

	// Respect FORCE_COLOR.
	if v, ok := os.LookupEnv("FORCE_COLOR"); ok && v != "0" {
		colorEnabled = true
		return
	}

	// Check TERM.
	term := os.Getenv("TERM")
	if term == "dumb" || term == "" {
		colorEnabled = false
		return
	}

	// Stat stdout — if it's a character device, it's a TTY.
	info, err := os.Stdout.Stat()
	if err != nil {
		colorEnabled = false
		return
	}
	colorEnabled = (info.Mode() & os.ModeCharDevice) != 0
}

// wrap applies ANSI code if color is enabled; returns plain string otherwise.
func wrap(code, s string) string {
	if !IsTTY() {
		return s
	}
	return code + s + ansiReset
}

// Green returns s in green if output is a TTY.
func Green(s string) string { return wrap(ansiGreen, s) }

// Red returns s in red if output is a TTY.
func Red(s string) string { return wrap(ansiRed, s) }

// Bold returns s in bold if output is a TTY.
func Bold(s string) string { return wrap(ansiBold, s) }

// Dim returns s in dim/faint if output is a TTY.
func Dim(s string) string { return wrap(ansiDim, s) }

// sectionChar returns the box-drawing character for section headers.
func sectionChar() string {
	if IsTTY() {
		return "═"
	}
	return "="
}

// sectionLine builds a section header line like "═══ Title ═══".
func sectionLine(title string) string {
	ch := sectionChar()
	bar := strings.Repeat(ch, 3)
	return bar + " " + title + " " + bar
}
