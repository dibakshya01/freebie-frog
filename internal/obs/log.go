// Package obs provides observability helpers (structured logging for now).
package obs

import (
	"log/slog"
	"os"
	"strings"
)

// NewLogger builds a slog.Logger writing to stderr.
//
// level is one of debug|info|warn|error and format is text|json; unrecognized
// values fall back to info and text respectively.
func NewLogger(level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var handler slog.Handler
	if strings.EqualFold(format, "json") {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	return slog.New(handler)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
