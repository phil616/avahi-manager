package config

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

type ServiceName struct {
	Value            string `xml:",chardata" json:"value"`
	ReplaceWildcards string `xml:"replace-wildcards,attr,omitempty" json:"replaceWildcards"`
}

type TXT struct {
	Value  string `xml:",chardata" json:"value"`
	Format string `xml:"value-format,attr,omitempty" json:"format,omitempty"`
}

type Service struct {
	Protocol string   `xml:"protocol,attr,omitempty" json:"protocol"`
	Type     string   `xml:"type" json:"type"`
	Domain   string   `xml:"domain-name,omitempty" json:"domain,omitempty"`
	Host     string   `xml:"host-name,omitempty" json:"host,omitempty"`
	Port     uint16   `xml:"port" json:"port"`
	Subtypes []string `xml:"subtype" json:"subtypes"`
	TXT      []TXT    `xml:"txt-record" json:"txt"`
}

type ServiceGroup struct {
	XMLName  xml.Name    `xml:"service-group" json:"-"`
	Name     ServiceName `xml:"name" json:"name"`
	Services []Service   `xml:"service" json:"services"`
}

var serviceType = regexp.MustCompile(`^_[a-zA-Z0-9][a-zA-Z0-9-]{0,62}\._(?:tcp|udp)$`)
var subtypeLabel = regexp.MustCompile(`^_[a-zA-Z0-9][a-zA-Z0-9-]{0,61}$`)

func validXMLText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r != '\t' && r != '\n' && r != '\r' && (r < 0x20 || r == 0xfffe || r == 0xffff) {
			return false
		}
	}
	return true
}

func (g ServiceGroup) Validate() error {
	if !validXMLText(g.Name.Value) || len(g.Name.Value) == 0 || len(g.Name.Value) > 63 || strings.ContainsAny(g.Name.Value, "\x00\r\n") {
		return fmt.Errorf("service name must be 1–63 UTF-8 bytes")
	}
	if g.Name.ReplaceWildcards != "" && g.Name.ReplaceWildcards != "yes" && g.Name.ReplaceWildcards != "no" {
		return fmt.Errorf("invalid replace-wildcards")
	}
	if len(g.Services) == 0 {
		return fmt.Errorf("service-group requires at least one service")
	}
	for _, s := range g.Services {
		if !serviceType.MatchString(s.Type) {
			return fmt.Errorf("invalid service type %q", s.Type)
		}
		if s.Protocol != "" && s.Protocol != "any" && s.Protocol != "ipv4" && s.Protocol != "ipv6" {
			return fmt.Errorf("invalid service protocol")
		}
		// Avahi accepts port 0 (used by metadata-only DNS-SD services).
		// The uint16 field enforces the upper bound; ParseService requires a port.
		if s.Domain != "" && !ValidDomain(s.Domain) {
			return fmt.Errorf("invalid service domain")
		}
		if s.Host != "" && (!ValidDomain(s.Host) || !strings.Contains(strings.TrimSuffix(s.Host, "."), ".")) {
			return fmt.Errorf("service host must be FQDN")
		}
		for _, sub := range s.Subtypes {
			prefix, ok := strings.CutSuffix(sub, "._sub."+s.Type)
			if !ok || !subtypeLabel.MatchString(prefix) {
				return fmt.Errorf("invalid subtype %q", sub)
			}
		}
		seen := map[string]bool{}
		for _, txt := range s.TXT {
			if !validXMLText(txt.Value) {
				return fmt.Errorf("TXT contains invalid XML text; use a binary format for binary values")
			}
			key, value, hasValue := strings.Cut(txt.Value, "=")
			if key == "" {
				return fmt.Errorf("TXT key cannot be empty")
			}
			for _, ch := range key {
				if ch < 0x20 || ch > 0x7e {
					return fmt.Errorf("TXT keys must be printable ASCII")
				}
			}
			if seen[strings.ToLower(key)] {
				return fmt.Errorf("duplicate TXT key %q", key)
			}
			seen[strings.ToLower(key)] = true
			data := []byte(value)
			var err error
			switch txt.Format {
			case "", "text":
			case "binary-hex":
				data, err = hex.DecodeString(value)
			case "binary-base64":
				data, err = base64.StdEncoding.Strict().DecodeString(value)
			default:
				return fmt.Errorf("invalid TXT format")
			}
			if err != nil || (txt.Format != "" && txt.Format != "text" && !hasValue) {
				return fmt.Errorf("invalid binary TXT value")
			}
			n := len(key)
			if hasValue {
				n += 1 + len(data)
			}
			if n > 255 {
				return fmt.Errorf("TXT record exceeds 255 bytes")
			}
		}
	}
	return nil
}

// Check XML shape before decoding: encoding/xml otherwise silently drops unknown
// elements/attributes and accepts repeated scalar fields, causing data loss.
func ParseService(data []byte) (ServiceGroup, error) {
	var g ServiceGroup
	d := xml.NewDecoder(bytes.NewReader(data))
	stack := []string{}
	counts := []map[string]int{}
	roots := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return g, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			parent := ""
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			allowed := (parent == "" && t.Name.Local == "service-group") || (parent == "service-group" && (t.Name.Local == "name" || t.Name.Local == "service")) || (parent == "service" && strings.Contains("|type|port|domain-name|host-name|subtype|txt-record|", "|"+t.Name.Local+"|"))
			if !allowed || t.Name.Space != "" {
				return g, fmt.Errorf("unsupported XML element %s", t.Name.Local)
			}
			if parent == "" {
				roots++
				if roots > 1 {
					return g, fmt.Errorf("multiple XML roots")
				}
			} else {
				c := counts[len(counts)-1]
				c[t.Name.Local]++
				if c[t.Name.Local] > 1 && t.Name.Local != "service" && t.Name.Local != "subtype" && t.Name.Local != "txt-record" {
					return g, fmt.Errorf("duplicate element %s", t.Name.Local)
				}
			}
			attrs := map[string]bool{}
			for _, a := range t.Attr {
				ok := (t.Name.Local == "name" && a.Name.Local == "replace-wildcards") || (t.Name.Local == "service" && a.Name.Local == "protocol") || (t.Name.Local == "txt-record" && a.Name.Local == "value-format")
				if !ok || a.Name.Space != "" || attrs[a.Name.Local] {
					return g, fmt.Errorf("invalid XML attribute %s", a.Name.Local)
				}
				attrs[a.Name.Local] = true
			}
			stack = append(stack, t.Name.Local)
			counts = append(counts, map[string]int{})
		case xml.EndElement:
			c := counts[len(counts)-1]
			if t.Name.Local == "service" && (c["type"] != 1 || c["port"] != 1) {
				return g, fmt.Errorf("service requires type and port")
			}
			stack = stack[:len(stack)-1]
			counts = counts[:len(counts)-1]
		case xml.CharData:
			if (len(stack) == 0 || stack[len(stack)-1] == "service" || stack[len(stack)-1] == "service-group") && strings.TrimSpace(string(t)) != "" {
				return g, fmt.Errorf("unexpected XML text")
			}
		}
	}
	if err := xml.Unmarshal(data, &g); err != nil {
		return g, err
	}
	return g, g.Validate()
}

func (g ServiceGroup) Marshal() ([]byte, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}
	b, err := xml.MarshalIndent(g, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(b, '\n')...), nil
}

func NewServiceID() string { return "awm-" + randomID() + ".service" }

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
