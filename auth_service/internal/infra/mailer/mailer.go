package mailer

import "fmt"

type Mailer interface {
	SendVerificationEmail(email, verifyURL string) error
}

type NoopMailer struct{}

func (NoopMailer) SendVerificationEmail(email, verifyURL string) error {
	_ = fmt.Sprintf("send verification email to %s via %s", email, verifyURL)
	return nil
}
