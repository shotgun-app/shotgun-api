package handlers

import (
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"shotgun-api/config"
	"shotgun-api/mailer"
)

// Auth groups the auth handlers and the dependencies they share
type Auth struct {
	DB     *pgxpool.Pool
	Cfg    *config.Config
	Mailer mailer.Mailer
}

// User is the JSON shape returned to the web app
type User struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Phone    *string   `json:"phone"`
	JoinedAt time.Time `json:"joinedAt"`
}

const userColumns = `id, name, email, phone, created_at`

func scanUser(row pgx.Row) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Phone, &u.JoinedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func fail(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"message": msg})
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

type registerRequest struct {
	Name     string  `json:"name"`
	Email    string  `json:"email"`
	Password string  `json:"password"`
	Phone    *string `json:"phone"`
}

// validateEmail accepts a bare address only ("a@b.c"), not "Name <a@b.c>"
func validateEmail(email string) bool {
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email && len(email) <= 320
}

var phoneRE = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

// normalizePhone strips spaces and dashes and requires E.164 (+ country code and digits).
// An empty value returns ("", true); callers decide whether that is allowed
func normalizePhone(raw string) (string, bool) {
	p := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(raw))
	if p == "" {
		return "", true
	}
	return p, phoneRE.MatchString(p)
}

func validName(name string) bool { return name != "" && len(name) <= 100 }

func (a *Auth) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "Invalid request body.")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.TrimSpace(req.Email)
	switch {
	case !validName(req.Name):
		fail(c, http.StatusBadRequest, "Name is required.")
		return
	case !validateEmail(req.Email):
		fail(c, http.StatusBadRequest, "Enter a valid email address.")
		return
	case len(req.Password) < 8:
		fail(c, http.StatusBadRequest, "Password must be at least 8 characters.")
		return
	case len(req.Password) > 72: // bcrypt ignores everything past 72 bytes
		fail(c, http.StatusBadRequest, "Password must be at most 72 characters.")
		return
	}
	// Drivers and passengers contact each other, so every account needs a number
	phone, ok := normalizePhone(derefString(req.Phone))
	if phone == "" {
		fail(c, http.StatusBadRequest, "Phone number is required.")
		return
	}
	if !ok {
		fail(c, http.StatusBadRequest, "Enter a valid phone number with country code.")
		return
	}
	req.Phone = &phone

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	ctx := c.Request.Context()
	user, err := scanUser(a.DB.QueryRow(ctx,
		`INSERT INTO users (name, email, password_hash, phone) VALUES ($1, $2, $3, $4)
		 RETURNING `+userColumns,
		req.Name, req.Email, string(hash), req.Phone))
	if err != nil {
		if isUniqueViolation(err) {
			fail(c, http.StatusConflict, "An account with that email already exists.")
			return
		}
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	if err := a.startSession(ctx, c, user.ID); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"user": user})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// dummyHash is compared against when the email is unknown so both failures take similar time
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)

func (a *Auth) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "Invalid request body.")
		return
	}
	email := strings.TrimSpace(req.Email)

	ctx := c.Request.Context()
	var hash string
	var user User
	err := a.DB.QueryRow(ctx,
		`SELECT `+userColumns+`, password_hash FROM users WHERE lower(email) = lower($1)`, email).
		Scan(&user.ID, &user.Name, &user.Email, &user.Phone, &user.JoinedAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(req.Password))
		fail(c, http.StatusUnauthorized, "Wrong email or password.")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		fail(c, http.StatusUnauthorized, "Wrong email or password.")
		return
	}
	if err := a.startSession(ctx, c, user.ID); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

// Logout is idempotent: no cookie or an unknown token still ends with a cleared cookie
func (a *Auth) Logout(c *gin.Context) {
	if token, err := c.Cookie(SessionCookie); err == nil {
		_, _ = a.DB.Exec(c.Request.Context(), `DELETE FROM sessions WHERE token = $1`, token)
	}
	a.clearCookie(c)
	c.Status(http.StatusNoContent)
}

func (a *Auth) Me(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"user": CurrentUser(c)})
}

type updateRequest struct {
	Name  *string `json:"name"`
	Email *string `json:"email"`
	Phone *string `json:"phone"`
}

func (a *Auth) UpdateMe(c *gin.Context) {
	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		req.Name = &trimmed
		if !validName(trimmed) {
			fail(c, http.StatusBadRequest, "Name is required.")
			return
		}
	}
	if req.Email != nil {
		trimmed := strings.TrimSpace(*req.Email)
		req.Email = &trimmed
		if !validateEmail(trimmed) {
			fail(c, http.StatusBadRequest, "Enter a valid email address.")
			return
		}
	}

	// A missing phone leaves it alone, but it can never be emptied
	setPhone := req.Phone != nil
	var phone *string
	if setPhone {
		p, ok := normalizePhone(*req.Phone)
		if p == "" {
			fail(c, http.StatusBadRequest, "Phone number is required.")
			return
		}
		if !ok {
			fail(c, http.StatusBadRequest, "Enter a valid phone number with country code.")
			return
		}
		phone = &p
	}

	// COALESCE keeps the current value for name/email that were not sent
	user, err := scanUser(a.DB.QueryRow(c.Request.Context(),
		`UPDATE users SET name = COALESCE($2, name), email = COALESCE($3, email),
		 phone = CASE WHEN $5 THEN $4 ELSE phone END, updated_at = now()
		 WHERE id = $1 RETURNING `+userColumns,
		CurrentUser(c).ID, req.Name, req.Email, phone, setPhone))
	if err != nil {
		if isUniqueViolation(err) {
			fail(c, http.StatusConflict, "An account with that email already exists.")
			return
		}
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

// DeleteMe removes the user, their sessions go with it (ON DELETE CASCADE)
func (a *Auth) DeleteMe(c *gin.Context) {
	if _, err := a.DB.Exec(c.Request.Context(), `DELETE FROM users WHERE id = $1`, CurrentUser(c).ID); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	a.clearCookie(c)
	c.Status(http.StatusNoContent)
}

type passwordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// ChangePassword checks the current password, then ends every other session so a stolen
// session cannot outlive the change. The session making the request stays signed in
func (a *Auth) ChangePassword(c *gin.Context) {
	var req passwordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "Invalid request body.")
		return
	}
	switch {
	case len(req.NewPassword) < 8:
		fail(c, http.StatusBadRequest, "Password must be at least 8 characters.")
		return
	case len(req.NewPassword) > 72:
		fail(c, http.StatusBadRequest, "Password must be at most 72 characters.")
		return
	}

	ctx := c.Request.Context()
	user := CurrentUser(c)
	var hash string
	if err := a.DB.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, user.ID).Scan(&hash); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.CurrentPassword)) != nil {
		fail(c, http.StatusUnauthorized, "Current password is incorrect.")
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	token, _ := c.Cookie(SessionCookie)
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, user.ID, string(newHash)); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1 AND token <> $2`, user.ID, token); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	c.Status(http.StatusNoContent)
}
