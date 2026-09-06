package config

import (
	"bufio"
	"fmt"
	"net/netip"
	"strings"
)

type Host struct {
	Address  string `json:"address"`
	Hostname string `json:"hostname"`
}

func ValidateHosts(hosts []Host) error {
	seen := map[string]bool{}
	for _, h := range hosts {
		ip, err := netip.ParseAddr(h.Address)
		if err != nil || ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() {
			return fmt.Errorf("invalid host address %q", h.Address)
		}
		if !ValidDomain(h.Hostname) || !strings.Contains(strings.TrimSuffix(h.Hostname, "."), ".") {
			return fmt.Errorf("hostname must be an FQDN: %q", h.Hostname)
		}
		key := ip.Unmap().String() + " " + strings.ToLower(strings.TrimSuffix(h.Hostname, "."))
		if seen[key] {
			return fmt.Errorf("duplicate host %s", key)
		}
		seen[key] = true
	}
	return nil
}

func ParseHosts(data []byte) ([]Host, error) {
	hosts := []Host{}
	s := bufio.NewScanner(strings.NewReader(string(data)))
	for line := 1; s.Scan(); line++ {
		v, _, _ := strings.Cut(s.Text(), "#")
		fields := strings.Fields(v)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			return nil, fmt.Errorf("hosts line %d: expected address and FQDN", line)
		}
		hosts = append(hosts, Host{fields[0], fields[1]})
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return hosts, ValidateHosts(hosts)
}

func MarshalHosts(hosts []Host) ([]byte, error) {
	if err := ValidateHosts(hosts); err != nil {
		return nil, err
	}
	var b strings.Builder
	for _, h := range hosts {
		fmt.Fprintf(&b, "%s\t%s\n", h.Address, h.Hostname)
	}
	return []byte(b.String()), nil
}
