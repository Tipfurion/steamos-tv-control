package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNetworkSleepHandlerHonorsAutomationAndBudget(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		calls := 0
		h := networkSleepHandler(enabled, 2, time.Second, func(ctx context.Context, action string, hdmi int) error {
			calls++
			if action != "off" || hdmi != 2 {
				t.Fatal(action, hdmi)
			}
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > time.Second {
				t.Fatal("missing bounded standby deadline")
			}
			return nil
		})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/network/pre-down", nil))
		want := 0
		if enabled {
			want = 1
		}
		if w.Code != http.StatusNoContent || calls != want {
			t.Fatal(w.Code, calls, want)
		}
	}
}

func TestNetworkSleepHandlerReleasesNetworkAfterDeadline(t *testing.T) {
	h := networkSleepHandler(true, 2, 20*time.Millisecond, func(ctx context.Context, _ string, _ int) error { <-ctx.Done(); return ctx.Err() })
	w := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/network/pre-down", nil))
	if w.Code != http.StatusBadGateway || time.Since(start) > time.Second {
		t.Fatal(w.Code, time.Since(start))
	}
}
