package main

import (
	"math"
	"math/rand"
)

// config holds the user-tunable knobs that shape the rain.
type config struct {
	hue       float64 // base hue, 0..1
	sat       float64 // base saturation, 0..1
	hueSpeed  float64 // hue turns per second
	speed     float64 // global speed multiplier
	density   float64 // stream density multiplier
	fps       int
	chars     []rune
	wide      bool // glyphs occupy two terminal cells
	truecolor bool
	bold      bool
}

// column is one vertical stream. The head is measured in rows and moves down.
// Cells above the head, up to length rows, make up the trail.
type column struct {
	head     float64
	speed    float64 // rows per second
	length   float64
	cooldown float64 // seconds of darkness before respawning
	gain     float64 // per-column brightness variation
}

// matrix owns the falling columns and the glyph grid that backs them.
// w is the number of glyph slots; the terminal is w*cellW columns wide.
type matrix struct {
	cfg    config
	w, h   int
	termW  int
	cellW  int
	cols   []column
	glyphs []rune
	rng    *rand.Rand
	time   float64
}

func newMatrix(cfg config, termW, termH int, seed int64) *matrix {
	m := &matrix{cfg: cfg, rng: rand.New(rand.NewSource(seed))}
	m.cellW = 1
	if cfg.wide {
		m.cellW = 2
	}
	m.resize(termW, termH)
	return m
}

// resize rebuilds the buffers, keeping the rain that still fits on screen.
// It takes the terminal size in cells and converts it to glyph slots.
func (m *matrix) resize(termW, termH int) {
	if termW < 1 {
		termW = 1
	}
	if termH < 1 {
		termH = 1
	}
	if m.cellW < 1 {
		m.cellW = 1
	}
	m.termW = termW

	w := termW / m.cellW
	if w < 1 {
		w = 1
	}
	h := termH

	oldW, oldH := m.w, m.h
	oldGlyphs := m.glyphs
	oldCols := m.cols
	haveOld := oldW > 0 && len(oldGlyphs) == oldW*oldH

	m.w, m.h = w, h
	m.glyphs = make([]rune, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if haveOld && x < oldW && y < oldH {
				m.glyphs[y*w+x] = oldGlyphs[y*oldW+x]
			} else {
				m.glyphs[y*w+x] = m.randRune()
			}
		}
	}

	m.cols = make([]column, w)
	for x := 0; x < w; x++ {
		if x < len(oldCols) {
			c := oldCols[x]
			if c.length > float64(h) {
				c.length = float64(h)
			}
			m.cols[x] = c
		} else {
			m.cols[x] = m.newColumn(true)
		}
	}
}

// setChars swaps the glyph set, optionally changing the cell width. The grid
// is refilled so no stale glyphs from the old language remain.
func (m *matrix) setChars(chars []rune, cellW int) {
	m.cfg.chars = chars
	if cellW < 1 {
		cellW = 1
	}
	m.cellW = cellW
	m.resize(m.termW, m.h)
	for i := range m.glyphs {
		m.glyphs[i] = m.randRune()
	}
}

func (m *matrix) newColumn(onScreen bool) column {
	var c column
	m.resetColumn(&c, onScreen)
	return c
}

func (m *matrix) resetColumn(c *column, onScreen bool) {
	h := float64(m.h)
	c.length = minTrail + m.rng.Float64()*(h*0.75)
	c.speed = (3 + m.rng.Float64()*16) * m.cfg.speed
	c.gain = 0.72 + m.rng.Float64()*0.28
	c.cooldown = 0
	if onScreen {
		// Start somewhere the trail is already partly visible so the
		// screen fills up immediately on startup and on resize.
		c.head = -c.length*0.9 + m.rng.Float64()*(h+c.length*0.9)
	} else {
		c.head = -c.length - m.rng.Float64()*c.length
	}
}

const minTrail = 4.0

// update advances the simulation by dt seconds.
func (m *matrix) update(dt float64) {
	if dt <= 0 || m.w <= 0 || m.h <= 0 {
		return
	}
	m.time += dt

	// Mutate a small fraction of the grid every second. This is what makes
	// the trails shimmer instead of looking like static glyphs.
	mutations := int(float64(m.w*m.h) * 0.03 * dt)
	for i := 0; i < mutations; i++ {
		m.glyphs[m.rng.Intn(len(m.glyphs))] = m.randRune()
	}

	for x := range m.cols {
		c := &m.cols[x]
		if c.cooldown > 0 {
			c.cooldown -= dt
			if c.cooldown > 0 {
				continue
			}
			m.resetColumn(c, false)
		}
		c.head += c.speed * dt

		// Classic matrix: the leading glyph changes every frame.
		row := int(math.Floor(c.head))
		if row >= 0 && row < m.h {
			m.glyphs[row*m.w+x] = m.randRune()
		}

		// Once the whole trail has left the screen, park the column above
		// the top and keep it dark for a moment before it rains again.
		if c.head-c.length > float64(m.h) {
			m.resetColumn(c, false)
			c.cooldown = m.rng.Float64() * 2.5 / m.cfg.density
			if c.cooldown < 0.05 {
				c.cooldown = 0.05
			}
		}
	}
}

// intensity returns the glow of cell (x, y) in 0..1. Zero means blank.
// The head is always at full intensity; gain only dims the tail, which lets
// each column fade at its own rate without losing the bright leading glyph.
func (m *matrix) intensity(x, y int) float64 {
	c := &m.cols[x]
	if c.cooldown > 0 {
		return 0
	}
	d := c.head - float64(y)
	if d < 0 || d >= c.length {
		return 0
	}
	t := d / c.length
	return (1 - t) * (1 - (1-c.gain)*t)
}

func (m *matrix) randRune() rune {
	return m.cfg.chars[m.rng.Intn(len(m.cfg.chars))]
}

// scaleSpeed adjusts every live column so speed changes apply immediately.
func (m *matrix) scaleSpeed(factor float64) {
	for i := range m.cols {
		m.cols[i].speed *= factor
	}
}
