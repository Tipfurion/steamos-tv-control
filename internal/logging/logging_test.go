package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestJournalPriorityAndNoDuplicateTimestamp(t *testing.T) {
	var b bytes.Buffer
	l := slog.New(Handler(&b, true, slog.LevelDebug))
	l.Info("TV action completed", "action", "off")
	l.Warn("TV unavailable")
	l.Error("Pairing failed")
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	for i, prefix := range []string{"<6>level=INFO", "<4>level=WARN", "<3>level=ERROR"} {
		if !strings.HasPrefix(lines[i], prefix) || strings.Contains(lines[i], "time=") {
			t.Fatal(lines[i])
		}
	}
}
