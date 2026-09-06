// Package helper defines a closed, typed protocol for the privileged process.
// No RPC accepts a shell command, arbitrary filename, or raw file contents.
package helper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"avahi-manager/internal/config"
)

type Empty struct{}
type Revision struct {
	Revision string `json:"revision"`
}
type WriteDaemon struct {
	Revision
	Config config.Daemon `json:"config"`
}
type WriteHosts struct {
	Revision
	Hosts []config.Host `json:"hosts"`
}
type WriteService struct {
	Revision
	ID    string              `json:"id,omitempty"`
	Group config.ServiceGroup `json:"group"`
}
type DeleteService struct {
	Revision
	ID string `json:"id"`
}
type SnapshotID struct {
	ID string `json:"id"`
}
type CreateSnapshot struct {
	Reason string `json:"reason"`
}
type RestoreSnapshot struct {
	Revision
	ID string `json:"id"`
}
type ServiceResult struct {
	config.ApplyResult
	ID string `json:"id"`
}
type PublishedService struct {
	ID       string              `json:"id"`
	Filename string              `json:"filename"`
	Group    config.ServiceGroup `json:"group"`
	Error    string              `json:"error,omitempty"`
}
type Configuration struct {
	Revision string             `json:"revision"`
	Daemon   config.Daemon      `json:"daemon"`
	Raw      string             `json:"raw"`
	Hosts    []config.Host      `json:"hosts"`
	Services []PublishedService `json:"services"`
	Hashes   map[string]string  `json:"hashes"`
	Errors   []string           `json:"errors"`
}
type Snapshot struct {
	Manifest      config.Manifest `json:"manifest"`
	Configuration Configuration   `json:"configuration"`
}

func hash(data []byte) string   { s := sha256.Sum256(data); return hex.EncodeToString(s[:]) }
func serviceID(p string) string { return hash([]byte(p)) }

func model(im config.Image) Configuration {
	v := Configuration{Revision: im.Revision(), Hosts: []config.Host{}, Services: []PublishedService{}, Hashes: map[string]string{}, Errors: []string{}}
	for p, f := range im {
		v.Hashes[p] = hash(f.Data)
	}
	v.Raw = string(im["avahi-daemon.conf"].Data)
	var err error
	v.Daemon, err = config.ParseDaemon([]byte(v.Raw))
	if err == nil {
		err = v.Daemon.Validate()
	}
	if err != nil {
		v.Errors = append(v.Errors, "avahi-daemon.conf: "+err.Error())
	}
	if _, ok := im["avahi-daemon.conf"]; !ok {
		v.Errors = append(v.Errors, "avahi-daemon.conf is missing")
	}
	v.Hosts, err = config.ParseHosts(im["hosts"].Data)
	if err != nil {
		v.Errors = append(v.Errors, "hosts: "+err.Error())
	}
	for p, f := range im {
		if strings.HasPrefix(p, "services/") {
			g, err := config.ParseService(f.Data)
			s := PublishedService{ID: serviceID(p), Filename: strings.TrimPrefix(p, "services/"), Group: g}
			if err != nil {
				s.Error = err.Error()
				v.Errors = append(v.Errors, p+": "+err.Error())
			}
			v.Services = append(v.Services, s)
		}
	}
	sort.Slice(v.Services, func(i, j int) bool { return v.Services[i].Filename < v.Services[j].Filename })
	return v
}

type Runtime interface {
	config.Controller
	Control(context.Context, string) error
}
type Backend struct {
	Store   *config.Store
	Runtime Runtime
}

func (b *Backend) Configuration() (Configuration, error) {
	im, err := b.Store.Read()
	if err != nil {
		return Configuration{}, err
	}
	return model(im), nil
}
func (b *Backend) write(ctx context.Context, rev, p string, data []byte) (config.ApplyResult, error) {
	return b.Store.Apply(ctx, rev, map[string]*[]byte{p: &data}, b.Runtime)
}
func (b *Backend) WriteDaemon(ctx context.Context, r WriteDaemon) (config.ApplyResult, error) {
	if r.Config == nil {
		return config.ApplyResult{}, fmt.Errorf("config object is required")
	}
	data, err := r.Config.Marshal()
	if err != nil {
		return config.ApplyResult{}, err
	}
	return b.write(ctx, r.Revision.Revision, "avahi-daemon.conf", data)
}
func (b *Backend) WriteHosts(ctx context.Context, r WriteHosts) (config.ApplyResult, error) {
	if r.Hosts == nil {
		return config.ApplyResult{}, fmt.Errorf("hosts array is required")
	}
	data, err := config.MarshalHosts(r.Hosts)
	if err != nil {
		return config.ApplyResult{}, err
	}
	return b.write(ctx, r.Revision.Revision, "hosts", data)
}

func (b *Backend) servicePath(id string) (string, error) {
	if len(id) != 64 {
		return "", fmt.Errorf("invalid service ID")
	}
	im, err := b.Store.Read()
	if err != nil {
		return "", err
	}
	for p := range im {
		if strings.HasPrefix(p, "services/") && serviceID(p) == id {
			return p, nil
		}
	}
	return "", fmt.Errorf("service not found")
}
func (b *Backend) WriteService(ctx context.Context, r WriteService) (ServiceResult, error) {
	data, err := r.Group.Marshal()
	if err != nil {
		return ServiceResult{}, err
	}
	p := "services/" + config.NewServiceID()
	if r.ID != "" {
		p, err = b.servicePath(r.ID)
		if err != nil {
			return ServiceResult{}, err
		}
	}
	result, err := b.write(ctx, r.Revision.Revision, p, data)
	return ServiceResult{result, serviceID(p)}, err
}
func (b *Backend) DeleteService(ctx context.Context, r DeleteService) (config.ApplyResult, error) {
	p, err := b.servicePath(r.ID)
	if err != nil {
		return config.ApplyResult{}, err
	}
	return b.Store.Apply(ctx, r.Revision.Revision, map[string]*[]byte{p: nil}, b.Runtime)
}
