//go:build !linux

package desktop

// New is the system's notifier: nothing yet off Linux.
// ponytail: macOS (osascript) and Windows (toast) when someone asks
func New() Notifier { return None{} }
