package alert

import "errors"

// EmailAlerter is a stub; SMTP support is not yet implemented.
type EmailAlerter struct{}

var _ Alerter = (*EmailAlerter)(nil)

func (e *EmailAlerter) Send(_ Alert) error {
	return errors.New("email alerts not yet implemented")
}
