package audio

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/monteirohat/batox/internal/video"
)

// Fixtures read from a Raspberry Pi 4 with a 7" panel on HDMI0 (ALSA card 1)
// and a TV on HDMI1 (card 2).
const (
	jack  = "alsa_output._sys_devices_platform_soc_fe00b840.mailbox_bcm2835-audio_sound_card0.stereo-fallback"
	hdmi0 = "alsa_output._sys_devices_platform_soc_fef00700.hdmi_sound_card1.hdmi-stereo"
	hdmi1 = "alsa_output._sys_devices_platform_soc_fef05700.hdmi_sound_card2.hdmi-stereo"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseSinks(t *testing.T) {
	sinks, err := ParseSinks(fixture(t, "pi4-sinks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sinks) != 3 {
		t.Fatalf("got %d sinks", len(sinks))
	}
	want := []struct {
		name, cardName, port, kind string
		card, volume               int
		available                  *bool
	}{
		{jack, "bcm2835 Headphones", "Analog", KindHeadphones, 0, 40, nil},
		{hdmi0, "vc4-hdmi-0", "HDMI", KindHDMI, 1, 79, ptr(true)},
		{hdmi1, "vc4-hdmi-1", "HDMI", KindHDMI, 2, 40, ptr(true)},
	}
	for i, w := range want {
		s := sinks[i]
		if s.Name != w.name || s.CardName != w.cardName || s.PortType != w.port || s.Kind() != w.kind || s.Card != w.card || s.Volume != w.volume {
			t.Errorf("sink %d: %+v (kind %s)", i, s, s.Kind())
		}
		if (s.Available == nil) != (w.available == nil) || (s.Available != nil && *s.Available != *w.available) {
			t.Errorf("sink %d available: %v", i, s.Available)
		}
	}
	if _, err := ParseSinks([]byte("Sink #1")); err == nil {
		t.Error("text output must be rejected")
	}
}

func ptr[T any](v T) *T { return &v }

func TestKind(t *testing.T) {
	cases := map[string]Sink{
		KindBluetooth:  {Name: "bluez_output.AA_BB.1"},
		KindUSB:        {Name: "alsa_output.usb-Logitech.analog-stereo", Bus: "usb", PortType: "Analog"},
		KindHDMI:       {Name: "alsa_output.pci-0000_00_1f.3.hdmi-stereo-extra1", PortType: "HDMI"},
		KindHeadphones: {Name: "alsa_output.pci-0000_00_1f.3.analog-stereo", PortType: "Headphones"},
		KindSpeakers:   {Name: "alsa_output.pci-0000_00_1f.3.analog-stereo", PortType: "Speaker"},
		KindAnalog:     {Name: "alsa_output.pci-0000_00_1f.3.analog-stereo", PortType: "Line"},
		KindOther:      {Name: "something"},
	}
	for want, s := range cases {
		if got := s.Kind(); got != want {
			t.Errorf("%s: got %s, want %s", s.Name, got, want)
		}
	}
}

func TestELD(t *testing.T) {
	if id := ELDControl(string(fixture(t, "vc4hdmi0-controls.txt"))); id != 5 {
		t.Errorf("ELD control of an HDMI card: %d, want 5", id)
	}
	if id := ELDControl(string(fixture(t, "headphones-controls.txt"))); id != 0 {
		t.Errorf("the headphone card has no ELD: got %d", id)
	}
	two := "numid=5,iface=PCM,name='ELD'\nnumid=11,iface=PCM,name='ELD',device=3\n"
	if id := ELDControl(two); id != 0 {
		t.Errorf("several ELDs can't be told apart: got %d", id)
	}

	for file, want := range map[string]Monitor{
		"vc4hdmi0-eld.txt": {"MPI", "MPI7002"},
		"vc4hdmi1-eld.txt": {"SKY", "SKY TV"},
	} {
		b := ParseAmixerValues(string(fixture(t, file)))
		if len(b) != 128 {
			t.Fatalf("%s: %d bytes", file, len(b))
		}
		if m, n := ParseELD(b); m != want.Manufacturer || n != want.Name {
			t.Errorf("%s: got %q %q", file, m, n)
		}
	}

	// Nothing plugged in: the ELD is all zeros.
	if m, n := ParseELD(make([]byte, 128)); m != "" || n != "" {
		t.Errorf("empty ELD: %q %q", m, n)
	}
	if b := ParseAmixerValues("  : values=0x10,zz"); b != nil {
		t.Errorf("bad values: %v", b)
	}
}

func TestDescribe(t *testing.T) {
	sinks, err := ParseSinks(fixture(t, "pi4-sinks.json"))
	if err != nil {
		t.Fatal(err)
	}
	monitors := map[int]Monitor{1: {"MPI", "MPI7002"}, 2: {"SKY", "SKY TV"}}
	displays := []video.Display{
		{Name: "HDMI-A-1", Connected: true, Enabled: true, Manufacturer: "MPI", Monitor: "MPI7002"},
		{Name: "HDMI-A-2", Connected: true, Enabled: true, Manufacturer: "SKY", Monitor: "SKY TV"},
	}
	st := Describe(sinks, monitors, displays, "HDMI-A-2", hdmi0, "")
	if st.Setting != Auto || st.Current != hdmi0 || len(st.Outputs) != 3 {
		t.Fatalf("state: %+v", st)
	}
	jackOut, panel, tv := st.Outputs[0], st.Outputs[1], st.Outputs[2]
	if jackOut.Label != "Headphone jack" || jackOut.Kind != KindHeadphones || jackOut.Screen != "" || jackOut.Connected != nil {
		t.Errorf("jack: %+v", jackOut)
	}
	if panel.Label != "MPI7002" || panel.Screen != "HDMI-A-1" || panel.ScreenRole != video.RoleSecond || !panel.Current {
		t.Errorf("panel: %+v", panel)
	}
	if tv.Label != "SKY TV" || tv.Screen != "HDMI-A-2" || tv.ScreenRole != video.RoleMain || tv.Current {
		t.Errorf("TV: %+v", tv)
	}

	// Without ELD or screens, HDMI outputs are told apart by their card.
	st = Describe(sinks, nil, nil, "", "", "auto")
	if st.Outputs[1].Label != "HDMI (vc4-hdmi-0)" || st.Outputs[2].Label != "HDMI (vc4-hdmi-1)" || st.Outputs[2].ScreenRole != "" {
		t.Errorf("no ELD: %+v", st.Outputs)
	}
	// A screen that is turned off.
	displays[0].Enabled = false
	st = Describe(sinks, monitors, displays, "HDMI-A-2", hdmi1, hdmi1)
	if st.Outputs[1].ScreenRole != video.RoleOff || st.Setting != hdmi1 || !st.Outputs[2].Current {
		t.Errorf("panel off: %+v", st.Outputs[1])
	}
	if got := Describe(nil, nil, nil, "", "", ""); got.Outputs == nil {
		t.Error("outputs must be an empty list, not null")
	}
}
