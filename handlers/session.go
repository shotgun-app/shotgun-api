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
		`INSERT INTO sessions (user_id, token, expires_at, user_agent) VALUES ($1, $2, $3, $4)`,
		userID, token, expires, c.Request.UserAgent())
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

// Session is one signed-in device of the user
type Session struct {
	ID        string    `json:"id"`
	UserAgent string    `json:"userAgent"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Current   bool      `json:"current"`
}

// ListSessions returns the user's unexpired sessions, newest first. Current marks the session
// making the request, so the web app knows that deleting it signs the user out
func (a *Auth) ListSessions(c *gin.Context) {
	token, _ := c.Cookie(SessionCookie)
	rows, err := a.DB.Query(c.Request.Context(),
		`SELECT id, user_agent, created_at, expires_at, token = $2
		 FROM sessions WHERE user_id = $1 AND expires_at > now()
		 ORDER BY created_at DESC`, CurrentUser(c).ID, token)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	defer rows.Close()

	list := make([]Session, 0)
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.UserAgent, &s.CreatedAt, &s.ExpiresAt, &s.Current); err != nil {
			fail(c, http.StatusInternalServerError, "Something went wrong.")
			return
		}
		list = append(list, s)
	}
	if err := rows.Err(); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"sessions": list})
}

// DeleteSession ends one of the user's sessions. Ending the current one also clears the cookie
func (a *Auth) DeleteSession(c *gin.Context) {
	id := c.Param("id")
	if !isUUID(id) {
		fail(c, http.StatusNotFound, "Session not found.")
		return
	}
	token, _ := c.Cookie(SessionCookie)
	var current bool
	err := a.DB.QueryRow(c.Request.Context(),
		`DELETE FROM sessions WHERE id = $1 AND user_id = $2 RETURNING token = $3`,
		id, CurrentUser(c).ID, token).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, http.StatusNotFound, "Session not found.")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	if current {
		a.clearCookie(c)
	}
	c.Status(http.StatusNoContent)
}
