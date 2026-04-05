package server

import (
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// quadrantChars maps a 4-bit mask (UL=bit3, UR=bit2, LL=bit1, LR=bit0) to the
// corresponding Unicode quadrant block character. true = black module.
var quadrantChars = []rune{
	' ',  // 0000
	'▗',  // 0001  lower-right
	'▖',  // 0010  lower-left
	'▄',  // 0011  lower half
	'▝',  // 0100  upper-right
	'▐',  // 0101  right half
	'▞',  // 0110  upper-right + lower-left
	'▟',  // 0111  upper-right + lower half
	'▘',  // 1000  upper-left
	'▚',  // 1001  upper-left + lower-right
	'▌',  // 1010  left half
	'▙',  // 1011  upper-left + lower half... wait
	'▀',  // 1100  upper half
	'▜',  // 1101  upper half + lower-right
	'▛',  // 1110  upper half + lower-left
	'█',  // 1111  full block
}

// renderQR returns a multi-line string containing an ASCII QR code for the
// given content, rendered using Unicode quadrant block characters so that two
// QR modules share each character cell (halving both width and height).
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
		for c := 0; c < cols; c += 2 {
			ul := r < rows && c < cols && bm[r][c]
			ur := r < rows && c+1 < cols && bm[r][c+1]
			ll := r+1 < rows && c < cols && bm[r+1][c]
			lr := r+1 < rows && c+1 < cols && bm[r+1][c+1]

			idx := 0
			if ul {
				idx |= 8
			}
			if ur {
				idx |= 4
			}
			if ll {
				idx |= 2
			}
			if lr {
				idx |= 1
			}
			sb.WriteRune(quadrantChars[idx])
		}
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}
