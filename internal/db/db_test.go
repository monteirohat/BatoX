package db

import (
	"path/filepath"
	"testing"
)

func TestRomCRUD(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	rom := Rom{ID: "abc123", System: "gba", Name: "Game.gba", Path: "/roms/gba/Game.gba", Size: 10, MTime: 1, Language: "en"}
	if err := s.InsertRom(rom); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertRom(rom); err == nil {
		t.Error("inserting the same ROM twice should fail")
	}

	got, err := s.GetRom("abc123")
	if err != nil || got == nil || got.Name != "Game.gba" || got.AddedAt == "" {
		t.Fatalf("GetRom = %+v, %v", got, err)
	}
	if list, _ := s.ListRoms("gba"); len(list) != 1 {
		t.Errorf("ListRoms(gba) = %d items, want 1", len(list))
	}
	if list, _ := s.ListRoms("snes"); len(list) != 0 {
		t.Errorf("ListRoms(snes) = %d items, want 0", len(list))
	}
	if counts, _ := s.CountBySystem(); counts["gba"] != 1 {
		t.Errorf("CountBySystem = %v", counts)
	}

	if err := s.UpdateRomFile("abc123", "gba", "Renamed.gba", "/roms/gba/Renamed.gba", 10, 2); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetRom("abc123"); got.Path != "/roms/gba/Renamed.gba" {
		t.Errorf("path after update = %q", got.Path)
	}

	if err := s.DeleteRom("abc123"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetRom("abc123"); got != nil {
		t.Error("ROM still exists after DeleteRom")
	}
}

func TestReopenKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InsertRom(Rom{ID: "x", System: "nes", Name: "a.nes", Path: "/a.nes", Size: 1, MTime: 1}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(path) // migrations must be idempotent
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, _ := s.GetRom("x"); got == nil {
		t.Error("data lost after reopening the database")
	}
}
