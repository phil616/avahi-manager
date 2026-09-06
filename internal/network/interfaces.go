package network

import (
	"net"
	"net/netip"
	"sort"
	"strings"

	"github.com/vishvananda/netlink"
)

type Interface struct {
	Name      string   `json:"name"`
	Index     int      `json:"index"`
	MAC       string   `json:"mac"`
	Flags     string   `json:"flags"`
	State     string   `json:"state"`
	MTU       int      `json:"mtu"`
	Type      string   `json:"type"`
	Addresses []string `json:"addresses"`
	Category  string   `json:"category"`
	MDNS      bool     `json:"mdns"`
}

func Interfaces(allow, deny []string) ([]Interface, error) {
	h, err := netlink.NewHandle()
	if err != nil {
		return nil, err
	}
	defer h.Close()
	links, err := h.LinkList()
	if err != nil {
		return nil, err
	}
	list := make([]Interface, 0, len(links))
	for _, l := range links {
		a := l.Attrs()
		item := Interface{Name: a.Name, Index: a.Index, MAC: a.HardwareAddr.String(), Flags: a.Flags.String(), State: a.OperState.String(), MTU: a.MTU, Type: l.Type(), Addresses: []string{}, Category: "LAN"}
		addresses, err := h.AddrList(l, netlink.FAMILY_ALL)
		if err != nil {
			return nil, err
		}
		for _, addr := range addresses {
			if addr.IPNet != nil {
				item.Addresses = append(item.Addresses, addr.IPNet.String())
				ip, ok := netip.AddrFromSlice(addr.IP)
				if ok && ip.IsGlobalUnicast() && !ip.IsPrivate() {
					item.Category = "Public candidate"
				}
			}
		}
		if a.Flags&net.FlagLoopback != 0 {
			item.Category = "Loopback"
		} else if l.Type() == "wireguard" {
			item.Category = "WireGuard"
		} else if strings.HasPrefix(a.Name, "docker") || strings.HasPrefix(a.Name, "br-") {
			item.Category = "Docker"
		} else if l.Type() == "tun" || l.Type() == "tuntap" {
			item.Category = "VPN"
		}
		item.MDNS = Enabled(a.Name, allow, deny) && a.Flags&(net.FlagLoopback|net.FlagPointToPoint) == 0
		sort.Strings(item.Addresses)
		list = append(list, item)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Index < list[j].Index })
	return list, nil
}

func Enabled(name string, allow, deny []string) bool {
	for _, s := range deny {
		if strings.TrimSpace(s) == name {
			return false
		}
	}
	count := 0
	for _, s := range allow {
		if strings.TrimSpace(s) == "" {
			continue
		}
		count++
		if strings.TrimSpace(s) == name {
			return true
		}
	}
	return count == 0
}
