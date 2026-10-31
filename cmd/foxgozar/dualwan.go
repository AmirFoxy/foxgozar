package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// `foxgozar dualwan on|off|status`: two ways out instead of one LAN port.
//
// The router's second port (normally LAN) becomes interface wan2 for a
// domestic line, while wan keeps the default route (Starlink, say). Every
// Iranian address goes over wan2 (the service keeps those routes, see
// app.syncIranRoutes), and the sharing server lets people on the domestic
// network use this router's way out. Wi-Fi stays the LAN.
//
// Only /etc/config/network and /etc/config/firewall are touched, and `off`
// undoes exactly what `on` did.

const findBridge = `dev=$(uci show network | sed -n "s/^network\.\([^.]*\)\.name='br-lan'$/\1/p" | head -n1)
[ -n "$dev" ] || { echo "no br-lan bridge in /etc/config/network" >&2; exit 1; }
zone=$(uci show firewall | sed -n "s/^firewall\.\([^.]*\)\.name='wan'$/\1/p" | head -n1)
[ -n "$zone" ] || { echo "no wan firewall zone in /etc/config/firewall" >&2; exit 1; }
`

// dualWANScript is the shell script for on or off. port is the device name
// of the port that changes role ("lan" on most two-port routers).
func dualWANScript(on bool, port string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString(findBridge)
	if on {
		fmt.Fprintf(&b, `uci -q del_list network.$dev.ports='%[1]s' || true
uci set network.wan2=interface
uci set network.wan2.device='%[1]s'
uci set network.wan2.proto='dhcp'
uci set network.wan2.ip4table='102'
uci set network.wan2.metric='20'
uci -q del_list firewall.$zone.network='wan2' || true
uci add_list firewall.$zone.network='wan2'
`, port)
	} else {
		fmt.Fprintf(&b, `uci -q delete network.wan2 || true
uci -q del_list network.$dev.ports='%[1]s' || true
uci add_list network.$dev.ports='%[1]s'
uci -q del_list firewall.$zone.network='wan2' || true
`, port)
	}
	b.WriteString("uci commit network\nuci commit firewall\n/etc/init.d/network reload\n/etc/init.d/firewall reload >/dev/null 2>&1\n")
	return b.String()
}

func dualWAN(args []string) error {
	sub, port, yes, dry := "status", "lan", false, false
	for _, a := range args {
		switch {
		case a == "--yes":
			yes = true
		case a == "--dry-run":
			dry = true
		case strings.HasPrefix(a, "--port="):
			port = strings.TrimPrefix(a, "--port=")
		case a == "on" || a == "off" || a == "status":
			sub = a
		default:
			return fmt.Errorf("usage: foxgozar dualwan on|off|status [--port=lan] [--dry-run] [--yes]")
		}
	}
	if sub == "status" {
		return dualWANStatus()
	}
	script := dualWANScript(sub == "on", port)
	if dry {
		fmt.Print(script)
		return nil
	}
	if !yes {
		if sub == "on" {
			fmt.Printf(`This turns port %q into a second internet port (wan2).
Anything plugged into it loses its LAN connection; use Wi-Fi for the router
page meanwhile. Undo with: foxgozar dualwan off --yes
Run again with --yes to do it, or --dry-run to see the commands.
`, port)
		} else {
			fmt.Printf("This makes port %q a LAN port again. Run again with --yes.\n", port)
		}
		return nil
	}
	cmd := exec.Command("sh", "-c", script)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	if sub == "on" {
		fmt.Println("Done. Plug the domestic line into that port; within a minute Iranian addresses go over it.")
	} else {
		fmt.Println("Done. The port is a LAN port again.")
	}
	return nil
}

func dualWANStatus() error {
	out, _ := exec.Command("uci", "-q", "get", "network.wan2").Output()
	if strings.TrimSpace(string(out)) != "interface" {
		fmt.Println("dual WAN: off")
		return nil
	}
	fmt.Println("dual WAN: on")
	for _, ifc := range []string{"wan", "wan2"} {
		o, _ := exec.Command("sh", "-c", "ifstatus "+ifc+" | jsonfilter -e '@.up' -e '@[\"ipv4-address\"][0].address'").Output()
		fmt.Printf("  %-5s %s\n", ifc, strings.Join(strings.Fields(string(o)), "  "))
	}
	o, _ := exec.Command("sh", "-c", "ip -4 route show table 103 | wc -l").Output()
	fmt.Printf("  Iranian ranges routed over wan2: %s\n", strings.TrimSpace(string(o)))
	return nil
}
