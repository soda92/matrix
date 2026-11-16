package main

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseFlagsLang(t *testing.T) {
	cases := []struct {
		args []string
		lang string
		wide bool
	}{
		{args: nil, lang: "classic", wide: false},
		{args: []string{"-L", "cn"}, lang: "cn", wide: true},
		{args: []string{"--lang", "jp"}, lang: "jp", wide: true},
		{args: []string{"--lang", "tw"}, lang: "tw", wide: true},
		{args: []string{"-L", "ko"}, lang: "kr", wide: true},
		{args: []string{"-L", "latin"}, lang: "latin", wide: false},
		{args: []string{"-C", "abc123"}, lang: "custom", wide: false},
		{args: []string{"-C", "漢字"}, lang: "custom", wide: true},
	}
	for _, c := range cases {
		opt, err := parseFlags(c.args)
		if err != nil {
			t.Fatalf("parseFlags(%v): %v", c.args, err)
		}
		if opt.lang != c.lang {
			t.Errorf("parseFlags(%v) lang = %q, want %q", c.args, opt.lang, c.lang)
		}
		if opt.cfg.wide != c.wide {
			t.Errorf("parseFlags(%v) wide = %v, want %v", c.args, opt.cfg.wide, c.wide)
		}
		if len(opt.cfg.chars) == 0 {
			t.Errorf("parseFlags(%v) returned no glyphs", c.args)
		}
	}

	if _, err := parseFlags([]string{"-L", "klingon"}); err == nil {
		t.Error("unknown language should fail")
	}
	if _, err := parseFlags([]string{"-C", ""}); err == nil {
		t.Error("empty charset should fail")
	}
}

func TestParseFlagsBasics(t *testing.T) {
	opt, err := parseFlags([]string{"-c", "rainbow", "-S", "250", "-d", "50", "-f", "60"})
	if err != nil {
		t.Fatal(err)
	}
	if opt.cfg.hueSpeed <= 0 {
		t.Error("rainbow should rotate the hue")
	}
	if opt.cfg.speed != 2.5 {
		t.Errorf("speed = %v, want 2.5", opt.cfg.speed)
	}
	if opt.cfg.density != 0.5 {
		t.Errorf("density = %v, want 0.5", opt.cfg.density)
	}
	if opt.cfg.fps != 60 {
		t.Errorf("fps = %d, want 60", opt.cfg.fps)
	}

	opt, err = parseFlags([]string{"--hue", "180", "--hue-speed", "0"})
	if err != nil {
		t.Fatal(err)
	}
	if diff := opt.cfg.hue - 0.5; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("hue = %v, want 0.5", opt.cfg.hue)
	}
	if opt.cfg.hueSpeed != 0 {
		t.Errorf("hue speed = %v, want 0", opt.cfg.hueSpeed)
	}

	if _, err := parseFlags([]string{"-v"}); !errors.Is(err, errVersion) {
		t.Errorf("-v should return errVersion, got %v", err)
	}
	if _, err := parseFlags([]string{"--nope"}); err == nil {
		t.Error("unknown flag should fail")
	}
	if _, err := parseFlags([]string{"extra"}); err == nil {
		t.Error("positional argument should fail")
	}
}

func TestVersionString(t *testing.T) {
	if got := versionString(); got == "" {
		t.Fatal("version string is empty")
	}
}

func TestParseFlagsCapture(t *testing.T) {
	opt, err := parseFlags([]string{"--seed", "42", "--frames", "10", "--size", "100x30"})
	if err != nil {
		t.Fatal(err)
	}
	if opt.seed != 42 {
		t.Errorf("seed = %d, want 42", opt.seed)
	}
	if opt.frames != 10 {
		t.Errorf("frames = %d, want 10", opt.frames)
	}
	if opt.width != 100 || opt.height != 30 {
		t.Errorf("size = %dx%d, want 100x30", opt.width, opt.height)
	}
	if !opt.cfg.truecolor {
		t.Error("capture mode should force truecolor")
	}

	if _, err := parseFlags([]string{"--size", "100"}); err == nil {
		t.Error("incomplete size should fail")
	}
	if _, err := parseFlags([]string{"--size", "0x30"}); err == nil {
		t.Error("zero width should fail")
	}
}

func TestParseSize(t *testing.T) {
	for _, in := range []string{"100x30", "100X30", "100×30"} {
		w, h, err := parseSize(in)
		if err != nil || w != 100 || h != 30 {
			t.Errorf("parseSize(%q) = %d,%d,%v", in, w, h, err)
		}
	}
	for _, in := range []string{"", "100", "x30", "100x", "a x b", "100x0"} {
		if _, _, err := parseSize(in); err == nil {
			t.Errorf("parseSize(%q) should fail", in)
		}
	}
}

func TestHueSpeedAcceptsZeroAndNegative(t *testing.T) {
	opt, err := parseFlags([]string{"-c", "green", "--hue-speed", "0"})
	if err != nil {
		t.Fatal(err)
	}
	if opt.cfg.hueSpeed != 0 {
		t.Errorf("hue speed = %v, want 0", opt.cfg.hueSpeed)
	}
	opt, err = parseFlags([]string{"-c", "rainbow", "--hue-speed", "-0.25"})
	if err != nil {
		t.Fatal(err)
	}
	if opt.cfg.hueSpeed != -0.25 {
		t.Errorf("hue speed = %v, want -0.25", opt.cfg.hueSpeed)
	}
}

func TestEscapeSequencesDoNotQuit(t *testing.T) {
	a := newApp(options{cfg: testConfig(), name: "green", lang: "classic", seed: 1})
	for _, b := range []byte{0x1b, '['} {
		if a.handleKey(b) {
			t.Fatal("escape sequence start should not quit")
		}
	}
	for _, b := range []byte{'A'} {
		if a.handleKey(b) {
			t.Fatal("arrow key should not quit")
		}
	}
	if a.inSeq || a.escPending {
		t.Fatal("sequence state was not cleared")
	}

	// A lone escape quits after the timeout.
	if a.handleKey(0x1b) {
		t.Fatal("escape should wait for a possible sequence")
	}
	if !a.escPending {
		t.Fatal("escape was not recorded")
	}
	if a.escTimedOut(time.Now()) {
		t.Fatal("escape timed out immediately")
	}
	if !a.escTimedOut(time.Now().Add(200 * time.Millisecond)) {
		t.Fatal("escape should time out")
	}
	if !a.handleKey('q') {
		t.Fatal("q should quit")
	}
}

func TestHeadlessFramesAreDeterministic(t *testing.T) {
	opt := options{
		cfg:    testConfig(),
		name:   "green",
		lang:   "classic",
		seed:   1234,
		frames: 6,
		width:  40,
		height: 12,
	}
	render := func() string {
		a := newApp(opt)
		var buf bytes.Buffer
		a.out = bufio.NewWriter(&buf)
		if err := a.runFrames(); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	first := render()
	second := render()
	if first != second {
		t.Fatal("same seed produced different frames")
	}
	if got := strings.Count(first, "\x1b[?2026h"); got != opt.frames {
		t.Errorf("frame count = %d, want %d", got, opt.frames)
	}
}
