package detect

import (
	"testing"

	"avahi-manager/internal/avahi"
	"avahi-manager/internal/systemd"
)

func TestStatusClassification(t *testing.T) {
	base := Status{Installed: true, Systemd: systemd.Status{Available: true, Service: systemd.Unit{ActiveState: "active", LoadState: "loaded"}, Socket: systemd.Unit{ActiveState: "active"}}, Avahi: avahi.State{Available: true, State: 2}}
	for _, tt := range []struct {
		want   string
		change func(*Status)
	}{
		{"RUNNING", func(*Status) {}},
		{"NOT_INSTALLED", func(s *Status) { s.Installed = false }},
		{"INSTALLED_STOPPED", func(s *Status) { s.Systemd.Service.ActiveState = "inactive"; s.Avahi.Available = false }},
		{"CONFIG_ERROR", func(s *Status) { s.ConfigErrors = []string{"invalid XML"} }},
		{"DBUS_UNAVAILABLE", func(s *Status) { s.Systemd.Available = false }},
		{"DBUS_UNAVAILABLE", func(s *Status) { s.Avahi.Available = false }},
		{"RUNNING_DEGRADED", func(s *Status) { s.Avahi.State = 3 }},
		{"RUNNING_DEGRADED", func(s *Status) { s.Systemd.Service.ActiveState = "failed" }},
		{"RUNNING_DEGRADED", func(s *Status) { s.Systemd.Socket.ActiveState = "inactive" }},
		{"UPDATE_AVAILABLE", func(s *Status) { s.UpdateAvailable = true }},
	} {
		t.Run(tt.want, func(t *testing.T) {
			s := base
			tt.change(&s)
			if got := Classify(s); got != tt.want {
				t.Fatalf("got %s want %s", got, tt.want)
			}
		})
	}
}

func TestOSRelease(t *testing.T) {
	s := OSRelease([]byte("# comment\nID=ubuntu\nPRETTY_NAME=\"Ubuntu 24.04\"\nID_LIKE='debian'\n"))
	if s["ID"] != "ubuntu" || s["PRETTY_NAME"] != "Ubuntu 24.04" || s["ID_LIKE"] != "debian" {
		t.Fatal(s)
	}
}
