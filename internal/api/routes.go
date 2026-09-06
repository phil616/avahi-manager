package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"avahi-manager/internal/config"
	"avahi-manager/internal/database"
	"avahi-manager/internal/helper"
	"avahi-manager/internal/network"
)

type ConfigView struct {
	helper.Configuration
	Drift           []database.Drift `json:"drift"`
	ManagedSnapshot string           `json:"managedSnapshot"`
}

func (s *Server) configuration(ctx context.Context) (ConfigView, error) {
	var v ConfigView
	if _, err := s.opts.DB.Setting(ctx, "managed_snapshot"); errors.Is(err, sql.ErrNoRows) {
		if err = s.syncBaseline(ctx); err != nil {
			return v, err
		}
	} else if err != nil {
		return v, err
	}
	if err := s.opts.Helper.Call(ctx, "ReadConfiguration", helper.Empty{}, &v.Configuration); err != nil {
		return v, err
	}
	var err error
	v.Drift, err = s.opts.DB.ObserveFiles(ctx, v.Hashes)
	if err != nil {
		return v, err
	}
	v.ManagedSnapshot, err = s.opts.DB.Setting(ctx, "managed_snapshot")
	return v, err
}

func (s *Server) mutate(w http.ResponseWriter, r *http.Request, method string, input any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.notifyConfig()
	var result json.RawMessage
	err := s.opts.Helper.Call(r.Context(), method, input, &result)
	// Reconcile from the durable helper baseline even if a response was lost or
	// a rollback occurred. Never accept the current disk merely because it exists.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	syncErr := s.syncBaseline(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	if syncErr != nil {
		reject(w, 503, "reconcile_failed", "operation completed but baseline synchronization failed: "+syncErr.Error())
		return
	}
	write(w, 200, result)
}

func (s *Server) Handler(ui http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
	mux.HandleFunc("GET /api/v1/auth/session", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, r.Context().Value(stateKey{}).(*requestState).session)
	})
	mux.HandleFunc("GET /api/v1/config", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		v, err := s.configuration(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, v)
	})
	mux.HandleFunc("GET /api/v1/config/schema", func(w http.ResponseWriter, r *http.Request) { write(w, 200, config.Kinds) })
	mux.HandleFunc("GET /api/v1/config/events", s.configEvents)
	mux.HandleFunc("PUT /api/v1/config", func(w http.ResponseWriter, r *http.Request) {
		var v helper.WriteDaemon
		if body(w, r, &v) {
			s.mutate(w, r, "WriteDaemonConfig", v)
		}
	})
	mux.HandleFunc("POST /api/v1/config/reload", func(w http.ResponseWriter, r *http.Request) {
		var v helper.Revision
		if body(w, r, &v) {
			s.mutate(w, r, "AcceptDisk", v)
		}
	})
	mux.HandleFunc("GET /api/v1/config/managed", func(w http.ResponseWriter, r *http.Request) {
		var v helper.Snapshot
		if err := s.opts.Helper.Call(r.Context(), "ReadBaseline", helper.Empty{}, &v); err != nil {
			fail(w, err)
			return
		}
		write(w, 200, v)
	})
	mux.HandleFunc("GET /api/v1/services", func(w http.ResponseWriter, r *http.Request) {
		var v helper.Configuration
		if err := s.opts.Helper.Call(r.Context(), "ReadConfiguration", helper.Empty{}, &v); err != nil {
			fail(w, err)
			return
		}
		write(w, 200, map[string]any{"revision": v.Revision, "services": v.Services})
	})
	for _, method := range []string{"POST", "PUT"} {
		pattern := "POST /api/v1/services"
		if method == "PUT" {
			pattern = "PUT /api/v1/services/{id}"
		}
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			var v helper.WriteService
			if !body(w, r, &v) {
				return
			}
			if v.ID != "" && v.ID != r.PathValue("id") {
				reject(w, 400, "invalid_id", "service ID must match URL")
				return
			}
			v.ID = r.PathValue("id")
			s.mutate(w, r, "WriteService", v)
		})
	}
	mux.HandleFunc("DELETE /api/v1/services/{id}", func(w http.ResponseWriter, r *http.Request) {
		var v helper.Revision
		if body(w, r, &v) {
			s.mutate(w, r, "DeleteService", helper.DeleteService{Revision: v, ID: r.PathValue("id")})
		}
	})
	mux.HandleFunc("GET /api/v1/hosts", s.hostsList)
	mux.HandleFunc("POST /api/v1/hosts", s.hostsChange)
	mux.HandleFunc("PUT /api/v1/hosts/{id}", s.hostsChange)
	mux.HandleFunc("DELETE /api/v1/hosts/{id}", s.hostsChange)
	for action, rpc := range map[string]string{"start": "StartAvahi", "stop": "StopAvahi", "restart": "RestartAvahi", "reload": "ReloadAvahi", "enable": "EnableAvahi", "disable": "DisableAvahi"} {
		mux.HandleFunc("POST /api/v1/avahi/"+action, func(w http.ResponseWriter, r *http.Request) {
			var v helper.Empty
			if body(w, r, &v) {
				s.mutate(w, r, rpc, v)
			}
		})
	}
	mux.HandleFunc("GET /api/v1/avahi/status", func(w http.ResponseWriter, r *http.Request) { write(w, 200, s.opts.Status(r.Context())) })
	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		status := s.opts.Status(r.Context())
		v, err := s.configuration(r.Context())
		result := map[string]any{"avahi": status, "discoveredServices": len(s.opts.Discovery.State().Services)}
		if err != nil {
			result["configurationError"] = err.Error()
		} else {
			result["configuration"] = v
			result["publishedServices"] = len(v.Services)
		}
		write(w, 200, result)
	})
	mux.HandleFunc("GET /api/v1/interfaces", func(w http.ResponseWriter, r *http.Request) {
		var v helper.Configuration
		if err := s.opts.Helper.Call(r.Context(), "ReadConfiguration", helper.Empty{}, &v); err != nil {
			fail(w, err)
			return
		}
		items, err := network.Interfaces(strings.Split(v.Daemon["server"]["allow-interfaces"], ","), strings.Split(v.Daemon["server"]["deny-interfaces"], ","))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, items)
	})
	mux.HandleFunc("GET /api/v1/discovery", func(w http.ResponseWriter, r *http.Request) { write(w, 200, s.opts.Discovery.State()) })
	mux.HandleFunc("GET /api/v1/discovery/events", s.discoveryEvents)
	mux.HandleFunc("GET /api/v1/logs", s.logs)
	mux.HandleFunc("GET /api/v1/logs/events", s.logEvents)
	mux.HandleFunc("GET /api/v1/snapshots", func(w http.ResponseWriter, r *http.Request) {
		var list []config.Manifest
		if err := s.opts.Helper.Call(r.Context(), "ListSnapshots", helper.Empty{}, &list); err != nil {
			fail(w, err)
			return
		}
		if err := s.opts.DB.SyncSnapshots(r.Context(), list); err != nil {
			fail(w, err)
			return
		}
		write(w, 200, list)
	})
	mux.HandleFunc("POST /api/v1/snapshots", func(w http.ResponseWriter, r *http.Request) {
		var v helper.CreateSnapshot
		if body(w, r, &v) {
			s.mutate(w, r, "CreateSnapshot", v)
		}
	})
	mux.HandleFunc("GET /api/v1/snapshots/{id}", func(w http.ResponseWriter, r *http.Request) {
		var v helper.Snapshot
		if err := s.opts.Helper.Call(r.Context(), "ViewSnapshot", helper.SnapshotID{ID: r.PathValue("id")}, &v); err != nil {
			fail(w, err)
			return
		}
		write(w, 200, v)
	})
	mux.HandleFunc("DELETE /api/v1/snapshots/{id}", func(w http.ResponseWriter, r *http.Request) {
		var v helper.Empty
		if body(w, r, &v) {
			s.mutate(w, r, "DeleteSnapshot", helper.SnapshotID{ID: r.PathValue("id")})
		}
	})
	mux.HandleFunc("POST /api/v1/snapshots/{id}/restore", func(w http.ResponseWriter, r *http.Request) {
		var v helper.Revision
		if body(w, r, &v) {
			s.mutate(w, r, "RestoreSnapshot", helper.RestoreSnapshot{Revision: v, ID: r.PathValue("id")})
		}
	})
	mux.HandleFunc("GET /api/v1/audit", func(w http.ResponseWriter, r *http.Request) {
		before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
		list, err := s.opts.DB.Audits(r.Context(), before, 100)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, list)
	})
	mux.HandleFunc("GET /api/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		list, err := s.opts.DB.Jobs(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, list)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { reject(w, 404, "not_found", "API endpoint not found") })
	if ui != nil {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" && r.Method != "HEAD" {
				w.Header().Set("Allow", "GET, HEAD")
				reject(w, 405, "method_not_allowed", "static assets only support GET and HEAD")
				return
			}
			ui.ServeHTTP(w, r)
		})
	}
	return s.audit(s.security(mux))
}

type HostEntry struct {
	config.Host
	ID string `json:"id"`
}

func hostID(h config.Host) string {
	sum := sha256.Sum256([]byte(h.Address + "\x00" + h.Hostname))
	return hex.EncodeToString(sum[:])
}
func (s *Server) hostsList(w http.ResponseWriter, r *http.Request) {
	var v helper.Configuration
	if err := s.opts.Helper.Call(r.Context(), "ReadConfiguration", helper.Empty{}, &v); err != nil {
		fail(w, err)
		return
	}
	entries := []HostEntry{}
	for _, h := range v.Hosts {
		entries = append(entries, HostEntry{h, hostID(h)})
	}
	write(w, 200, map[string]any{"revision": v.Revision, "hosts": entries})
}
func (s *Server) hostsChange(w http.ResponseWriter, r *http.Request) {
	var input struct {
		helper.Revision
		Host *config.Host `json:"host,omitempty"`
	}
	if !body(w, r, &input) {
		return
	}
	if r.Method != "DELETE" && input.Host == nil {
		reject(w, 400, "invalid_host", "host required")
		return
	}
	var v helper.Configuration
	if err := s.opts.Helper.Call(r.Context(), "ReadConfiguration", helper.Empty{}, &v); err != nil {
		fail(w, err)
		return
	}
	if v.Revision != input.Revision.Revision {
		reject(w, 409, "conflict", "configuration changed on disk; reload before saving")
		return
	}
	if r.Method == "POST" {
		v.Hosts = append(v.Hosts, *input.Host)
	} else {
		found := false
		for i, h := range v.Hosts {
			if hostID(h) == r.PathValue("id") {
				found = true
				if r.Method == "DELETE" {
					v.Hosts = append(v.Hosts[:i], v.Hosts[i+1:]...)
				} else {
					v.Hosts[i] = *input.Host
				}
				break
			}
		}
		if !found {
			reject(w, 404, "not_found", "host not found")
			return
		}
	}
	s.mutate(w, r, "WriteHosts", helper.WriteHosts{Revision: input.Revision, Hosts: v.Hosts})
}

func (s *Server) discoveryEvents(w http.ResponseWriter, r *http.Request) {
	select {
	case s.streamSlots <- struct{}{}:
		defer func() { <-s.streamSlots }()
	default:
		reject(w, 429, "stream_limit", "too many event streams")
		return
	}
	f, ok := w.(http.Flusher)
	if !ok {
		reject(w, 500, "stream_unavailable", "streaming unsupported")
		return
	}
	ch, unsubscribe := s.opts.Discovery.Subscribe()
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	state := r.Context().Value(stateKey{}).(*requestState)
	rc := http.NewResponseController(w)
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			if time.Now().After(state.session.ExpiresAt) {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				return
			}
			rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err = fmt.Fprintf(w, "event: discovery\ndata: %s\n\n", data); err != nil {
				return
			}
			f.Flush()
		case <-ticker.C:
			if _, err := s.opts.Auth.Session(r.Context(), state.token); err != nil {
				return
			}
			rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			f.Flush()
		}
	}
}
