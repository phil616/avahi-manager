// Package config models Avahi's native files. The filesystem, not SQLite,
// remains the source of truth. Parsing never writes or normalizes disk files.
package config

import (
	"bufio"
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Daemon map[string]map[string]string

var label = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
var iface = regexp.MustCompile(`^[^\s/:,\x00]{1,15}$`)

func ValidDomain(s string) bool {
	s = strings.TrimSuffix(s, ".")
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, part := range strings.Split(s, ".") {
		if !label.MatchString(part) {
			return false
		}
	}
	return true
}

func ParseDaemon(data []byte) (Daemon, error) {
	d := Daemon{}
	section := ""
	s := bufio.NewScanner(strings.NewReader(string(data)))
	s.Buffer(make([]byte, 4096), 1<<20)
	for line := 1; s.Scan(); line++ {
		v := strings.TrimSpace(s.Text())
		if v == "" || strings.HasPrefix(v, "#") || strings.HasPrefix(v, ";") {
			continue
		}
		if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
			section = strings.TrimSpace(v[1 : len(v)-1])
			if section == "" {
				return nil, fmt.Errorf("line %d: empty section", line)
			}
			if d[section] == nil {
				d[section] = map[string]string{}
			}
			continue
		}
		key, value, ok := strings.Cut(v, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || section == "" || key == "" {
			return nil, fmt.Errorf("line %d: expected section and key=value", line)
		}
		if _, exists := d[section][key]; exists {
			return nil, fmt.Errorf("line %d: duplicate %s.%s", line, section, key)
		}
		d[section][key] = value
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return d, nil
}

// Kinds is also the schema used by the structured advanced settings editor.
var Kinds = map[string]map[string]string{
	"server": {
		"host-name": "label", "domain-name": "domain", "host-name-from-machine-id": "bool",
		"browse-domains": "domains", "use-ipv4": "bool", "use-ipv6": "bool",
		"allow-interfaces": "interfaces", "deny-interfaces": "interfaces", "check-response-ttl": "bool",
		"use-iff-running": "bool", "enable-dbus": "dbus", "disallow-other-stacks": "bool", "allow-point-to-point": "bool",
		"cache-entries-max": "uint", "clients-max": "uint", "objects-per-client-max": "uint",
		"entries-per-entry-group-max": "uint", "ratelimit-interval-usec": "uint", "ratelimit-burst": "uint",
	},
	"wide-area": {"enable-wide-area": "bool"},
	"publish": {
		"disable-publishing": "bool", "disable-user-service-publishing": "bool", "add-service-cookie": "bool",
		"publish-addresses": "bool", "publish-hinfo": "bool", "publish-workstation": "bool", "publish-domain": "bool",
		"publish-dns-servers": "ips", "publish-resolv-conf-dns-servers": "bool", "publish-aaaa-on-ipv4": "bool", "publish-a-on-ipv6": "bool",
	},
	"reflector": {"enable-reflector": "bool", "reflect-ipv": "bool", "reflect-filters": "text"},
	"rlimits":   {"rlimit-as": "uint", "rlimit-core": "uint", "rlimit-data": "uint", "rlimit-fsize": "uint", "rlimit-nofile": "uint", "rlimit-stack": "uint", "rlimit-nproc": "uint"},
}

func (d Daemon) Validate() error {
	for section, entries := range d {
		kinds, ok := Kinds[section]
		if !ok {
			return fmt.Errorf("unsupported section %q", section)
		}
		for key, value := range entries {
			kind, ok := kinds[key]
			if !ok {
				return fmt.Errorf("unsupported setting %s.%s", section, key)
			}
			if strings.ContainsAny(value, "\r\n\x00") {
				return fmt.Errorf("invalid value for %s.%s", section, key)
			}
			valid := true
			switch kind {
			case "bool":
				valid = value == "yes" || value == "no"
			case "dbus":
				valid = value == "yes" || value == "no" || value == "warn"
			case "label":
				valid = label.MatchString(value)
			case "domain":
				valid = ValidDomain(value)
			case "uint":
				_, err := strconv.ParseUint(value, 10, 64)
				valid = err == nil
			case "domains", "interfaces", "ips":
				if value != "" {
					for _, item := range strings.Split(value, ",") {
						item = strings.TrimSpace(item)
						switch kind {
						case "domains":
							valid = valid && ValidDomain(item)
						case "interfaces":
							valid = valid && iface.MatchString(item)
						case "ips":
							_, err := netip.ParseAddr(item)
							valid = valid && err == nil
						}
					}
				}
			}
			if !valid {
				return fmt.Errorf("invalid %s.%s: %q", section, key, value)
			}
		}
	}
	if d["server"]["use-ipv4"] == "no" && d["server"]["use-ipv6"] == "no" {
		return fmt.Errorf("at least one IP protocol must be enabled")
	}
	return nil
}

func (d Daemon) Marshal() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	var b strings.Builder
	for _, section := range []string{"server", "wide-area", "publish", "reflector", "rlimits"} {
		entries, ok := d[section]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "[%s]\n", section)
		keys := make([]string, 0, len(entries))
		for key := range entries {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(&b, "%s=%s\n", key, entries[key])
		}
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}
