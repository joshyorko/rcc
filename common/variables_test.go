package common

import "testing"

func TestResolveSharedHolotreeMode(t *testing.T) {
	tests := []struct {
		marker bool
		mode   string
		want   bool
	}{
		{marker: false, mode: "", want: false},
		{marker: true, mode: "", want: true},
		{marker: true, mode: "private", want: false},
		{marker: false, mode: "shared", want: true},
		{marker: true, mode: "unexpected", want: true},
	}
	for _, test := range tests {
		if got := resolveSharedHolotreeMode(test.marker, test.mode); got != test.want {
			t.Fatalf("marker=%v mode=%q: got %v, want %v", test.marker, test.mode, got, test.want)
		}
	}
}
