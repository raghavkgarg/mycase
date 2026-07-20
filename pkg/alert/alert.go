package alert

// Alert is a single notification payload.
type Alert struct {
	Title string
	Body  string
	Level string // "info", "warn", "critical"
}

// Alerter sends alert notifications over a channel.
type Alerter interface {
	Send(a Alert) error
}
