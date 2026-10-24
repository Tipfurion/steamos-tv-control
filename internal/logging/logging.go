package logging

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Configure keeps terminal logs readable and lets journald attach its own
// timestamp and parse the syslog priority prefix into PRIORITY.
func Configure() {
	level := slog.LevelInfo
	if strings.EqualFold(os.Getenv("STEAMOS_TV_CONTROL_LOG_LEVEL"), "debug") {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(Handler(os.Stderr, os.Getenv("JOURNAL_STREAM") != "", level)))
}
func Handler(w io.Writer, journal bool, level slog.Level) slog.Handler {
	if journal {
		w = journalWriter{w}
	}
	return slog.NewTextHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey && journal {
			return slog.Attr{}
		}
		return a
	}})
}

type journalWriter struct{ io.Writer }

func (w journalWriter) Write(p []byte) (int, error) {
	priority := "<6>"
	switch {
	case bytes.HasPrefix(p, []byte("level=ERROR")):
		priority = "<3>"
	case bytes.HasPrefix(p, []byte("level=WARN")):
		priority = "<4>"
	case bytes.HasPrefix(p, []byte("level=DEBUG")):
		priority = "<7>"
	}
	payload := append([]byte(priority), p...)
	n, err := w.Writer.Write(payload)
	return max(0, n-len(priority)), err
}
