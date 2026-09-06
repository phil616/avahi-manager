package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"

	"avahi-manager/internal/avahi"
	"avahi-manager/internal/config"
	"avahi-manager/internal/helper"
	"avahi-manager/internal/systemd"
	"github.com/coreos/go-systemd/v22/activation"
)

type helperRuntime struct{}

func (r helperRuntime) Control(ctx context.Context, action string) error {
	m, close, err := systemd.Connect(ctx)
	if err != nil {
		return err
	}
	defer close()
	return m.Control(ctx, action)
}

func (r helperRuntime) Apply(ctx context.Context, action config.Action) error {
	if action == config.NoAction {
		return nil
	}
	if action != config.Reload && action != config.Restart {
		return fmt.Errorf("invalid apply action")
	}
	return r.Control(ctx, string(action))
}

func (r helperRuntime) Healthy(ctx context.Context) error {
	m, close, err := systemd.Connect(ctx)
	if err != nil {
		return err
	}
	defer close()
	a, err := avahi.Connect()
	if err != nil {
		return err
	}
	defer a.Close()
	return (systemd.Controller{Manager: m, Avahi: a}).Healthy(ctx)
}

func runHelper(args []string) error {
	fs := flag.NewFlagSet("helper", flag.ContinueOnError)
	configDir := fs.String("config-dir", "/etc/avahi", "Avahi configuration directory")
	backupDir := fs.String("backups", "/var/lib/avahi-manager/backups", "root-owned backup directory")
	socket := fs.String("socket", "/run/avahi-manager/helper.sock", "Unix socket (unless socket-activated)")
	account := fs.String("manager-user", "avahi-manager", "only account allowed to call helper")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected helper arguments")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("helper must run as root")
	}
	u, err := user.Lookup(*account)
	if err != nil {
		return err
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return err
	}
	if uid == 0 {
		return fmt.Errorf("manager account must be non-root")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	s, err := config.OpenStore(*configDir, *backupDir)
	if err != nil {
		return err
	}
	defer s.Close()
	listeners, err := activation.Listeners()
	if err != nil {
		return err
	}
	var listener net.Listener
	if len(listeners) > 0 {
		if len(listeners) != 1 {
			for _, l := range listeners {
				if l != nil {
					l.Close()
				}
			}
			return fmt.Errorf("helper expects exactly one activated socket")
		}
		listener = listeners[0]
		if listener == nil || listener.Addr().Network() != "unix" {
			if listener != nil {
				listener.Close()
			}
			return fmt.Errorf("helper requires a Unix activation socket")
		}
	} else {
		// Never remove an existing socket here: it may belong to a live helper.
		listener, err = net.Listen("unix", *socket)
		if err != nil {
			return err
		}
		if err = os.Chown(*socket, 0, gid); err == nil {
			err = os.Chmod(*socket, 0660)
		}
		if err != nil {
			listener.Close()
			return err
		}
	}
	defer listener.Close()
	return helper.Serve(ctx, listener, uint32(uid), &helper.Backend{Store: s, Runtime: helperRuntime{}})
}
