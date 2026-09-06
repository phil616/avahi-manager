package avahi

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

type DiscoveryState struct {
	Services  []Service `json:"services"`
	Connected bool      `json:"connected"`
	Error     string    `json:"error,omitempty"`
}

// Discovery is an in-memory cache. Each subscriber receives complete states,
// so coalescing updates for slow browsers cannot leave ghost services behind.
type Discovery struct {
	mu        sync.Mutex
	services  map[string]Service
	connected bool
	err       string
	subs      map[chan DiscoveryState]struct{}
}

func NewDiscovery() *Discovery {
	return &Discovery{services: map[string]Service{}, subs: map[chan DiscoveryState]struct{}{}}
}

func (d *Discovery) state() DiscoveryState {
	s := DiscoveryState{Services: make([]Service, 0, len(d.services)), Connected: d.connected, Error: d.err}
	for _, v := range d.services {
		s.Services = append(s.Services, v)
	}
	sort.Slice(s.Services, func(i, j int) bool { return s.Services[i].Key() < s.Services[j].Key() })
	return s
}

func (d *Discovery) State() DiscoveryState { d.mu.Lock(); defer d.mu.Unlock(); return d.state() }

func (d *Discovery) Subscribe() (<-chan DiscoveryState, func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	ch := make(chan DiscoveryState, 1)
	d.subs[ch] = struct{}{}
	ch <- d.state()
	var once sync.Once
	return ch, func() { once.Do(func() { d.mu.Lock(); defer d.mu.Unlock(); delete(d.subs, ch); close(ch) }) }
}

func (d *Discovery) notify() {
	s := d.state()
	for ch := range d.subs {
		select {
		case ch <- s:
		default:
			select {
			case <-ch:
			default:
			}
			ch <- s
		}
	}
}

func (d *Discovery) reset(connected bool, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.services = map[string]Service{}
	d.connected = connected
	d.err = ""
	if err != nil {
		d.err = err.Error()
	}
	d.notify()
}

func (d *Discovery) upsert(s Service) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if old, ok := d.services[s.Key()]; ok {
		s.FirstSeen = old.FirstSeen
	} else if s.FirstSeen.IsZero() {
		s.FirstSeen = time.Now().UTC()
	}
	s.LastSeen = time.Now().UTC()
	d.services[s.Key()] = s
	d.notify()
}

func (d *Discovery) remove(i Identity) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.services, i.Key())
	d.notify()
}

// Run reconnects only after a transport/browser failure. Service discovery itself
// is driven exclusively by Avahi D-Bus signals, never avahi-browse or polling.
func (d *Discovery) Run(ctx context.Context) {
	for ctx.Err() == nil {
		c, err := Connect()
		if err == nil {
			err = d.browse(ctx, c)
			c.Close()
		}
		if ctx.Err() != nil {
			return
		}
		d.reset(false, err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

type resolveRequest struct {
	Identity
	Generation uint64
}
type resolveResult struct {
	Request resolveRequest
	Service Service
	Err     error
}

func (d *Discovery) browse(ctx context.Context, c *Client) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	signals := make(chan *dbus.Signal, 1024)
	c.Conn.Signal(signals)
	defer c.Conn.RemoveSignal(signals)
	for _, opts := range [][]dbus.MatchOption{
		{dbus.WithMatchSender(Destination), dbus.WithMatchInterface(Destination + ".ServiceTypeBrowser")},
		{dbus.WithMatchSender(Destination), dbus.WithMatchInterface(Destination + ".ServiceBrowser")},
		{dbus.WithMatchSender("org.freedesktop.DBus"), dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, Destination)},
	} {
		if err := c.Conn.AddMatchSignalContext(ctx, opts...); err != nil {
			return err
		}
	}
	objects := map[dbus.ObjectPath]string{}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		for p, iface := range objects {
			c.Conn.Object(Destination, p).CallWithContext(cleanup, iface+".Free", dbus.FlagNoAutoStart)
		}
	}()
	var root dbus.ObjectPath
	if err := c.Conn.Object(Destination, "/").CallWithContext(ctx, Server+".ServiceTypeBrowserNew", dbus.FlagNoAutoStart, int32(-1), int32(-1), "", uint32(0)).Store(&root); err != nil {
		return err
	}
	objects[root] = Destination + ".ServiceTypeBrowser"
	d.reset(true, nil)
	jobs := make(chan resolveRequest, 512)
	results := make(chan resolveResult, 512)
	var workers sync.WaitGroup
	defer workers.Wait()
	defer cancel()
	for range 8 {
		workers.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case req := <-jobs:
					query, stop := context.WithTimeout(ctx, 5*time.Second)
					s, err := c.Resolve(query, req.Identity)
					stop()
					select {
					case results <- resolveResult{req, s, err}:
					case <-ctx.Done():
						return
					}
				}
			}
		})
	}
	types := map[string]dbus.ObjectPath{}
	generations := map[string]uint64{}
	var generation uint64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.Conn.Context().Done():
			return errors.New("system D-Bus disconnected")
		case result := <-results:
			if generations[result.Request.Key()] != result.Request.Generation {
				continue
			}
			s := result.Service
			if result.Err != nil {
				s = Service{Identity: result.Request.Identity, Error: result.Err.Error()}
			}
			d.upsert(s)
		case sig, ok := <-signals:
			if !ok {
				return errors.New("D-Bus signal stream closed")
			}
			if sig == nil {
				continue
			}
			if sig.Name == "org.freedesktop.DBus.NameOwnerChanged" {
				return errors.New("Avahi owner changed; reconnecting")
			}
			if _, ok := objects[sig.Path]; !ok {
				continue
			}
			if strings.HasSuffix(sig.Name, ".Failure") {
				return fmt.Errorf("Avahi browser failure: %v", sig.Body)
			}
			switch sig.Name {
			case Destination + ".ServiceTypeBrowser.ItemNew":
				var intf, proto int32
				var typ, domain string
				var flags uint32
				if err := dbus.Store(sig.Body, &intf, &proto, &typ, &domain, &flags); err != nil {
					return err
				}
				key := typ + "\x00" + domain
				if _, ok := types[key]; ok {
					continue
				}
				if len(types) >= 256 {
					return errors.New("service type limit exceeded")
				}
				var p dbus.ObjectPath
				if err := c.Conn.Object(Destination, "/").CallWithContext(ctx, Server+".ServiceBrowserNew", dbus.FlagNoAutoStart, int32(-1), int32(-1), typ, domain, uint32(0)).Store(&p); err != nil {
					return err
				}
				types[key] = p
				objects[p] = Destination + ".ServiceBrowser"
			case Destination + ".ServiceBrowser.ItemNew", Destination + ".ServiceBrowser.ItemRemove":
				var i Identity
				var flags uint32
				if err := dbus.Store(sig.Body, &i.Interface, &i.Protocol, &i.Name, &i.Type, &i.Domain, &flags); err != nil {
					return err
				}
				if strings.HasSuffix(sig.Name, ".ItemRemove") {
					delete(generations, i.Key())
					d.remove(i)
					continue
				}
				if len(generations) >= 4096 {
					return errors.New("discovered service limit exceeded")
				}
				generation++
				generations[i.Key()] = generation
				d.upsert(Service{Identity: i, Flags: flags})
				select {
				case jobs <- resolveRequest{i, generation}:
				default:
					return errors.New("service resolution queue full")
				}
			}
		}
	}
}
