package database

import (
	"context"
	"encoding/json"

	"avahi-manager/internal/config"
)

// SaveBaseline commits the file hashes, pointer and snapshot index together.
// The helper's durable baseline is authoritative after a process interruption.
func (d *DB) SaveBaseline(ctx context.Context, m config.Manifest, revision string, hashes map[string]string) error {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM managed_files"); err != nil {
		return err
	}
	for p, h := range hashes {
		if _, err = tx.ExecContext(ctx, "INSERT INTO managed_files(path,sha256,last_seen_at,externally_modified) VALUES(?,?,?,0)", p, h, Now()); err != nil {
			return err
		}
	}
	for k, v := range map[string]string{"managed_snapshot": m.ID, "managed_revision": revision} {
		if _, err = tx.ExecContext(ctx, "INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at", k, v, Now()); err != nil {
			return err
		}
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO config_snapshots(id,reason,manifest_json,created_at) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET manifest_json=excluded.manifest_json", m.ID, m.Reason, string(data), m.CreatedAt.Format("2006-01-02T15:04:05.000000000Z")); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) SyncSnapshots(ctx context.Context, list []config.Manifest) error {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM config_snapshots"); err != nil {
		return err
	}
	for _, m := range list {
		b, err := json.Marshal(m)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO config_snapshots(id,reason,manifest_json,created_at) VALUES(?,?,?,?)", m.ID, m.Reason, string(b), m.CreatedAt.Format("2006-01-02T15:04:05.000000000Z")); err != nil {
			return err
		}
	}
	return tx.Commit()
}
