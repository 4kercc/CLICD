package lxc

import (
	"reflect"
	"testing"
)

func TestCommentMatchesMarker(t *testing.T) {
	cases := []struct {
		name    string
		comment string
		marker  string
		want    bool
	}{
		{"container namespace prefix", "clicd-c3-snat-23_95_253_12", "clicd-c3-", true},
		{"container masquerade", "clicd-c3-masq", "clicd-c3-", true},
		{"container port mapping", "clicd-c3-any-22005", "clicd-c3-", true},
		{"other container", "clicd-c4-snat-23_95_253_10", "clicd-c3-", false},
		{"hairpin rule is never a container rule", "clicd-hairpin-192.168.122.0_24", "clicd-c3-", false},
		{"libvirt chain is not a clicd rule", "", "clicd-c3-", false},
		{"exact guard comment", "clicd-pubguard-23_95_253_12", "clicd-pubguard-23_95_253_12", true},
		{"shorter address must not claim a longer one", "clicd-pubguard-23_95_253_12", "clicd-pubguard-23_95_253_1", false},
		{"unrelated guard address", "clicd-pubguard-23_95_253_13", "clicd-pubguard-23_95_253_12", false},
		{"empty comment", "", "clicd-c3-", false},
		{"empty marker", "clicd-c3-masq", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := commentMatchesMarker(tc.comment, tc.marker); got != tc.want {
				t.Fatalf("commentMatchesMarker(%q, %q) = %v, want %v", tc.comment, tc.marker, got, tc.want)
			}
		})
	}
}

// The listing below is the shape of a real nat POSTROUTING chain: per-container
// rules plus the global hairpin rules that sit at the end of the chain. Before
// the fix the cleanup deleted by position, so a stale index could land on a
// hairpin rule; selecting by comment must never include one.
func TestTaggedRuleSpecsOnlySelectsItsOwnContainer(t *testing.T) {
	listing := []string{
		`-A POSTROUTING -s 192.168.122.156/32 -o eno1 -m comment --comment clicd-c25-snat-23_95_253_11 -j SNAT --to-source 23.95.253.11`,
		`-A POSTROUTING -s 192.168.122.84/32 -o eno1 -m comment --comment clicd-c4-snat-23_95_253_10 -j SNAT --to-source 23.95.253.10`,
		`-A POSTROUTING -s 192.168.122.251/32 -o eno1 -m comment --comment clicd-c3-snat-23_95_253_12 -j SNAT --to-source 23.95.253.12`,
		`-A POSTROUTING -s 10.0.3.102/32 -o eno1 -m comment --comment clicd-c7-masq -j MASQUERADE`,
		`-A POSTROUTING -s 10.0.3.0/24 -d 10.0.3.0/24 -m conntrack --ctstate DNAT -m comment --comment "clicd-hairpin-10.0.3.0_24" -j MASQUERADE`,
		`-A POSTROUTING -s 192.168.122.0/24 -d 192.168.122.0/24 -m conntrack --ctstate DNAT -m comment --comment "clicd-hairpin-192.168.122.0_24" -j MASQUERADE`,
		`-A POSTROUTING -j LIBVIRT_PRT`,
	}

	got := taggedRuleSpecs(listing, "clicd-c3-")
	want := []string{
		`-A POSTROUTING -s 192.168.122.251/32 -o eno1 -m comment --comment clicd-c3-snat-23_95_253_12 -j SNAT --to-source 23.95.253.12`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("taggedRuleSpecs selected %#v, want %#v", got, want)
	}

	for _, marker := range []string{"clicd-c1-", "clicd-c2-", "clicd-c7-", "clicd-c25-"} {
		for _, rule := range taggedRuleSpecs(listing, marker) {
			if containsHairpinComment(rule) {
				t.Fatalf("marker %q selected a hairpin rule: %s", marker, rule)
			}
		}
	}
}

func containsHairpinComment(rule string) bool {
	return commentMatchesMarker(iptablesCommentOf(rule), "clicd-hairpin-")
}

func TestSplitIPTablesRuleDropsChainName(t *testing.T) {
	got := splitIPTablesRule(`-A POSTROUTING -s 192.168.122.0/24 -d 192.168.122.0/24 -m conntrack --ctstate DNAT -m comment --comment "clicd-hairpin-192.168.122.0_24" -j MASQUERADE`)
	want := []string{
		"-s", "192.168.122.0/24",
		"-d", "192.168.122.0/24",
		"-m", "conntrack", "--ctstate", "DNAT",
		"-m", "comment", "--comment", "clicd-hairpin-192.168.122.0_24",
		"-j", "MASQUERADE",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitIPTablesRule = %#v, want %#v", got, want)
	}

	if got := splitIPTablesRule("-P POSTROUTING ACCEPT"); got != nil {
		t.Fatalf("policy lines must not be treated as rules, got %#v", got)
	}
}

func TestIPTablesRuleMissingRecognisesAlreadyDeleted(t *testing.T) {
	missing := []string{
		"iptables: Bad rule (does a matching rule exist in that chain?).",
		"iptables v1.8.9 (nf_tables):  RULE_DELETE failed (No such file or directory): rule in chain OUTPUT",
		"iptables: Index of deletion too big.",
	}
	for _, output := range missing {
		if !iptablesRuleMissing(output) {
			t.Fatalf("iptablesRuleMissing(%q) = false, want true", output)
		}
	}

	realFailures := []string{
		"iptables: Permission denied (you must be root).",
		"iptables v1.8.9 (nf_tables): CHAIN_DELETE failed (Device or resource busy)",
		"exec: \"iptables\": executable file not found in $PATH",
		"",
	}
	for _, output := range realFailures {
		if iptablesRuleMissing(output) {
			t.Fatalf("iptablesRuleMissing(%q) = true, want false", output)
		}
	}
}

func TestTokenizeIPTablesArgsHandlesQuotesAndEscapes(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		{`-m comment --comment clicd-c3-masq -j MASQUERADE`, []string{"-m", "comment", "--comment", "clicd-c3-masq", "-j", "MASQUERADE"}},
		{`--comment "a comment with spaces" -j DROP`, []string{"--comment", "a comment with spaces", "-j", "DROP"}},
		{`--comment escaped\ space -j DROP`, []string{"--comment", "escaped space", "-j", "DROP"}},
		{`   spaced   out   `, []string{"spaced", "out"}},
		{``, []string{}},
	}
	for _, tc := range cases {
		got := tokenizeIPTablesArgs(tc.line)
		if len(got) == 0 && len(tc.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("tokenizeIPTablesArgs(%q) = %#v, want %#v", tc.line, got, tc.want)
		}
	}
}
