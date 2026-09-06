// Package database persists only Manager state. Avahi configuration is always
// read from its native files and is never stored as authoritative SQL models.
package database

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type DB struct{ SQL *sql.DB }

func Now() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z") }

func Open(path string) (*DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(abs); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("database must be a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}
	f.Close()
	u := url.URL{Scheme: "file", Path: abs}
	q := url.Values{}
	for _, p := range []string{"foreign_keys(1)", "journal_mode(WAL)", "busy_timeout(5000)", "synchronous(FULL)"} {
		q.Add("_pragma", p)
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		db.Close()
		return nil, err
	}
	data, err := migrations.ReadFile("migrations/001_initial.sql")
	if err == nil {
		_, err = tx.ExecContext(ctx, string(data))
	}
	if err == nil {
		err = tx.Commit()
	} else {
		tx.Rollback()
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return &DB{db}, nil
}

func (d *DB) Close() error { return d.SQL.Close() }

type Audit struct {
	ID        int64           `json:"id"`
	Actor     string          `json:"actor"`
	Action    string          `json:"action"`
	Resource  string          `json:"resource"`
	Detail    json.RawMessage `json:"detail"`
	Result    string          `json:"result"`
	CreatedAt string          `json:"createdAt"`
}

func (d *DB) Audit(ctx context.Context, actor, action, resource, result string, detail any) error {
	data, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = d.SQL.ExecContext(ctx, "INSERT INTO audit_logs(actor,action,resource,detail_json,result,created_at) VALUES(?,?,?,?,?,?)", actor, action, resource, string(data), result, Now())
	return err
}

func (d *DB) Audits(ctx context.Context, before int64, limit int) ([]Audit, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	if before <= 0 {
		before = 1<<63 - 1
	}
	rows, err := d.SQL.QueryContext(ctx, "SELECT id,actor,action,resource,detail_json,result,created_at FROM audit_logs WHERE id < ? ORDER BY id DESC LIMIT ?", before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Audit{}
	for rows.Next() {
		var a Audit
		var detail string
		if err = rows.Scan(&a.ID, &a.Actor, &a.Action, &a.Resource, &detail, &a.Result, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Detail = json.RawMessage(detail)
		result = append(result, a)
	}
	return result, rows.Err()
}

func (d *DB) Setting(ctx context.Context, key string) (string, error) {
	var value string
	err := d.SQL.QueryRowContext(ctx, "SELECT value FROM app_settings WHERE key=?", key).Scan(&value)
	return value, err
}
func (d *DB) SetSetting(ctx context.Context, key, value string) error {
	_, err := d.SQL.ExecContext(ctx, "INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at", key, value, Now())
	return err
}

// AcceptFiles is called only after initial backup, successful apply, or an
// explicit Reload from disk. Observing a drift must not silently accept it.
func (d *DB) AcceptFiles(ctx context.Context, hashes map[string]string) error {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM managed_files"); err != nil {
		return err
	}
	for p, hash := range hashes {
		if _, err = tx.ExecContext(ctx, "INSERT INTO managed_files(path,sha256,last_seen_at,externally_modified) VALUES(?,?,?,0)", p, hash, Now()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type Drift struct {
	Path        string `json:"path"`
	ManagedHash string `json:"managedHash"`
	DiskHash    string `json:"diskHash"`
}

func (d *DB) ObserveFiles(ctx context.Context, hashes map[string]string) ([]Drift, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT path,sha256 FROM managed_files")
	if err != nil {
		return nil, err
	}
	known := map[string]string{}
	for rows.Next() {
		var p, h string
		if err = rows.Scan(&p, &h); err != nil {
			rows.Close()
			return nil, err
		}
		known[p] = h
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	drift := []Drift{}
	for p, h := range known {
		different := hashes[p] != h
		if different {
			drift = append(drift, Drift{p, h, hashes[p]})
		}
		if _, err = tx.ExecContext(ctx, "UPDATE managed_files SET externally_modified=?,last_seen_at=? WHERE path=?", different, Now(), p); err != nil {
			return nil, err
		}
	}
	for p, h := range hashes {
		if _, ok := known[p]; !ok {
			drift = append(drift, Drift{p, "", h})
		}
	}
	return drift, tx.Commit()
}

type Job struct {
	ID         string  `json:"id"`
	Type       string  `json:"type"`
	State      string  `json:"state"`
	Progress   int     `json:"progress"`
	Result     string  `json:"result"`
	CreatedAt  string  `json:"createdAt"`
	FinishedAt *string `json:"finishedAt"`
}

func (d *DB) CreateJob(ctx context.Context, id, kind string) error {
	_, err := d.SQL.ExecContext(ctx, "INSERT INTO operation_jobs(id,type,state,created_at) VALUES(?,?,'queued',?)", id, kind, Now())
	return err
}
func (d *DB) UpdateJob(ctx context.Context, id, state string, progress int, result string) error {
	var finished any
	if state == "succeeded" || state == "failed" || state == "interrupted" {
		finished = Now()
	}
	r, err := d.SQL.ExecContext(ctx, "UPDATE operation_jobs SET state=?,progress=?,result=?,finished_at=? WHERE id=? AND state IN ('queued','running')", state, progress, result, finished, id)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("job missing or already terminal")
	}
	return nil
}
func (d *DB) InterruptJobs(ctx context.Context) error {
	_, err := d.SQL.ExecContext(ctx, "UPDATE operation_jobs SET state='interrupted',result='Manager restarted before job completion; inspect host state before retrying',finished_at=? WHERE state IN ('queued','running')", Now())
	return err
}
func (d *DB) Jobs(ctx context.Context) ([]Job, error) {
	rows, err := d.SQL.QueryContext(ctx, "SELECT id,type,state,progress,result,created_at,finished_at FROM operation_jobs ORDER BY created_at DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		var j Job
		if err = rows.Scan(&j.ID, &j.Type, &j.State, &j.Progress, &j.Result, &j.CreatedAt, &j.FinishedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}
