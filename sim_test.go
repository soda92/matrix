package main

import (
	"math"
	"testing"
)

func testConfig() config {
	return config{
		hue:       0.33,
		sat:       1,
		speed:     1,
		density:   1,
		fps:       30,
		chars:     []rune("abcdefgh"),
		truecolor: true,
	}
}

func TestResizePreservesGrid(t *testing.T) {
	m := newMatrix(testConfig(), 40, 20, 1)
	before := make([]rune, len(m.glyphs))
	copy(before, m.glyphs)

	m.resize(60, 30)
	if len(m.glyphs) != 60*30 {
		t.Fatalf("glyph grid = %d cells, want %d", len(m.glyphs), 60*30)
	}
	if len(m.cols) != 60 {
		t.Fatalf("columns = %d, want 60", len(m.cols))
	}
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			if got, want := m.glyphs[y*60+x], before[y*40+x]; got != want {
				t.Fatalf("glyph at %d,%d = %q, want %q", x, y, got, want)
			}
		}
	}

	// Shrinking to something silly must not panic or leave a bad grid.
	m.resize(0, 0)
	if len(m.glyphs) != 1 {
		t.Fatalf("glyph grid = %d cells, want 1", len(m.glyphs))
	}
}

func TestIntensityStaysInRange(t *testing.T) {
	m := newMatrix(testConfig(), 20, 10, 2)
	for i := 0; i < 500; i++ {
		m.update(0.033)
	}
	for x := 0; x < m.w; x++ {
		for y := 0; y < m.h; y++ {
			v := m.intensity(x, y)
			if math.IsNaN(v) || v < 0 || v > 1 {
				t.Fatalf("intensity(%d,%d) = %v", x, y, v)
			}
		}
	}
}

func TestUpdateAdvancesHeads(t *testing.T) {
	m := newMatrix(testConfig(), 10, 10, 3)
	for i := range m.cols {
		m.cols[i] = column{head: 0, speed: 5, length: 3, gain: 1}
	}
	m.update(1)
	for x := range m.cols {
		if got := m.cols[x].head; math.Abs(got-5) > 1e-9 {
			t.Fatalf("column %d head = %v, want 5", x, got)
		}
	}
}

func TestTrailIsContiguousAndFadesUpward(t *testing.T) {
	m := newMatrix(testConfig(), 50, 30, 7)
	for i := 0; i < 300; i++ {
		m.update(0.033)
	}
	for x := 0; x < m.w; x++ {
		first, last := -1, -1
		for y := 0; y < m.h; y++ {
			if m.intensity(x, y) > 0 {
				if first < 0 {
					first = y
				}
				last = y
			}
		}
		if first < 0 {
			continue // column is between streams
		}
		prev := 0.0
		for y := first; y <= last; y++ {
			v := m.intensity(x, y)
			if v <= 0 {
				t.Fatalf("column %d has a gap at row %d", x, y)
			}
			if v <= prev {
				t.Fatalf("column %d does not brighten toward the head at row %d (%v <= %v)", x, y, v, prev)
			}
			prev = v
		}
	}
}

func TestColumnRespawnsAfterLeavingScreen(t *testing.T) {
	m := newMatrix(testConfig(), 4, 10, 4)
	for i := range m.cols {
		m.cols[i] = column{head: 20, speed: 5, length: 2, gain: 1}
	}
	m.update(0.01)
	for x := range m.cols {
		if m.cols[x].cooldown <= 0 {
			t.Fatalf("column %d did not start cooldown", x)
		}
		if m.cols[x].head >= 20 {
			t.Fatalf("column %d kept its old head", x)
		}
	}

	// After the cooldown the column is live again and moving.
	m.update(5)
	if m.cols[0].cooldown > 0 {
		t.Fatalf("column 0 still cooling down")
	}
}

func TestLevelFor(t *testing.T) {
	cases := []struct {
		in   float64
		want int
	}{
		{-1, 0},
		{0, 0},
		{0.001, 1},
		{0.5, 16},
		{1, 32},
		{2, 32},
	}
	for _, c := range cases {
		if got := levelFor(c.in, 32); got != c.want {
			t.Errorf("levelFor(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestHSV(t *testing.T) {
	cases := []struct {
		h       float64
		r, g, b uint8
	}{
		{0, 255, 0, 0},
		{1.0 / 3, 0, 255, 0},
		{2.0 / 3, 0, 0, 255},
		{-1.0 / 3, 0, 0, 255},
	}
	for _, c := range cases {
		r, g, b := hsv2rgb(c.h, 1, 1)
		if r != c.r || g != c.g || b != c.b {
			t.Errorf("hsv2rgb(%v,1,1) = %d,%d,%d, want %d,%d,%d", c.h, r, g, b, c.r, c.g, c.b)
		}
	}
}

func BenchmarkDraw200x60(b *testing.B) {
	cfg := testConfig()
	m := newMatrix(cfg, 200, 60, 5)
	pal := newPalette(true)
	pal.update(0.33, 1)
	frame := newFrameBuf(200, 60)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		frame.draw(m, pal, false)
	}
}
