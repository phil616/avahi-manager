package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"time"
)

type Action string

const (
	NoAction Action = "none"
	Reload   Action = "reload"
	Restart  Action = "restart"
)

// Controller must wait for systemd jobs and check Avahi's actual D-Bus state.
type Controller interface {
	Apply(context.Context, Action) error
	Healthy(context.Context) error
}

type ApplyResult struct {
	Action     Action `json:"action"`
	SnapshotID string `json:"snapshotId,omitempty"`
	Revision   string `json:"revision"`
	Warning    string `json:"warning,omitempty"`
}

type pending struct {
	SnapshotID string `json:"snapshotId"`
	BaselineID string `json:"baselineId,omitempty"`
}

func ChangeAction(before, after Image) Action {
	a := NoAction
	all := map[string]bool{}
	for p := range before {
		all[p] = true
	}
	for p := range after {
		all[p] = true
	}
	for p := range all {
		b, bok := before[p]
		n, nok := after[p]
		if bok == nok && bytes.Equal(b.Data, n.Data) && b.Mode == n.Mode && b.UID == n.UID && b.GID == n.GID {
			continue
		}
		if p == "avahi-daemon.conf" || p == "hosts" {
			return Restart
		}
		a = Reload
	}
	return a
}

// replace stages ALL files before any rename. Multi-file atomicity is achieved
// by the durable pending journal + compensating rollback, not claimed from rename.
func (s *Store) replace(before, after Image) error {
	staged := map[string]string{}
	defer func() {
		for _, tmp := range staged {
			s.root.Remove(tmp)
		}
	}()
	if err := s.root.MkdirAll("services", 0755); err != nil {
		return err
	}
	info, err := s.root.Lstat("services")
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("services must be a real directory")
	}
	for _, p := range sortedPaths(after) {
		f := after[p]
		if prev, ok := before[p]; ok && bytes.Equal(prev.Data, f.Data) && prev.Mode == f.Mode && prev.UID == f.UID && prev.GID == f.GID {
			continue
		}
		tmp, err := stage(s.root, p, f, true)
		if err != nil {
			return err
		}
		staged[p] = tmp
	}
	for _, p := range sortedPaths(staged) {
		if err := s.root.Rename(staged[p], p); err != nil {
			return err
		}
	}
	for _, p := range sortedPaths(before) {
		if _, ok := after[p]; !ok {
			if err := s.root.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
	}
	return errors.Join(syncDir(s.root, "services"), syncDir(s.root, "."))
}

func (s *Store) Apply(ctx context.Context, expected string, changes map[string]*[]byte, c Controller) (ApplyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, err := s.read()
	if err != nil {
		return ApplyResult{}, err
	}
	after := Image{}
	for p, f := range before {
		after[p] = f
	}
	for p, data := range changes {
		if !validPath(p) {
			return ApplyResult{}, fmt.Errorf("invalid configuration path")
		}
		if data == nil {
			delete(after, p)
			continue
		}
		f, ok := before[p]
		if !ok {
			f = File{Mode: 0644, UID: os.Geteuid(), GID: os.Getegid()}
		}
		f.Data = bytes.Clone(*data)
		after[p] = f
	}
	return s.apply(ctx, expected, before, after, "Save & Apply", c)
}

func (s *Store) apply(ctx context.Context, expected string, before, after Image, reason string, c Controller) (ApplyResult, error) {
	result := ApplyResult{Action: ChangeAction(before, after), Revision: before.Revision()}
	if expected == "" || expected != result.Revision {
		return result, ErrConflict
	}
	if _, err := s.backups.Stat("pending.json"); err == nil {
		return result, fmt.Errorf("transaction recovery required")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return result, err
	}
	if err := ValidateImage(after); err != nil {
		return result, err
	}
	if result.Action == NoAction {
		id, err := s.baselineID()
		if err != nil {
			return result, err
		}
		var baseline Image
		if id != "" {
			_, baseline, err = s.loadSnapshot(id)
			if err != nil {
				return result, err
			}
		}
		if id == "" || baseline.Revision() != after.Revision() {
			if _, err = s.recordBaseline(after, "Managed configuration"); err != nil {
				return result, err
			}
			if err = s.prune(20); err != nil {
				return result, err
			}
		}
		return result, nil
	}
	if c == nil {
		return result, fmt.Errorf("runtime controller required")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	m, err := s.snapshot(before, reason)
	if err != nil {
		return result, err
	}
	result.SnapshotID = m.ID
	// Check again after snapshot I/O to avoid overwriting a concurrent CLI edit.
	current, err := s.read()
	if err != nil {
		return result, err
	}
	if current.Revision() != expected {
		return result, ErrConflict
	}
	oldBaseline, err := s.baselineID()
	if err != nil {
		return result, err
	}
	data, _ := json.Marshal(pending{SnapshotID: m.ID, BaselineID: oldBaseline})
	if err = atomicFile(s.backups, "pending.json", File{Data: data, Mode: 0600}); err != nil {
		return result, err
	}
	err = s.replace(before, after)
	if err == nil {
		err = c.Apply(ctx, result.Action)
	}
	if err == nil {
		err = c.Healthy(ctx)
	}
	if err == nil {
		_, err = s.recordBaseline(after, "Managed configuration")
	}
	if err != nil {
		rollbackErr := s.recover(c)
		if rollbackErr != nil {
			return result, errors.Join(err, fmt.Errorf("rollback failed; recovery remains pending: %w", rollbackErr))
		}
		return result, fmt.Errorf("apply failed; previous configuration restored: %w", err)
	}
	if err = s.clearPending(); err != nil {
		return result, fmt.Errorf("configuration applied but commit journal failed: %w", err)
	}
	result.Revision = after.Revision()
	if err = s.prune(20); err != nil {
		result.Warning = "Configuration committed; snapshot retention failed: " + err.Error()
	}
	return result, nil
}

func (s *Store) clearPending() error {
	if err := s.backups.Remove("pending.json"); err != nil {
		return err
	}
	return syncDir(s.backups, ".")
}

// Recover must run before accepting helper mutations. A fresh context allows
// rollback to finish even if the browser disconnected or the apply timed out.
func (s *Store) Recover(c Controller) error { s.mu.Lock(); defer s.mu.Unlock(); return s.recover(c) }

func (s *Store) recover(c Controller) error {
	f, err := readFile(s.backups, "pending.json")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if c == nil {
		return fmt.Errorf("runtime controller required for recovery")
	}
	var p pending
	if err = json.Unmarshal(f.Data, &p); err != nil {
		return err
	}
	_, before, err := s.loadSnapshot(p.SnapshotID)
	if err != nil {
		return err
	}
	current, err := s.read()
	if err != nil {
		return err
	}
	if err = s.replace(current, before); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err = c.Apply(ctx, Restart); err != nil {
		return err
	}
	if err = c.Healthy(ctx); err != nil {
		return err
	}
	if err = s.setBaseline(p.BaselineID); err != nil {
		return err
	}
	return s.clearPending()
}

func (s *Store) Restore(ctx context.Context, id, expected string, c Controller) (ApplyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, after, err := s.loadSnapshot(id)
	if err != nil {
		return ApplyResult{}, err
	}
	before, err := s.read()
	if err != nil {
		return ApplyResult{}, err
	}
	return s.apply(ctx, expected, before, after, "Restore "+id, c)
}

func (s *Store) prune(keep int) error {
	list, err := s.snapshots()
	if err != nil {
		return err
	}
	pinned, err := s.baselineID()
	if err != nil {
		return err
	}
	remaining := len(list)
	for i := len(list) - 1; i >= 0 && remaining > keep; i-- {
		id := list[i].ID
		if id == pinned {
			continue
		}
		if !validSnapshotID(id) || path.Base(id) != id {
			return fmt.Errorf("invalid snapshot ID")
		}
		if err = s.backups.RemoveAll(id); err != nil {
			return err
		}
		remaining--
	}
	return syncDir(s.backups, ".")
}
