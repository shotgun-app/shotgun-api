package mailer

import (
	"context"
	"strings"
	"testing"
)

func TestRenderPasswordReset(t *testing.T) {
	data := PasswordReset{Name: "Ana <b>", Email: "ana@x.com", Link: "http://app/reset-password?token=abc", ExpiresInMins: 30}
	msg, err := RenderPasswordReset(data)
	if err != nil {
		t.Fatal(err)
	}
	if msg.To != "ana@x.com" || msg.Subject != PasswordResetSubject {
		t.Errorf("unexpected envelope: %+v", msg)
	}
	for name, body := range map[string]string{"html": msg.HTML, "text": msg.Text} {
		if !strings.Contains(body, data.Link) || !strings.Contains(body, "30 minutes") {
			t.Errorf("%s body misses the link or expiry:\n%s", name, body)
		}
	}
	if strings.Contains(msg.HTML, "<b>") {
		t.Error("html body must escape the user's name")
	}
}

func TestLogMailerDoesNotFail(t *testing.T) {
	if err := (LogMailer{}).SendPasswordReset(context.Background(), PasswordReset{Email: "a@b.co"}); err != nil {
		t.Fatal(err)
	}
}
