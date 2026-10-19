package handlers

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

const maxProfilePicSize = 5 << 20

// profilePicTypes maps the sniffed content type to the extension the picture is stored with
var profilePicTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// UploadProfilePic stores the "picture" form file as <user id>.<ext> and returns the updated user.
// The type comes from the file's content, not from the client's filename or Content-Type
func (a *Auth) UploadProfilePic(c *gin.Context) {
	// The limit leaves room for the multipart headers around the file
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxProfilePicSize+64<<10)
	header, err := c.FormFile("picture")
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) || (err == nil && header.Size > maxProfilePicSize) {
		fail(c, http.StatusRequestEntityTooLarge, "Picture must be at most 5 MB.")
		return
	}
	if err != nil {
		fail(c, http.StatusBadRequest, "Choose a picture to upload.")
		return
	}

	file, err := header.Open()
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	file.Close()
	ext, ok := profilePicTypes[http.DetectContentType(head[:n])]
	if !ok {
		fail(c, http.StatusBadRequest, "Picture must be a JPEG, PNG or WebP image.")
		return
	}

	userID := CurrentUser(c).ID
	name := userID + ext
	if err := c.SaveUploadedFile(header, filepath.Join(a.Cfg.UploadDir, name)); err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	user, err := scanUser(a.DB.QueryRow(c.Request.Context(),
		`UPDATE users SET profile_pic_file_name = $2, updated_at = now() WHERE id = $1 RETURNING `+userColumns,
		userID, name))
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	// A picture in another format would otherwise stay on disk under the old name
	a.removeProfilePics(userID, ext)
	c.JSON(http.StatusOK, gin.H{"user": user})
}

// DeleteProfilePic clears the user's picture and returns the updated user
func (a *Auth) DeleteProfilePic(c *gin.Context) {
	userID := CurrentUser(c).ID
	user, err := scanUser(a.DB.QueryRow(c.Request.Context(),
		`UPDATE users SET profile_pic_file_name = NULL, updated_at = now() WHERE id = $1 RETURNING `+userColumns,
		userID))
	if err != nil {
		fail(c, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	a.removeProfilePics(userID, "")
	c.JSON(http.StatusOK, gin.H{"user": user})
}

// removeProfilePics deletes the user's stored pictures except the one with extension keep.
// Most of the files do not exist, so errors are ignored
func (a *Auth) removeProfilePics(userID, keep string) {
	for _, ext := range profilePicTypes {
		if ext != keep {
			_ = os.Remove(filepath.Join(a.Cfg.UploadDir, userID+ext))
		}
	}
}
