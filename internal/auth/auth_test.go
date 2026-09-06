package auth

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"avahi-manager/internal/database"
)

func TestPasswordHash(t *testing.T) {
	h, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$") || strings.Contains(h, "correct horse") {
		t.Fatal("password not hashed")
	}
	if !VerifyPassword(h, "correct horse battery staple") || VerifyPassword(h, "wrong") {
		t.Fatal("password verification failure")
	}
	if _, err = HashPassword("short"); err == nil {
		t.Fatal("short password accepted")
	}
	for _, bad := range []string{"", "$argon2id$v=19$m=4294967295,t=3,p=2$aaaa$bbbb", strings.Replace(h, "$v=19$", "$v=16$", 1), h + "extra"} {
		if VerifyPassword(bad, "correct horse battery staple") {
			t.Fatal("invalid hash accepted")
		}
	}
}

func authFixture(t *testing.T) (*Auth, *database.DB) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "manager.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.CreateAdmin(context.Background(), "admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	return a, db
}

func TestSessionLifecycleAndSingleAdmin(t *testing.T) {
	a, db := authFixture(t)
	ctx := context.Background()
	if err := a.CreateAdmin(ctx, "other", "correct horse battery staple"); err == nil {
		t.Fatal("second administrator accepted")
	}
	if _, _, err := a.Login(ctx, "127.0.0.1", "admin", "wrong"); !errors.Is(err, ErrCredentials) {
		t.Fatal(err)
	}
	token, s, err := a.Login(ctx, "127.0.0.1", "admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || s.CSRF == "" || s.CSRF == token {
		t.Fatal("invalid tokens")
	}
	var stored string
	if err = db.SQL.QueryRow("SELECT token_hash FROM sessions").Scan(&stored); err != nil || stored == token || stored != tokenHash(token) {
		t.Fatal("session token stored in plaintext", err)
	}
	got, err := a.Session(ctx, token)
	if err != nil || got.Username != "admin" || got.CSRF != s.CSRF {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err = a.Session(ctx, strings.Repeat("a", 64)); !errors.Is(err, ErrSession) {
		t.Fatal(err)
	}
	if err = a.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Session(ctx, token); !errors.Is(err, ErrSession) {
		t.Fatal("logout did not revoke session", err)
	}
	token, _, err = a.Login(ctx, "127.0.0.1", "admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL.Exec("UPDATE sessions SET expires_at='2000-01-01T00:00:00.000000000Z'"); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Session(ctx, token); !errors.Is(err, ErrSession) {
		t.Fatal("expired session accepted", err)
	}
}

func TestLoginRateLimit(t *testing.T) {
	a, _ := authFixture(t)
	ctx := context.Background()
	for range 5 {
		if _, _, err := a.Login(ctx, "192.0.2.1", "admin", "bad"); !errors.Is(err, ErrCredentials) {
			t.Fatal(err)
		}
	}
	if _, _, err := a.Login(ctx, "192.0.2.1", "admin", "correct horse battery staple"); !errors.Is(err, ErrRateLimit) {
		t.Fatal("rate limit not enforced", err)
	}
	if _, _, err := a.Login(ctx, "192.0.2.2", "admin", "correct horse battery staple"); err != nil {
		t.Fatal("rate limit leaked to another peer", err)
	}
}
