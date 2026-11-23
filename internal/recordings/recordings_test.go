package recordings

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/monteirohat/batox/internal/logx"
)

func newService(t *testing.T) (*Service, *Fake, string) {
	t.Helper()
	log, err := logx.New(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(log.Close)
	dir := filepath.Join(t.TempDir(), "recordings")
	rec := &Fake{Dir: dir}
	return New(dir, t.TempDir(), rec, log), rec, dir
}

func write(t *testing.T, dir, name string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(p, when, when); err != nil {
		t.Fatal(err)
	}
}

func TestStatusListsVideosNewestFirst(t *testing.T) {
	s, _, dir := newService(t)

	st, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !st.Available || st.Recording || len(st.Files) != 0 || st.Settings != DefaultSettings {
		t.Fatalf("empty folder: %+v", st)
	}

	write(t, dir, "rec-2026.11.20-10h00.00.mkv", 3*time.Hour)
	write(t, dir, "clip.mp4", 2*time.Hour)
	write(t, dir, "rec-2026.11.23-20h36.14.mkv", time.Hour)
	write(t, dir, "readme.txt", 0)
	write(t, dir, ".hidden.mkv", 0)
	if err := os.Mkdir(filepath.Join(dir, "folder.mkv"), 0o755); err != nil {
		t.Fatal(err)
	}

	st, err = s.Status()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range st.Files {
		names = append(names, f.Name)
	}
	want := []string{"rec-2026.11.23-20h36.14.mkv", "clip.mp4", "rec-2026.11.20-10h00.00.mkv"}
	if len(names) != len(want) {
		t.Fatalf("files = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("files = %v, want %v", names, want)
		}
	}
	started, _ := time.ParseInLocation("2006.01.02-15h04.05", "2026.11.23-20h36.14", time.Local)
	if st.Files[0].StartedAt != started.UTC().Format(time.RFC3339) {
		t.Errorf("startedAt = %q", st.Files[0].StartedAt)
	}
	if st.Files[1].StartedAt != "" {
		t.Errorf("a file without batocera-record's name has no start time: %q", st.Files[1].StartedAt)
	}
	if st.Files[0].Active || st.Current != nil {
		t.Error("nothing is active while not recording")
	}
}

func TestStartAndStop(t *testing.T) {
	s, rec, _ := newService(t)

	if _, err := s.Stop(); !errors.Is(err, ErrNotRecording) {
		t.Errorf("stop with nothing running: %v", err)
	}
	if _, err := s.Start(&Settings{Quality: "best", Audio: "game"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown quality: %v", err)
	}

	st, err := s.Start(&Settings{Quality: "high", Audio: "none", AllScreens: true})
	if err != nil {
		t.Fatal(err)
	}
	if !st.Recording || st.Current == nil || !st.Current.Active {
		t.Fatalf("after start: %+v", st)
	}
	if got := rec.LastSettings(); got != (Settings{Quality: "high", Audio: "none", AllScreens: true}) {
		t.Errorf("recorder got %+v", got)
	}
	if s.Settings().Quality != "high" {
		t.Error("the settings are kept for the next recordings")
	}
	if _, err := s.Start(nil); !errors.Is(err, ErrBusy) {
		t.Errorf("second start: %v", err)
	}
	if err := s.Delete(st.Current.Name); !errors.Is(err, ErrInUse) {
		t.Errorf("deleting the file being written: %v", err)
	}

	st, err = s.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if st.Recording || st.Current != nil || len(st.Files) != 1 || st.Files[0].Active {
		t.Fatalf("after stop: %+v", st)
	}
	if err := s.Delete(st.Files[0].Name); err != nil {
		t.Errorf("deleting after stop: %v", err)
	}
}

func TestStartFailsWithoutRecorder(t *testing.T) {
	s, rec, _ := newService(t)
	rec.Unavailable = true
	if _, err := s.Start(nil); !errors.Is(err, ErrUnavailable) {
		t.Errorf("start: %v", err)
	}
	st, err := s.Status()
	if err != nil || st.Available {
		t.Errorf("status: %+v %v", st, err)
	}

	rec.Unavailable = false
	rec.StartErr = errors.New("wf-recorder failed to start")
	if _, err := s.Start(nil); err == nil || err.Error() != "wf-recorder failed to start" {
		t.Errorf("recorder failure: %v", err)
	}
}

func TestResolveStaysInsideFolder(t *testing.T) {
	s, _, dir := newService(t)
	write(t, dir, "rec-2026.11.23-20h36.14.mkv", 0)
	outside := filepath.Join(filepath.Dir(dir), "secret.mkv")
	write(t, filepath.Dir(dir), "secret.mkv", 0)

	if p, err := s.Resolve("rec-2026.11.23-20h36.14.mkv"); err != nil || p != filepath.Join(dir, "rec-2026.11.23-20h36.14.mkv") {
		t.Errorf("good name: %q %v", p, err)
	}
	for _, bad := range []string{"", "../secret.mkv", "/" + outside, `..\secret.mkv`, "sub/x.mkv", " rec-2026.11.23-20h36.14.mkv", "missing.mkv", "readme.txt", ".hidden.mkv"} {
		if _, err := s.Resolve(bad); !errors.Is(err, ErrNotFound) {
			t.Errorf("%q: %v, want ErrNotFound", bad, err)
		}
	}
	if err := s.Delete("../secret.mkv"); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete outside: %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Error("the file outside the folder must stay")
	}
}

func TestSettingsFallBackToDefaults(t *testing.T) {
	s, _, _ := newService(t)
	if err := os.WriteFile(s.settingsFile, []byte(`{"quality":"best","audio":"game"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if s.Settings() != DefaultSettings {
		t.Errorf("invalid saved settings: %+v", s.Settings())
	}
	if err := s.SaveSettings(Settings{Quality: "low", Audio: "mic"}); err != nil {
		t.Fatal(err)
	}
	if s.Settings() != (Settings{Quality: "low", Audio: "mic"}) {
		t.Errorf("saved: %+v", s.Settings())
	}
}

func TestLastLogLine(t *testing.T) {
	log := filepath.Join(t.TempDir(), "recording.log")
	content := "2026-11-23 20:34:18 - Recording primary output:  -> /userdata/recordings/rec.mkv\n" +
		"failed to create display: No such file or directory\n" +
		"2026-11-23 20:34:18 - wf-recorder failed to start.\n"
	if err := os.WriteFile(log, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &Script{LogFile: log}
	if got := r.lastLogLine(); got != "failed to create display: No such file or directory wf-recorder failed to start." {
		t.Errorf("lastLogLine = %q", got)
	}
}
