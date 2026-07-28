package pathlib

import "testing"

func TestUnifyLockpidSeparators(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "unchanged", text: "lockpid", want: "lockpid"},
		{name: "spaces", text: "  lock pid  ", want: "lock_pid"},
		{name: "tabs", text: "lock\tpid", want: "lock_pid"},
		{name: "forward slashes", text: "lock///pid", want: "lock_pid"},
		{name: "backslashes", text: `lock\\\pid`, want: "lock_pid"},
		{name: "underscores", text: "lock___pid", want: "lock_pid"},
		{name: "mixed separator run", text: "lock \t/_\\\\__ pid", want: "lock_pid"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := unify(test.text); got != test.want {
				t.Errorf("unify(%q) = %q, want %q", test.text, got, test.want)
			}
		})
	}
}
