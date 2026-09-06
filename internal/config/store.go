package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const MaxFileSize = 1 << 20
const MaxTotalSize = 16 << 20

var ErrConflict = errors.New("configuration changed on disk; reload before saving")

type File struct {
	Data []byte      `json:"data"`
	Mode fs.FileMode `json:"mode"`
	UID  int         `json:"uid"`
	GID  int         `json:"gid"`
}

type Image map[string]File

type Manifest struct {
	ID        string              `json:"id"`
	Reason    string              `json:"reason"`
	CreatedAt time.Time           `json:"createdAt"`
	Files     map[string]FileMeta `json:"files"`
}

type FileMeta struct {
	SHA256 string      `json:"sha256"`
	Mode   fs.FileMode `json:"mode"`
	UID    int         `json:"uid"`
	GID    int         `json:"gid"`
}

type Store struct {
	mu      sync.Mutex
	root    *os.Root
	backups *os.Root
	lock    *os.File
}

// OpenStore never creates or rewrites any Avahi configuration. The helper owns
// the backup directory; the unprivileged web process uses RPC for mutations.
func OpenStore(configDir, backupDir string) (*Store, error) {
	r, err := os.OpenRoot(configDir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(backupDir, 0700); err != nil {
		r.Close()
		return nil, err
	}
	b, err := os.OpenRoot(backupDir)
	if err != nil {
		r.Close()
		return nil, err
	}
	lock, err := b.OpenFile("writer.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err == nil {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	}
	if err != nil {
		if lock != nil {
			lock.Close()
		}
		r.Close()
		b.Close()
		return nil, fmt.Errorf("acquire configuration writer lock: %w", err)
	}
	return &Store{root: r, backups: b, lock: lock}, nil
}

func (s *Store) Close() error { return errors.Join(s.root.Close(), s.backups.Close(), s.lock.Close()) }

func validPath(p string) bool {
	if p == "avahi-daemon.conf" || p == "hosts" {
		return true
	}
	return strings.HasPrefix(p, "services/") && path.Dir(p) == "services" && strings.HasSuffix(p, ".service") && len(path.Base(p)) > 8 && !strings.ContainsAny(p, "\\\x00\r\n")
}

func digest(data []byte) string { v := sha256.Sum256(data); return hex.EncodeToString(v[:]) }

func (im Image) Revision() string {
	h := sha256.New()
	keys := sortedPaths(im)
	for _, p := range keys {
		f := im[p]
		fmt.Fprintf(h, "%s\x00%s\x00%o:%d:%d\n", p, digest(f.Data), f.Mode, f.UID, f.GID)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func sortedPaths[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for p := range m {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	return keys
}

func readFile(r *os.Root, p string) (File, error) {
	info, err := r.Lstat(p)
	if err != nil {
		return File{}, err
	}
	if !info.Mode().IsRegular() {
		return File{}, fmt.Errorf("%s must be a regular file (symlinks are not managed)", p)
	}
	f, err := r.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return File{}, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return File{}, err
	}
	if !info.Mode().IsRegular() {
		return File{}, fmt.Errorf("%s is not regular", p)
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return File{}, err
	}
	if len(data) > MaxFileSize {
		return File{}, fmt.Errorf("%s exceeds size limit", p)
	}
	stat := info.Sys().(*syscall.Stat_t)
	return File{Data: data, Mode: info.Mode().Perm(), UID: int(stat.Uid), GID: int(stat.Gid)}, nil
}

func (s *Store) Read() (Image, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.read() }

// ReadDirectory provides a bounded, symlink-safe read without opening backups,
// taking a writer lock, or creating any directory. Used by the web process.
func ReadDirectory(dir string) (Image, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return (&Store{root: r}).read()
}

func (s *Store) read() (Image, error) {
	im := Image{}
	names := []string{"avahi-daemon.conf", "hosts"}
	info, err := s.root.Lstat("services")
	if err == nil {
		if !info.IsDir() {
			return nil, fmt.Errorf("services must be a directory, not a symlink")
		}
		d, err := s.root.Open("services")
		if err != nil {
			return nil, err
		}
		entries, err := d.ReadDir(-1)
		d.Close()
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".service") {
				names = append(names, "services/"+e.Name())
			}
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	total := 0
	for _, p := range names {
		f, err := readFile(s.root, p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		total += len(f.Data)
		if total > MaxTotalSize {
			return nil, fmt.Errorf("configuration exceeds total size limit")
		}
		im[p] = f
	}
	return im, nil
}

func ValidateImage(im Image) error {
	if _, ok := im["avahi-daemon.conf"]; !ok {
		return fmt.Errorf("avahi-daemon.conf is required")
	}
	total := 0
	for p, f := range im {
		if !validPath(p) {
			return fmt.Errorf("invalid configuration path %q", p)
		}
		total += len(f.Data)
		if len(f.Data) > MaxFileSize || total > MaxTotalSize {
			return fmt.Errorf("configuration too large")
		}
		var err error
		switch p {
		case "avahi-daemon.conf":
			var d Daemon
			d, err = ParseDaemon(f.Data)
			if err == nil {
				err = d.Validate()
			}
		case "hosts":
			_, err = ParseHosts(f.Data)
		default:
			_, err = ParseService(f.Data)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	return nil
}

func syncDir(r *os.Root, p string) error {
	d, err := r.Open(p)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// stage writes into a sibling temporary file and fsyncs it before rename.
func stage(r *os.Root, p string, f File, owner bool) (string, error) {
	tmp := path.Join(path.Dir(p), ".awm-"+randomID()+".tmp")
	h, err := r.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		h.Close()
		if !ok {
			r.Remove(tmp)
		}
	}()
	if _, err = h.Write(f.Data); err != nil {
		return "", err
	}
	if owner && os.Geteuid() == 0 {
		if err = h.Chown(f.UID, f.GID); err != nil {
			return "", err
		}
	}
	if err = h.Chmod(f.Mode.Perm()); err != nil {
		return "", err
	}
	if err = h.Sync(); err != nil {
		return "", err
	}
	if err = h.Close(); err != nil {
		return "", err
	}
	ok = true
	return tmp, nil
}

func atomicFile(r *os.Root, p string, f File) error {
	tmp, err := stage(r, p, f, false)
	if err != nil {
		return err
	}
	defer r.Remove(tmp)
	if err = r.Rename(tmp, p); err != nil {
		return err
	}
	return syncDir(r, path.Dir(p))
}

func (s *Store) snapshot(im Image, reason string) (Manifest, error) {
	m := Manifest{ID: time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + randomID(), Reason: reason, CreatedAt: time.Now().UTC(), Files: map[string]FileMeta{}}
	if len(reason) > 512 {
		return m, fmt.Errorf("snapshot reason too long")
	}
	if err := s.backups.Mkdir(m.ID, 0700); err != nil {
		return m, err
	}
	complete := false
	defer func() {
		if !complete {
			s.backups.RemoveAll(m.ID)
		}
	}()
	r, err := s.backups.OpenRoot(m.ID)
	if err != nil {
		return m, err
	}
	defer r.Close()
	for _, p := range sortedPaths(im) {
		f := im[p]
		if !validPath(p) {
			return m, fmt.Errorf("invalid snapshot path")
		}
		if err = r.MkdirAll(path.Dir(p), 0700); err != nil {
			return m, err
		}
		if err = atomicFile(r, p, File{Data: f.Data, Mode: 0600}); err != nil {
			return m, err
		}
		m.Files[p] = FileMeta{digest(f.Data), f.Mode, f.UID, f.GID}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return m, err
	}
	if err = atomicFile(r, "manifest.json", File{Data: data, Mode: 0600}); err != nil {
		return m, err
	}
	if err = syncDir(s.backups, "."); err != nil {
		return m, err
	}
	complete = true
	return m, nil
}

func (s *Store) Snapshot(reason string) (Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	im, err := s.read()
	if err != nil {
		return Manifest{}, err
	}
	if _, err := s.backups.Stat("pending.json"); err == nil {
		return Manifest{}, fmt.Errorf("transaction recovery pending")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Manifest{}, err
	}
	m, err := s.snapshot(im, reason)
	if err != nil {
		return m, err
	}
	return m, s.prune(20)
}

func validSnapshotID(id string) bool {
	return len(id) > 30 && len(id) < 100 && path.Base(id) == id && !strings.ContainsAny(id, "/\\\x00") && strings.Contains(id, "Z-")
}

func (s *Store) loadSnapshot(id string) (Manifest, Image, error) {
	var m Manifest
	if !validSnapshotID(id) {
		return m, nil, fmt.Errorf("invalid snapshot ID")
	}
	i, err := s.backups.Lstat(id)
	if err != nil {
		return m, nil, err
	}
	if !i.IsDir() {
		return m, nil, fmt.Errorf("invalid snapshot directory")
	}
	r, err := s.backups.OpenRoot(id)
	if err != nil {
		return m, nil, err
	}
	defer r.Close()
	f, err := readFile(r, "manifest.json")
	if err != nil {
		return m, nil, err
	}
	if err = json.Unmarshal(f.Data, &m); err != nil {
		return m, nil, err
	}
	if m.ID != id {
		return m, nil, fmt.Errorf("snapshot ID mismatch")
	}
	im := Image{}
	total := 0
	for p, meta := range m.Files {
		if !validPath(p) || meta.Mode & ^fs.FileMode(0777) != 0 {
			return m, nil, fmt.Errorf("invalid snapshot entry")
		}
		f, err := readFile(r, p)
		if err != nil {
			return m, nil, err
		}
		if digest(f.Data) != meta.SHA256 {
			return m, nil, fmt.Errorf("snapshot integrity failure: %s", p)
		}
		total += len(f.Data)
		if total > MaxTotalSize {
			return m, nil, fmt.Errorf("snapshot too large")
		}
		f.Mode, f.UID, f.GID = meta.Mode, meta.UID, meta.GID
		im[p] = f
	}
	return m, im, nil
}

func (s *Store) Snapshots() ([]Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshots()
}

func (s *Store) snapshots() ([]Manifest, error) {
	d, err := s.backups.Open(".")
	if err != nil {
		return nil, err
	}
	defer d.Close()
	entries, err := d.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	result := []Manifest{}
	for _, e := range entries {
		if !e.IsDir() || !validSnapshotID(e.Name()) {
			continue
		}
		m, _, err := s.loadSnapshot(e.Name())
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, nil
}

func (s *Store) SnapshotContent(id string) (Manifest, Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadSnapshot(id)
}

func (s *Store) DeleteSnapshot(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pinned, err := s.baselineID()
	if err != nil {
		return err
	}
	if id == pinned {
		return fmt.Errorf("cannot delete the current managed baseline")
	}
	if _, err := s.backups.Stat("pending.json"); err == nil {
		return fmt.Errorf("transaction recovery pending")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if _, _, err := s.loadSnapshot(id); err != nil {
		return err
	}
	return errors.Join(s.backups.RemoveAll(id), syncDir(s.backups, "."))
}
