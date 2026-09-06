package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"avahi-manager/internal/auth"
	"avahi-manager/internal/avahi"
	"avahi-manager/internal/database"
	"avahi-manager/internal/detect"
	"avahi-manager/internal/helper"
)

type RPC interface {
	Call(context.Context, string, any, any) error
}
type Options struct {
	DB        *database.DB
	Auth      *auth.Auth
	Helper    RPC
	Discovery *avahi.Discovery
	Origins   []string
	ConfigDir string
	Status    func(context.Context) detect.Status
}
type Server struct {
	opts              Options
	hosts, origins    map[string]bool
	secure            bool
	mu                sync.Mutex
	streamSlots       chan struct{}
	configMu          sync.Mutex
	configSubscribers map[chan struct{}]struct{}
}
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type requestState struct {
	actor, token string
	session      auth.Session
}
type stateKey struct{}

func New(o Options) (*Server, error) {
	if o.DB == nil || o.Auth == nil || o.Helper == nil {
		return nil, fmt.Errorf("API requires database, auth and helper")
	}
	if o.Discovery == nil {
		o.Discovery = avahi.NewDiscovery()
	}
	if o.ConfigDir == "" {
		o.ConfigDir = "/etc/avahi"
	}
	if o.Status == nil {
		o.Status = func(ctx context.Context) detect.Status { return detect.Probe(ctx, o.ConfigDir) }
	}
	if len(o.Origins) == 0 {
		o.Origins = []string{"http://127.0.0.1:8053", "http://localhost:8053"}
	}
	s := &Server{opts: o, hosts: map[string]bool{}, origins: map[string]bool{}, streamSlots: make(chan struct{}, 16), configSubscribers: map[chan struct{}]struct{}{}}
	for _, origin := range o.Origins {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, fmt.Errorf("invalid public origin %q", origin)
		}
		s.hosts[u.Host] = true
		s.origins[origin] = true
		if u.Scheme == "https" {
			s.secure = true
		}
	}
	return s, nil
}

func (s *Server) syncBaseline(ctx context.Context) error {
	var baseline helper.Snapshot
	if err := s.opts.Helper.Call(ctx, "ReadBaseline", helper.Empty{}, &baseline); err != nil {
		return err
	}
	return s.opts.DB.SaveBaseline(ctx, baseline.Manifest, baseline.Configuration.Revision, baseline.Configuration.Hashes)
}

// Bootstrap accepts only the helper's persisted baseline, never a fresh disk
// read on every startup. External changes remain drift after a Manager restart.
func (s *Server) Bootstrap(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.opts.DB.InterruptJobs(ctx); err != nil {
		return err
	}
	return s.syncBaseline(ctx)
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func reject(w http.ResponseWriter, status int, code, message string) {
	write(w, status, Error{code, message})
}
func fail(w http.ResponseWriter, err error) {
	var e *helper.RPCError
	if errors.As(err, &e) {
		status := 422
		if e.Code == "conflict" {
			status = 409
		}
		reject(w, status, e.Code, e.Message)
		return
	}
	reject(w, 503, "unavailable", err.Error())
}

func body(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Header.Get("Content-Type") != "application/json" {
		reject(w, 415, "content_type", "Content-Type must be application/json")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, helper.MaxRequest))
	var raw json.RawMessage
	if err := d.Decode(&raw); err != nil {
		reject(w, 400, "invalid_json", err.Error())
		return false
	}
	if len(raw) == 0 || raw[0] != '{' {
		reject(w, 400, "invalid_json", "expected JSON object")
		return false
	}
	inner := json.NewDecoder(bytes.NewReader(raw))
	inner.DisallowUnknownFields()
	if err := inner.Decode(v); err != nil {
		reject(w, 400, "invalid_json", err.Error())
		return false
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		reject(w, 400, "invalid_json", "expected one JSON object")
		return false
	}
	return true
}

func (s *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		if !s.hosts[r.Host] {
			reject(w, 403, "host_rejected", "unrecognized Host")
			return
		}
		mutating := r.Method != "GET" && r.Method != "HEAD"
		origin := r.Header.Get("Origin")
		if (mutating && origin == "") || (origin != "" && !s.origins[origin]) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			reject(w, 403, "origin_rejected", "same-origin request required")
			return
		}
		state := r.Context().Value(stateKey{}).(*requestState)
		if r.URL.Path == "/api/v1/auth/login" && r.Method == "POST" {
			next.ServeHTTP(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie("awm_session")
		if err != nil {
			reject(w, 401, "unauthorized", "sign in required")
			return
		}
		session, err := s.opts.Auth.Session(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, auth.ErrSession) {
				reject(w, 401, "unauthorized", "session expired or invalid")
			} else {
				fail(w, err)
			}
			return
		}
		state.actor, state.token, state.session = session.Username, cookie.Value, session
		if mutating && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1 {
			reject(w, 403, "csrf_rejected", "invalid CSRF token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type buffered struct {
	header http.Header
	status int
	data   bytes.Buffer
}

func (w *buffered) Header() http.Header { return w.header }
func (w *buffered) WriteHeader(n int) {
	if w.status == 0 {
		w.status = n
	}
}
func (w *buffered) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.data.Write(b)
}

// Mutation responses are held until the result audit is durable. Request bodies
// (especially passwords and TXT secrets) are never copied into the audit log.
func (s *Server) audit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := &requestState{actor: "anonymous"}
		r = r.WithContext(context.WithValue(r.Context(), stateKey{}, state))
		if r.Method == "GET" || r.Method == "HEAD" {
			next.ServeHTTP(w, r)
			return
		}
		if cookie, err := r.Cookie("awm_session"); err == nil {
			if session, err := s.opts.Auth.Session(r.Context(), cookie.Value); err == nil {
				state.actor = session.Username
			}
		}
		id := rand.Text()
		detail := map[string]any{"requestId": id, "method": r.Method}
		if err := s.opts.DB.Audit(r.Context(), state.actor, r.Method, r.URL.Path, "started", detail); err != nil {
			reject(w, 503, "audit_unavailable", "cannot record operation; no action performed")
			return
		}
		b := &buffered{header: http.Header{}}
		next.ServeHTTP(b, r)
		if b.status == 0 {
			b.status = 200
		}
		detail["status"] = b.status
		result := "success"
		if b.status >= 400 {
			result = "failed"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.opts.DB.Audit(ctx, state.actor, r.Method, r.URL.Path, result, detail); err != nil {
			reject(w, 503, "audit_incomplete", "operation may have completed; audit completion failed; inspect current state before retrying")
			return
		}
		for k, v := range b.header {
			w.Header()[k] = v
		}
		w.WriteHeader(b.status)
		w.Write(b.data.Bytes())
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !body(w, r, &input) {
		return
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	token, session, err := s.opts.Auth.Login(r.Context(), ip, input.Username, input.Password)
	if err != nil {
		if errors.Is(err, auth.ErrRateLimit) {
			w.Header().Set("Retry-After", "900")
			reject(w, 429, "rate_limited", err.Error())
		} else if errors.Is(err, auth.ErrCredentials) {
			reject(w, 401, "invalid_credentials", err.Error())
		} else {
			fail(w, err)
		}
		return
	}
	r.Context().Value(stateKey{}).(*requestState).actor = session.Username
	http.SetCookie(w, &http.Cookie{Name: "awm_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: s.secure || r.TLS != nil, Expires: session.ExpiresAt, MaxAge: 43200})
	write(w, 200, session)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	var empty helper.Empty
	if !body(w, r, &empty) {
		return
	}
	state := r.Context().Value(stateKey{}).(*requestState)
	if err := s.opts.Auth.Logout(r.Context(), state.token); err != nil {
		fail(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "awm_session", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: s.secure || r.TLS != nil})
	write(w, 200, helper.Empty{})
}
