package api

import "testing"

// Public addresses are moved between instances, so the audit trail has to name
// the addresses involved instead of just recording that something changed.
func TestDescribeAddressChange(t *testing.T) {
	cases := []struct {
		name   string
		before []string
		after  []string
		want   string
	}{
		{"swap one address for another", []string{"23.95.253.11"}, []string{"23.95.253.10"}, "23.95.253.11 -> 23.95.253.10"},
		{"first assignment", nil, []string{"23.95.253.10"}, "(none) -> 23.95.253.10"},
		{"release everything", []string{"23.95.253.11"}, nil, "23.95.253.11 -> (none)"},
		{"no change is still recorded", []string{"23.95.253.11"}, []string{"23.95.253.11"}, "23.95.253.11 -> 23.95.253.11"},
		{"several addresses", []string{"23.95.253.11", "23.95.253.12"}, []string{"23.95.253.10"}, "23.95.253.11,23.95.253.12 -> 23.95.253.10"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := describeAddressChange(tc.before, tc.after); got != tc.want {
				t.Fatalf("describeAddressChange(%v, %v) = %q, want %q", tc.before, tc.after, got, tc.want)
			}
		})
	}
}
