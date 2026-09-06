package config

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch reports possible changes; consumers compare SHA256 revisions against
// managed_files. Watching directories catches editor atomic-rename saves.
// It never writes files or decides that an external edit should be overwritten.
func Watch(ctx context.Context, dir string, changed func(), report func(error)) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	parent := filepath.Dir(filepath.Clean(dir))
	if err = w.Add(parent); err != nil {
		return err
	}
	attach := func() {
		for _, p := range []string{dir, filepath.Join(dir, "services")} {
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				if err = w.Add(p); err != nil {
					report(err)
				}
			}
		}
	}
	attach()
	changed()
	var timer *time.Timer
	var tick <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			report(err)
			changed()
		case event, ok := <-w.Events:
			if !ok {
				return nil
			}
			// Parent watch also sees unrelated siblings; only Avahi changes matter.
			if event.Name != dir && filepath.Dir(event.Name) != dir && filepath.Dir(event.Name) != filepath.Join(dir, "services") {
				continue
			}
			if timer == nil {
				timer = time.NewTimer(150 * time.Millisecond)
			} else {
				timer.Stop()
				timer.Reset(150 * time.Millisecond)
			}
			tick = timer.C
		case <-tick:
			tick = nil
			attach()
			changed()
		}
	}
}
