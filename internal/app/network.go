package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// NetworkManager waits for pre-down handlers before disconnecting Wi-Fi.
// The existing lifecycle job and this request share the controller's gate;
// once standby is confirmed, a second request observes it without toggling.
func networkSleepHandler(autoOff bool, hdmi int, budget time.Duration, execute func(context.Context, string, int) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !autoOff {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		slog.Info("NetworkManager is waiting for TV standby")
		ctx, cancel := context.WithTimeout(req.Context(), budget)
		defer cancel()
		if err := execute(ctx, "off", hdmi); err != nil {
			slog.Warn("NetworkManager standby action failed", "error", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
