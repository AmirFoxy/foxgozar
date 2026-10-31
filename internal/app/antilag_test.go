package app

import (
	"strings"
	"testing"
)

func TestSQMScript(t *testing.T) {
	on := sqmScript(true, "wan", 50, 10)
	for _, want := range []string{
		"uci set sqm.foxgozar=queue", "sqm.foxgozar.interface='wan'",
		"sqm.foxgozar.download='45000'", "sqm.foxgozar.upload='9000'", // 90% of the line, in kbit/s
		"sqm.foxgozar.qdisc='cake'", "sqm.foxgozar.script='piece_of_cake.qos'", "uci commit sqm",
	} {
		if !strings.Contains(on, want) {
			t.Errorf("on: missing %q", want)
		}
	}
	off := sqmScript(false, "", 0, 0)
	if !strings.Contains(off, "sqm.foxgozar.enabled='0'") || strings.Contains(off, "delete") {
		t.Fatalf("off must only disable FoxGozar's own section:\n%s", off)
	}
}
