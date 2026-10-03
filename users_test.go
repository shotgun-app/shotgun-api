package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestGetUser(t *testing.T) {
	e := newTestEnv(t)
	owner, viewer := uniqueEmail(), uniqueEmail()
	e.cleanup(t, owner)
	e.cleanup(t, viewer)

	var body struct{ User struct{ ID string } }
	_ = json.Unmarshal(e.do("POST", "/auth/register", registerBody(owner), nil).Body.Bytes(), &body)
	cookie := sessionCookie(e.do("POST", "/auth/register", registerBody(viewer), nil))

	if rec := e.do("GET", "/api/users/"+body.User.ID, "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("without cookie = %d, want 401", rec.Code)
	}
	rec := e.do("GET", "/api/users/"+body.User.ID, "", cookie)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), owner) {
		t.Fatalf("get = %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Errorf("response leaks password data: %s", rec.Body)
	}
	for _, id := range []string{"00000000-0000-0000-0000-000000000000", "nope"} {
		if rec := e.do("GET", "/api/users/"+id, "", cookie); rec.Code != http.StatusNotFound {
			t.Errorf("id %s = %d, want 404", id, rec.Code)
		}
	}
}
