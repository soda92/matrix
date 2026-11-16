package main

import (
	"errors"
	"testing"
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
