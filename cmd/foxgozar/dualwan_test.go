package main

import (
	"strings"
	"testing"
)

func TestDualWANScript(t *testing.T) {
	on, off := dualWANScript(true, "lan"), dualWANScript(false, "lan")
	for _, want := range []string{"uci -q del_list network.$dev.ports='lan'", "uci set network.wan2.device='lan'",
		"uci set network.wan2.ip4table='102'", "uci add_list firewall.$zone.network='wan2'", "set -e"} {
		if !strings.Contains(on, want) {
			t.Errorf("on lacks %q", want)
		}
	}
	// off undoes each step of on.
	for _, want := range []string{"uci -q delete network.wan2", "uci add_list network.$dev.ports='lan'", "uci -q del_list firewall.$zone.network='wan2'"} {
		if !strings.Contains(off, want) {
			t.Errorf("off lacks %q", want)
		}
	}
	if strings.Contains(off, "wan2.device") {
		t.Error("off must not create wan2")
	}
}
