package lxc

import "testing"

// The guard sweep deletes INPUT rules whose comment it does not recognise, so a
// parsing mistake silently removes the protection it just installed: the sweep
// once swallowed the whole remainder of the line ("clicd-pubguard-… -j DROP"),
// failed to match the keep set, and deleted every guard rule on the next pass.
func TestIptablesCommentOfExtractsOnlyTheCommentToken(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{
			line: "-A INPUT -d 23.95.253.13/32 -m comment --comment clicd-pubguard-23_95_253_13 -j DROP",
			want: "clicd-pubguard-23_95_253_13",
		},
		{
			line: `-A INPUT -d 23.95.253.10/32 -m comment --comment "clicd-pubguard-23_95_253_10" -j DROP`,
			want: "clicd-pubguard-23_95_253_10",
		},
		{
			line: "-A INPUT -j LIBVIRT_INP",
			want: "",
		},
		{
			line: "-A INPUT -d 23.95.253.12/32 -m comment --comment clicd-c3-23_95_253_12-all-tcp -j DNAT --to-destination 192.168.122.251",
			want: "clicd-c3-23_95_253_12-all-tcp",
		},
	}
	for _, tc := range cases {
		if got := iptablesCommentOf(tc.line); got != tc.want {
			t.Errorf("iptablesCommentOf(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

// A guard comment must round-trip through the keep set, otherwise the sweep
// treats a live guard as stale and deletes it.
func TestPublicIPv4GuardCommentIsStable(t *testing.T) {
	for _, address := range []string{"23.95.253.10", "23.95.253.12", "10.0.3.5"} {
		comment := publicIPv4GuardComment(address)
		if got := iptablesCommentOf("-A INPUT -d " + address + "/32 -m comment --comment " + comment + " -j DROP"); got != comment {
			t.Fatalf("guard comment for %s did not survive parsing: got %q, want %q", address, got, comment)
		}
		if !hasPublicIPv4GuardPrefix(comment) {
			t.Fatalf("guard comment %q is not recognised as a guard rule", comment)
		}
	}
}

func hasPublicIPv4GuardPrefix(comment string) bool {
	return len(comment) > len(publicIPv4GuardCommentPrefix) &&
		comment[:len(publicIPv4GuardCommentPrefix)] == publicIPv4GuardCommentPrefix
}
