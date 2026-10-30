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

func TestBlockedDevicesCannotLeaveViaIPv6(t *testing.T) {
	s := Script(Config{TProxyPort: 7893, DNSPort: 1053, LANInterfaces: []string{"br-lan"}, CoreMark: 0xff,
		BlockMACs: []string{"aa:bb:cc:dd:ee:ff"}})
	block := strings.Index(s, "@block_mac")
	v6 := strings.Index(s, "meta nfproto ipv6 return")
	if block < 0 || v6 < 0 || block > v6 {
		t.Fatal("the block rule must come before the IPv6 return")
	}
}
