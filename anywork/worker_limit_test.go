package anywork

import "testing"

func TestWorkerLimitAppliesMinimumDefaultAndMaximum(t *testing.T) {
	for _, test := range []struct {
		name                   string
		cpus, configured, want int
	}{
		{name: "minimum", cpus: 1, want: 2},
		{name: "default", cpus: 8, want: 7},
		{name: "configured minimum", cpus: 8, configured: 2, want: 2},
		{name: "configured maximum", cpus: 8, configured: 97, want: 96},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := workerLimit(test.cpus, test.configured); got != uint64(test.want) {
				t.Fatalf("workerLimit(%d, %d) = %d, want %d", test.cpus, test.configured, got, test.want)
			}
		})
	}
}
