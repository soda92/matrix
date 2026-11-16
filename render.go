package main

import (
	"strconv"
	"unicode/utf8"
)

// Synchronized output: terminals that support it flip the whole frame at
// once, which removes tearing while the rain scrolls.
const (
	syncStart = "\x1b[?2026h"
	syncEnd   = "\x1b[?2026l"
)

// frameBuf renders one full screen into a single buffer so the frame can be
// written with one syscall.
type frameBuf struct {
	buf []byte
}

func newFrameBuf(w, h int) *frameBuf {
	// Worst case is a truecolor escape plus a rune for every cell.
	return &frameBuf{buf: make([]byte, 0, w*h*22+h*8+64)}
}

func (f *frameBuf) draw(m *matrix, pal *palette, bold bool) []byte {
	buf := f.buf[:0]
	buf = append(buf, syncStart...)
	if bold {
		buf = append(buf, "\x1b[1m"...)
	}
	for y := 0; y < m.h; y++ {
		buf = appendCursor(buf, y+1)
		row := y * m.w
		for x := 0; x < m.w; x++ {
			level := levelFor(m.intensity(x, y), pal.levels)
			if level == 0 {
				for i := 0; i < m.cellW; i++ {
					buf = append(buf, ' ')
				}
				continue
			}
			r := m.glyphs[row+x]
			buf = append(buf, pal.codes[level]...)
			buf = utf8.AppendRune(buf, r)
			// A narrow glyph in a wide cell needs a filler so the next
			// glyph lands in the right terminal column.
			if m.cellW == 2 && !isWideRune(r) {
				buf = append(buf, ' ')
			}
		}
		for i := 0; i < m.termW-m.w*m.cellW; i++ {
			buf = append(buf, ' ')
		}
	}
	buf = append(buf, "\x1b[0m"...)
	buf = append(buf, syncEnd...)
	f.buf = buf
	return buf
}

func appendCursor(buf []byte, row int) []byte {
	buf = append(buf, "\x1b["...)
	buf = strconv.AppendInt(buf, int64(row), 10)
	buf = append(buf, ";1H"...)
	return buf
}
