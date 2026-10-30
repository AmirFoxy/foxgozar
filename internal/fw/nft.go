// Package fw installs the transparent-proxy rules: one nftables table and
// one policy route. Everything is applied in a single `nft -f` transaction,
// so the network is never half-redirected.
package fw

import (
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"strings"
)

const (
	Table   = "zirgozar"
	FwMark  = 0x1
	RouteID = 100
)

type Config struct {
	TProxyPort    int
	DNSPort       int
	LANInterfaces []string
	ProxyRouter   bool
	IPv6          bool
	CoreMark      int      // packets the core itself sends
	DirectV4      []string // CIDRs that never enter the core (Iran, ...)
	DirectV6      []string
	ServerV4      []string // proxy server addresses, always direct
	ServerV6      []string
	ACLMode       string // all | only | except
	MACs          []string
	FakeIP        bool
}

var reservedV4 = []string{
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
	"172.16.0.0/12", "192.0.0.0/24", "192.168.0.0/16", "198.51.100.0/24",
	"203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
}

var reservedV6 = []string{
	"::/127", "::ffff:0:0/96", "64:ff9b::/96", "100::/64", "fc00::/7", "fe80::/10", "ff00::/8",
}

func set(name, typ string, elems []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\tset %s {\n\t\ttype %s\n\t\tflags interval\n\t\tauto-merge\n", name, typ)
	if len(elems) > 0 {
		b.WriteString("\t\telements = { ")
		b.WriteString(strings.Join(elems, ", "))
		b.WriteString(" }\n")
	}
	b.WriteString("\t}\n")
	return b.String()
}

func quoteList(xs []string) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = `"` + x + `"`
	}
	return "{ " + strings.Join(q, ", ") + " }"
}

// Script renders the whole table. Exported for tests and for `zgzd fw dump`.
func Script(c Config) string {
	var b strings.Builder
	// Deleting and re-adding in one transaction: atomic replace.
	fmt.Fprintf(&b, "table inet %s\ndelete table inet %s\n", Table, Table)
	fmt.Fprintf(&b, "table inet %s {\n", Table)
	b.WriteString(set("bypass4", "ipv4_addr", reservedV4))
	b.WriteString(set("direct4", "ipv4_addr", cleanCIDRs(append(append([]string{}, c.DirectV4...), c.ServerV4...), false)))
	if c.IPv6 {
		b.WriteString(set("bypass6", "ipv6_addr", reservedV6))
		b.WriteString(set("direct6", "ipv6_addr", cleanCIDRs(append(append([]string{}, c.DirectV6...), c.ServerV6...), true)))
	}
	if len(c.MACs) > 0 {
		fmt.Fprintf(&b, "\tset acl_mac {\n\t\ttype ether_addr\n\t\telements = { %s }\n\t}\n", strings.Join(c.MACs, ", "))
	}
	lan := quoteList(c.LANInterfaces)

	// Which packets are ours to look at, shared by prerouting and output.
	filter := func(b *strings.Builder) {
		fmt.Fprintf(b, "\t\tmeta mark & 0xff == 0x%x return\n", c.CoreMark)
		b.WriteString("\t\tfib daddr type { local, broadcast, multicast } return\n")
		b.WriteString("\t\tip daddr @bypass4 return\n")
		b.WriteString("\t\tip daddr @direct4 return\n")
		if c.IPv6 {
			b.WriteString("\t\tip6 daddr @bypass6 return\n")
			b.WriteString("\t\tip6 daddr @direct6 return\n")
		} else {
			b.WriteString("\t\tmeta nfproto ipv6 return\n")
		}
	}

	// LAN traffic: hand to the core's tproxy socket.
	b.WriteString("\tchain prerouting {\n\t\ttype filter hook prerouting priority mangle; policy accept;\n")
	// Router's own traffic re-enters here through lo carrying our mark.
	fmt.Fprintf(&b, "\t\tiifname \"lo\" meta mark & 0xff == 0x%x meta l4proto { tcp, udp } tproxy ip to 127.0.0.1:%d accept\n", FwMark, c.TProxyPort)
	if c.IPv6 {
		fmt.Fprintf(&b, "\t\tiifname \"lo\" meta mark & 0xff == 0x%x meta l4proto { tcp, udp } tproxy ip6 to [::1]:%d accept\n", FwMark, c.TProxyPort)
	}
	fmt.Fprintf(&b, "\t\tiifname != %s return\n", lan)
	switch {
	case c.ACLMode == "only" && len(c.MACs) > 0:
		b.WriteString("\t\tether saddr != @acl_mac return\n")
	case c.ACLMode == "except" && len(c.MACs) > 0:
		b.WriteString("\t\tether saddr @acl_mac return\n")
	}
	filter(&b)
	fmt.Fprintf(&b, "\t\tmeta l4proto { tcp, udp } meta mark set meta mark & 0xffffff00 | 0x%x tproxy ip to 127.0.0.1:%d accept\n", FwMark, c.TProxyPort)
	if c.IPv6 {
		fmt.Fprintf(&b, "\t\tmeta l4proto { tcp, udp } meta mark set meta mark & 0xffffff00 | 0x%x tproxy ip6 to [::1]:%d accept\n", FwMark, c.TProxyPort)
	}
	b.WriteString("\t}\n")

	// The router's own traffic: mark it so the policy route loops it back
	// through lo into prerouting above.
	if c.ProxyRouter {
		b.WriteString("\tchain output {\n\t\ttype route hook output priority mangle; policy accept;\n")
		filter(&b)
		fmt.Fprintf(&b, "\t\tmeta l4proto { tcp, udp } meta mark set meta mark & 0xffffff00 | 0x%x\n", FwMark)
		b.WriteString("\t}\n")
	}

	// DNS sent to the router goes to the core's DNS inbound. dnsmasq stays
	// as it was: nothing in /etc/config is touched, so a crash or a power cut
	// can never leave the router without DNS.
	b.WriteString("\tchain dstnat {\n\t\ttype nat hook prerouting priority dstnat - 5; policy accept;\n")
	fmt.Fprintf(&b, "\t\tiifname != %s return\n", lan)
	switch {
	case c.ACLMode == "only" && len(c.MACs) > 0:
		b.WriteString("\t\tether saddr != @acl_mac return\n")
	case c.ACLMode == "except" && len(c.MACs) > 0:
		b.WriteString("\t\tether saddr @acl_mac return\n")
	}
	fmt.Fprintf(&b, "\t\tmeta l4proto { tcp, udp } th dport 53 fib daddr type local redirect to :%d\n", c.DNSPort)
	b.WriteString("\t}\n")

	b.WriteString("}\n")
	return b.String()
}

// KillSwitchScript blocks LAN traffic to the internet while the tunnel is
// down. Only forwarding is blocked; the router itself stays reachable.
func KillSwitchScript(lan []string) string {
	return fmt.Sprintf(`table inet %[1]s_ks
delete table inet %[1]s_ks
table inet %[1]s_ks {
	chain forward {
		type filter hook forward priority filter - 5; policy accept;
		iifname %[2]s oifname != %[2]s reject with icmpx type admin-prohibited
	}
}
`, Table, quoteList(lan))
}

func cleanCIDRs(in []string, v6 bool) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			if ip := net.ParseIP(s); ip != nil {
				if ip.To4() != nil {
					s += "/32"
				} else {
					s += "/128"
				}
			}
		}
		ip, _, err := net.ParseCIDR(s)
		if err != nil || (ip.To4() == nil) != v6 {
			continue
		}
		out = append(out, s)
	}
	return out
}

func run(stdin string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var e bytes.Buffer
	cmd.Stderr = &e
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(e.String()))
	}
	return nil
}

// Up installs (or atomically replaces) the rules and the policy route.
func Up(c Config) error {
	if err := routes(true, c.IPv6); err != nil {
		return err
	}
	return run(Script(c), "nft", "-f", "-")
}

// Down removes everything Up added. Safe to call when nothing is there.
func Down() {
	_ = run("", "nft", "delete", "table", "inet", Table)
	_ = routes(false, true)
}

func KillSwitch(on bool, lan []string) error {
	if on {
		return run(KillSwitchScript(lan), "nft", "-f", "-")
	}
	_ = run("", "nft", "delete", "table", "inet", Table+"_ks")
	return nil
}

func routes(up, v6 bool) error {
	mark := fmt.Sprintf("0x%x/0xff", FwMark)
	table := fmt.Sprint(RouteID)
	fams := []string{"-4"}
	if v6 {
		fams = append(fams, "-6")
	}
	for _, f := range fams {
		// Delete first, so repeated Up calls do not stack rules.
		for run("", "ip", f, "rule", "del", "fwmark", mark, "table", table) == nil {
		}
		_ = run("", "ip", f, "route", "flush", "table", table)
		if !up {
			continue
		}
		if err := run("", "ip", f, "rule", "add", "fwmark", mark, "table", table, "pref", "100"); err != nil {
			return err
		}
		dst := "0.0.0.0/0"
		if f == "-6" {
			dst = "::/0"
		}
		if err := run("", "ip", f, "route", "add", "local", dst, "dev", "lo", "table", table); err != nil {
			return err
		}
	}
	return nil
}
