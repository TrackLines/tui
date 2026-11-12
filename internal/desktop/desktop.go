// Package desktop shows desktop notifications: on Linux through org.freedesktop.Notifications on the
// session bus (TL-886); elsewhere nothing, for now.
package desktop

// Notifier shows one notification. Never fails the caller: no bus or no daemon is no notification.
type Notifier interface {
	Notify(summary, body string, urgent bool)
}

// None shows nothing (other systems, tests).
type None struct{}

func (None) Notify(string, string, bool) {}
