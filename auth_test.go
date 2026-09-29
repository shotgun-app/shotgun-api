package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"shotgun-api/config"
)

// Integration tests run against the real database from docker compose and are skipped without TEST_DATABASE_URL
type testEnv struct {
	router *gin.Engine
	pool   *pgxpool.Pool
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	cfg := &config.Config{AllowedOrigin: "http://localhost:5173", SessionTTL: time.Hour}
	return &testEnv{router: setupRouter(cfg, pool), pool: pool}
}

func (e *testEnv) do(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session" {
			return c
		}
	}
	return nil
}

func uniqueEmail() string { return fmt.Sprintf("test-%d@shotgun.test", time.Now().UnixNano()) }

func registerBody(email string) string {
	return fmt.Sprintf(`{"name":"Test User","email":%q,"password":"password123"}`, email)
}

func (e *testEnv) cleanup(t *testing.T, email string) {
	t.Cleanup(func() {
		_, _ = e.pool.Exec(context.Background(), `DELETE FROM users WHERE lower(email) = lower($1)`, email)
	})
}

func TestAuthFlow(t *testing.T) {
	e := newTestEnv(t)
	email := uniqueEmail()
	e.cleanup(t, email)

	rec := e.do("POST", "/auth/register", registerBody(email), nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d, body %s", rec.Code, rec.Body)
	}
	cookie := sessionCookie(rec)
	if cookie == nil || !cookie.HttpOnly || cookie.Value == "" {
		t.Fatalf("want HttpOnly session cookie, got %+v", cookie)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Errorf("response leaks password data: %s", rec.Body)
	}

	rec = e.do("GET", "/auth/me", "", cookie)
	var body struct{ User struct{ Email string } }
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusOK || body.User.Email != email {
		t.Fatalf("me = %d %s", rec.Code, rec.Body)
	}

	rec = e.do("PATCH", "/auth/me", `{"name":"Renamed","phone":"+386 40 123 456"}`, cookie)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Renamed") {
		t.Fatalf("patch = %d %s", rec.Code, rec.Body)
	}

	rec = e.do("POST", "/auth/logout", "", cookie)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d", rec.Code)
	}
	if c := sessionCookie(rec); c == nil || c.MaxAge >= 0 {
		t.Errorf("logout must clear the cookie, got %+v", c)
	}
	var n int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE token = $1`, cookie.Value).Scan(&n)
	if n != 0 {
		t.Errorf("session row still exists after logout")
	}
	if rec = e.do("GET", "/auth/me", "", cookie); rec.Code != http.StatusUnauthorized {
		t.Errorf("me after logout = %d, want 401", rec.Code)
	}
}

func TestRegisterValidationAndDuplicate(t *testing.T) {
	e := newTestEnv(t)
	email := uniqueEmail()
	e.cleanup(t, email)

	bad := []string{
		`{"name":"","email":"a@b.co","password":"password123"}`,
		`{"name":"A","email":"nope","password":"password123"}`,
		`{"name":"A","email":"a@b.co","password":"short"}`,
		`not json`,
	}
	for _, b := range bad {
		if rec := e.do("POST", "/auth/register", b, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", b, rec.Code)
		}
	}

	if rec := e.do("POST", "/auth/register", registerBody(email), nil); rec.Code != http.StatusCreated {
		t.Fatalf("register = %d", rec.Code)
	}
	rec := e.do("POST", "/auth/register", registerBody(strings.ToUpper(email)), nil)
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate (different case) = %d, want 409", rec.Code)
	}
}

func TestLogin(t *testing.T) {
	e := newTestEnv(t)
	email := uniqueEmail()
	e.cleanup(t, email)
	e.do("POST", "/auth/register", registerBody(email), nil)

	rec := e.do("POST", "/auth/login", fmt.Sprintf(`{"email":%q,"password":"password123"}`, strings.ToUpper(email)), nil)
	if rec.Code != http.StatusOK || sessionCookie(rec) == nil {
		t.Fatalf("login = %d %s", rec.Code, rec.Body)
	}
	for _, b := range []string{
		fmt.Sprintf(`{"email":%q,"password":"wrongpassword"}`, email),
		`{"email":"nobody@shotgun.test","password":"password123"}`,
	} {
		if rec := e.do("POST", "/auth/login", b, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("login %s = %d, want 401", b, rec.Code)
		}
	}
}

func TestExpiredSessionRejected(t *testing.T) {
	e := newTestEnv(t)
	email := uniqueEmail()
	e.cleanup(t, email)
	cookie := sessionCookie(e.do("POST", "/auth/register", registerBody(email), nil))

	_, err := e.pool.Exec(context.Background(),
		`UPDATE sessions SET expires_at = now() - interval '1 minute' WHERE token = $1`, cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	if rec := e.do("GET", "/auth/me", "", cookie); rec.Code != http.StatusUnauthorized {
		t.Errorf("expired session = %d, want 401", rec.Code)
	}
}

func TestUpdateEmailConflictAndDelete(t *testing.T) {
	e := newTestEnv(t)
	a, b := uniqueEmail(), uniqueEmail()
	e.cleanup(t, a)
	e.cleanup(t, b)
	e.do("POST", "/auth/register", registerBody(a), nil)
	cookie := sessionCookie(e.do("POST", "/auth/register", registerBody(b), nil))

	if rec := e.do("PATCH", "/auth/me", fmt.Sprintf(`{"email":%q}`, a), cookie); rec.Code != http.StatusConflict {
		t.Errorf("patch to taken email = %d, want 409", rec.Code)
	}
	if rec := e.do("DELETE", "/auth/me", "", cookie); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	if rec := e.do("GET", "/auth/me", "", cookie); rec.Code != http.StatusUnauthorized {
		t.Errorf("me after delete = %d, want 401", rec.Code)
	}
	var n int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE lower(email) = lower($1)`, b).Scan(&n)
	if n != 0 {
		t.Errorf("user row still exists after delete")
	}
}

func TestProtectedRoutesNeedCookie(t *testing.T) {
	router := setupRouter(&config.Config{}, nil)
	for _, m := range []string{"GET", "PATCH", "DELETE"} {
		req := httptest.NewRequest(m, "/auth/me", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s /auth/me without cookie = %d, want 401", m, rec.Code)
		}
	}
}
