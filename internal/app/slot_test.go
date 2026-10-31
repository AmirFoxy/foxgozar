package app

import "testing"

func TestPickISPDNS(t *testing.T) {
	one := "# Interface wan\nnameserver 192.168.100.1\nnameserver fe80::1\n"
	if got := pickISPDNS(one); got != "udp://192.168.100.1" {
		t.Errorf("one provider: %q", got)
	}
	// Starlink on wan, the domestic line on wan2: Iranian sites ask wan2.
	two := "# Interface wan\nnameserver 100.64.0.1\n# Interface wan2\nnameserver 2001:db8::1\nnameserver 10.0.0.1\n"
	if got := pickISPDNS(two); got != "udp://10.0.0.1" {
		t.Errorf("dual WAN: %q", got)
	}
	if got := pickISPDNS("# nothing\n"); got != "" {
		t.Errorf("none: %q", got)
	}
}
