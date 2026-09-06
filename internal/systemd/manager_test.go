package systemd

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
)

type fakeBus struct {
	calls  []string
	fail   string
	result string
	block  bool
}

func (f *fakeBus) GetUnitPropertiesContext(context.Context, string) (map[string]any, error) {
	return map[string]any{"LoadState": "loaded", "ActiveState": "inactive", "SubState": "dead", "UnitFileState": "enabled"}, nil
}
func (f *fakeBus) job(a, u string, ch chan<- string) (int, error) {
	f.calls = append(f.calls, a+" "+u)
	if f.fail == a+" "+u {
		return 0, errors.New("failure")
	}
	if !f.block {
		result := f.result
		if result == "" {
			result = "done"
		}
		ch <- result
	}
	return 1, nil
}
func (f *fakeBus) StartUnitContext(_ context.Context, u, _ string, ch chan<- string) (int, error) {
	return f.job("start", u, ch)
}
func (f *fakeBus) StopUnitContext(_ context.Context, u, _ string, ch chan<- string) (int, error) {
	return f.job("stop", u, ch)
}
func (f *fakeBus) RestartUnitContext(_ context.Context, u, _ string, ch chan<- string) (int, error) {
	return f.job("restart", u, ch)
}
func (f *fakeBus) ReloadUnitContext(_ context.Context, u, _ string, ch chan<- string) (int, error) {
	return f.job("reload", u, ch)
}
func (f *fakeBus) EnableUnitFilesContext(_ context.Context, units []string, _, _ bool) (bool, []dbus.EnableUnitFileChange, error) {
	for _, u := range units {
		f.calls = append(f.calls, "enable "+u)
	}
	return true, nil, nil
}
func (f *fakeBus) DisableUnitFilesContext(_ context.Context, units []string, _ bool) ([]dbus.DisableUnitFileChange, error) {
	for _, u := range units {
		f.calls = append(f.calls, "disable "+u)
	}
	return nil, nil
}
func (f *fakeBus) ReloadContext(context.Context) error {
	f.calls = append(f.calls, "daemon-reload")
	return nil
}

func TestLifecycleExactUnitsAndOrder(t *testing.T) {
	for _, tt := range []struct {
		action string
		want   []string
	}{
		{"start", []string{"start " + SocketUnit, "start " + ServiceUnit}},
		{"stop", []string{"stop " + ServiceUnit, "stop " + SocketUnit}},
		{"restart", []string{"restart " + ServiceUnit}},
		{"reload", []string{"reload " + ServiceUnit}},
		{"enable", []string{"enable " + ServiceUnit, "enable " + SocketUnit, "daemon-reload"}},
		{"disable", []string{"disable " + ServiceUnit, "disable " + SocketUnit, "daemon-reload"}},
	} {
		t.Run(tt.action, func(t *testing.T) {
			b := &fakeBus{}
			m := Manager{b}
			if err := m.Control(context.Background(), tt.action); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(b.calls, tt.want) {
				t.Fatalf("got %v want %v", b.calls, tt.want)
			}
		})
	}
}

func TestStopStillStopsSocketOnServiceFailure(t *testing.T) {
	b := &fakeBus{fail: "stop " + ServiceUnit}
	m := Manager{b}
	if err := m.Control(context.Background(), "stop"); err == nil {
		t.Fatal("lost failure")
	}
	if len(b.calls) != 2 || b.calls[1] != "stop "+SocketUnit {
		t.Fatal(b.calls)
	}
}

func TestRejectArbitraryActionAndFailedJobs(t *testing.T) {
	b := &fakeBus{}
	m := Manager{b}
	if err := m.Control(context.Background(), "start ssh.service"); err == nil || len(b.calls) != 0 {
		t.Fatal("arbitrary action accepted")
	}
	b.result = "failed"
	if err := m.Control(context.Background(), "restart"); err == nil {
		t.Fatal("accepted failed job")
	}
	b.block = true
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := m.Control(ctx, "restart"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
