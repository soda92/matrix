package main

import (
	"math"
	"strconv"
)

const (
	colorLevels = 32
	// headStart is where the tail starts whitening into the bright head.
	headStart = 0.70
)

// palette turns a 0..1 intensity into an ANSI color sequence. Colors are
// rebuilt once per frame, so hue animation costs 33 small conversions.
type palette struct {
	levels    int
	codes     [][]byte
	arena     []byte
	truecolor bool
	table     []rgb // 256-color fallback table
}

type rgb struct{ r, g, b uint8 }

func newPalette(truecolor bool) *palette {
	p := &palette{
		levels:    colorLevels,
		truecolor: truecolor,
		codes:     make([][]byte, colorLevels+1),
		arena:     make([]byte, 0, (colorLevels+1)*22),
	}
	if truecolor {
		return p
	}
	p.table = make([]rgb, 256)
	for i := 0; i < 256; i++ {
		p.table[i] = xtermRGB(i)
	}
	return p
}

// update rebuilds the color table for the given hue.
func (p *palette) update(hue, sat float64) {
	p.arena = p.arena[:0]
	for i := 0; i <= p.levels; i++ {
		f := float64(i) / float64(p.levels)
		v := 0.05 + 0.95*math.Pow(f, 1.5)
		s := sat
		if f > headStart {
			k := (f - headStart) / (1 - headStart)
			s = sat * (1 - 0.95*k*k)
			v = math.Min(1, v+0.3*k*k)
		}
		r, g, b := hsv2rgb(hue, s, v)
		start := len(p.arena)
		p.arena = appendColor(p.arena, r, g, b, p.truecolor, p.table)
		p.codes[i] = p.arena[start:len(p.arena)]
	}
}

// levelFor quantizes intensity into 0 (blank) or 1..levels.
func levelFor(intensity float64, levels int) int {
	if intensity <= 0 {
		return 0
	}
	l := 1 + int(intensity*float64(levels-1))
	if l > levels {
		l = levels
	}
	return l
}

func appendColor(dst []byte, r, g, b uint8, truecolor bool, table []rgb) []byte {
	if truecolor {
		dst = append(dst, "\x1b[38;2;"...)
		dst = strconv.AppendUint(dst, uint64(r), 10)
		dst = append(dst, ';')
		dst = strconv.AppendUint(dst, uint64(g), 10)
		dst = append(dst, ';')
		dst = strconv.AppendUint(dst, uint64(b), 10)
		dst = append(dst, 'm')
		return dst
	}
	idx := nearest256(r, g, b, table)
	dst = append(dst, "\x1b[38;5;"...)
	dst = strconv.AppendUint(dst, uint64(idx), 10)
	dst = append(dst, 'm')
	return dst
}

// nearest256 finds the closest color in the 6x6x6 cube plus grayscale ramp.
// It weights green most heavily because the eye is most sensitive to it.
func nearest256(r, g, b uint8, table []rgb) int {
	best, bestDist := 16, 1<<30
	for i := 16; i < 256; i++ {
		c := table[i]
		dr := int(c.r) - int(r)
		dg := int(c.g) - int(g)
		db := int(c.b) - int(b)
		d := 2*dr*dr + 4*dg*dg + 3*db*db
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

// xtermRGB returns the RGB value of a 256-color palette entry.
func xtermRGB(i int) rgb {
	switch {
	case i < 16:
		basic := [16]rgb{
			{0, 0, 0}, {128, 0, 0}, {0, 128, 0}, {128, 128, 0},
			{0, 0, 128}, {128, 0, 128}, {0, 128, 128}, {192, 192, 192},
			{128, 128, 128}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
			{0, 0, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
		}
		return basic[i]
	case i < 232:
		i -= 16
		steps := [6]uint8{0, 95, 135, 175, 215, 255}
		return rgb{steps[i/36], steps[(i/6)%6], steps[i%6]}
	default:
		v := uint8(8 + (i-232)*10)
		return rgb{v, v, v}
	}
}

func hsv2rgb(h, s, v float64) (uint8, uint8, uint8) {
	h = math.Mod(h, 1)
	if h < 0 {
		h++
	}
	s = clamp01(s)
	v = clamp01(v)

	i := int(h * 6)
	f := h*6 - float64(i)
	p := v * (1 - s)
	q := v * (1 - f*s)
	t := v * (1 - (1-f)*s)

	var r, g, b float64
	switch i % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	default:
		r, g, b = v, p, q
	}
	return to8(r), to8(g), to8(b)
}

func to8(x float64) uint8 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 255
	}
	return uint8(x*255 + 0.5)
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
