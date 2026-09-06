//go:build browser

package api

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"avahi-manager/frontend"
)

func TestBrowser(t *testing.T) {
	a := fixture(t)
	server := httptest.NewUnstartedServer(nil)
	origin := "http://" + server.Listener.Addr().String()
	a.s.hosts = map[string]bool{server.Listener.Addr().String(): true}
	a.s.origins = map[string]bool{origin: true}
	server.Config.Handler = a.s.Handler(frontend.Handler())
	server.Start()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	watchDone := make(chan struct{})
	go func() { defer close(watchDone); a.s.WatchConfiguration(ctx, func(err error) { t.Log(err) }) }()
	defer func() { cancel(); <-watchDone }()
	cmd := exec.CommandContext(ctx, "npx", "playwright", "test")
	cmd.Dir = "../../frontend"
	cmd.Env = append(os.Environ(), "AWM_E2E_URL="+origin)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser tests: %v\n%s", err, output)
	}
	t.Log(string(output))
	data, err := os.ReadFile(filepath.Join(a.dir, "avahi-daemon.conf"))
	if err != nil || !strings.Contains(string(data), "host-name=browser-second") {
		t.Fatalf("browser save did not reach native config: %s %v", data, err)
	}
}
