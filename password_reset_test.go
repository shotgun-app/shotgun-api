package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The emailed token is only stored hashed, so tests plant a row with a token they know
func (e *testEnv) plantReset(t *testing.T, email, token string, expiresIn time.Duration) {
	t.Helper()
	sum := sha256.Sum256([]byte(token))
	_, err := e.pool.Exec(context.Background(),
		`INSERT INTO password_resets (user_id, token_hash, expires_at)
		 SELECT id, $2, $3 FROM users WHERE lower(email) = lower($1)`,
		email, hex.EncodeToString(sum[:]), time.Now().Add(expiresIn))
	if err != nil {
		t.Fatal(err)
	}
}

func (e *testEnv) resetCount(email string) int {
	var n int
	_ = e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM password_resets r JOIN users u ON u.id = r.user_id WHERE lower(u.email) = lower($1)`,
		email).Scan(&n)
	return n
}

func TestForgotPassword(t *testing.T) {
	e := newTestEnv(t)
	email := uniqueEmail()
	e.cleanup(t, email)
	e.do("POST", "/auth/register", registerBody(email), nil)

	if rec := e.do("POST", "/auth/forgot-password", `{"email":"nope"}`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid email = %d, want 400", rec.Code)
	}
	// Unknown accounts get the same answer and leave no row behind
	if rec := e.do("POST", "/auth/forgot-password", `{"email":"nobody@shotgun.test"}`, nil); rec.Code != http.StatusNoContent {
		t.Errorf("unknown email = %d, want 204", rec.Code)
	}

	body := fmt.Sprintf(`{"email":%q}`, strings.ToUpper(email))
	if rec := e.do("POST", "/auth/forgot-password", body, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("forgot = %d %s", rec.Code, rec.Body)
	}
	if n := e.resetCount(email); n != 1 {
		t.Fatalf("reset rows = %d, want 1", n)
	}
	var expires time.Time
	_ = e.pool.QueryRow(context.Background(),
		`SELECT r.expires_at FROM password_resets r JOIN users u ON u.id = r.user_id WHERE u.email = $1`, email).Scan(&expires)
	if d := time.Until(expires); d < 29*time.Minute || d > 30*time.Minute {
		t.Errorf("token expires in %v, want about 30m", d)
	}

	// A second request replaces the first link instead of piling up
	e.do("POST", "/auth/forgot-password", body, nil)
	if n := e.resetCount(email); n != 1 {
		t.Errorf("reset rows after second request = %d, want 1", n)
	}
}

func TestCheckResetToken(t *testing.T) {
	e := newTestEnv(t)
	email := uniqueEmail()
	e.cleanup(t, email)
	e.do("POST", "/auth/register", registerBody(email), nil)
	e.plantReset(t, email, "live-token", 10*time.Minute)
	e.plantReset(t, email, "dead-token", -time.Minute)

	if rec := e.do("GET", "/auth/reset-password?token=live-token", "", nil); rec.Code != http.StatusNoContent {
		t.Errorf("live token = %d, want 204", rec.Code)
	}
	// Checking does not use the token up
	if rec := e.do("GET", "/auth/reset-password?token=live-token", "", nil); rec.Code != http.StatusNoContent {
		t.Errorf("live token checked twice = %d, want 204", rec.Code)
	}
	for _, path := range []string{
		"/auth/reset-password?token=dead-token",
		"/auth/reset-password?token=d",
		"/auth/reset-password",
	} {
		if rec := e.do("GET", path, "", nil); rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", path, rec.Code)
		}
	}
}

func TestResetPassword(t *testing.T) {
	e := newTestEnv(t)
	email := uniqueEmail()
	e.cleanup(t, email)
	session := sessionCookie(e.do("POST", "/auth/register", registerBody(email), nil))
	e.plantReset(t, email, "good-token", 10*time.Minute)

	if rec := e.do("POST", "/auth/reset-password", `{"token":"wrong-token","password":"newpassword1"}`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown token = %d, want 400", rec.Code)
	}
	// A weak password is rejected without using up the token
	if rec := e.do("POST", "/auth/reset-password", `{"token":"good-token","password":"short"}`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("short password = %d, want 400", rec.Code)
	}
	if n := e.resetCount(email); n != 1 {
		t.Fatalf("token must survive a rejected password, rows = %d", n)
	}

	if rec := e.do("POST", "/auth/reset-password", `{"token":"good-token","password":"newpassword1"}`, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("reset = %d %s", rec.Code, rec.Body)
	}
	if rec := e.do("POST", "/auth/reset-password", `{"token":"good-token","password":"another-pass1"}`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("reusing the token = %d, want 400", rec.Code)
	}
	if rec := e.do("GET", "/auth/me", "", session); rec.Code != http.StatusUnauthorized {
		t.Errorf("old session after reset = %d, want 401", rec.Code)
	}
	old := fmt.Sprintf(`{"email":%q,"password":"password123"}`, email)
	if rec := e.do("POST", "/auth/login", old, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("login with old password = %d, want 401", rec.Code)
	}
	fresh := fmt.Sprintf(`{"email":%q,"password":"newpassword1"}`, email)
	if rec := e.do("POST", "/auth/login", fresh, nil); rec.Code != http.StatusOK {
		t.Errorf("login with new password = %d, want 200", rec.Code)
	}
}

func TestResetPasswordExpiredToken(t *testing.T) {
	e := newTestEnv(t)
	email := uniqueEmail()
	e.cleanup(t, email)
	e.do("POST", "/auth/register", registerBody(email), nil)
	e.plantReset(t, email, "old-token", -time.Minute)

	rec := e.do("POST", "/auth/reset-password", `{"token":"old-token","password":"newpassword1"}`, nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "expired") {
		t.Errorf("expired token = %d %s, want 400 expired", rec.Code, rec.Body)
	}
	login := fmt.Sprintf(`{"email":%q,"password":"password123"}`, email)
	if rec := e.do("POST", "/auth/login", login, nil); rec.Code != http.StatusOK {
		t.Errorf("password must be unchanged, login = %d", rec.Code)
	}
}
