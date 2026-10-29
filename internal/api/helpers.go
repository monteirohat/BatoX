package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/monteirohat/batox/internal/roms"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) internalError(w http.ResponseWriter, err error) {
	s.Log.Errorf("%v", err)
	writeError(w, http.StatusInternalServerError, err.Error())
}

func isTooLarge(err error) bool {
	var tooLarge *http.MaxBytesError
	return errors.As(err, &tooLarge) || strings.Contains(err.Error(), "request body too large")
}

// handleError maps known errors to the right HTTP status.
func (s *Server) handleError(w http.ResponseWriter, err error) {
	switch {
	case isTooLarge(err):
		writeError(w, http.StatusRequestEntityTooLarge, "file larger than the configured limit (maxUploadMB/maxThemeMB)")
	case errors.Is(err, roms.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, roms.ErrDuplicate),
		errors.Is(err, roms.ErrFileExists),
		errors.Is(err, roms.ErrSyncRunning):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, roms.ErrUnknownSystem),
		errors.Is(err, roms.ErrInvalidName),
		errors.Is(err, roms.ErrInvalidExtension),
		errors.Is(err, roms.ErrEmptyFile):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.internalError(w, err)
	}
}

// uploadError handles multipart read failures: 413 when over the limit, 400 otherwise.
func (s *Server) uploadError(w http.ResponseWriter, err error) {
	if isTooLarge(err) {
		s.handleError(w, err)
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

// filePart returns the first file in the given field, reading the multipart body as
// a stream (the upload is never buffered in memory or in /tmp).
func filePart(r *http.Request, field string) (io.Reader, string, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, "", fmt.Errorf("send the file as multipart/form-data: %w", err)
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			return nil, "", fmt.Errorf("file field %q not found", field)
		}
		if err != nil {
			return nil, "", err
		}
		if part.FormName() == field && part.FileName() != "" {
			return part, part.FileName(), nil
		}
		_ = part.Close()
	}
}

func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

func queryInt(r *http.Request, key string, def, max int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil || v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}

// tailFile returns the last n lines, reading at most the final 2 MB of the file.
func tailFile(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	const maxRead = 2 << 20
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	var offset int64
	if st.Size() > maxRead {
		offset = st.Size() - maxRead
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	text := strings.TrimRight(string(data), "\r\n")
	if text == "" {
		return []string{}, nil
	}
	lines := strings.Split(text, "\n")
	if offset > 0 && len(lines) > 1 {
		lines = lines[1:] // the first line may have been cut
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}
