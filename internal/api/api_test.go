package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"avahi-manager/internal/auth"
	"avahi-manager/internal/config"
	"avahi-manager/internal/database"
	"avahi-manager/internal/detect"
	"avahi-manager/internal/helper"
)

type runtime struct{}

func (runtime) Apply(context.Context, config.Action) error { return nil }
func (runtime) Control(context.Context, string) error      { return nil }
func (runtime) Healthy(context.Context) error              { return nil }

type testAPI struct {
	s                 *Server
	h                 http.Handler
	db                *database.DB
	dir, cookie, csrf string
}

func fixture(t *testing.T) *testAPI {
	t.Helper()
	dir := t.TempDir()
	conf := filepath.Join(dir, "avahi")
	if err := os.MkdirAll(filepath.Join(conf, "services"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conf, "avahi-daemon.conf"), []byte("# untouched\n[server]\nuse-ipv4=yes\n"), 0644); err != nil {
		t.Fatal(err)
	}
	store, err := config.OpenStore(conf, filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	socket := filepath.Join(dir, "helper.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- helper.Serve(ctx, listener, uint32(os.Geteuid()), &helper.Backend{Store: store, Runtime: runtime{}})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			listener.Close()
			t.Error("helper did not shut down")
		}
	})
	client := helper.NewClient(socket, uint32(os.Geteuid()))
	t.Cleanup(client.Close)
	db, err := database.Open(filepath.Join(dir, "manager.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a, err := auth.New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.CreateAdmin(ctx, "admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{DB: db, Auth: a, Helper: client, Origins: []string{"http://manager.test"}, ConfigDir: conf, Status: func(context.Context) detect.Status { return detect.Status{State: "RUNNING"} }})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	return &testAPI{s: s, h: s.Handler(nil), db: db, dir: conf}
}

func (a *testAPI) request(t *testing.T, method, path string, input any) *httptest.ResponseRecorder {
	t.Helper()
	var payload string
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		payload = string(b)
	}
	r := httptest.NewRequest(method, "http://manager.test"+path, strings.NewReader(payload))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://manager.test")
	if a.cookie != "" {
		r.Header.Set("Cookie", a.cookie)
	}
	if a.csrf != "" {
		r.Header.Set("X-CSRF-Token", a.csrf)
	}
	w := httptest.NewRecorder()
	a.h.ServeHTTP(w, r)
	return w
}
func (a *testAPI) login(t *testing.T) {
	t.Helper()
	w := a.request(t, "POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "correct horse battery staple"})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe cookie", cookies)
	}
	a.cookie = cookies[0].Name + "=" + cookies[0].Value
	var s auth.Session
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	a.csrf = s.CSRF
}
func (a *testAPI) config(t *testing.T) ConfigView {
	t.Helper()
	w := a.request(t, "GET", "/api/v1/config", nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var v ConfigView
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestAuthenticatedConfigServiceHostsAndAudit(t *testing.T) {
	a := fixture(t)
	a.login(t)
	initial := a.config(t)
	if !strings.HasPrefix(initial.Raw, "# untouched") || len(initial.Drift) != 0 {
		t.Fatal(initial)
	}
	group := config.ServiceGroup{Name: config.ServiceName{Value: "Web & files"}, Services: []config.Service{{Type: "_http._tcp", Port: 8080, TXT: []config.TXT{{Value: "path=/a?x=1&y=2"}}}}}
	w := a.request(t, "POST", "/api/v1/services", helper.WriteService{Revision: helper.Revision{Revision: initial.Revision}, Group: group})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var added helper.ServiceResult
	json.Unmarshal(w.Body.Bytes(), &added)
	if added.Action != config.Reload {
		t.Fatal(added)
	}
	v := a.config(t)
	if len(v.Services) != 1 || len(v.Drift) != 0 || v.ManagedSnapshot == initial.ManagedSnapshot {
		t.Fatal(v)
	}
	w = a.request(t, "POST", "/api/v1/hosts", map[string]any{"revision": v.Revision, "host": config.Host{Address: "192.168.1.9", Hostname: "nas.local"}})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	v = a.config(t)
	if len(v.Hosts) != 1 {
		t.Fatal(v)
	}
	w = a.request(t, "PUT", "/api/v1/hosts/"+hostID(v.Hosts[0]), map[string]any{"revision": v.Revision, "host": config.Host{Address: "192.168.1.10", Hostname: "nas.local"}})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	v = a.config(t)
	w = a.request(t, "DELETE", "/api/v1/hosts/"+hostID(v.Hosts[0]), helper.Revision{Revision: v.Revision})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	v = a.config(t)
	w = a.request(t, "DELETE", "/api/v1/services/"+added.ID, helper.Revision{Revision: v.Revision})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	v = a.config(t)
	w = a.request(t, "PUT", "/api/v1/config", helper.WriteDaemon{Revision: helper.Revision{Revision: v.Revision}, Config: config.Daemon{"server": {"host-name": "new-host", "use-ipv4": "yes"}}})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if a.config(t).Daemon["server"]["host-name"] != "new-host" {
		t.Fatal("daemon not written")
	}
	w = a.request(t, "GET", "/api/v1/audit", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"result":"success"`) || strings.Contains(w.Body.String(), "correct horse") {
		t.Fatal("audit invalid", w.Body.String())
	}
}

func TestRestartPreservesDriftUntilExplicitReload(t *testing.T) {
	a := fixture(t)
	a.login(t)
	initial := a.config(t)
	if err := os.WriteFile(filepath.Join(a.dir, "hosts"), []byte("192.168.1.88 external.local\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := a.s.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	v := a.config(t)
	if len(v.Drift) != 1 || v.ManagedSnapshot != initial.ManagedSnapshot {
		t.Fatal("restart accepted external change", v)
	}
	w := a.request(t, "PUT", "/api/v1/config", helper.WriteDaemon{Revision: helper.Revision{Revision: initial.Revision}, Config: initial.Daemon})
	if w.Code != 409 {
		t.Fatal("stale version accepted", w.Code, w.Body.String())
	}
	w = a.request(t, "POST", "/api/v1/config/reload", helper.Revision{Revision: v.Revision})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	v = a.config(t)
	if len(v.Drift) != 0 || v.ManagedSnapshot == initial.ManagedSnapshot {
		t.Fatal("explicit disk reload not accepted", v)
	}
}

func TestSecurityChecksAndSessionRevocation(t *testing.T) {
	a := fixture(t)
	if w := a.request(t, "GET", "/api/v1/config", nil); w.Code != 401 {
		t.Fatal("anonymous read accepted", w.Code)
	}
	a.login(t)
	for _, tt := range []struct {
		name, host, origin, csrf string
		want                     int
	}{
		{"CSRF", "manager.test", "http://manager.test", "bad", 403},
		{"origin", "manager.test", "http://evil.test", a.csrf, 403},
		{"missing origin", "manager.test", "", a.csrf, 403},
		{"rebinding", "evil.test", "http://manager.test", a.csrf, 403},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://manager.test/api/v1/avahi/stop", strings.NewReader(`{}`))
			r.Host = tt.host
			r.Header.Set("Origin", tt.origin)
			r.Header.Set("Cookie", a.cookie)
			r.Header.Set("X-CSRF-Token", tt.csrf)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			a.h.ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	w := a.request(t, "POST", "/api/v1/auth/logout", helper.Empty{})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = a.request(t, "GET", "/api/v1/auth/session", nil); w.Code != 401 {
		t.Fatal("revoked session accepted")
	}
	w = a.request(t, "GET", "/api/v1/config", nil)
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing security headers")
	}
}

func TestAuditFailurePreventsMutation(t *testing.T) {
	a := fixture(t)
	a.login(t)
	v := a.config(t)
	if _, err := a.db.SQL.Exec("DROP TABLE audit_logs"); err != nil {
		t.Fatal(err)
	}
	w := a.request(t, "PUT", "/api/v1/config", helper.WriteDaemon{Revision: helper.Revision{Revision: v.Revision}, Config: config.Daemon{"server": {"host-name": "must-not-write"}}})
	if w.Code != 503 {
		t.Fatal(w.Code, w.Body.String())
	}
	if got := a.config(t); got.Revision != v.Revision {
		t.Fatal("write occurred without durable audit")
	}
}

func TestPublicOriginsValidated(t *testing.T) {
	a := fixture(t)
	for _, origin := range []string{"*", "http://", "ftp://host", "http://host/path", "http://user:password@host"} {
		o := a.s.opts
		o.Origins = []string{origin}
		if _, err := New(o); err == nil {
			t.Fatal(fmt.Sprintf("accepted origin %s", origin))
		}
	}
}

func TestConfigurationSSEReportsExternalChanges(t *testing.T) {
	a := fixture(t)
	a.login(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); a.s.WatchConfiguration(ctx, func(err error) { t.Log(err) }) }()
	defer func() { cancel(); <-done }()
	server := httptest.NewServer(a.h)
	defer server.Close()
	req, err := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/config/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "manager.test"
	req.Header.Set("Cookie", a.cookie)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	next := func() ConfigView {
		t.Helper()
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "data: ") {
				var v ConfigView
				if err := json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &v); err != nil {
					t.Fatal(err)
				}
				return v
			}
		}
		t.Fatal("stream stopped", scanner.Err())
		return ConfigView{}
	}
	first := next()
	if first.Revision == "" {
		t.Fatal("missing initial stream state")
	}
	if err = os.WriteFile(filepath.Join(a.dir, "hosts"), []byte("192.168.1.88 streamed.local\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for {
		v := next()
		if len(v.Drift) > 0 {
			if len(v.Hosts) != 1 || v.Hosts[0].Hostname != "streamed.local" {
				t.Fatal(v)
			}
			break
		}
	}
}
