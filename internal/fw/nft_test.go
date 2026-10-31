package fw

import (
	"strings"
	"testing"
)

func TestKillSwitchSparesDirectDevices(t *testing.T) {
	lan := []string{"br-lan"}
	s := KillSwitchScript(Config{LANInterfaces: lan, DirectMACs: []string{"aa:bb:cc:dd:ee:ff"}})
	if !strings.Contains(s, "ether saddr { aa:bb:cc:dd:ee:ff } return") || !strings.Contains(s, "reject") {
		t.Fatalf("direct device must be exempt, others blocked:\n%s", s)
	}
	// Default direct: only tunnel devices are held back.
	s = KillSwitchScript(Config{LANInterfaces: lan, DefaultDirect: true, TunnelMACs: []string{"11:22:33:44:55:66"}})
	if !strings.Contains(s, "ether saddr != { 11:22:33:44:55:66 } return") {
		t.Fatalf("only tunnel devices should be blocked:\n%s", s)
	}
	// Default direct and no tunnel devices: nothing to block.
	s = KillSwitchScript(Config{LANInterfaces: lan, DefaultDirect: true})
	if strings.Contains(s, "reject") {
		t.Fatalf("nothing should be blocked:\n%s", s)
	}
}

func TestIPv6OffRejectsLANIPv6(t *testing.T) {
	c := Config{TProxyPort: 7893, DNSPort: 1053, LANInterfaces: []string{"br-lan"}, CoreMark: 0xff,
		DirectMACs: []string{"aa:bb:cc:dd:ee:ff"}}
	s := Script(c)
	fwd := strings.Index(s, "chain forward")
	if fwd < 0 || !strings.Contains(s[fwd:], "reject with icmpx type admin-prohibited") {
		t.Fatalf("IPv6 off must reject forwarded IPv6:\n%s", s)
	}
	// Direct devices are not the tunnel's concern: they keep IPv6.
	if r, d := strings.Index(s[fwd:], "reject"), strings.Index(s[fwd:], "@direct_mac return"); d < 0 || d > r {
		t.Fatalf("direct devices must be exempt before the reject:\n%s", s)
	}
	c.IPv6 = true
	if strings.Contains(Script(c), "chain forward") {
		t.Fatal("IPv6 on: IPv6 goes through the core, nothing to reject")
	}
}

func TestBlockTable(t *testing.T) {
	c := Config{LANInterfaces: []string{"br-lan"}, BlockMACs: []string{"aa:bb:cc:dd:ee:ff"}, MixedPort: 7890}
	s := BlockScript(c)
	for _, want := range []string{
		"priority mangle - 10", // ahead of the tunnel's prerouting chain
		"ether saddr @mac fib daddr type != { local, broadcast, multicast } drop",
		"ether saddr @mac tcp dport 7890 reject",
		"ether saddr @mac udp dport 7890 reject",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q:\n%s", want, s)
		}
	}
	// Every family alike: IPv6 is no way around it.
	if strings.Contains(s, "nfproto") {
		t.Fatal("the block must not depend on the address family")
	}
	c.MixedPort = 0
	if strings.Contains(BlockScript(c), "chain input") {
		t.Fatal("no mixed port, no input chain")
	}
	c.BlockMACs = nil
	if s := BlockScript(c); strings.Contains(s, "chain") || !strings.Contains(s, "delete table inet foxgozar_block") {
		t.Fatalf("nobody blocked: the table only goes away:\n%s", s)
	}
	if strings.Contains(Script(Config{TProxyPort: 1, DNSPort: 2, LANInterfaces: []string{"br-lan"}, BlockMACs: []string{"aa:bb:cc:dd:ee:ff"}}), "aa:bb:cc:dd:ee:ff") {
		t.Fatal("blocked MACs belong in the block table only")
	}
}
