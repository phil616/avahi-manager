package avahi

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

const Destination = "org.freedesktop.Avahi"
const Server = Destination + ".Server"

type State struct {
	Available bool   `json:"available"`
	State     int32  `json:"state"`
	Version   string `json:"version"`
	Hostname  string `json:"hostname"`
	Domain    string `json:"domain"`
	Error     string `json:"error,omitempty"`
}

type Client struct{ Conn *dbus.Conn }

func Connect() (*Client, error) {
	c, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	return &Client{c}, nil
}

func (c *Client) Close() error { return c.Conn.Close() }

func (c *Client) Status(ctx context.Context) (State, error) {
	s := State{State: -1}
	o := c.Conn.Object(Destination, "/")
	// A read-only status probe must not socket/D-Bus-activate a stopped daemon.
	for _, call := range []struct {
		method string
		dest   any
	}{{"GetState", &s.State}, {"GetVersionString", &s.Version}, {"GetHostNameFqdn", &s.Hostname}, {"GetDomainName", &s.Domain}} {
		if err := o.CallWithContext(ctx, Server+"."+call.method, dbus.FlagNoAutoStart).Store(call.dest); err != nil {
			s.Error = err.Error()
			return s, err
		}
	}
	s.Available = true
	return s, nil
}

type Identity struct {
	Interface int32  `json:"interface"`
	Protocol  int32  `json:"protocol"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Domain    string `json:"domain"`
}

func (i Identity) Key() string {
	return fmt.Sprintf("%d\x00%d\x00%s\x00%s\x00%s", i.Interface, i.Protocol, i.Name, i.Type, i.Domain)
}

type Service struct {
	Identity
	Host            string    `json:"host"`
	AddressProtocol int32     `json:"addressProtocol"`
	Address         string    `json:"address"`
	Port            uint16    `json:"port"`
	TXT             [][]byte  `json:"txt"`
	Flags           uint32    `json:"flags"`
	FirstSeen       time.Time `json:"firstSeen"`
	LastSeen        time.Time `json:"lastSeen"`
	Error           string    `json:"error,omitempty"`
}

func (c *Client) Resolve(ctx context.Context, i Identity) (Service, error) {
	s := Service{Identity: i}
	err := c.Conn.Object(Destination, "/").CallWithContext(ctx, Server+".ResolveService", dbus.FlagNoAutoStart, i.Interface, i.Protocol, i.Name, i.Type, i.Domain, int32(-1), uint32(0)).Store(
		&s.Interface, &s.Protocol, &s.Name, &s.Type, &s.Domain, &s.Host, &s.AddressProtocol, &s.Address, &s.Port, &s.TXT, &s.Flags)
	return s, err
}

type Host struct {
	Interface       int32  `json:"interface"`
	Protocol        int32  `json:"protocol"`
	Name            string `json:"name"`
	AddressProtocol int32  `json:"addressProtocol"`
	Address         string `json:"address"`
	Flags           uint32 `json:"flags"`
}

func (c *Client) ResolveHost(ctx context.Context, name string) (Host, error) {
	var h Host
	err := c.Conn.Object(Destination, "/").CallWithContext(ctx, Server+".ResolveHostName", dbus.FlagNoAutoStart, int32(-1), int32(-1), name, int32(-1), uint32(0)).Store(&h.Interface, &h.Protocol, &h.Name, &h.AddressProtocol, &h.Address, &h.Flags)
	return h, err
}
