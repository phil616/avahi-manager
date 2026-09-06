package helper

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"avahi-manager/internal/config"
)

type fakeRuntime struct {
	mu         sync.Mutex
	actions    []string
	failHealth int
}

func (r *fakeRuntime) Control(_ context.Context, a string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.actions = append(r.actions, a)
	return nil
}
func (r *fakeRuntime) Apply(ctx context.Context, a config.Action) error {
	return r.Control(ctx, string(a))
}
func (r *fakeRuntime) Healthy(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failHealth > 0 {
		r.failHealth--
		return errors.New("unhealthy")
	}
	return nil
}

func backend(t *testing.T) (*Backend, string) {
	t.Helper()
	dir := t.TempDir()
	conf := filepath.Join(dir, "avahi")
	if err := os.MkdirAll(filepath.Join(conf, "services"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conf, "avahi-daemon.conf"), []byte("# original\n[server]\nuse-ipv4=yes\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := config.OpenStore(conf, filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &Backend{s, &fakeRuntime{}}, dir
}

func start(t *testing.T, b *Backend, dir string, uid uint32) *Client {
	t.Helper()
	socket := filepath.Join(dir, "helper.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, l, uid, b) }()
	t.Cleanup(func() {
		cancel()
		l.Close()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, net.ErrClosed) {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("helper did not stop")
		}
	})
	c := NewClient(socket, uint32(os.Geteuid()))
	t.Cleanup(c.Close)
	return c
}

func readConfig(t *testing.T, c *Client) Configuration {
	t.Helper()
	var v Configuration
	if err := c.Call(context.Background(), "ReadConfiguration", Empty{}, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestUnixRPCApplyServicesAndRestore(t *testing.T) {
	b, dir := backend(t)
	c := start(t, b, dir, uint32(os.Geteuid()))
	ctx := context.Background()
	initial := readConfig(t, c)
	var snap config.Manifest
	if err := c.Call(ctx, "CreateSnapshot", CreateSnapshot{"initial"}, &snap); err != nil {
		t.Fatal(err)
	}
	if got := readConfig(t, c); got.Revision != initial.Revision || got.Raw != initial.Raw {
		t.Fatal("takeover changed config")
	}
	g := config.ServiceGroup{Name: config.ServiceName{Value: "Example"}, Services: []config.Service{{Type: "_http._tcp", Port: 8080, TXT: []config.TXT{{Value: "path=/"}}}}}
	var added ServiceResult
	if err := c.Call(ctx, "WriteService", WriteService{Revision: Revision{initial.Revision}, Group: g}, &added); err != nil {
		t.Fatal(err)
	}
	v := readConfig(t, c)
	if len(v.Services) != 1 || v.Services[0].ID != added.ID || !strings.HasPrefix(v.Services[0].Filename, "awm-") {
		t.Fatal(v)
	}
	if added.Action != config.Reload {
		t.Fatal("service must reload")
	}
	g.Services[0].Port = 9090
	if err := c.Call(ctx, "WriteService", WriteService{Revision: Revision{v.Revision}, ID: added.ID, Group: g}, &added); err != nil {
		t.Fatal(err)
	}
	v = readConfig(t, c)
	if v.Services[0].Group.Services[0].Port != 9090 {
		t.Fatal("service not edited")
	}
	var applied config.ApplyResult
	if err := c.Call(ctx, "WriteHosts", WriteHosts{Revision: Revision{v.Revision}, Hosts: []config.Host{{Address: "192.168.1.2", Hostname: "nas.local"}}}, &applied); err != nil {
		t.Fatal(err)
	}
	if applied.Action != config.Restart {
		t.Fatal("hosts must restart")
	}
	if err := c.Call(ctx, "RestoreSnapshot", RestoreSnapshot{Revision: Revision{applied.Revision}, ID: snap.ID}, &applied); err != nil {
		t.Fatal(err)
	}
	if got := readConfig(t, c); got.Revision != initial.Revision || len(got.Services) != 0 {
		t.Fatal("restore mismatch", got)
	}
}

func TestConflictsAndRollbackAcrossRPC(t *testing.T) {
	b, dir := backend(t)
	c := start(t, b, dir, uint32(os.Geteuid()))
	ctx := context.Background()
	v := readConfig(t, c)
	r := WriteHosts{Revision: Revision{v.Revision}, Hosts: []config.Host{{Address: "192.168.1.2", Hostname: "nas.local"}}}
	b.Runtime.(*fakeRuntime).failHealth = 1
	if err := c.Call(ctx, "WriteHosts", r, nil); err == nil || !strings.Contains(err.Error(), "previous configuration restored") {
		t.Fatal(err)
	}
	if readConfig(t, c).Revision != v.Revision {
		t.Fatal("RPC rollback failed")
	}
	if err := c.Call(ctx, "WriteHosts", r, nil); err != nil {
		t.Fatal(err)
	}
	err := c.Call(ctx, "WriteHosts", r, nil)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != "conflict" {
		t.Fatal("missing conflict error", err)
	}
}

func TestRPCRejectsUntrustedInput(t *testing.T) {
	b, _ := backend(t)
	h := handler(b)
	for _, tt := range []struct {
		method, body string
		status       int
	}{
		{"RunCommand", `{"command":"id"}`, 404},
		{"WriteFile", `{"path":"/etc/passwd","data":"x"}`, 404},
		{"StartAvahi", `{"command":"other.service"}`, 400},
		{"ReadConfiguration", `null`, 400},
		{"ReadConfiguration", `{} {}`, 400},
		{"WriteHosts", `{"revision":"x","hosts":[],"path":"../../hosts"}`, 400},
		{"DeleteService", `{"revision":"x","id":"../../outside"}`, 422},
	} {
		t.Run(tt.method+tt.body, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/rpc/"+tt.method, strings.NewReader(tt.body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.status {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
		})
	}
	if len(b.Runtime.(*fakeRuntime).actions) != 0 {
		t.Fatal("rejected input invoked runtime")
	}
}

func TestPeerCredentialsRejectWrongClientAndServer(t *testing.T) {
	b, dir := backend(t)
	c := start(t, b, dir, uint32(os.Geteuid()+1))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Call(ctx, "ReadConfiguration", Empty{}, nil); err == nil {
		t.Fatal("wrong client UID accepted")
	}
	// Use a separate listener accepting the current UID, then reject its server UID.
	b2, dir2 := backend(t)
	_ = start(t, b2, dir2, uint32(os.Geteuid()))
	wrong := NewClient(filepath.Join(dir2, "helper.sock"), uint32(os.Geteuid()+1))
	defer wrong.Close()
	if err := wrong.Call(ctx, "ReadConfiguration", Empty{}, nil); err == nil {
		t.Fatal("wrong server UID accepted")
	}
}

func TestRevisionEmbeddedJSON(t *testing.T) {
	b, err := json.Marshal(WriteHosts{Revision: Revision{"abc"}, Hosts: []config.Host{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"revision":"abc"`) {
		t.Fatal(string(b))
	}
}
