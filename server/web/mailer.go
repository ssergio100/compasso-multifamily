package web

import (
	"context"
	"errors"
)

type AccountMessage struct {
	To      string
	Subject string
	Text    string
}

type AccountMailer interface {
	Available() bool
	SendAccountMessage(context.Context, AccountMessage) error
}

type Option func(*App) error

func WithAccountMailer(mailer AccountMailer) Option {
	return func(app *App) error {
		if mailer == nil {
			return errors.New("account mailer is required")
		}
		app.accountMailer = mailer
		return nil
	}
}

type unavailableAccountMailer struct{}

func (unavailableAccountMailer) Available() bool { return false }

func (unavailableAccountMailer) SendAccountMessage(context.Context, AccountMessage) error {
	return errors.New("account e-mail delivery is not configured")
}
