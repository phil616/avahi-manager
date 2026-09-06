package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"avahi-manager/frontend"
	"avahi-manager/internal/api"
	"avahi-manager/internal/auth"
	"avahi-manager/internal/avahi"
	"avahi-manager/internal/database"
	"avahi-manager/internal/helper"
	"golang.org/x/term"
)

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := fs.String("listen", "127.0.0.1:8053", "HTTP listen address")
	origins := fs.String("origins", "http://127.0.0.1:8053,http://localhost:8053", "comma-separated exact browser origins")
	dbPath := fs.String("database", "/var/lib/avahi-manager/manager.db", "manager SQLite database")
	socket := fs.String("helper", "/run/avahi-manager/helper.sock", "root helper Unix socket")
	dir := fs.String("config-dir", "/etc/avahi", "native Avahi configuration directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected serve arguments")
	}
	if os.Geteuid() == 0 {
		return fmt.Errorf("HTTP server must run as a non-root account")
	}
	db, err := database.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	a, err := auth.New(db)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	has, err := a.HasAdmin(ctx)
	if err != nil {
		return err
	}
	if !has {
		return fmt.Errorf("administrator is not initialized; run init-admin as the manager account first")
	}
	h := helper.NewClient(*socket, 0)
	defer h.Close()
	discovery := avahi.NewDiscovery()
	s, err := api.New(api.Options{DB: db, Auth: a, Helper: h, Discovery: discovery, Origins: strings.Split(*origins, ","), ConfigDir: *dir})
	if err != nil {
		return err
	}
	bootstrap, cancel := context.WithTimeout(ctx, 15*time.Second)
	err = s.Bootstrap(bootstrap)
	cancel()
	if err != nil {
		log.Printf("configuration bootstrap unavailable: %v; status API remains available", err)
	}
	done := make(chan struct{})
	go func() { defer close(done); discovery.Run(ctx) }()
	defer func() { stop(); <-done }()
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		err := s.WatchConfiguration(ctx, func(err error) { log.Printf("configuration watcher: %v", err) })
		if err != nil && ctx.Err() == nil {
			log.Printf("configuration watcher stopped: %v", err)
		}
	}()
	defer func() { stop(); <-watchDone }()
	server := &http.Server{Addr: *listen, Handler: s.Handler(frontend.Handler()), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384, BaseContext: func(net.Listener) context.Context { return ctx }}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	shutdownDone := make(chan struct{})
	serveDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdown); err != nil {
				server.Close()
			}
		case <-serveDone:
		}
	}()
	log.Printf("Avahi Manager API listening on %s", listener.Addr())
	err = server.Serve(listener)
	close(serveDone)
	<-shutdownDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func runInitAdmin(args []string) error {
	fs := flag.NewFlagSet("init-admin", flag.ContinueOnError)
	dbPath := fs.String("database", "/var/lib/avahi-manager/manager.db", "manager SQLite database")
	username := fs.String("username", "admin", "administrator username")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected init-admin arguments")
	}
	if os.Geteuid() == 0 {
		return fmt.Errorf("initialize the administrator as the non-root manager account")
	}
	db, err := database.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	a, err := auth.New(db)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if has, err := a.HasAdmin(ctx); err != nil {
		return err
	} else if has {
		return fmt.Errorf("administrator already exists")
	}
	var raw []byte
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "Administrator password (at least 12 bytes): ")
		raw, err = term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
	} else {
		raw, err = io.ReadAll(io.LimitReader(os.Stdin, 1026))
		raw = []byte(strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r"))
	}
	if err != nil {
		return err
	}
	if err = a.CreateAdmin(ctx, *username, string(raw)); err != nil {
		return err
	}
	if err = db.Audit(ctx, *username, "CreateAdmin", "administrator", "success", nil); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "Administrator initialized.")
	return nil
}
