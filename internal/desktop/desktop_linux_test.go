package desktop

import "testing"

func TestUrgency(t *testing.T) {
	if urgency(true) != 2 || urgency(false) != 1 {
		t.Fatal("urgent is critical (2), the rest normal (1)")
	}
}

// No session bus (CI, SSH): nothing shown, nothing fails.
func TestNoBus(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent")
	(&Session{}).Notify("TrackLin.es", "Sam assigned you TL-1", false)
}
