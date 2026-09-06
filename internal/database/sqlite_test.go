package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func testDB(t *testing.T) (*DB, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "manager.db")
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, p
}

func TestMigrationsPersistenceAndPermissions(t *testing.T) {
	d, p := testDB(t)
	ctx := context.Background()
	if err := d.SetSetting(ctx, "theme", "dark"); err != nil {
		t.Fatal(err)
	}
	if err := d.Audit(ctx, "admin", "SaveConfig", "avahi-daemon.conf", "success", map[string]string{"revision": "abc"}); err != nil {
		t.Fatal(err)
	}
	d2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	if v, err := d2.Setting(ctx, "theme"); err != nil || v != "dark" {
		t.Fatalf("%s %v", v, err)
	}
	logs, err := d2.Audits(ctx, 0, 100)
	if err != nil || len(logs) != 1 || logs[0].Actor != "admin" {
		t.Fatalf("%+v %v", logs, err)
	}
	info, err := os.Stat(p)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("unsafe DB permissions", err)
	}
	var foreignKeys int
	if err = d.SQL.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatal("foreign keys disabled", err)
	}
	var journal string
	if err = d.SQL.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil || journal != "wal" {
		t.Fatal("WAL disabled", err)
	}
	for _, name := range []string{"services", "hosts", "avahi_config"} {
		var n int
		if err = d.SQL.QueryRow("SELECT count(*) FROM sqlite_master WHERE name=?", name).Scan(&n); err != nil || n != 0 {
			t.Fatal("Avahi primary data table must not exist", name, err)
		}
	}
}

func TestDriftDoesNotAcceptExternalEdits(t *testing.T) {
	d, _ := testDB(t)
	ctx := context.Background()
	if err := d.AcceptFiles(ctx, map[string]string{"hosts": "old", "services/removed.service": "removed"}); err != nil {
		t.Fatal(err)
	}
	disk := map[string]string{"hosts": "edited", "services/new.service": "new"}
	for range 2 {
		drift, err := d.ObserveFiles(ctx, disk)
		if err != nil || len(drift) != 3 {
			t.Fatalf("%+v %v", drift, err)
		}
	}
	var hash string
	if err := d.SQL.QueryRow("SELECT sha256 FROM managed_files WHERE path='hosts'").Scan(&hash); err != nil || hash != "old" {
		t.Fatal("observing drift accepted it", hash, err)
	}
	if err := d.AcceptFiles(ctx, disk); err != nil {
		t.Fatal(err)
	}
	if drift, err := d.ObserveFiles(ctx, disk); err != nil || len(drift) != 0 {
		t.Fatalf("%+v %v", drift, err)
	}
}

func TestJobsTerminalStatesAndRecovery(t *testing.T) {
	d, _ := testDB(t)
	ctx := context.Background()
	if err := d.CreateJob(ctx, "one", "check-update"); err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateJob(ctx, "one", "running", 25, ""); err != nil {
		t.Fatal(err)
	}
	if err := d.InterruptJobs(ctx); err != nil {
		t.Fatal(err)
	}
	jobs, err := d.Jobs(ctx)
	if err != nil || len(jobs) != 1 || jobs[0].State != "interrupted" || jobs[0].FinishedAt == nil {
		t.Fatalf("%+v %v", jobs, err)
	}
	if err := d.UpdateJob(ctx, "one", "running", 30, ""); err == nil {
		t.Fatal("terminal job was revived")
	}
	if err := d.CreateJob(ctx, "two", "upgrade"); err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateJob(ctx, "two", "bogus", 30, ""); err == nil {
		t.Fatal("invalid state accepted")
	}
	if err := d.UpdateJob(ctx, "two", "running", 101, ""); err == nil {
		t.Fatal("invalid progress accepted")
	}
}
