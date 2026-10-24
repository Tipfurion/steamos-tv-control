package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Tipfurion/steamos-tv-control/internal/control"
)

func Client(socket string) *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}, Timeout: 65 * time.Second}
}
func Status(ctx context.Context, socket string) (control.Status, error) {
	var s control.Status
	req, err := http.NewRequestWithContext(ctx, "GET", "http://local/status", nil)
	if err != nil {
		return s, err
	}
	client := Client(socket)
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return s, err
	}
	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(&s)
	return s, err
}
func Command(ctx context.Context, socket, action string, hdmi int) (control.Status, error) {
	var result struct {
		Status control.Status `json:"status"`
		Error  string         `json:"error,omitempty"`
	}
	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("http://local/command?action=%s&hdmi=%d", action, hdmi), nil)
	if err != nil {
		return result.Status, err
	}
	client := Client(socket)
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return result.Status, err
	}
	defer resp.Body.Close()
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return result.Status, err
	}
	if result.Error != "" {
		return result.Status, errors.New(result.Error)
	}
	return result.Status, nil
}
func Listen(socket string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(filepath.Dir(socket), 0700); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(socket); err == nil {
		conn, err := net.DialTimeout("unix", socket, 300*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil, errors.New("daemon already running")
		}
		info, err := os.Lstat(socket)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("refusing to remove non-socket file")
		}
		if err = os.Remove(socket); err != nil {
			return nil, err
		}
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(socket, 0600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}
