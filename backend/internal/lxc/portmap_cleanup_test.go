package lxc

import (
	"reflect"
	"testing"

	"clicd/internal/config"
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

func TestContainerRuleSetMatchesTheRulesApplyWrites(t *testing.T) {
	c := &config.Container{
		ID:          25,
		IP:          "192.168.122.156",
		PublicIPv4s: []config.PublicIPv4Assignment{{Address: "23.95.253.10"}},
		PortMappings: []config.PortMapping{
			// No HostIP: expanded to the assigned address, mirrored on OUTPUT.
			{ContainerPort: 22, HostPort: 22003, Protocol: "tcp"},
			// Explicit HostIP: also mirrored on OUTPUT.
			{ContainerPort: 80, HostPort: 20004, Protocol: "tcp", HostIP: "23.95.253.10"},
		},
	}
	got := containerRuleSet(c)

	wantPre := []string{
		"clicd-c25-23_95_253_10-all-tcp",
		"clicd-c25-23_95_253_10-all-udp",
		"clicd-c25-23_95_253_10-all-icmp",
		"clicd-c25-23_95_253_10-22003",
		"clicd-c25-23_95_253_10-20004",
	}
	wantOut := []string{
		"clicd-c25-23_95_253_10-out-tcp",
		"clicd-c25-23_95_253_10-out-udp",
		"clicd-c25-23_95_253_10-out-icmp",
		"clicd-c25-23_95_253_10-22003-out",
		"clicd-c25-23_95_253_10-20004-out",
	}
	wantPost := []string{"clicd-c25-snat-23_95_253_10"}

	if !sameCommentSet(got.prerouting, wantPre) {
		t.Fatalf("prerouting = %#v, want %#v", got.prerouting, wantPre)
	}
	if !sameCommentSet(got.output, wantOut) {
		t.Fatalf("output = %#v, want %#v", got.output, wantOut)
	}
	if !sameCommentSet(got.postrouting, wantPost) {
		t.Fatalf("postrouting = %#v, want %#v", got.postrouting, wantPost)
	}
}

// A container with no public address falls back to masquerading, and one that
// may not egress at all expects no postrouting rule of its own.
func TestContainerRuleSetEgressFallback(t *testing.T) {
	mapped := &config.Container{
		ID:           7,
		IP:           "10.0.3.102",
		PortMappings: []config.PortMapping{{ContainerPort: 22, HostPort: 22005, Protocol: "tcp"}},
	}
	if got := containerRuleSet(mapped).postrouting; !sameCommentSet(got, []string{"clicd-c7-masq"}) {
		t.Fatalf("mapped container postrouting = %#v, want [clicd-c7-masq]", got)
	}

	sealed := &config.Container{ID: 9, IP: "10.0.3.103"}
	if got := containerRuleSet(sealed).postrouting; len(got) != 0 {
		t.Fatalf("sealed container postrouting = %#v, want none", got)
	}
}

// A mapping without a host address is not reflected on OUTPUT, so the expected
// set must not claim it is.
func TestContainerRuleSetSkipsUnreflectedMapping(t *testing.T) {
	c := &config.Container{
		ID:           8,
		IP:           "10.0.3.101",
		PortMappings: []config.PortMapping{{ContainerPort: 22, HostPort: 22004, Protocol: "tcp"}},
	}
	got := containerRuleSet(c)
	if !sameCommentSet(got.prerouting, []string{"clicd-c8-any-22004"}) {
		t.Fatalf("prerouting = %#v, want [clicd-c8-any-22004]", got.prerouting)
	}
	if len(got.output) != 0 {
		t.Fatalf("output = %#v, want none (a mapping without a host address is not reflected)", got.output)
	}
}

func TestSameCommentSetCountsDuplicates(t *testing.T) {
	if !sameCommentSet([]string{"a", "b"}, []string{"b", "a"}) {
		t.Fatal("order must not matter")
	}
	if sameCommentSet([]string{"a", "a"}, []string{"a"}) {
		t.Fatal("a duplicated rule must not compare equal to a single one")
	}
	if sameCommentSet([]string{"a"}, []string{"a", "b"}) {
		t.Fatal("a missing rule must not compare equal")
	}
	if sameCommentSet(nil, nil) != true {
		t.Fatal("two empty sets are equal")
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
