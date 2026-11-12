package desktop

import (
	"os"
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

// hints are the urgency and who sent it, as notify-send gives them.
func hints(urgent bool, pid int) map[string]dbus.Variant {
	return map[string]dbus.Variant{"urgency": dbus.MakeVariant(urgency(urgent)), "sender-pid": dbus.MakeVariant(int64(pid))}
}

func (s *Session) Notify(summary, body string, urgent bool) {
	s.once.Do(func() { s.conn, _ = dbus.ConnectSessionBus() })
	if s.conn == nil {
		return
	}
	// Notify(app_name, replaces_id, app_icon, summary, body, actions, hints, expire_timeout), sent as
	// notify-send sends it: some daemons (quickshell here) didn't show one named "TrackLin.es" without sender-pid
	s.conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").Call(
		"org.freedesktop.Notifications.Notify", 0, "tracklines", uint32(0), "", summary, body, []string{},
		hints(urgent, os.Getpid()), int32(-1))
}
