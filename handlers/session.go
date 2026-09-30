package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

const SessionCookie = "session"

// newToken returns 32 random bytes, base64url encoded. Sessions are looked up by this value
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// startSession stores a new session for the user and sets the cookie on the response
func (a *Auth) startSession(ctx context.Context, c *gin.Context, userID string) error {
	token, err := newToken()
	if err != nil {
		return err
	}
	expires := time.Now().Add(a.Cfg.SessionTTL)
	_, err = a.DB.Exec(ctx,
		`INSERT INTO sessions (user_id, token, expires_at) VALUES ($1, $2, $3)`,
		userID, token, expires)
	if err != nil {
		return err
	}
	a.setCookie(c, token, int(a.Cfg.SessionTTL.Seconds()))
	return nil
}

func (a *Auth) setCookie(c *gin.Context, value string, maxAge int) {
	// SameSite=Lax: the web app and API are same-site (different ports), so the cookie is still sent
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(SessionCookie, value, maxAge, "/", "", a.Cfg.CookieSecure, true)
}

func (a *Auth) clearCookie(c *gin.Context) {
	a.setCookie(c, "", -1)
}

const userKey = "user"

// CurrentUser returns the user that RequireAuth put on the request context
func CurrentUser(c *gin.Context) *User {
	return c.MustGet(userKey).(*User)
}

// RequireAuth rejects requests without a valid, unexpired session cookie
func (a *Auth) RequireAuth(c *gin.Context) {
	token, err := c.Cookie(SessionCookie)
	if err != nil || token == "" {
		fail(c, http.StatusUnauthorized, "Not signed in.")
		return
	}
	user, err := scanUser(a.DB.QueryRow(c.Request.Context(),
		`SELECT u.id, u.name, u.email, u.phone, u.created_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token = $1 AND s.expires_at > now()`, token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Expired or unknown: drop the stale cookie (and row) so the client stops sending it
			_, _ = a.DB.Exec(c.Request.Context(), `DELETE FROM sessions WHERE token = $1`, token)
			a.clearCookie(c)
			fail(c, http.StatusUnauthorized, "Session expired.")
			return
		}
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	c.Set(userKey, user)
	c.Next()
}
