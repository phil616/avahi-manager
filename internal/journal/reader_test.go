package journal

import (
	"strings"
	"testing"
)

func TestFixedJournalArguments(t *testing.T) {
	args, err := Arguments(Query{Search: "foo.*; $(id)", Priority: "warning", Since: "2026-09-06T00:00:00Z", Limit: 50}, false)
	if err != nil {
		t.Fatal(err)
	}
	if args[0] != "--unit" || args[1] != "avahi-daemon.service" {
		t.Fatal(args)
	}
	joined := strings.Join(args, "|")
	if !strings.Contains(joined, `foo\.\*; \$\(id\)`) {
		t.Fatal("search treated as regex", joined)
	}
	for _, q := range []Query{{Priority: "--unit=ssh"}, {Limit: -1}, {Limit: 1001}, {Since: "today; id"}, {Search: strings.Repeat("x", 257)}, {Since: "2026-09-07T00:00:00Z", Until: "2026-09-06T00:00:00Z"}} {
		if _, err := Arguments(q, false); err == nil {
			t.Fatal("invalid query accepted", q)
		}
	}
}
func TestParseJournal(t *testing.T) {
	e, err := Parse([]byte(`{"__REALTIME_TIMESTAMP":"1000000","MESSAGE":"ready","PRIORITY":"6","__CURSOR":"one"}`))
	if err != nil || e.Message != "ready" || e.Timestamp.Unix() != 1 {
		t.Fatal(e, err)
	}
	e, err = Parse([]byte(`{"__REALTIME_TIMESTAMP":"1000000","MESSAGE":[65,66],"PRIORITY":"3"}`))
	if err != nil || e.Message != "AB" {
		t.Fatal(e, err)
	}
	for _, bad := range []string{`{}`, `{"__REALTIME_TIMESTAMP":"bad"}`, `{"__REALTIME_TIMESTAMP":"0","PRIORITY":"99"}`} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Fatal("bad journal entry accepted")
		}
	}
}
