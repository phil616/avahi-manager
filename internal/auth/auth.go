package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"avahi-manager/internal/database"
)

var ErrCredentials = errors.New("invalid username or password")
var ErrRateLimit = errors.New("too many login attempts; try again later")
var ErrSession = errors.New("session expired or invalid")
var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,64}$`)

type attempt struct {
	Count   int
	Expires time.Time
}
type Auth struct {
	db       *database.DB
	mu       sync.Mutex
	attempts map[string]attempt
	verify   chan struct{}
	dummy    string
}

func New(db *database.DB) (*Auth, error) {
	raw, err := randomToken()
	if err != nil {
		return nil, err
	}
	dummy, err := HashPassword(raw)
	if err != nil {
		return nil, err
	}
	return &Auth{db: db, attempts: map[string]attempt{}, verify: make(chan struct{}, 2), dummy: dummy}, nil
}

func (a *Auth) HasAdmin(ctx context.Context) (bool, error) {
	var count int
	err := a.db.SQL.QueryRowContext(ctx, "SELECT count(*) FROM users").Scan(&count)
	return count == 1, err
}

func (a *Auth) CreateAdmin(ctx context.Context, username, password string) error {
	if !usernamePattern.MatchString(username) {
		return fmt.Errorf("username must be 1–64 letters, digits, dots, underscores or hyphens")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = a.db.SQL.ExecContext(ctx, "INSERT INTO users(id,username,password_hash,created_at) VALUES(1,?,?,?)", username, hash, database.Now())
	return err
}

func tokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (a *Auth) allow(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for k, v := range a.attempts {
		if now.After(v.Expires) {
			delete(a.attempts, k)
		}
	}
	v := a.attempts[key]
	if v.Count >= 5 {
		return false
	}
	if v.Count == 0 {
		if len(a.attempts) >= 4096 {
			return false
		}
		v.Expires = now.Add(15 * time.Minute)
	}
	v.Count++
	a.attempts[key] = v
	return true
}

type Session struct {
	Username  string    `json:"username"`
	CSRF      string    `json:"csrf"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// CSRF is bound to the high-entropy session token, requires no extra SQL column,
// and changes whenever the session rotates. It is returned only through the
// same-origin authenticated session endpoint.
func csrf(token string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("avahi-manager csrf v1"))
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *Auth) Login(ctx context.Context, ip, username, password string) (string, Session, error) {
	if !a.allow(ip) {
		return "", Session{}, ErrRateLimit
	}
	select {
	case a.verify <- struct{}{}:
		defer func() { <-a.verify }()
	default:
		return "", Session{}, ErrRateLimit
	}
	var storedUser, hash string
	err := a.db.SQL.QueryRowContext(ctx, "SELECT username,password_hash FROM users WHERE id=1").Scan(&storedUser, &hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", Session{}, err
	}
	if hash == "" {
		hash = a.dummy
	}
	valid := VerifyPassword(hash, password)
	if err != nil || !valid || username != storedUser {
		return "", Session{}, ErrCredentials
	}
	token, err := randomToken()
	if err != nil {
		return "", Session{}, err
	}
	id, err := randomToken()
	if err != nil {
		return "", Session{}, err
	}
	expires := time.Now().UTC().Add(12 * time.Hour)
	tx, err := a.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return "", Session{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", database.Now()); err != nil {
		return "", Session{}, err
	}
	// Keep a small number of concurrently signed-in browsers for one admin.
	if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE id IN (SELECT id FROM sessions ORDER BY created_at DESC LIMIT -1 OFFSET 19)"); err != nil {
		return "", Session{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO sessions(id,user_id,token_hash,expires_at,created_at) VALUES(?,1,?,?,?)", id, tokenHash(token), expires.Format("2006-01-02T15:04:05.000000000Z"), database.Now()); err != nil {
		return "", Session{}, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE users SET last_login_at=? WHERE id=1", database.Now()); err != nil {
		return "", Session{}, err
	}
	if err = tx.Commit(); err != nil {
		return "", Session{}, err
	}
	a.mu.Lock()
	delete(a.attempts, ip)
	a.mu.Unlock()
	return token, Session{storedUser, csrf(token), expires}, nil
}

func (a *Auth) Session(ctx context.Context, token string) (Session, error) {
	if len(token) != 64 {
		return Session{}, ErrSession
	}
	var s Session
	var expires string
	err := a.db.SQL.QueryRowContext(ctx, "SELECT u.username,s.expires_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>?", tokenHash(token), database.Now()).Scan(&s.Username, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrSession
	}
	if err != nil {
		return s, err
	}
	s.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	s.CSRF = csrf(token)
	return s, err
}

func (a *Auth) Logout(ctx context.Context, token string) error {
	_, err := a.db.SQL.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash=?", tokenHash(token))
	return err
}
