// Package qrsvg draws a QR code as a small SVG, so share links never have
// to leave the router to be shown as a code.
package qrsvg

import (
	"fmt"
	"strings"

	"rsc.io/qr"
)

// Encode returns an SVG of text. Short links get more error correction;
// long ones (VLESS with ML-KEM keys run past 1,500 characters) need the
// lowest level to fit at all.
func Encode(text string) (string, error) {
	level := qr.M
	if len(text) > 1200 {
		level = qr.L
	}
	c, err := qr.Encode(text, level)
	if err != nil {
		return "", fmt.Errorf("too long for a QR code (%d characters)", len(text))
	}
	const quiet = 4 // the blank border scanners need
	n := c.Size + 2*quiet
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`, n, n)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, n, n)
	for y := 0; y < c.Size; y++ {
		for x := 0; x < c.Size; x++ {
			if c.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String(), nil
}
