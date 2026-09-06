package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBaselinePinsAcceptedVersionAcrossDriftAndRetention(t *testing.T) {
	s, dir, _ := fixture(t)
	original := revision(t, s)
	m, im, err := s.Baseline()
	if err != nil || im.Revision() != original {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "hosts"), []byte("192.168.1.99 cli.local\n"), 0640); err != nil {
		t.Fatal(err)
	}
	for range 22 {
		if _, err = s.Snapshot("ordinary backup"); err != nil {
			t.Fatal(err)
		}
	}
	got, im, err := s.Baseline()
	if err != nil || got.ID != m.ID || im.Revision() != original {
		t.Fatal("drift or retention replaced baseline", err)
	}
	if err = s.DeleteSnapshot(m.ID); err == nil {
		t.Fatal("deleted pinned baseline")
	}
	if _, _, err = s.AcceptDisk(original); err == nil {
		t.Fatal("accepted stale disk revision")
	}
	current := revision(t, s)
	got, im, err = s.AcceptDisk(current)
	if err != nil || got.ID == m.ID || im.Revision() != current {
		t.Fatal(err)
	}
}

func TestBaselineFollowsSuccessfulApplyButNotRollback(t *testing.T) {
	s, _, _ := fixture(t)
	m, _, err := s.Baseline()
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Apply(context.Background(), revision(t, s), change("hosts", "192.168.1.8 new.local\n"), &fakeController{healthFailures: 1})
	if err == nil {
		t.Fatal("expected apply failure")
	}
	got, _, err := s.Baseline()
	if err != nil || got.ID != m.ID {
		t.Fatal("rollback changed baseline", err)
	}
	_, err = s.Apply(context.Background(), revision(t, s), change("hosts", "192.168.1.8 new.local\n"), &fakeController{})
	if err != nil {
		t.Fatal(err)
	}
	got, im, err := s.Baseline()
	if err != nil || got.ID == m.ID || im.Revision() != revision(t, s) {
		t.Fatal("committed baseline mismatch", err)
	}
}

func TestCrashAfterBaselineWriteRestoresPreviousPointer(t *testing.T) {
	s, dir, backups := fixture(t)
	old, im, err := s.Baseline()
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.Snapshot("before interrupted transaction")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "hosts"), []byte("192.168.1.44 uncommitted.local\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.AcceptDisk(revision(t, s)); err != nil {
		t.Fatal(err)
	}
	p, _ := json.Marshal(pending{SnapshotID: before.ID, BaselineID: old.ID})
	if err = os.WriteFile(filepath.Join(backups, "pending.json"), p, 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.Recover(&fakeController{}); err != nil {
		t.Fatal(err)
	}
	got, restored, err := s.Baseline()
	if err != nil || got.ID != old.ID || restored.Revision() != im.Revision() || revision(t, s) != im.Revision() {
		t.Fatal("crash failed to restore baseline and files", err)
	}
}
