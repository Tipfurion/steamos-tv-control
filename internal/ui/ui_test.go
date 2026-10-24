package ui

import (
	"context"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/Tipfurion/steamos-tv-control/internal/config"
)

func TestSequenceRejectsPowerAndParsesDelays(t *testing.T) {
	if _, err := ParseSequence("POWER@100"); err == nil {
		t.Fatal("accepted power in input macro")
	}
	v, err := ParseSequence("TV_INPUT@700,DPAD_CENTER@200")
	if err != nil || len(v) != 2 || v[0].DelayMS != 700 {
		t.Fatal(v, err)
	}
}
func TestEscapeDoesNotSavePartialSetup(t *testing.T) {
	m := &model{ctx: context.Background(), c: config.Default(), screen: "host", input: textinput.New()}
	m.input.SetValue("unfinished")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.screen != "menu" || cmd != nil {
		t.Fatal("escape did not cancel")
	}
}

func TestPairingResultRetainsIdentityBeforeNextScreen(t *testing.T) {
	c := config.Default()
	c.Host = "tv"
	c.CertDir = "identity-new"
	m := &model{ctx: context.Background(), c: config.Default(), screen: "pin", input: textinput.New(), certDir: "identity-new"}
	m.Update(result{config: &c, next: "paired"})
	if m.c.CertDir != "identity-new" || m.certDir != "" || !m.wizard {
		t.Fatal("paired identity not retained")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.screen != "power" {
		t.Fatal(m.screen)
	}
}
