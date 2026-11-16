package main

import (
	"fmt"
	"strings"
	"testing"
	"unicode"
)

func TestLangAliases(t *testing.T) {
	aliases := map[string]string{
		"default":     "classic",
		"kana":        "classic",
		"en":          "latin",
		"ascii":       "latin",
		"ja":          "jp",
		"japanese":    "jp",
		"zh":          "cn",
		"zh-cn":       "cn",
		"zh-hans":     "cn",
		"chinese":     "cn",
		"simplified":  "cn",
		"zh-tw":       "tw",
		"zh-hant":     "tw",
		"traditional": "tw",
		"ko":          "kr",
		"korean":      "kr",
		"hangul":      "kr",
	}
	for alias, want := range aliases {
		def, ok := lookupLang(alias)
		if !ok {
			t.Fatalf("alias %q is not registered", alias)
		}
		if def.name != want {
			t.Fatalf("alias %q resolves to %q, want %q", alias, def.name, want)
		}
	}

	for _, name := range langOrder {
		def, ok := lookupLang(name)
		if !ok {
			t.Fatalf("language %q is not registered", name)
		}
		if len(def.chars) < 50 {
			t.Fatalf("language %q only has %d glyphs", name, len(def.chars))
		}
	}
}

func TestLangWidths(t *testing.T) {
	for _, name := range langOrder {
		def := langs[name]
		seen := make(map[rune]bool, len(def.chars))
		for _, r := range def.chars {
			if r == 0 || unicode.IsSpace(r) || !unicode.IsPrint(r) {
				t.Fatalf("%s contains an unusable rune %U", name, r)
			}
			if seen[r] {
				t.Fatalf("%s contains duplicate rune %q", name, r)
			}
			seen[r] = true
			if isWideRune(r) != def.wide {
				t.Fatalf("%s: %q (%U) has width mismatch", name, r, r)
			}
		}
	}
}

func TestWideRuneClassification(t *testing.T) {
	wide := []rune{'漢', '字', 'あ', 'ア', '한', '々', 'ー', 'Ａ', '｜'}
	narrow := []rune{'a', 'Z', '0', 'ﾊ', 'ﾐ', 'ｰ', 'ç', '¦', '-'}
	for _, r := range wide {
		if !isWideRune(r) {
			t.Errorf("%q should be wide", r)
		}
	}
	for _, r := range narrow {
		if isWideRune(r) {
			t.Errorf("%q should be narrow", r)
		}
	}
}

func TestWideRenderingFillsTerminalWidth(t *testing.T) {
	for _, termW := range []int{40, 41, 1, 2} {
		cfg := testConfig()
		cfg.wide = true
		cfg.chars = splitRunes("漢字日本語한국어ab")
		m := newMatrix(cfg, termW, 8, 9)
		pal := newPalette(true)
		pal.update(0.33, 1)
		frame := newFrameBuf(termW, 8)

		for i := 0; i < 40; i++ {
			m.update(0.05)
			out := frame.draw(m, pal, false)
			rows := parseFrameRows(t, out, m.h)
			for y, row := range rows {
				want := termW
				if m.w*m.cellW > termW {
					// One-column terminal cannot fit a wide glyph.
					want = m.w * m.cellW
				}
				if got := displayWidth(row); got != want {
					t.Fatalf("termW=%d frame %d row %d: width %d, want %d", termW, i, y, got, want)
				}
			}
		}
	}
}

func TestSetCharsChangesCellWidth(t *testing.T) {
	m := newMatrix(testConfig(), 40, 10, 11)
	m.setChars(splitRunes("漢字日本語"), 2)
	if m.cellW != 2 || m.w != 20 || len(m.glyphs) != 20*10 {
		t.Fatalf("wide grid: cellW=%d w=%d glyphs=%d", m.cellW, m.w, len(m.glyphs))
	}
	m.setChars([]rune("abc"), 1)
	if m.cellW != 1 || m.w != 40 || len(m.glyphs) != 40*10 {
		t.Fatalf("narrow grid: cellW=%d w=%d glyphs=%d", m.cellW, m.w, len(m.glyphs))
	}
}

// parseFrameRows returns the visible text of each row, with escape sequences
// removed, in row order.
func parseFrameRows(t *testing.T, out []byte, h int) [][]byte {
	t.Helper()
	rows := make([][]byte, h)
	row := -1
	for i := 0; i < len(out); {
		if out[i] != 0x1b || i+1 >= len(out) || out[i+1] != '[' {
			if row >= 0 {
				rows[row] = append(rows[row], out[i])
			}
			i++
			continue
		}
		j := i + 2
		for j < len(out) && (out[j] < 0x40 || out[j] > 0x7e) {
			j++
		}
		if j >= len(out) {
			break
		}
		seq := string(out[i : j+1])
		var n int
		if _, err := fmt.Sscanf(seq, "\x1b[%d;1H", &n); err == nil {
			if n < 1 || n > h {
				t.Fatalf("cursor moved to row %d of %d", n, h)
			}
			row = n - 1
		}
		i = j + 1
	}
	return rows
}

func displayWidth(b []byte) int {
	width := 0
	for _, r := range string(b) {
		if isWideRune(r) {
			width += 2
		} else {
			width++
		}
	}
	return width
}

func TestSchemeTableIsConsistent(t *testing.T) {
	seen := make(map[string]bool)
	for _, name := range schemeOrder {
		if seen[name] {
			t.Fatalf("duplicate scheme %q", name)
		}
		seen[name] = true
		if _, ok := schemes[name]; !ok {
			t.Fatalf("scheme %q missing", name)
		}
	}
	if strings.Contains(classicChars, "・") || strings.Contains(classicChars, "｜") {
		t.Fatal("classic set must not contain wide glyphs")
	}
}
