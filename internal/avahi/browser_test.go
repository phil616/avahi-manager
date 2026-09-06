package avahi

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

type mockAvahi struct {
	conn         *dbus.Conn
	typeReady    chan struct{}
	serviceReady chan struct{}
	once         sync.Once
	resolveGate  chan struct{}
}

func (m *mockAvahi) GetState() (int32, *dbus.Error)          { return 2, nil }
func (m *mockAvahi) GetVersionString() (string, *dbus.Error) { return "test 0.8", nil }
func (m *mockAvahi) GetHostNameFqdn() (string, *dbus.Error)  { return "test.local", nil }
func (m *mockAvahi) GetDomainName() (string, *dbus.Error)    { return "local", nil }
func (m *mockAvahi) ServiceTypeBrowserNew(int32, int32, string, uint32) (dbus.ObjectPath, *dbus.Error) {
	close(m.typeReady)
	return "/types", nil
}
func (m *mockAvahi) ServiceBrowserNew(int32, int32, string, string, uint32) (dbus.ObjectPath, *dbus.Error) {
	m.once.Do(func() { close(m.serviceReady) })
	return "/services", nil
}
func (m *mockAvahi) ResolveService(i, p int32, n, typ, domain string, ap int32, flags uint32) (int32, int32, string, string, string, string, int32, string, uint16, [][]byte, uint32, *dbus.Error) {
	if m.resolveGate != nil {
		<-m.resolveGate
	}
	return i, p, n, typ, domain, "test.local", int32(0), "192.168.1.50", uint16(8080), [][]byte{[]byte("path=/")}, uint32(0), nil
}
func (m *mockAvahi) Free() *dbus.Error { return nil }

func privateBus(t *testing.T) string {
	t.Helper()
	binary, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is required for isolated D-Bus integration tests")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary, "--session", "--nofork", "--print-address=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); cmd.Wait() })
	scanner := bufio.NewScanner(out)
	if !scanner.Scan() {
		t.Fatal("no private D-Bus address")
	}
	return scanner.Text()
}

func setupBus(t *testing.T) (*Client, *mockAvahi) {
	t.Helper()
	addr := privateBus(t)
	server, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	m := &mockAvahi{conn: server, typeReady: make(chan struct{}), serviceReady: make(chan struct{})}
	if err = server.Export(m, "/", Server); err != nil {
		t.Fatal(err)
	}
	for p, iface := range map[dbus.ObjectPath]string{"/types": Destination + ".ServiceTypeBrowser", "/services": Destination + ".ServiceBrowser"} {
		if err = server.Export(m, p, iface); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = server.RequestName(Destination, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	conn, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return &Client{conn}, m
}

func waitReady(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("D-Bus browser did not start")
	}
}

func waitState(t *testing.T, ch <-chan DiscoveryState, predicate func(DiscoveryState) bool) DiscoveryState {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case s := <-ch:
			if predicate(s) {
				return s
			}
		case <-timer.C:
			t.Fatal("timed out waiting for discovery state")
			return DiscoveryState{}
		}
	}
}

func TestDBusStatusAndDiscoveryLifecycle(t *testing.T) {
	c, m := setupBus(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status, err := c.Status(ctx)
	if err != nil || status.Hostname != "test.local" || status.State != 2 {
		t.Fatalf("%+v %v", status, err)
	}
	d := NewDiscovery()
	events, unsub := d.Subscribe()
	defer unsub()
	done := make(chan error, 1)
	go func() { done <- d.browse(ctx, c) }()
	waitReady(t, m.typeReady)
	if err = m.conn.Emit("/types", Destination+".ServiceTypeBrowser.ItemNew", int32(2), int32(0), "_http._tcp", "local", uint32(0)); err != nil {
		t.Fatal(err)
	}
	waitReady(t, m.serviceReady)
	args := []any{int32(2), int32(0), "Example", "_http._tcp", "local", uint32(0)}
	if err = m.conn.Emit("/services", Destination+".ServiceBrowser.ItemNew", args...); err != nil {
		t.Fatal(err)
	}
	s := waitState(t, events, func(s DiscoveryState) bool { return len(s.Services) == 1 && s.Services[0].Port == 8080 })
	if s.Services[0].Address != "192.168.1.50" || string(s.Services[0].TXT[0]) != "path=/" || s.Services[0].FirstSeen.IsZero() {
		t.Fatal(s)
	}
	if err = m.conn.Emit("/services", Destination+".ServiceBrowser.ItemRemove", args...); err != nil {
		t.Fatal(err)
	}
	waitState(t, events, func(s DiscoveryState) bool { return s.Connected && len(s.Services) == 0 })
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("browser leaked on cancellation")
	}
}

func TestOwnerLossEndsBrowser(t *testing.T) {
	c, m := setupBus(t)
	d := NewDiscovery()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.browse(ctx, c) }()
	waitReady(t, m.typeReady)
	if _, err := m.conn.ReleaseName(Destination); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("owner loss was not reported")
		}
	case <-ctx.Done():
		t.Fatal("owner change not handled")
	}
}

func TestLateResolveDoesNotResurrectRemovedService(t *testing.T) {
	c, m := setupBus(t)
	m.resolveGate = make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(m.resolveGate) })
	d := NewDiscovery()
	events, unsub := d.Subscribe()
	defer unsub()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.browse(ctx, c) }()
	waitReady(t, m.typeReady)
	if err := m.conn.Emit("/types", Destination+".ServiceTypeBrowser.ItemNew", int32(2), int32(0), "_http._tcp", "local", uint32(0)); err != nil {
		t.Fatal(err)
	}
	waitReady(t, m.serviceReady)
	args := []any{int32(2), int32(0), "Example", "_http._tcp", "local", uint32(0)}
	if err := m.conn.Emit("/services", Destination+".ServiceBrowser.ItemNew", args...); err != nil {
		t.Fatal(err)
	}
	waitState(t, events, func(s DiscoveryState) bool { return len(s.Services) == 1 })
	if err := m.conn.Emit("/services", Destination+".ServiceBrowser.ItemRemove", args...); err != nil {
		t.Fatal(err)
	}
	waitState(t, events, func(s DiscoveryState) bool { return s.Connected && len(s.Services) == 0 })
	release.Do(func() { close(m.resolveGate) })
	select {
	case s := <-events:
		if len(s.Services) > 0 {
			t.Fatal("late resolve resurrected service")
		}
	case <-time.After(200 * time.Millisecond):
	}
	if len(d.State().Services) != 0 {
		t.Fatal("removed service returned")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("browser leaked")
	}
}

func TestSlowSubscriberReceivesCurrentSnapshot(t *testing.T) {
	d := NewDiscovery()
	ch, close := d.Subscribe()
	defer close()
	i := Identity{Name: "example", Type: "_http._tcp", Domain: "local"}
	for range 100 {
		d.upsert(Service{Identity: i})
	}
	d.remove(i)
	select {
	case s := <-ch:
		if len(s.Services) != 0 {
			t.Fatal("slow subscriber retained removed service")
		}
	default:
		t.Fatal("missing latest state")
	}
}
