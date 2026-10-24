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

func TestChangingHDMIDoesNotReuseAnotherPortsLink(t *testing.T) {
	c := config.Default()
	c.HDMI = 2
	c.InputMode = "app-link"
	c.InputURI = "content://android.media.tv/passthrough/com.tcl.tvinput%2F.TvPassThroughService%2FHW16"
	c.InputVerified = true
	m := &model{ctx: context.Background(), c: c, screen: "hdmi", input: textinput.New()}
	m.input.SetValue("3")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.c.HDMI != 3 || m.c.InputMode != "direct" || m.c.InputURI != "" || m.c.InputVerified {
		t.Fatal("HDMI 2 link was retained for HDMI 3", m.c)
	}
}
