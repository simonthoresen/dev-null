package server

import (
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// renderQR returns a multi-line string containing an ASCII QR code for the
// given content, rendered using Unicode half-block characters. Each character
// cell encodes one QR module horizontally and two modules vertically (▀/▄/█/ ).
// This compensates for the ~2:1 height-to-width aspect ratio of terminal
// characters, so the rendered QR code appears square on screen.
func renderQR(content string) (string, error) {
	qr, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return "", err
	}
	bm := qr.Bitmap() // true = black module
	rows := len(bm)
	if rows == 0 {
		return "", nil
	}
	cols := len(bm[0])

	var sb strings.Builder
	for r := 0; r < rows; r += 2 {
		for c := 0; c < cols; c++ {
			top := bm[r][c]
			bot := r+1 < rows && bm[r+1][c]
			switch {
			case top && bot:
				sb.WriteRune('█')
			case top:
				sb.WriteRune('▀')
			case bot:
				sb.WriteRune('▄')
			default:
				sb.WriteRune(' ')
			}
		}
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}
