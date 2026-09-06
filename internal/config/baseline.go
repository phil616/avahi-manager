package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
)

type baselinePointer struct {
	ID string `json:"id"`
}

func (s *Store) baselineID() (string, error) {
	f, err := readFile(s.backups, "baseline.json")
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var p baselinePointer
	if err = json.Unmarshal(f.Data, &p); err != nil {
		return "", err
	}
	if !validSnapshotID(p.ID) {
		return "", fmt.Errorf("invalid baseline pointer")
	}
	return p.ID, nil
}

func (s *Store) setBaseline(id string) error {
	if id == "" {
		err := s.backups.Remove("baseline.json")
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return syncDir(s.backups, ".")
	}
	if _, _, err := s.loadSnapshot(id); err != nil {
		return err
	}
	b, _ := json.Marshal(baselinePointer{id})
	return atomicFile(s.backups, "baseline.json", File{Data: b, Mode: 0600})
}

func (s *Store) recordBaseline(im Image, reason string) (Manifest, error) {
	m, err := s.snapshot(im, reason)
	if err != nil {
		return m, err
	}
	return m, s.setBaseline(m.ID)
}

// Baseline creates the initial snapshot only once, without normalizing files.
// Thereafter it returns the last explicitly accepted version, even during drift.
func (s *Store) Baseline() (Manifest, Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.noPending(); err != nil {
		return Manifest{}, nil, err
	}
	id, err := s.baselineID()
	if err != nil {
		return Manifest{}, nil, err
	}
	if id != "" {
		return s.loadSnapshot(id)
	}
	im, err := s.read()
	if err != nil {
		return Manifest{}, nil, err
	}
	m, err := s.recordBaseline(im, "Initial takeover")
	return m, im, err
}

func (s *Store) noPending() error {
	_, err := s.backups.Lstat("pending.json")
	if err == nil {
		return fmt.Errorf("transaction recovery required")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// AcceptDisk is the explicit Reload from disk action. Merely reading a changed
// file never updates the baseline, nor does creating an ordinary backup.
func (s *Store) AcceptDisk(expected string) (Manifest, Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.noPending(); err != nil {
		return Manifest{}, nil, err
	}
	im, err := s.read()
	if err != nil {
		return Manifest{}, nil, err
	}
	if expected == "" || expected != im.Revision() {
		return Manifest{}, nil, ErrConflict
	}
	m, err := s.recordBaseline(im, "Reload from disk")
	if err == nil {
		err = s.prune(20)
	}
	return m, im, err
}
