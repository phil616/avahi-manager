package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"avahi-manager/internal/config"
)

func (s *Server) notifyConfig() {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	for ch := range s.configSubscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (s *Server) WatchConfiguration(ctx context.Context, report func(error)) error {
	return config.Watch(ctx, s.opts.ConfigDir, func() { s.notifyConfig() }, report)
}

func (s *Server) configEvents(w http.ResponseWriter, r *http.Request) {
	ch := make(chan struct{}, 1)
	s.configMu.Lock()
	s.configSubscribers[ch] = struct{}{}
	s.configMu.Unlock()
	defer func() { s.configMu.Lock(); delete(s.configSubscribers, ch); s.configMu.Unlock() }()
	ch <- struct{}{}
	s.stream(w, r, "configuration", ch, func() any {
		s.mu.Lock()
		defer s.mu.Unlock()
		v, err := s.configuration(r.Context())
		if err != nil {
			return Error{"unavailable", err.Error()}
		}
		return v
	})
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request, event string, updates <-chan struct{}, value func() any) {
	select {
	case s.streamSlots <- struct{}{}:
		defer func() { <-s.streamSlots }()
	default:
		reject(w, 429, "stream_limit", "too many event streams")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		reject(w, 500, "stream_unavailable", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	state := r.Context().Value(stateKey{}).(*requestState)
	rc := http.NewResponseController(w)
	for {
		select {
		case <-r.Context().Done():
			return
		case <-updates:
			if time.Now().After(state.session.ExpiresAt) {
				return
			}
			payload, err := json.Marshal(value())
			if err != nil {
				return
			}
			rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload); err != nil {
				return
			}
			flusher.Flush()
		case <-tick.C:
			if _, err := s.opts.Auth.Session(r.Context(), state.token); err != nil {
				return
			}
			rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
