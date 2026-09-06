package journal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Query struct {
	Search   string
	Priority string
	Since    string
	Until    string
	Limit    int
}
type Entry struct {
	Cursor     string    `json:"cursor"`
	Timestamp  time.Time `json:"timestamp"`
	Priority   int       `json:"priority"`
	Message    string    `json:"message"`
	Identifier string    `json:"identifier"`
}

func Arguments(q Query, follow bool) ([]string, error) {
	if q.Limit == 0 {
		q.Limit = 200
	}
	if q.Limit < 1 || q.Limit > 1000 {
		return nil, fmt.Errorf("log limit must be 1–1000")
	}
	args := []string{"--unit", "avahi-daemon.service", "--output=json", "--no-pager", "--all"}
	if follow {
		args = append(args, "--follow", "--lines=0")
	} else {
		args = append(args, "--lines", strconv.Itoa(q.Limit))
	}
	if len(q.Search) > 256 || strings.ContainsRune(q.Search, 0) {
		return nil, fmt.Errorf("invalid log search")
	}
	if q.Search != "" {
		args = append(args, "--grep", regexp.QuoteMeta(q.Search), "--case-sensitive=no")
	}
	level := map[string]string{"info": "6", "warning": "4", "error": "3", "all": "", "": ""}
	p, ok := level[q.Priority]
	if !ok {
		return nil, fmt.Errorf("invalid priority")
	}
	if p != "" {
		args = append(args, "--priority", p)
	}
	var since, until time.Time
	for _, v := range []struct {
		flag, value string
		target      *time.Time
	}{{"--since", q.Since, &since}, {"--until", q.Until, &until}} {
		if v.value != "" {
			t, err := time.Parse(time.RFC3339, v.value)
			if err != nil {
				return nil, fmt.Errorf("%s requires RFC3339 timestamp", v.flag)
			}
			*v.target = t
			args = append(args, v.flag, t.UTC().Format("2006-01-02 15:04:05 UTC"))
		}
	}
	if !since.IsZero() && !until.IsZero() && since.After(until) {
		return nil, fmt.Errorf("since must precede until")
	}
	return args, nil
}

func Parse(data []byte) (Entry, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return Entry{}, err
	}
	text := func(key string) string {
		var value string
		if json.Unmarshal(fields[key], &value) == nil {
			return value
		}
		var binary []byte
		if json.Unmarshal(fields[key], &binary) == nil {
			return string(binary)
		}
		return ""
	}
	micros, err := strconv.ParseInt(text("__REALTIME_TIMESTAMP"), 10, 64)
	if err != nil {
		return Entry{}, fmt.Errorf("invalid journal timestamp")
	}
	p := 6
	if raw := text("PRIORITY"); raw != "" {
		p, err = strconv.Atoi(raw)
		if err != nil || p < 0 || p > 7 {
			return Entry{}, fmt.Errorf("invalid journal priority")
		}
	}
	return Entry{Cursor: text("__CURSOR"), Timestamp: time.UnixMicro(micros).UTC(), Priority: p, Message: text("MESSAGE"), Identifier: text("SYSLOG_IDENTIFIER")}, nil
}

type Reader struct{}
type cappedBuffer struct{ bytes.Buffer }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < 8192 {
		limit := 8192 - b.Len()
		if len(p) > limit {
			p = p[:limit]
		}
		b.Buffer.Write(p)
	}
	return n, nil
}

func (Reader) run(ctx context.Context, q Query, follow bool, emit func(Entry) error) error {
	args, err := Arguments(q, follow)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/journalctl", args...)
	cmd.Env = []string{"LANG=C.UTF-8", "LC_ALL=C.UTF-8", "PATH=/usr/bin:/bin"}
	cmd.WaitDelay = 2 * time.Second
	var stderr cappedBuffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	total := 0
	for scanner.Scan() {
		total += len(scanner.Bytes())
		if !follow && total > 8<<20 {
			err = fmt.Errorf("journal response exceeds 8 MiB")
			break
		}
		var e Entry
		e, err = Parse(scanner.Bytes())
		if err != nil {
			break
		}
		if err = emit(e); err != nil {
			break
		}
	}
	if err == nil {
		err = scanner.Err()
	}
	if err != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// journalctl returns 1 when --grep matches no entries; other errors retain
	// their diagnostic stderr and are not presented as an empty healthy journal.
	var exit *exec.ExitError
	if errors.As(waitErr, &exit) && exit.ExitCode() == 1 && total == 0 && stderr.Len() == 0 && q.Search != "" {
		return nil
	}
	if waitErr != nil {
		return fmt.Errorf("journalctl: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (r Reader) Read(ctx context.Context, q Query) ([]Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	entries := []Entry{}
	err := r.run(ctx, q, false, func(e Entry) error { entries = append(entries, e); return nil })
	return entries, err
}
func (r Reader) Follow(ctx context.Context, q Query, emit func(Entry) error) error {
	return r.run(ctx, q, true, emit)
}
