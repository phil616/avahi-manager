package detect

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"avahi-manager/internal/avahi"
	"avahi-manager/internal/config"
	"avahi-manager/internal/systemd"
)

type Status struct {
	State           string            `json:"state"`
	OS              map[string]string `json:"os"`
	Installed       bool              `json:"installed"`
	Systemd         systemd.Status    `json:"systemd"`
	Avahi           avahi.State       `json:"avahi"`
	Files           map[string]bool   `json:"files"`
	ConfigErrors    []string          `json:"configErrors"`
	CheckedAt       time.Time         `json:"checkedAt"`
	UpdateAvailable bool              `json:"updateAvailable"`
}

func OSRelease(data []byte) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok {
			result[key] = strings.Trim(value, "\"'")
		}
	}
	return result
}

func Probe(ctx context.Context, dir string) Status {
	s := Status{Files: map[string]bool{}, ConfigErrors: []string{}, CheckedAt: time.Now().UTC()}
	osData, _ := os.ReadFile("/etc/os-release")
	s.OS = OSRelease(osData)
	info, err := os.Stat("/usr/sbin/avahi-daemon")
	s.Installed = err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
	m, close, err := systemd.Connect(ctx)
	if err != nil {
		s.Systemd.Error = err.Error()
	} else {
		s.Systemd = m.Status(ctx)
		close()
	}
	a, err := avahi.Connect()
	if err != nil {
		s.Avahi.Error = err.Error()
	} else {
		s.Avahi, _ = a.Status(ctx)
		a.Close()
	}
	for _, p := range []string{"avahi-daemon.conf", "hosts", "services"} {
		_, err := os.Stat(filepath.Join(dir, p))
		s.Files[p] = err == nil
	}
	if s.Installed {
		im, err := config.ReadDirectory(dir)
		if err == nil {
			err = config.ValidateImage(im)
		}
		if err != nil {
			s.ConfigErrors = append(s.ConfigErrors, err.Error())
		}
	}
	s.State = Classify(s)
	return s
}

func Classify(s Status) string {
	if !s.Installed {
		return "NOT_INSTALLED"
	}
	if len(s.ConfigErrors) > 0 {
		return "CONFIG_ERROR"
	}
	if !s.Systemd.Available {
		return "DBUS_UNAVAILABLE"
	}
	if s.Systemd.Service.Error != "" || s.Systemd.Service.LoadState == "not-found" {
		return "RUNNING_DEGRADED"
	}
	if s.Systemd.Service.ActiveState != "active" {
		if s.Systemd.Service.ActiveState == "failed" {
			return "RUNNING_DEGRADED"
		}
		return "INSTALLED_STOPPED"
	}
	if !s.Avahi.Available {
		return "DBUS_UNAVAILABLE"
	}
	if s.Avahi.State != 2 || s.Systemd.Socket.ActiveState != "active" {
		return "RUNNING_DEGRADED"
	}
	if s.UpdateAvailable {
		return "UPDATE_AVAILABLE"
	}
	return "RUNNING"
}
