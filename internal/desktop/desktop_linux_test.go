package desktop

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestUrgency(t *testing.T) {
	if urgency(true) != 2 || urgency(false) != 1 {
		t.Fatal("urgent is critical (2), the rest normal (1)")
	}
}

// As notify-send sends them: some daemons drop ones without sender-pid.
func TestHints(t *testing.T) {
	h := hints(true, 42)
	if h["urgency"] != dbus.MakeVariant(byte(2)) || h["sender-pid"] != dbus.MakeVariant(int64(42)) {
		t.Fatalf("hints: %v", h)
	}
}

// No session bus (CI, SSH): nothing shown, nothing fails.
func TestNoBus(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent")
	(&Session{}).Notify("TrackLin.es", "Sam assigned you TL-1", false)
}
