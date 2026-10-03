package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Users serves public profiles to signed-in users
type Users struct {
	DB *pgxpool.Pool
}

func (u *Users) Get(c *gin.Context) {
	id := c.Param("id")
	if !isUUID(id) {
		fail(c, http.StatusNotFound, "User not found.")
		return
	}
	user, err := scanUser(u.DB.QueryRow(c.Request.Context(),
		`SELECT `+userColumns+` FROM users WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, http.StatusNotFound, "User not found.")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}
