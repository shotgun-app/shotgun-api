package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"shotgun-api/mailer"
)

// ResetTokenTTL is how long an emailed reset link works
const ResetTokenTTL = 30 * time.Minute

// hashResetToken is what the database stores. The token is random, so a fast hash is enough
func hashResetToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

// ForgotPassword emails a reset link. It answers 204 whether or not the account exists,
// so the endpoint cannot be used to find out which emails are registered
func (a *Auth) ForgotPassword(c *gin.Context) {
	var req forgotPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "Invalid request body.")
		return
	}
	email := strings.TrimSpace(req.Email)
	if !validateEmail(email) {
		fail(c, http.StatusBadRequest, "Enter a valid email address.")
		return
	}

	ctx := c.Request.Context()
	var userID, name, userEmail string
	err := a.DB.QueryRow(ctx, `SELECT id, name, email FROM users WHERE lower(email) = lower($1)`, email).
		Scan(&userID, &name, &userEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		c.Status(http.StatusNoContent)
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	token, err := newToken()
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	// One live link per user: a new request replaces the previous one. Expired rows are swept here too
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM password_resets WHERE user_id = $1 OR expires_at <= now()`, userID); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO password_resets (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, hashResetToken(token), time.Now().Add(ResetTokenTTL)); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	// A delivery failure is only logged: reporting it would reveal that the account exists
	link := strings.TrimRight(a.Cfg.AppURL, "/") + "/reset-password?token=" + url.QueryEscape(token)
	if err := a.Mailer.SendPasswordReset(ctx, mailer.PasswordReset{
		Name:          name,
		Email:         userEmail,
		Link:          link,
		ExpiresInMins: int(ResetTokenTTL.Minutes()),
	}); err != nil {
		log.Printf("password reset email for user %s: %v", userID, err)
	}
	c.Status(http.StatusNoContent)
}

// CheckResetToken tells the reset page whether a link is still usable before the user types a
// new password. It only reads, so the token stays valid
func (a *Auth) CheckResetToken(c *gin.Context) {
	var found bool
	err := a.DB.QueryRow(c.Request.Context(),
		`SELECT EXISTS (SELECT 1 FROM password_resets WHERE token_hash = $1 AND expires_at > now())`,
		hashResetToken(c.Query("token"))).Scan(&found)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	if !found {
		fail(c, http.StatusBadRequest, "This reset link is invalid or has expired.")
		return
	}
	c.Status(http.StatusNoContent)
}

type resetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// ResetPassword sets a new password when the token is valid and unexpired, then ends every
// session of that user. The token is single use
func (a *Auth) ResetPassword(c *gin.Context) {
	var req resetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "Invalid request body.")
		return
	}
	// Checked before the token is used up, so a too short password does not burn the link
	switch {
	case len(req.Password) < 8:
		fail(c, http.StatusBadRequest, "Password must be at least 8 characters.")
		return
	case len(req.Password) > 72:
		fail(c, http.StatusBadRequest, "Password must be at most 72 characters.")
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	ctx := c.Request.Context()
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	defer tx.Rollback(ctx)

	// DELETE ... RETURNING claims the token atomically, so two requests cannot both use it
	var userID string
	err = tx.QueryRow(ctx,
		`DELETE FROM password_resets WHERE token_hash = $1 AND expires_at > now() RETURNING user_id`,
		hashResetToken(req.Token)).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, http.StatusBadRequest, "This reset link is invalid or has expired.")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, userID, string(newHash)); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	c.Status(http.StatusNoContent)
}
