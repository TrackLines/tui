package desktop

import (
	"sync"

	"github.com/godbus/dbus/v5"
)

// Session is the desktop's notification daemon, over the D-Bus session bus.
type Session struct {
	once sync.Once
	conn *dbus.Conn // nil: no session bus (a server, a container, SSH without one)
}

// New is the system's notifier.
func New() Notifier { return &Session{} }

// urgency is the freedesktop hint: 1 normal, 2 critical (stays until dismissed on most desktops).
func urgency(urgent bool) byte {
	if urgent {
		return 2
	}
	return 1
}

func (s *Session) Notify(summary, body string, urgent bool) {
	s.once.Do(func() { s.conn, _ = dbus.ConnectSessionBus() })
	if s.conn == nil {
		return
	}
	// Notify(app_name, replaces_id, app_icon, summary, body, actions, hints, expire_timeout)
	s.conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").Call(
		"org.freedesktop.Notifications.Notify", 0, "TrackLin.es", uint32(0), "", summary, body, []string{},
		map[string]dbus.Variant{"urgency": dbus.MakeVariant(urgency(urgent))}, int32(-1))
}
