package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A ticket that needs a flags.gg flag shows it (TL-793): name, where it's on, off or not there yet, and
// what flags.gg refused; tickets without one show nothing.
func TestFlagLine(t *testing.T) {
	name, refused := "tl-7", "a flag with that name already exists"
	if flagLine(&Ticket{}, nil) != "" {
		t.Fatal("no flag needed: no line")
	}
	waiting := ansi.Strip(flagLine(&Ticket{NeedsFlag: true, FlagName: &name}, nil))
	if !strings.Contains(waiting, "Flag tl-7") || !strings.Contains(waiting, "made when it goes onto a board") {
		t.Fatalf("not made yet: %q", waiting)
	}
	var f TicketFlag
	_ = json.Unmarshal([]byte(`{"name":"tl-7","environments":[{"name":"dev","present":true,"enabled":true},
		{"name":"staging","present":true},{"name":"prod"}]}`), &f)
	got := ansi.Strip(flagLine(&Ticket{NeedsFlag: true, FlagName: &name, FlagError: &refused}, &f))
	for _, want := range []string{"dev on", "staging off", "prod not yet", "flags.gg: " + refused} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if ansi.Strip(flagMark(&Ticket{NeedsFlag: true})) != "⚑" {
		t.Fatal("marker")
	}
}
