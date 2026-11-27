package api

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/monteirohat/batox/internal/bios"
	"github.com/monteirohat/batox/internal/config"
	"github.com/monteirohat/batox/internal/db"
)

func md5Of(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func biosServer(t *testing.T, load bios.Loader) *Server {
	t.Helper()
	s := nowPlayingServer(t, nil)
	s.Config = &config.Config{MaxUploadMB: 1}
	// Without es_systems.cfg every system of the table is listed.
	s.Bios = bios.NewWithLoader(t.TempDir(), filepath.Join(t.TempDir(), "none.cfg"), load, s.Log)
	return s
}

func biosTable() (bios.Table, error) {
	return bios.Table{
		"psx": {Name: "PSX", Files: []bios.Entry{
			{File: "bios/scph5501.bin", MD5: md5Of("psx")},
			{File: "bios/scph1001.bin", MD5: md5Of("other")},
		}},
		"nds": {Name: "NDS", Files: []bios.Entry{{File: "bios/firmware.bin"}}},
	}, nil
}

type biosList struct {
	Dir     string       `json:"dir"`
	Systems []biosSystem `json:"systems"`
}

func getBios(t *testing.T, s *Server, query string) biosList {
	t.Helper()
	rec := httptest.NewRecorder()
	s.listBios(rec, httptest.NewRequest(http.MethodGet, "/bios"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var out biosList
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func uploadBiosFile(t *testing.T, s *Server, query, name, content string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte(content))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/bios"+query, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	s.uploadBios(rec, req)
	return rec
}

func TestListBios(t *testing.T) {
	s := biosServer(t, biosTable)
	if err := s.DB.InsertRom(db.Rom{ID: "x", System: "psx", Name: "a.chd", Path: "/r/psx/a.chd", Size: 1, MTime: 1}); err != nil {
		t.Fatal(err)
	}

	out := getBios(t, s, "")
	if out.Dir != s.Bios.Dir() || len(out.Systems) != 2 {
		t.Fatalf("got %+v", out)
	}
	psx := out.Systems[1]
	if psx.Name != "psx" || psx.Games != 1 || psx.Total != 2 || psx.Found != 0 || psx.Title != "PSX" {
		t.Fatalf("psx = %+v", psx)
	}
	// The built-in systems list has no full names: the BIOS list's name stands in.
	if psx.FullName != "PSX" {
		t.Fatalf("fullName = %q", psx.FullName)
	}

	one := getBios(t, s, "?system=PSX")
	if len(one.Systems) != 1 || one.Systems[0].Name != "psx" {
		t.Fatalf("?system=psx: %+v", one.Systems)
	}
}

func TestUploadBios(t *testing.T) {
	s := biosServer(t, biosTable)

	rec := uploadBiosFile(t, s, "", "SCPH-5501 (USA).bin", "psx")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var res bios.UploadResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Match != bios.MatchMD5 || len(res.Saved) != 1 || res.Saved[0] != "bios/scph5501.bin" {
		t.Fatalf("result = %+v", res)
	}
	if psx := getBios(t, s, "?system=psx").Systems[0]; psx.Found != 1 {
		t.Fatalf("after upload: %+v", psx)
	}

	// Again: already there.
	if rec := uploadBiosFile(t, s, "", "x.bin", "psx"); rec.Code != http.StatusOK {
		t.Fatalf("same file again: status %d: %s", rec.Code, rec.Body)
	}
	// Another file under that name: only with overwrite.
	if rec := uploadBiosFile(t, s, "", "scph5501.bin", "bad dump"); rec.Code != http.StatusConflict {
		t.Fatalf("conflict: status %d: %s", rec.Code, rec.Body)
	}
	if rec := uploadBiosFile(t, s, "?overwrite=true", "scph5501.bin", "bad dump"); rec.Code != http.StatusCreated {
		t.Fatalf("overwrite: status %d: %s", rec.Code, rec.Body)
	}
	if rec := uploadBiosFile(t, s, "", "empty.bin", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty: status %d: %s", rec.Code, rec.Body)
	}
}

func TestBiosWithoutTable(t *testing.T) {
	s := biosServer(t, func() (bios.Table, error) { return nil, bios.ErrNoTable })
	rec := httptest.NewRecorder()
	s.listBios(rec, httptest.NewRequest(http.MethodGet, "/bios", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestDeleteBios(t *testing.T) {
	s := biosServer(t, biosTable)
	if rec := uploadBiosFile(t, s, "", "scph1001.bin", "a different version"); rec.Code != http.StatusCreated {
		t.Fatalf("upload: status %d: %s", rec.Code, rec.Body)
	}
	del := func(path string) int {
		rec := httptest.NewRecorder()
		s.deleteBios(rec, httptest.NewRequest(http.MethodDelete, "/bios?path="+path, nil))
		return rec.Code
	}
	if code := del("bios/scph1001.bin"); code != http.StatusOK {
		t.Fatalf("delete: status %d", code)
	}
	if code := del("bios/scph1001.bin"); code != http.StatusNotFound {
		t.Fatalf("again: status %d", code)
	}
	if code := del("../etc/passwd"); code != http.StatusBadRequest {
		t.Fatalf("outside the list: status %d", code)
	}
}
