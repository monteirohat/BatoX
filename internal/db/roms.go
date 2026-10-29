package db

import (
	"database/sql"
	"errors"
)

type Rom struct {
	ID       string `json:"id"`
	System   string `json:"system"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	MTime    int64  `json:"mtime"`
	Language string `json:"language"`
	AddedAt  string `json:"addedAt"`
}

const romColumns = "id, system, name, path, size, mtime, language, added_at"

type scanner interface {
	Scan(dest ...any) error
}

func scanRom(sc scanner) (Rom, error) {
	var r Rom
	err := sc.Scan(&r.ID, &r.System, &r.Name, &r.Path, &r.Size, &r.MTime, &r.Language, &r.AddedAt)
	return r, err
}

func (s *Store) InsertRom(r Rom) error {
	_, err := s.conn.Exec(
		`INSERT INTO roms (id, system, name, path, size, mtime, language) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.System, r.Name, r.Path, r.Size, r.MTime, r.Language,
	)
	return err
}

// GetRom returns nil, nil when the ROM doesn't exist.
func (s *Store) GetRom(id string) (*Rom, error) {
	r, err := scanRom(s.conn.QueryRow("SELECT "+romColumns+" FROM roms WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetRomByPath returns the ROM stored at path, or nil, nil when there is none.
func (s *Store) GetRomByPath(path string) (*Rom, error) {
	r, err := scanRom(s.conn.QueryRow("SELECT "+romColumns+" FROM roms WHERE path = ?", path))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListRoms lists every ROM, or only those of the given system.
func (s *Store) ListRoms(system string) ([]Rom, error) {
	query := "SELECT " + romColumns + " FROM roms"
	var args []any
	if system != "" {
		query += " WHERE system = ?"
		args = append(args, system)
	}
	query += " ORDER BY system, name COLLATE NOCASE"

	rows, err := s.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []Rom{}
	for rows.Next() {
		r, err := scanRom(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// UpdateRomFile updates the file data (used when a ROM was moved or touched).
func (s *Store) UpdateRomFile(id, system, name, path string, size, mtime int64) error {
	_, err := s.conn.Exec(
		`UPDATE roms SET system = ?, name = ?, path = ?, size = ?, mtime = ? WHERE id = ?`,
		system, name, path, size, mtime, id,
	)
	return err
}

func (s *Store) DeleteRom(id string) error {
	_, err := s.conn.Exec(`DELETE FROM roms WHERE id = ?`, id)
	return err
}

func (s *Store) DeleteRomByPath(path string) error {
	_, err := s.conn.Exec(`DELETE FROM roms WHERE path = ?`, path)
	return err
}

func (s *Store) CountBySystem() (map[string]int, error) {
	rows, err := s.conn.Query(`SELECT system, COUNT(*) FROM roms GROUP BY system`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var system string
		var n int
		if err := rows.Scan(&system, &n); err != nil {
			return nil, err
		}
		out[system] = n
	}
	return out, rows.Err()
}
