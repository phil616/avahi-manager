package systemd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"avahi-manager/internal/avahi"
	"avahi-manager/internal/config"
	"github.com/coreos/go-systemd/v22/dbus"
)

const ServiceUnit = "avahi-daemon.service"
const SocketUnit = "avahi-daemon.socket"

type Unit struct {
	Name          string `json:"name"`
	LoadState     string `json:"loadState"`
	ActiveState   string `json:"activeState"`
	SubState      string `json:"subState"`
	UnitFileState string `json:"unitFileState"`
	Error         string `json:"error,omitempty"`
}

type Status struct {
	Available bool   `json:"available"`
	Service   Unit   `json:"service"`
	Socket    Unit   `json:"socket"`
	Error     string `json:"error,omitempty"`
}

type Bus interface {
	GetUnitPropertiesContext(context.Context, string) (map[string]any, error)
	StartUnitContext(context.Context, string, string, chan<- string) (int, error)
	StopUnitContext(context.Context, string, string, chan<- string) (int, error)
	RestartUnitContext(context.Context, string, string, chan<- string) (int, error)
	ReloadUnitContext(context.Context, string, string, chan<- string) (int, error)
	EnableUnitFilesContext(context.Context, []string, bool, bool) (bool, []dbus.EnableUnitFileChange, error)
	DisableUnitFilesContext(context.Context, []string, bool) ([]dbus.DisableUnitFileChange, error)
	ReloadContext(context.Context) error
}

type Manager struct{ Bus Bus }

func Connect(ctx context.Context) (*Manager, func(), error) {
	c, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	return &Manager{c}, c.Close, nil
}

func (m *Manager) Status(ctx context.Context) Status {
	s := Status{Available: true, Service: m.unit(ctx, ServiceUnit), Socket: m.unit(ctx, SocketUnit)}
	return s
}

func (m *Manager) unit(ctx context.Context, name string) Unit {
	u := Unit{Name: name}
	p, err := m.Bus.GetUnitPropertiesContext(ctx, name)
	if err != nil {
		u.Error = err.Error()
		return u
	}
	u.LoadState, _ = p["LoadState"].(string)
	u.ActiveState, _ = p["ActiveState"].(string)
	u.SubState, _ = p["SubState"].(string)
	u.UnitFileState, _ = p["UnitFileState"].(string)
	return u
}

func (m *Manager) job(ctx context.Context, action, unit string) error {
	done := make(chan string, 1)
	var err error
	switch action {
	case "start":
		_, err = m.Bus.StartUnitContext(ctx, unit, "replace", done)
	case "stop":
		_, err = m.Bus.StopUnitContext(ctx, unit, "replace", done)
	case "restart":
		_, err = m.Bus.RestartUnitContext(ctx, unit, "replace", done)
	case "reload":
		_, err = m.Bus.ReloadUnitContext(ctx, unit, "replace", done)
	default:
		return fmt.Errorf("unsupported systemd action")
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w", action, unit, err)
	}
	select {
	case result := <-done:
		if result != "done" {
			return fmt.Errorf("%s %s job: %s", action, unit, result)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) Control(ctx context.Context, action string) error {
	switch action {
	case "start":
		if err := m.job(ctx, "start", SocketUnit); err != nil {
			return err
		}
		return m.job(ctx, "start", ServiceUnit)
	case "stop":
		// Attempt both even if one job fails. Never leave socket activation enabled
		// merely because stopping the service failed.
		a := m.job(ctx, "stop", ServiceUnit)
		b := m.job(ctx, "stop", SocketUnit)
		if a == nil && b == nil && m.unit(ctx, ServiceUnit).ActiveState == "active" {
			return m.job(ctx, "stop", ServiceUnit)
		}
		return errors.Join(a, b)
	case "restart", "reload":
		return m.job(ctx, action, ServiceUnit)
	case "enable":
		_, _, err := m.Bus.EnableUnitFilesContext(ctx, []string{ServiceUnit, SocketUnit}, false, false)
		if err != nil {
			return err
		}
		return m.Bus.ReloadContext(ctx)
	case "disable":
		_, err := m.Bus.DisableUnitFilesContext(ctx, []string{ServiceUnit, SocketUnit}, false)
		if err != nil {
			return err
		}
		return m.Bus.ReloadContext(ctx)
	default:
		return fmt.Errorf("unsupported action %q", action)
	}
}

type Controller struct {
	Manager *Manager
	Avahi   *avahi.Client
}

func (c Controller) Apply(ctx context.Context, a config.Action) error {
	if a == config.NoAction {
		return nil
	}
	if a != config.Restart && a != config.Reload {
		return fmt.Errorf("invalid apply action")
	}
	return c.Manager.Control(ctx, string(a))
}

func (c Controller) Healthy(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	var last error
	for {
		u := c.Manager.unit(ctx, ServiceUnit)
		s, err := c.Avahi.Status(ctx)
		if u.ActiveState == "active" && err == nil && s.State == 2 {
			return nil
		}
		last = fmt.Errorf("service=%s, Avahi state=%d: %v", u.ActiveState, s.State, err)
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), last)
		case <-tick.C:
		}
	}
}
