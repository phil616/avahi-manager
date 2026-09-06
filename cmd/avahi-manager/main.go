package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"avahi-manager/internal/avahi"
	"avahi-manager/internal/detect"
	"avahi-manager/internal/network"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: avahi-manager detect [--config-dir /etc/avahi] | interfaces | discover [--duration 10s]")
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "init-admin":
		return runInitAdmin(args[1:])
	case "helper":
		return runHelper(args[1:])
	case "detect":
		fs := flag.NewFlagSet("detect", flag.ContinueOnError)
		dir := fs.String("config-dir", "/etc/avahi", "Avahi native configuration directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return output(detect.Probe(ctx, *dir))
	case "interfaces":
		list, err := network.Interfaces(nil, nil)
		if err != nil {
			return err
		}
		return output(list)
	case "discover":
		fs := flag.NewFlagSet("discover", flag.ContinueOnError)
		duration := fs.Duration("duration", 10*time.Second, "duration to listen for Avahi D-Bus discovery events")
		stream := fs.Bool("events", false, "stream cache updates instead of printing only the final discovery snapshot")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *duration <= 0 || *duration > time.Hour {
			return fmt.Errorf("duration must be between 0 and 1h")
		}
		parent, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		ctx, cancel := context.WithTimeout(parent, *duration)
		defer cancel()
		d := avahi.NewDiscovery()
		events, unsubscribe := d.Subscribe()
		defer unsubscribe()
		done := make(chan struct{})
		go func() { defer close(done); d.Run(ctx) }()
		defer func() { cancel(); <-done }()
		var last avahi.DiscoveryState
		for {
			select {
			case <-ctx.Done():
				if !*stream {
					return output(last)
				}
				return nil
			case event := <-events:
				last = event
				if *stream {
					if err := output(event); err != nil {
						return err
					}
				}
			}
		}
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func output(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
