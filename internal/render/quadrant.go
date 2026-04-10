package render

import (
	"image"
	"image/color"
)

// Unicode quadrant block characters indexed by a 4-bit mask:
//   bit 3 = upper-left, bit 2 = upper-right, bit 1 = lower-left, bit 0 = lower-right.
// Index 0 (all off) maps to space; index 15 (all on) maps to full block.
var quadrantRunes = [16]rune{
	' ',    // 0000
	'▗',   // 0001 — lower-right
	'▖',   // 0010 — lower-left
	'▄',   // 0011 — lower half
	'▝',   // 0100 — upper-right
	'▐',   // 0101 — right half
	'▞',   // 0110 — diagonal (UR+LL)
	'▟',   // 0111 — all except upper-left
	'▘',   // 1000 — upper-left
	'▚',   // 1001 — diagonal (UL+LR)
	'▌',   // 1010 — left half
	'▙',   // 1011 — all except upper-right
	'▀',   // 1100 — upper half
	'▛',   // 1101 — all except lower-right
	'▜',   // 1110 — all except lower-left
	'█',   // 1111 — full block
}

// luminance returns the perceived brightness of an RGBA color (0–255 scale).
func luminance(r, g, b uint8) uint8 {
	// Fast integer approximation of 0.299R + 0.587G + 0.114B.
	return uint8((19595*uint32(r) + 38470*uint32(g) + 7471*uint32(b) + 1<<15) >> 16)
}

// colorDist returns squared Euclidean distance between two RGB colors.
func colorDist(r1, g1, b1, r2, g2, b2 uint8) uint32 {
	dr := int32(r1) - int32(r2)
	dg := int32(g1) - int32(g2)
	db := int32(b1) - int32(b2)
	return uint32(dr*dr + dg*dg + db*db)
}

// ImageToQuadrants converts an RGBA image into quadrant block characters and
// writes them into buf at position (bx, by) spanning (w, h) cells.
//
// The image should be (w*2) x (h*N) pixels where N ≥ 2. Each cell maps to a
// 2×N pixel block; the block is split vertically in half and each half is
// box-averaged to produce the two quadrant rows. N=2 is lossless (1 pixel per
// quadrant sub-pixel); N=4 corrects for typical 1:2 terminal cell aspect ratio
// so that game pixels come out visually square.
//
// For each 2×2 (after vertical averaging) block, the 4 sub-pixel colors are
// partitioned into two groups (fg/bg) by finding the pair of colors that best
// separates the block. The quadrant character encodes which positions are
// "foreground".
func ImageToQuadrants(img *image.RGBA, buf *ImageBuffer, bx, by, w, h int) {
	imgW := img.Bounds().Dx()
	imgH := img.Bounds().Dy()
	minX := img.Bounds().Min.X
	minY := img.Bounds().Min.Y

	// Pixels per cell in each dimension. halfH rows are averaged per quadrant row.
	cellPxW := imgW / w
	cellPxH := imgH / h
	if cellPxW < 2 {
		cellPxW = 2
	}
	if cellPxH < 2 {
		cellPxH = 2
	}
	halfH := cellPxH / 2

	for cy := 0; cy < h; cy++ {
		for cx := 0; cx < w; cx++ {
			// Compute box-averaged RGBA for each of the 4 quadrant positions.
			// Layout: index 0=UL, 1=UR, 2=LL, 3=LR (matches original pixel order).
			var pr, pg, pb [4]uint8
			var pa [4]uint8

			for qi := 0; qi < 4; qi++ {
				qxOff := qi & 1  // 0 for UL/LL, 1 for UR/LR
				qyOff := qi >> 1 // 0 for UL/UR, 1 for LL/LR

				baseX := minX + cx*cellPxW + qxOff
				baseY := minY + cy*cellPxH + qyOff*halfH

				// Box-average halfH rows for this quadrant sub-pixel (X is always 1 pixel wide).
				var rSum, gSum, bSum, aSum, count uint32
				for dy := 0; dy < halfH; dy++ {
					px := baseX
					py := baseY + dy
					if px < minX+imgW && py < minY+imgH {
						off := (py-minY)*img.Stride + (px-minX)*4
						rSum += uint32(img.Pix[off+0])
						gSum += uint32(img.Pix[off+1])
						bSum += uint32(img.Pix[off+2])
						aSum += uint32(img.Pix[off+3])
						count++
					}
				}
				if count > 0 {
					pr[qi] = uint8(rSum / count)
					pg[qi] = uint8(gSum / count)
					pb[qi] = uint8(bSum / count)
					pa[qi] = uint8(aSum / count)
				}
			}

			// Check if all 4 sub-pixels are the same color.
			// Light cells render as space (bg = that color); dark cells as full block (fg = that color).
			// This makes stripped ANSI output match visual intent: white areas stay blank.
			allSame := pr[0] == pr[1] && pr[0] == pr[2] && pr[0] == pr[3] &&
				pg[0] == pg[1] && pg[0] == pg[2] && pg[0] == pg[3] &&
				pb[0] == pb[1] && pb[0] == pb[2] && pb[0] == pb[3]
			if allSame {
				c := color.RGBA{pr[0], pg[0], pb[0], pa[0]}
				if luminance(pr[0], pg[0], pb[0]) > 127 {
					buf.SetChar(bx+cx, by+cy, ' ', c, c, AttrNone)
				} else {
					buf.SetChar(bx+cx, by+cy, '█', c, c, AttrNone)
				}
				continue
			}

			// Partition into fg/bg using 2-means on the 4 colors.
			// With only 4 points, we try all 7 non-trivial 2-partitions
			// and pick the one minimizing total squared error.
			// Partition encoded as bitmask: bit set = foreground group.
			bestMask := uint8(0)
			bestErr := uint32(0xFFFFFFFF)

			for mask := uint8(1); mask < 15; mask++ {
				// Compute centroids for each group.
				var sr0, sg0, sb0, sr1, sg1, sb1 uint32
				var n0, n1 uint32
				for i := uint8(0); i < 4; i++ {
					if mask&(1<<(3-i)) != 0 {
						sr1 += uint32(pr[i])
						sg1 += uint32(pg[i])
						sb1 += uint32(pb[i])
						n1++
					} else {
						sr0 += uint32(pr[i])
						sg0 += uint32(pg[i])
						sb0 += uint32(pb[i])
						n0++
					}
				}
				// Compute total squared error for this partition.
				var cr0, cg0, cb0, cr1, cg1, cb1 uint8
				if n0 > 0 {
					cr0 = uint8(sr0 / n0)
					cg0 = uint8(sg0 / n0)
					cb0 = uint8(sb0 / n0)
				}
				if n1 > 0 {
					cr1 = uint8(sr1 / n1)
					cg1 = uint8(sg1 / n1)
					cb1 = uint8(sb1 / n1)
				}
				var totalErr uint32
				for i := uint8(0); i < 4; i++ {
					if mask&(1<<(3-i)) != 0 {
						totalErr += colorDist(pr[i], pg[i], pb[i], cr1, cg1, cb1)
					} else {
						totalErr += colorDist(pr[i], pg[i], pb[i], cr0, cg0, cb0)
					}
				}
				if totalErr < bestErr {
					bestErr = totalErr
					bestMask = mask
				}
			}

			// Compute final fg/bg colors from the best partition.
			var fgR, fgG, fgB, bgR, bgG, bgB uint32
			var fgN, bgN uint32
			var fgA, bgA uint32
			for i := uint8(0); i < 4; i++ {
				if bestMask&(1<<(3-i)) != 0 {
					fgR += uint32(pr[i])
					fgG += uint32(pg[i])
					fgB += uint32(pb[i])
					fgA += uint32(pa[i])
					fgN++
				} else {
					bgR += uint32(pr[i])
					bgG += uint32(pg[i])
					bgB += uint32(pb[i])
					bgA += uint32(pa[i])
					bgN++
				}
			}

			var fg, bg color.Color
			ch := quadrantRunes[bestMask]

			if fgN > 0 {
				fg = color.RGBA{uint8(fgR / fgN), uint8(fgG / fgN), uint8(fgB / fgN), uint8(fgA / fgN)}
			}
			if bgN > 0 {
				bg = color.RGBA{uint8(bgR / bgN), uint8(bgG / bgN), uint8(bgB / bgN), uint8(bgA / bgN)}
			}

			// Ensure fg is the darker group so the quadrant character's "filled"
			// positions correspond to dark pixels. This keeps the ANSI-stripped
			// golden output readable: edge chars like ▗ represent sparse dark marks
			// rather than their mostly-filled inverses.
			if fgN > 0 && bgN > 0 {
				fgLum := luminance(uint8(fgR/fgN), uint8(fgG/fgN), uint8(fgB/fgN))
				bgLum := luminance(uint8(bgR/bgN), uint8(bgG/bgN), uint8(bgB/bgN))
				if fgLum > bgLum {
					// fg is lighter — swap so fg becomes the darker group.
					fg, bg = bg, fg
					ch = quadrantRunes[bestMask^0x0F]
				}
			}

			buf.SetChar(bx+cx, by+cy, ch, fg, bg, AttrNone)
		}
	}
}
