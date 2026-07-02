package config

import (
	"io"
	"log/slog"
	"os"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorGreen  = "\033[32m"
	colorGray   = "\033[90m"
	colorBold   = "\033[1m"
)

// levelColors maps slog levels to ANSI color codes
var levelColors = map[slog.Level]string{
	slog.LevelDebug: colorGray,
	slog.LevelInfo:  colorGreen,
	slog.LevelWarn:  colorYellow,
	slog.LevelError: colorRed + colorBold,
}

// ColorWriter wraps an io.Writer and colorizes lines based on detected log level.
type ColorWriter struct {
	inner    io.Writer
	colorize bool
}

// NewColorWriter creates a writer that detects log levels from slog output
// and wraps them with ANSI colors. Colors are disabled if not a terminal.
func NewColorWriter(out io.Writer) *ColorWriter {
	return &ColorWriter{
		inner:    out,
		colorize: isTerminal(out),
	}
}

func (w *ColorWriter) Write(p []byte) (int, error) {
	if !w.colorize {
		return w.inner.Write(p)
	}

	levelColor := ""
	text := string(p)

	switch {
	case contains(text, "level=DEBUG"):
		levelColor = levelColors[slog.LevelDebug]
	case contains(text, "level=INFO"):
		levelColor = levelColors[slog.LevelInfo]
	case contains(text, "level=WARN"):
		levelColor = levelColors[slog.LevelWarn]
	case contains(text, "level=ERROR"):
		levelColor = levelColors[slog.LevelError]
	}

	if levelColor != "" {
		colored := levelColor + text + colorReset
		_, err := w.inner.Write([]byte(colored))
		return len(p), err
	}

	return w.inner.Write(p)
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// isTerminal checks if the writer is a terminal (TTY).
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
