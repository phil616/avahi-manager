package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchAtomicRenameAndServicesRecreation(t *testing.T) {
	_, dir, _ := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes := make(chan struct{}, 10)
	errs := make(chan error, 10)
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, dir, func() { changes <- struct{}{} }, func(err error) { errs <- err }) }()
	wait := func() {
		t.Helper()
		select {
		case <-changes:
		case err := <-errs:
			t.Fatal(err)
		case <-time.After(3 * time.Second):
			t.Fatal("file event not observed")
		}
	}
	wait()
	tmp := filepath.Join(dir, ".editor-save")
	if err := os.WriteFile(tmp, []byte("192.168.1.9 cli.local\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, "hosts")); err != nil {
		t.Fatal(err)
	}
	wait()
	if err := os.Rename(filepath.Join(dir, "services"), filepath.Join(dir, "services.old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "services"), 0755); err != nil {
		t.Fatal(err)
	}
	wait()
	if err := os.WriteFile(filepath.Join(dir, "services", "new.service"), []byte(serviceXML), 0644); err != nil {
		t.Fatal(err)
	}
	wait()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop")
	}
}
