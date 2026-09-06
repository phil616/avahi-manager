package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"avahi-manager/internal/config"
	"golang.org/x/sys/unix"
)

const MaxRequest = 4 << 20

type RPCError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return e.Code + ": " + e.Message }

func peerUID(conn net.Conn) (uint32, error) {
	u, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("helper requires Unix sockets")
	}
	raw, err := u.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Ucred
	var credentialErr error
	err = raw.Control(func(fd uintptr) {
		cred, credentialErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if err != nil {
		return 0, err
	}
	if credentialErr != nil {
		return 0, credentialErr
	}
	return cred.Uid, nil
}

type peerListener struct {
	net.Listener
	UID uint32
}

func (l peerListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		uid, err := peerUID(c)
		if err != nil || uid != l.UID {
			c.Close()
			continue
		}
		return c, nil
	}
}

// Serve requires an already bound socket. The process entry point controls
// root ownership/mode and socket activation. Peer credentials are checked on
// every accepted connection, independent of HTTP headers or JSON identity.
func Serve(ctx context.Context, listener net.Listener, uid uint32, b *Backend) error {
	if b.Store == nil || b.Runtime == nil {
		return fmt.Errorf("helper store and runtime are required")
	}
	if err := b.Store.Recover(b.Runtime); err != nil {
		return fmt.Errorf("startup recovery: %w", err)
	}
	server := &http.Server{Handler: handler(b), BaseContext: func(net.Listener) context.Context { return ctx }, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 2 * time.Minute, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 55*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdown); err != nil {
				server.Close()
			}
		case <-done:
		}
	}()
	err := server.Serve(peerListener{listener, uid})
	close(done)
	<-stopped
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, err error) {
	status := http.StatusUnprocessableEntity
	code := "operation_failed"
	if errors.Is(err, config.ErrConflict) {
		status = http.StatusConflict
		code = "conflict"
	}
	reply(w, status, RPCError{code, err.Error()})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return fmt.Errorf("Content-Type must be application/json")
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxRequest))
	var raw json.RawMessage
	if err := d.Decode(&raw); err != nil {
		return err
	}
	if len(raw) == 0 || raw[0] != '{' {
		return fmt.Errorf("expected a JSON object")
	}
	inner := json.NewDecoder(bytes.NewReader(raw))
	inner.DisallowUnknownFields()
	if err := inner.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON object")
	}
	return nil
}

func rpc[T any](fn func(context.Context, T) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var v T
		if err := decode(w, r, &v); err != nil {
			reply(w, 400, RPCError{"invalid_request", err.Error()})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		result, err := fn(ctx, v)
		if err != nil {
			failure(w, err)
			return
		}
		reply(w, 200, result)
	}
}

func handler(b *Backend) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /rpc/ReadBaseline", rpc(func(_ context.Context, _ Empty) (any, error) {
		m, im, err := b.Store.Baseline()
		if err != nil {
			return nil, err
		}
		return Snapshot{m, model(im)}, nil
	}))
	mux.HandleFunc("POST /rpc/AcceptDisk", rpc(func(_ context.Context, r Revision) (any, error) {
		m, im, err := b.Store.AcceptDisk(r.Revision)
		if err != nil {
			return nil, err
		}
		return Snapshot{m, model(im)}, nil
	}))
	mux.HandleFunc("POST /rpc/ReadConfiguration", rpc(func(_ context.Context, _ Empty) (any, error) { return b.Configuration() }))
	mux.HandleFunc("POST /rpc/WriteDaemonConfig", rpc(func(ctx context.Context, r WriteDaemon) (any, error) { return b.WriteDaemon(ctx, r) }))
	mux.HandleFunc("POST /rpc/WriteHosts", rpc(func(ctx context.Context, r WriteHosts) (any, error) { return b.WriteHosts(ctx, r) }))
	mux.HandleFunc("POST /rpc/WriteService", rpc(func(ctx context.Context, r WriteService) (any, error) { return b.WriteService(ctx, r) }))
	mux.HandleFunc("POST /rpc/DeleteService", rpc(func(ctx context.Context, r DeleteService) (any, error) { return b.DeleteService(ctx, r) }))
	for method, action := range map[string]string{"StartAvahi": "start", "StopAvahi": "stop", "RestartAvahi": "restart", "ReloadAvahi": "reload", "EnableAvahi": "enable", "DisableAvahi": "disable"} {
		mux.HandleFunc("POST /rpc/"+method, rpc(func(ctx context.Context, _ Empty) (any, error) {
			err := b.Runtime.Control(ctx, action)
			if err == nil && (action == "start" || action == "restart" || action == "reload") {
				err = b.Runtime.Healthy(ctx)
			}
			return Empty{}, err
		}))
	}
	mux.HandleFunc("POST /rpc/CreateSnapshot", rpc(func(_ context.Context, r CreateSnapshot) (any, error) { return b.Store.Snapshot(r.Reason) }))
	mux.HandleFunc("POST /rpc/ListSnapshots", rpc(func(_ context.Context, _ Empty) (any, error) { return b.Store.Snapshots() }))
	mux.HandleFunc("POST /rpc/ViewSnapshot", rpc(func(_ context.Context, r SnapshotID) (any, error) {
		m, im, err := b.Store.SnapshotContent(r.ID)
		if err != nil {
			return nil, err
		}
		return Snapshot{m, model(im)}, nil
	}))
	mux.HandleFunc("POST /rpc/DeleteSnapshot", rpc(func(_ context.Context, r SnapshotID) (any, error) { return Empty{}, b.Store.DeleteSnapshot(r.ID) }))
	mux.HandleFunc("POST /rpc/RestoreSnapshot", rpc(func(ctx context.Context, r RestoreSnapshot) (any, error) {
		return b.Store.Restore(ctx, r.ID, r.Revision.Revision, b.Runtime)
	}))
	// Serialize lifecycle, apply and snapshot RPCs: a concurrent Stop must not
	// interrupt the health check of a configuration transaction.
	var mu sync.Mutex
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Context().Err() != nil {
			return
		}
		mux.ServeHTTP(w, r)
	})
}
