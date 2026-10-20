package reporting

import (
	"os"
	"testing"
)

// reports = off: nothing is set up and nothing is sent; stderr is left alone.
func TestReportsOff(t *testing.T) {
	stderr := os.Stderr
	restore := Setup("v1.0.0", false)
	defer restore()
	if on {
		t.Fatal("reporting on with reports = off")
	}
	if os.Stderr != stderr {
		t.Fatal("stderr moved with reporting off")
	}
	Report("never sent: %s", "x") // no agent set up: a no-op
}
