package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestDaemonValidation(t *testing.T) {
	for _, tt := range []struct {
		name, input string
		valid       bool
	}{
		{"defaults", "# preserved until save\n[server]\nuse-ipv4=yes\nuse-ipv6=yes\n", true},
		{"sections", "[wide-area]\nenable-wide-area=yes\n[reflector]\nenable-reflector=no\n[rlimits]\nrlimit-core=0\n", true},
		{"duplicate", "[server]\nhost-name=one\nhost-name=two\n", false},
		{"no section", "host-name=x", false},
		{"bad bool", "[server]\nuse-ipv4=true", false},
		{"no protocols", "[server]\nuse-ipv4=no\nuse-ipv6=no", false},
		{"bad host", "[server]\nhost-name=bad.local", false},
		{"unknown", "[server]\nmagic=yes", false},
		{"negative limit", "[rlimits]\nrlimit-nofile=-1", false},
		{"interfaces", "[server]\nallow-interfaces=eth0, wg0", true},
		{"bad interfaces", "[server]\nallow-interfaces=eth0, ../etc", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, err := ParseDaemon([]byte(tt.input))
			if err == nil {
				err = d.Validate()
			}
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%v error=%v", tt.valid, err)
			}
			if err == nil {
				b, err := d.Marshal()
				if err != nil {
					t.Fatal(err)
				}
				again, err := ParseDaemon(b)
				if err != nil {
					t.Fatal(err)
				}
				b2, _ := again.Marshal()
				if !bytes.Equal(b, b2) {
					t.Fatal("unstable serialization")
				}
			}
		})
	}
	d := Daemon{"server": {"host-name": "foo\nuse-ipv4=no"}}
	if _, err := d.Marshal(); err == nil {
		t.Fatal("accepted newline injection")
	}
}

func TestHosts(t *testing.T) {
	good := []byte("# local hosts\n192.168.1.2 nas.local # NAS\n2001:db8::1 nas.local\n")
	h, err := ParseHosts(good)
	if err != nil || len(h) != 2 {
		t.Fatalf("%v %v", h, err)
	}
	b, err := MarshalHosts(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ParseHosts(b); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"not-ip nas.local", "192.168.1.1 nas", "192.168.1.1 nas.local alias.local", "192.168.1.1 NAS.local\n192.168.1.1 nas.local.", "ff02::fb multicast.local", "fe80::1%eth0 nas.local", "0.0.0.0 nas.local"} {
		if _, err := ParseHosts([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

const serviceXML = `<?xml version="1.0"?><!DOCTYPE service-group SYSTEM "avahi-service.dtd"><service-group><name replace-wildcards="yes">NAS &amp; %h</name><service protocol="ipv4"><type>_http._tcp</type><port>3000</port><subtype>_admin._sub._http._tcp</subtype><txt-record>path=/a?x=1&amp;y=2</txt-record><txt-record value-format="binary-hex">blob=00ff</txt-record></service><service><type>_ssh._tcp</type><port>22</port></service></service-group>`

func TestService(t *testing.T) {
	g, err := ParseService([]byte(serviceXML))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Services) != 2 {
		t.Fatal("lost service")
	}
	b, err := g.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("&amp;")) {
		t.Fatal("XML not escaped")
	}
	round, err := ParseService(b)
	if err != nil || round.Services[0].TXT[1].Value != "blob=00ff" {
		t.Fatalf("%+v %v", round, err)
	}
	for _, bad := range []string{
		strings.Replace(serviceXML, "<port>22</port>", "<port>65536</port>", 1),
		strings.Replace(serviceXML, "<port>22</port>", "<port>-1</port>", 1),
		strings.Replace(serviceXML, "<port>22</port>", "<port>22</port><port>80</port>", 1),
		strings.Replace(serviceXML, "<port>22</port>", "<port>22</port><unknown>x</unknown>", 1),
		strings.Replace(serviceXML, "blob=00ff", "blob=xx", 1),
		strings.Replace(serviceXML, "blob=00ff", "PATH=00ff", 1),
		strings.Replace(serviceXML, "protocol=\"ipv4\"", "protocol=\"bogus\"", 1),
		strings.Replace(serviceXML, "_admin._sub._http._tcp", "_admin._sub._ssh._tcp", 1),
		serviceXML + `<service-group><name>x</name></service-group>`,
	} {
		if _, err := ParseService([]byte(bad)); err == nil {
			t.Errorf("accepted invalid XML: %s", bad)
		}
	}
	for range 100 {
		id := NewServiceID()
		if !validPath("services/"+id) || !strings.HasPrefix(id, "awm-") {
			t.Fatal(id)
		}
	}
}

type fakeController struct {
	actions        []Action
	applyErr       error
	healthFailures int
	cancel         context.CancelFunc
}

func (f *fakeController) Apply(ctx context.Context, a Action) error {
	f.actions = append(f.actions, a)
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.applyErr
}
func (f *fakeController) Healthy(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.healthFailures > 0 {
		f.healthFailures--
		return errors.New("unhealthy")
	}
	return nil
}

func fixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	dir := t.TempDir()
	conf := filepath.Join(dir, "avahi")
	backups := filepath.Join(dir, "backups")
	if err := os.MkdirAll(filepath.Join(conf, "services"), 0755); err != nil {
		t.Fatal(err)
	}
	for p, b := range map[string]string{"avahi-daemon.conf": "# original comment\n[server]\nuse-ipv4=yes\n", "hosts": "# hosts\n192.168.1.2 nas.local\n", "services/existing.service": serviceXML} {
		if err := os.WriteFile(filepath.Join(conf, p), []byte(b), 0640); err != nil {
			t.Fatal(err)
		}
	}
	s, err := OpenStore(conf, backups)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, conf, backups
}

func revision(t *testing.T, s *Store) string {
	t.Helper()
	im, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	return im.Revision()
}
func change(p, b string) map[string]*[]byte { data := []byte(b); return map[string]*[]byte{p: &data} }

func TestBootstrapSnapshotDoesNotNormalize(t *testing.T) {
	s, conf, _ := fixture(t)
	before := revision(t, s)
	m, err := s.Snapshot("initial takeover")
	if err != nil {
		t.Fatal(err)
	}
	if revision(t, s) != before {
		t.Fatal("snapshot mutated live configuration")
	}
	_, im, err := s.SnapshotContent(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if im.Revision() != before {
		t.Fatal("snapshot mismatch")
	}
	b, _ := os.ReadFile(filepath.Join(conf, "avahi-daemon.conf"))
	if !strings.HasPrefix(string(b), "# original comment") {
		t.Fatal("comment lost")
	}
}

func TestApplyActionsAndRestore(t *testing.T) {
	s, conf, _ := fixture(t)
	c := &fakeController{}
	before := revision(t, s)
	result, err := s.Apply(context.Background(), before, change("services/new.service", serviceXML), c)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != Reload || len(c.actions) != 1 || c.actions[0] != Reload {
		t.Fatalf("%+v %+v", result, c)
	}
	if result.Revision != revision(t, s) {
		t.Fatal("wrong committed revision")
	}
	if _, err = s.Restore(context.Background(), result.SnapshotID, result.Revision, c); err != nil {
		t.Fatal(err)
	}
	if revision(t, s) != before {
		t.Fatal("restore did not restore full image")
	}
	if _, err = os.Stat(filepath.Join(conf, "services/new.service")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("new file was not removed")
	}
	result, err = s.Apply(context.Background(), before, change("avahi-daemon.conf", "[server]\nhost-name=new-host\n"), c)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != Restart {
		t.Fatal("daemon change must restart")
	}
	info, _ := os.Stat(filepath.Join(conf, "avahi-daemon.conf"))
	if info.Mode().Perm() != 0640 {
		t.Fatal("mode not preserved")
	}
}

func TestApplyRollbackAndCancellation(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "health", true: "cancellation"}[cancel], func(t *testing.T) {
			s, _, _ := fixture(t)
			before := revision(t, s)
			c := &fakeController{healthFailures: 1}
			ctx := context.Background()
			if cancel {
				var stop context.CancelFunc
				ctx, stop = context.WithCancel(ctx)
				defer stop()
				c.cancel = stop
				c.healthFailures = 0
			}
			_, err := s.Apply(ctx, before, change("services/new.service", serviceXML), c)
			if err == nil || !strings.Contains(err.Error(), "previous configuration restored") {
				t.Fatal(err)
			}
			if revision(t, s) != before {
				t.Fatal("rollback lost original bytes")
			}
			if len(c.actions) != 2 || c.actions[1] != Restart {
				t.Fatalf("rollback actions: %v", c.actions)
			}
		})
	}
}

func TestFailedRollbackCanRecoverAfterRestart(t *testing.T) {
	s, _, backups := fixture(t)
	before := revision(t, s)
	c := &fakeController{applyErr: errors.New("systemd disconnected")}
	_, err := s.Apply(context.Background(), before, change("hosts", "192.168.1.3 new.local\n"), c)
	if err == nil || !strings.Contains(err.Error(), "rollback failed") {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(backups, "pending.json")); err != nil {
		t.Fatal("missing recovery journal", err)
	}
	if _, err = s.Apply(context.Background(), revision(t, s), change("hosts", "192.168.1.4 next.local\n"), c); err == nil {
		t.Fatal("accepted transaction while recovery pending")
	}
	if err = s.Recover(&fakeController{}); err != nil {
		t.Fatal(err)
	}
	if revision(t, s) != before {
		t.Fatal("recovery mismatch")
	}
	if _, err = os.Stat(filepath.Join(backups, "pending.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("journal not cleared")
	}
}

func TestCrashRecovery(t *testing.T) {
	s, conf, backups := fixture(t)
	before := revision(t, s)
	m, err := s.Snapshot("before crash")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(pending{SnapshotID: m.ID})
	if err = os.WriteFile(filepath.Join(backups, "pending.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(conf, "hosts"), []byte("partially written invalid content"), 0640); err != nil {
		t.Fatal(err)
	}
	if err = s.Recover(&fakeController{}); err != nil {
		t.Fatal(err)
	}
	if revision(t, s) != before {
		t.Fatal("crash recovery failed")
	}
}

func TestConflictsAndValidationDoNotWrite(t *testing.T) {
	s, conf, _ := fixture(t)
	stale := revision(t, s)
	if err := os.WriteFile(filepath.Join(conf, "hosts"), []byte("192.168.1.8 cli.local\n"), 0640); err != nil {
		t.Fatal(err)
	}
	current := revision(t, s)
	c := &fakeController{}
	if _, err := s.Apply(context.Background(), stale, change("hosts", "192.168.1.9 gui.local\n"), c); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	for _, ch := range []map[string]*[]byte{change("hosts", "not an IP"), change("../outside", "x"), change("services/../../outside.service", serviceXML), {"avahi-daemon.conf": nil}} {
		if _, err := s.Apply(context.Background(), current, ch, c); err == nil {
			t.Fatal("invalid change accepted")
		}
	}
	if revision(t, s) != current || len(c.actions) != 0 {
		t.Fatal("invalid/stale request had effects")
	}
	list, err := s.Snapshots()
	if err != nil || len(list) != 0 {
		t.Fatal("validation should precede snapshot", list, err)
	}
}

func TestSymlinkAndSnapshotTampering(t *testing.T) {
	s, conf, backups := fixture(t)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte(serviceXML), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(conf, "services/link.service")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(); err == nil {
		t.Fatal("followed symlink")
	}
	if err := os.Remove(filepath.Join(conf, "services/link.service")); err != nil {
		t.Fatal(err)
	}
	m, err := s.Snapshot("tamper test")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(backups, m.ID, "hosts"), []byte("192.168.1.99 changed.local\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.SnapshotContent(m.ID); err == nil {
		t.Fatal("accepted corrupted snapshot")
	}
	if _, _, err = s.SnapshotContent("../../etc"); err == nil {
		t.Fatal("accepted traversal")
	}
}

func TestConcurrentApplyOnlyOneWins(t *testing.T) {
	s, _, _ := fixture(t)
	rev := revision(t, s)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, name := range []string{"one", "two"} {
		wg.Go(func() {
			_, err := s.Apply(context.Background(), rev, change("hosts", "192.168.1.9 "+name+".local\n"), &fakeController{})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for err := range results {
		if err == nil {
			wins++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins %d conflicts %d", wins, conflicts)
	}
}

func TestRetention(t *testing.T) {
	s, _, _ := fixture(t)
	for range 22 {
		if _, err := s.Snapshot("manual"); err != nil {
			t.Fatal(err)
		}
	}
	_, err := s.Apply(context.Background(), revision(t, s), change("hosts", "192.168.1.9 latest.local\n"), &fakeController{})
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.Snapshots()
	if err != nil || len(list) != 20 {
		t.Fatalf("snapshots=%d err=%v", len(list), err)
	}
}

func TestZeroPortAndInvalidXMLText(t *testing.T) {
	g, err := ParseService([]byte(strings.Replace(serviceXML, "<port>22</port>", "<port>0</port>", 1)))
	if err != nil {
		t.Fatal("Avahi permits metadata service port zero", err)
	}
	g.Services[0].TXT = []TXT{{Value: "key=\x01"}}
	if _, err = g.Marshal(); err == nil {
		t.Fatal("XML would silently replace invalid control character")
	}
	g.Services[0].TXT = []TXT{{Value: "key=01", Format: "binary-hex"}}
	if _, err = g.Marshal(); err != nil {
		t.Fatal(err)
	}
}

func TestSingleWriterAndMetadataRestore(t *testing.T) {
	s, conf, backups := fixture(t)
	other, err := OpenStore(conf, backups)
	if err == nil {
		other.Close()
		t.Fatal("second writer acquired the store")
	}
	m, err := s.Snapshot("permissions")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(filepath.Join(conf, "hosts"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Restore(context.Background(), m.ID, revision(t, s), &fakeController{}); err != nil {
		t.Fatal(err)
	}
	i, err := os.Stat(filepath.Join(conf, "hosts"))
	if err != nil || i.Mode().Perm() != 0640 {
		t.Fatal("snapshot permissions not restored", err)
	}
}

func FuzzParsers(f *testing.F) {
	f.Add(serviceXML)
	f.Add("[server]\nuse-ipv4=yes\n")
	f.Add("192.168.1.2 nas.local\n")
	f.Fuzz(func(t *testing.T, input string) {
		ParseDaemon([]byte(input))
		ParseHosts([]byte(input))
		ParseService([]byte(input))
	})
}
