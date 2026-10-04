// Package mailer renders the emails the API sends. Delivery is mocked for now: LogMailer renders
// the real template and logs the result, so swapping in an SMTP or provider client later only
// means adding another Mailer implementation
package mailer

import (
	"bytes"
	"context"
	"embed"
	htmltemplate "html/template"
	"log"
	texttemplate "text/template"
)

//go:embed templates/*
var templates embed.FS

var (
	resetHTML = htmltemplate.Must(htmltemplate.ParseFS(templates, "templates/password_reset.html"))
	resetText = texttemplate.Must(texttemplate.ParseFS(templates, "templates/password_reset.txt"))
)

const PasswordResetSubject = "Reset your Shotgun password"

// PasswordReset is the data the password reset email is rendered with
type PasswordReset struct {
	Name          string
	Email         string
	Link          string
	ExpiresInMins int
}

// Message is a rendered email, ready to hand to a delivery service
type Message struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

// Mailer sends the emails the API needs. Implementations must not block for long
type Mailer interface {
	SendPasswordReset(ctx context.Context, data PasswordReset) error
}

// RenderPasswordReset builds the password reset email from its templates
func RenderPasswordReset(data PasswordReset) (*Message, error) {
	var html, text bytes.Buffer
	if err := resetHTML.Execute(&html, data); err != nil {
		return nil, err
	}
	if err := resetText.Execute(&text, data); err != nil {
		return nil, err
	}
	return &Message{To: data.Email, Subject: PasswordResetSubject, HTML: html.String(), Text: text.String()}, nil
}

// LogMailer is the mock: it renders the email and logs it instead of sending it.
// In development the reset link in the log is how you reach the reset page
type LogMailer struct{}

func (LogMailer) SendPasswordReset(_ context.Context, data PasswordReset) error {
	msg, err := RenderPasswordReset(data)
	if err != nil {
		return err
	}
	log.Printf("mailer (mock, not sent): to=%s subject=%q link=%s", msg.To, msg.Subject, data.Link)
	return nil
}
