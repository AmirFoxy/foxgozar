package qrsvg

import (
	"strings"
	"testing"
)

func TestLongVLESSFits(t *testing.T) {
	// The length of a VLESS link with an ML-KEM-768 key, about 1,700 characters.
	link := "vless://11111111-2222-3333-4444-555555555555@dl5.example.xyz:12082?alpn=h2%2Ch3&encryption=mlkem768x25519plus.native.0rtt." +
		strings.Repeat("ekPNZCqVq-MrMXaDMvKSwwmRHTeAK0ecGvGJjLmf75NATPNQT_Vq", 30) + "&fp=chrome&security=tls&type=tcp#Germany-Tunnel"
	svg, err := Encode(link)
	if err != nil {
		t.Fatalf("%d characters: %v", len(link), err)
	}
	if !strings.HasPrefix(svg, "<svg") {
		t.Fatal("not an SVG")
	}
	if _, err := Encode(strings.Repeat("x", 5000)); err == nil {
		t.Fatal("5000 characters cannot fit; want an error")
	}
}
