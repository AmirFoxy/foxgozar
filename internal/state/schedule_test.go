package state

import (
	"testing"
	"time"
)

func TestScheduleActive(t *testing.T) {
	at := func(hhmm string) time.Time { // a UTC clock time
		x, _ := time.Parse("15:04", hhmm)
		return time.Date(2026, 10, 31, x.Hour(), x.Minute(), 0, 0, time.UTC)
	}
	night := Schedule{From: "22:00", To: "07:00"} // across midnight, UTC
	for hhmm, want := range map[string]bool{"21:59": false, "22:00": true, "03:00": true, "06:59": true, "07:00": false, "12:00": false} {
		if got := night.Active(at(hhmm)); got != want {
			t.Errorf("22-07 at %s: got %v", hhmm, got)
		}
	}
	day := Schedule{From: "08:00", To: "14:00"}
	for hhmm, want := range map[string]bool{"07:59": false, "08:00": true, "13:59": true, "14:00": false} {
		if got := day.Active(at(hhmm)); got != want {
			t.Errorf("08-14 at %s: got %v", hhmm, got)
		}
	}
	// Tehran is UTC+3:30: 23:00 there is 19:30 UTC.
	tehran := Schedule{From: "23:00", To: "06:00", TZ: 210}
	if !tehran.Active(at("19:30")) || tehran.Active(at("19:29")) || !tehran.Active(at("02:29")) || tehran.Active(at("02:30")) {
		t.Error("the user's offset must be applied")
	}
	for _, bad := range []Schedule{{From: "7:00", To: "7:00"}, {From: "25:00", To: "07:00"}, {From: "x", To: "07:00"}, {From: "", To: ""}} {
		if bad.Valid() || bad.Active(at("07:00")) {
			t.Errorf("%+v must be invalid and never active", bad)
		}
	}
	d := Device{Policy: "proxy", Schedule: &night}
	if !d.Blocked(at("23:00")) || d.Blocked(at("12:00")) {
		t.Error("a scheduled device is blocked inside its hours only")
	}
	if !(Device{Policy: "block"}).Blocked(at("12:00")) {
		t.Error("policy block is always blocked")
	}
}

func TestMigrateDirectDNS(t *testing.T) {
	s := State{Version: 1, Settings: Defaults()}
	s.Settings.DirectDNS = "udp://8.8.8.8"
	migrate(&s)
	if s.Settings.DirectDNS != "isp" || s.Version != CurrentVersion {
		t.Fatalf("old default must move to isp: %+v", s.Settings.DirectDNS)
	}
	s = State{Version: 1, Settings: Defaults()}
	s.Settings.DirectDNS = "tls://9.9.9.9"
	migrate(&s)
	if s.Settings.DirectDNS != "tls://9.9.9.9" {
		t.Fatal("a DNS the user chose stays")
	}
}

func TestMigrateShareAccount(t *testing.T) {
	s := State{Version: 2, Settings: Defaults(), Share: &Share{Enabled: true, UUID: "11111111-2222-3333-4444-555555555555"}}
	migrate(&s)
	if s.Share.UUID != "" || len(s.Share.Users) != 1 || s.Share.Users[0].UUID != "11111111-2222-3333-4444-555555555555" || !s.Share.Users[0].Enabled {
		t.Fatalf("the single account must become the first one: %+v", s.Share)
	}
	migrate(&s)
	if len(s.Share.Users) != 1 {
		t.Fatal("migrating twice must not add accounts")
	}
}
