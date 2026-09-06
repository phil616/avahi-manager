package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"avahi-manager/internal/journal"
)

func logQuery(r *http.Request) (journal.Query, error) {
	q := r.URL.Query()
	limit := 200
	if q.Has("limit") {
		var err error
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil {
			return journal.Query{}, err
		}
	}
	v := journal.Query{Search: q.Get("search"), Priority: q.Get("priority"), Since: q.Get("since"), Until: q.Get("until"), Limit: limit}
	_, err := journal.Arguments(v, false)
	return v, err
}
func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	q, err := logQuery(r)
	if err != nil {
		reject(w, 400, "invalid_query", err.Error())
		return
	}
	entries, err := (journal.Reader{}).Read(r.Context(), q)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, entries)
}
func (s *Server) logEvents(w http.ResponseWriter, r *http.Request) {
	q, err := logQuery(r)
	if err != nil {
		reject(w, 400, "invalid_query", err.Error())
		return
	}
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
	ctx, cancel := context.WithCancel(r.Context())
	entries := make(chan journal.Entry, 100)
	done := make(chan error, 1)
	go func() {
		done <- (journal.Reader{}).Follow(ctx, q, func(e journal.Entry) error {
			select {
			case entries <- e:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	finished := false
	defer func() {
		cancel()
		if !finished {
			<-done
		}
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, ": connected\n\n")
	f.Flush()
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	rc := http.NewResponseController(w)
	state := r.Context().Value(stateKey{}).(*requestState)
	send := func(event string, v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		f.Flush()
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-done:
			finished = true
			if err != nil && ctx.Err() == nil {
				send("failure", Error{"journal_unavailable", err.Error()})
			}
			return
		case e := <-entries:
			if time.Now().After(state.session.ExpiresAt) {
				return
			}
			if send("log", e) != nil {
				return
			}
		case <-tick.C:
			if _, err := s.opts.Auth.Session(ctx, state.token); err != nil {
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
