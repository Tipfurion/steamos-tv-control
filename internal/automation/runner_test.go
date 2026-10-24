package automation

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tipfurion/steamos-tv-control/internal/config"
)

type executor struct {
	calls    chan string
	canceled chan struct{}
}

func (e executor) ResetConnection() { e.calls <- "reset" }

func (e executor) Execute(ctx context.Context, action string, _ int) error {
	e.calls <- action
	if action == "on" {
		<-ctx.Done()
		close(e.canceled)
		return ctx.Err()
	}
	return nil
}
func TestSleepCancelsWakeAndRebootDoesNothing(t *testing.T) {
	c := config.Default()
	events := make(chan Event)
	e := executor{make(chan string, 4), make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go Run(ctx, events, c, e)
	events <- Event{Kind: "boot"}
	if <-e.calls != "on" {
		t.Fatal("missing wake")
	}
	done := make(chan struct{})
	events <- Event{Kind: "sleep", Budget: time.Second, Done: done}
	select {
	case <-e.canceled:
	case <-ctx.Done():
		t.Fatal("wake not canceled")
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("sleep not acknowledged")
	}
	if <-e.calls != "off" {
		t.Fatal("missing sleep")
	}
	done = make(chan struct{})
	events <- Event{Kind: "reboot", Done: done}
	<-done
	select {
	case v := <-e.calls:
		t.Fatal("reboot issued", v)
	default:
	}
}
func TestBootClaimSurvivesRestart(t *testing.T) {
	p := config.Paths{State: t.TempDir()}
	for i, id := range []string{"boot-a", "boot-a", "boot-b"} {
		got, err := ClaimBoot(p, id)
		if err != nil || got != (i != 1) {
			t.Fatal(got, err)
		}
	}
	b, _ := os.ReadFile(filepath.Join(p.State, "last-boot"))
	if string(b) != "boot-b\n" {
		t.Fatal(string(b))
	}
}

func TestResumeDiscardsOldConnectionBeforeWake(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	e := executor{make(chan string, 4), make(chan struct{})}
	events := make(chan Event, 1)
	events <- Event{Kind: "resume"}
	go Run(ctx, events, config.Default(), e)
	for _, want := range []string{"reset", "on"} {
		select {
		case got := <-e.calls:
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
