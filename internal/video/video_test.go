package video

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseEDID(t *testing.T) {
	// EDIDs read from a Raspberry Pi 4 with a 7" panel and a TV.
	for file, want := range map[string][2]string{
		"mpi7002.edid": {"MPI", "MPI7002"},
		"sky-tv.edid":  {"SKY", "SKY TV"},
	} {
		b, err := os.ReadFile(filepath.Join("testdata", file))
		if err != nil {
			t.Fatal(err)
		}
		if m, name := ParseEDID(b); m != want[0] || name != want[1] {
			t.Errorf("%s: got %q %q, want %q %q", file, m, name, want[0], want[1])
		}
	}
}

func TestParseEDIDRejectsJunk(t *testing.T) {
	for name, b := range map[string][]byte{
		"empty":      nil,
		"short":      edidHeader,
		"bad header": make([]byte, 128),
	} {
		if m, n := ParseEDID(b); m != "" || n != "" {
			t.Errorf("%s: got %q %q", name, m, n)
		}
	}
	// A valid header without a name descriptor and with control characters
	// where the name would be.
	b := make([]byte, 128)
	copy(b, edidHeader)
	b[8], b[9] = 0x4d, 0x79 // SKY
	copy(b[54:], []byte{0, 0, 0, 0xfc, 0, 'T', 'V', 0x07, 0x0a})
	if m, n := ParseEDID(b); m != "SKY" || n != "TV" {
		t.Errorf("got %q %q, want SKY TV", m, n)
	}
}

// fakeDRM builds a /sys/class/drm with the given connectors.
func fakeDRM(t *testing.T, connectors map[string][3]string) string {
	t.Helper()
	dir := t.TempDir()
	tv, err := os.ReadFile(filepath.Join("testdata", "sky-tv.edid"))
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range connectors { // status, enabled, modes
		d := filepath.Join(dir, name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		files := map[string]string{"status": c[0] + "\n", "enabled": c[1] + "\n", "modes": c[2]}
		if name == "card1-HDMI-A-2" {
			files["edid"] = string(tv)
		}
		for f, v := range files {
			if err := os.WriteFile(filepath.Join(d, f), []byte(v), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The GPU itself and the render node are not connectors.
	if err := os.MkdirAll(filepath.Join(dir, "card1"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDisplays(t *testing.T) {
	dir := fakeDRM(t, map[string][3]string{
		"card1-HDMI-A-2":    {"connected", "enabled", "3840x2160\n1920x1080\n"},
		"card1-HDMI-A-1":    {"disconnected", "disabled", ""},
		"card1-Writeback-1": {"unknown", "disabled", ""},
	})
	got := Probe{DRMDir: dir}.Displays()
	want := []Display{
		{Name: "HDMI-A-1"},
		{Name: "HDMI-A-2", Connected: true, Enabled: true, Manufacturer: "SKY", Monitor: "SKY TV", PreferredMode: "3840x2160"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if got := (Probe{}).Displays(); got != nil {
		t.Errorf("no DRM directory: %+v", got)
	}
}

func TestCurrentMain(t *testing.T) {
	f := filepath.Join(t.TempDir(), "switch_screen_current")
	if got := (Probe{CurrentFile: f}).CurrentMain(); got != "" {
		t.Errorf("missing file: %q", got)
	}
	if err := os.WriteFile(f, []byte("HDMI-A-2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := (Probe{CurrentFile: f}).CurrentMain(); got != "HDMI-A-2" {
		t.Errorf("got %q", got)
	}
}

func TestFromConfAndConfValues(t *testing.T) {
	if got := FromConf("", " AUTO "); got != (Settings{Main: Auto, Second: Auto}) {
		t.Errorf("empty and auto: %+v", got)
	}
	s := FromConf("HDMI-A-2", "none")
	if s != (Settings{Main: "HDMI-A-2", Second: Off}) {
		t.Errorf("explicit: %+v", s)
	}
	if m, sec := s.ConfValues(); m != "HDMI-A-2" || sec != "none" {
		t.Errorf("conf values: %q %q", m, sec)
	}
	// "auto" is never written: sway-launch would take it for a screen name.
	if m, sec := (Settings{Main: Auto, Second: Auto}).ConfValues(); m != "" || sec != "" {
		t.Errorf("auto is written as empty, got %q %q", m, sec)
	}
}

func TestValidate(t *testing.T) {
	outputs := []string{"HDMI-A-1", "HDMI-A-2"}
	ok := []Settings{
		{Main: Auto, Second: Auto},
		{Main: Auto, Second: Off},
		{Main: "HDMI-A-2", Second: Auto},
		{Main: "HDMI-A-2", Second: Off},
		{Main: "HDMI-A-2", Second: "HDMI-A-1"},
	}
	for _, s := range ok {
		if err := s.Validate(outputs); err != nil {
			t.Errorf("%+v: %v", s, err)
		}
	}
	bad := []Settings{
		{Main: "DP-1", Second: Auto},
		{Main: "HDMI-A-2", Second: "DP-1"},
		{Main: "HDMI-A-2", Second: "HDMI-A-2"},
		{Main: Auto, Second: "HDMI-A-1"},
		{Main: "", Second: Auto},
	}
	for _, s := range bad {
		if err := s.Validate(outputs); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: got %v, want ErrInvalid", s, err)
		}
	}
}

func TestPlanFor(t *testing.T) {
	// The console lists the TV first: it was lit first at boot.
	outputs := []string{"HDMI-A-2", "HDMI-A-1"}
	cases := []struct {
		s    Settings
		want Plan
	}{
		{Settings{Main: Auto, Second: Auto}, Plan{Main: "HDMI-A-1", Second: "HDMI-A-2"}},
		{Settings{Main: Auto, Second: Off}, Plan{Main: "HDMI-A-1"}},
		{Settings{Main: "HDMI-A-2", Second: Auto}, Plan{Main: "HDMI-A-2", Second: "HDMI-A-1"}},
		{Settings{Main: "HDMI-A-2", Second: Off}, Plan{Main: "HDMI-A-2"}},
		{Settings{Main: "HDMI-A-2", Second: "HDMI-A-1"}, Plan{Main: "HDMI-A-2", Second: "HDMI-A-1"}},
		// What Batocera does with choices it can't use.
		{Settings{Main: "DP-1", Second: Auto}, Plan{Main: "HDMI-A-1", Second: "HDMI-A-2"}},
		{Settings{Main: "DP-1", Second: "HDMI-A-1"}, Plan{Main: "HDMI-A-1", Second: "HDMI-A-2"}},
		{Settings{Main: "HDMI-A-2", Second: "HDMI-A-2"}, Plan{Main: "HDMI-A-2", Second: "HDMI-A-1"}},
		{Settings{Main: "HDMI-A-2", Second: "DP-1"}, Plan{Main: "HDMI-A-2", Second: "HDMI-A-1"}},
	}
	for _, c := range cases {
		if got := PlanFor(c.s, outputs); got != c.want {
			t.Errorf("%+v: got %+v, want %+v", c.s, got, c.want)
		}
	}
	if got := PlanFor(Settings{Main: Auto, Second: Auto}, []string{"HDMI-A-1"}); got != (Plan{Main: "HDMI-A-1"}) {
		t.Errorf("one screen: %+v", got)
	}
	if got := PlanFor(Settings{Main: Auto, Second: Auto}, nil); got != (Plan{}) {
		t.Errorf("no screens: %+v", got)
	}
}

func TestDescribe(t *testing.T) {
	names := []string{"HDMI-A-2", "HDMI-A-1"}
	displays := []Display{
		{Name: "HDMI-A-1", Connected: true, Enabled: true, Manufacturer: "MPI", Monitor: "MPI7002", PreferredMode: "1920x1080"},
		{Name: "HDMI-A-2", Connected: true, Enabled: true, Manufacturer: "SKY", Monitor: "SKY TV", PreferredMode: "3840x2160"},
	}
	tvMain := Settings{Main: "HDMI-A-2", Second: Auto}

	st := Describe(names, displays, "HDMI-A-2", tvMain)
	if st.RestartNeeded {
		t.Error("the screens in use match the settings")
	}
	if len(st.Outputs) != 2 || st.Outputs[0].Role != RoleMain || st.Outputs[1].Role != RoleSecond {
		t.Fatalf("roles: %+v", st.Outputs)
	}
	if o := st.Outputs[0]; o.Monitor != "SKY TV" || o.Manufacturer != "SKY" || o.PreferredMode != "3840x2160" || o.Active == nil || !*o.Active {
		t.Errorf("TV: %+v", o)
	}
	if st.Next != (Plan{Main: "HDMI-A-2", Second: "HDMI-A-1"}) {
		t.Errorf("next: %+v", st.Next)
	}

	// Turning the second screen off is pending until the console restarts...
	off := Settings{Main: "HDMI-A-2", Second: Off}
	if st := Describe(names, displays, "HDMI-A-2", off); !st.RestartNeeded {
		t.Error("the 7\" panel is still on: a restart is needed")
	}
	// ...and done once it is dark.
	displays[0].Enabled = false
	st = Describe(names, displays, "HDMI-A-2", off)
	if st.RestartNeeded || st.Outputs[1].Role != RoleOff {
		t.Errorf("after the restart: needed %v, roles %+v", st.RestartNeeded, st.Outputs)
	}

	// A different main screen needs a restart.
	if st := Describe(names, displays, "HDMI-A-1", tvMain); !st.RestartNeeded {
		t.Error("main screen in use is the panel: a restart is needed")
	}
}

func TestDescribeWithUnknownState(t *testing.T) {
	// No kernel information (dev mode, or names that differ under X11) and no
	// main screen on record: nothing is guessed.
	st := Describe([]string{"HDMI-1", "DP-1"}, nil, "", Settings{Main: "DP-1", Second: Off})
	if st.RestartNeeded {
		t.Error("unknown state must not ask for a restart")
	}
	for _, o := range st.Outputs {
		if o.Role != "" || o.Active != nil {
			t.Errorf("%s: role %q, active %v", o.Name, o.Role, o.Active)
		}
	}
	if got := Describe(nil, nil, "", Settings{Main: Auto, Second: Auto}); got.Outputs == nil {
		t.Error("outputs must be an empty list, not null")
	}
}
