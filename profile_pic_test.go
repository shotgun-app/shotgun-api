package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (e *testEnv) uploadPic(content []byte, cookie *http.Cookie) *httptest.ResponseRecorder {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, _ := w.CreateFormFile("picture", "me.png")
	part.Write(content)
	w.Close()

	req := httptest.NewRequest("PUT", "/auth/me/picture", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func avatarURL(t *testing.T, rec *httptest.ResponseRecorder) *string {
	t.Helper()
	var body struct{ User struct{ AvatarURL *string } }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.User.AvatarURL
}

func TestProfilePic(t *testing.T) {
	e := newTestEnv(t)
	email := uniqueEmail()
	e.cleanup(t, email)
	rec := e.do("POST", "/auth/register", registerBody(email), nil)
	cookie := sessionCookie(rec)
	if url := avatarURL(t, rec); url != nil {
		t.Fatalf("new user avatarUrl = %q, want null", *url)
	}

	pic := pngBytes(t)
	rec = e.uploadPic(pic, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload = %d %s", rec.Code, rec.Body)
	}
	url := avatarURL(t, rec)
	if url == nil {
		t.Fatal("avatarUrl is null after upload")
	}

	// Served without a session, so the web app can use it as an <img> src
	rec = e.do("GET", *url, "", nil)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), pic) {
		t.Fatalf("get picture = %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}

	if rec := e.uploadPic([]byte("not an image"), cookie); rec.Code != http.StatusBadRequest {
		t.Errorf("text file = %d, want 400", rec.Code)
	}
	if rec := e.uploadPic(make([]byte, 6<<20), cookie); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("6 MB file = %d, want 413", rec.Code)
	}

	rec = e.do("DELETE", "/auth/me/picture", "", cookie)
	if rec.Code != http.StatusOK || avatarURL(t, rec) != nil {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body)
	}
	if rec := e.do("GET", *url, "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("picture after delete = %d, want 404", rec.Code)
	}
}
