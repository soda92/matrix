package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

const (
	enterScreen = "\x1b[?1049h\x1b[2J\x1b[H\x1b[?25l\x1b[?7l"
	leaveScreen = "\x1b[0m\x1b[?7h\x1b[?25h\x1b[?1049l"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

var (
	errQuit    = errors.New("quit")
	errVersion = errors.New("version printed")
)

// scheme is a named starting point for the hue animation.
type scheme struct {
	hue      float64
	sat      float64
	hueSpeed float64
}

var schemes = map[string]scheme{
	"green":   {hue: 0.33, sat: 1.0},
	"red":     {hue: 0.00, sat: 1.0},
	"orange":  {hue: 0.07, sat: 1.0},
	"yellow":  {hue: 0.15, sat: 1.0},
	"cyan":    {hue: 0.50, sat: 1.0},
	"blue":    {hue: 0.62, sat: 1.0},
	"purple":  {hue: 0.78, sat: 1.0},
	"magenta": {hue: 0.90, sat: 1.0},
	"white":   {hue: 0.33, sat: 0.12},
	"rainbow": {hue: 0.33, sat: 1.0, hueSpeed: 0.12},
}

// schemeOrder is the order used by the 1-9/0 keyboard shortcuts.
var schemeOrder = []string{
	"green", "red", "orange", "yellow", "cyan",
	"blue", "purple", "magenta", "white", "rainbow",
}

type options struct {
	cfg    config
	name   string
	lang   string
	seed   int64
	frames int
	width  int
	height int
}

type app struct {
	cfg      config
	name     string
	langName string
	m        *matrix
	pal      *palette
	frame    *frameBuf
	out      *bufio.Writer
	term     *termState

	hue           float64
	frames        int
	paused        bool
	closed        bool
	screenActive  bool
	lastSizeCheck time.Time

	escAt      time.Time
	escPending bool
	inSeq      bool
}

func main() {
	os.Exit(realMain())
}

func realMain() int {
	opt, err := parseFlags(os.Args[1:])
	switch {
	case err == nil:
	case errors.Is(err, flag.ErrHelp):
		fmt.Print(usageText)
		return 0
	case errors.Is(err, errVersion):
		return 0
	default:
		fmt.Fprintln(os.Stderr, "matrix:", err)
		return 2
	}

	a := newApp(opt)
	defer a.shutdown()
	if err := a.run(); err != nil && !errors.Is(err, errQuit) {
		fmt.Fprintln(os.Stderr, "matrix:", err)
		return 1
	}
	return 0
}

func newApp(opt options) *app {
	w, h := termSize()
	if opt.width > 0 && opt.height > 0 {
		w, h = opt.width, opt.height
	}
	seed := opt.seed
	if seed < 0 {
		seed = time.Now().UnixNano()
	}
	return &app{
		cfg:      opt.cfg,
		name:     opt.name,
		langName: opt.lang,
		m:        newMatrix(opt.cfg, w, h, seed),
		pal:      newPalette(opt.cfg.truecolor),
		frame:    newFrameBuf(w, h),
		out:      bufio.NewWriterSize(os.Stdout, 1<<18),
		term:     &termState{fd: int(os.Stdin.Fd())},
		hue:      opt.cfg.hue,
		frames:   opt.frames,
	}
}

func (a *app) run() error {
	if a.frames > 0 {
		return a.runFrames()
	}

	t, err := setupTerminal()
	if err != nil {
		return err
	}
	a.term = t

	a.out.WriteString(enterScreen)
	a.screenActive = true
	if err := a.out.Flush(); err != nil {
		return err
	}

	keys := make(chan byte, 128)
	go readKeys(keys)

	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	defer signal.Stop(sigs)

	ticker := time.NewTicker(time.Second / time.Duration(a.cfg.fps))
	defer ticker.Stop()

	last := time.Now()
	a.lastSizeCheck = last
	for {
		select {
		case now := <-ticker.C:
			dt := now.Sub(last).Seconds()
			last = now
			if dt > 0.25 {
				dt = 0.25
			}
			if a.escTimedOut(now) {
				return errQuit
			}
			if !a.paused {
				a.m.update(dt)
				a.hue += dt * a.cfg.hueSpeed
			}
			a.checkResize(now)
			if err := a.render(); err != nil {
				return err
			}
		case b := <-keys:
			if a.handleKey(b) {
				return errQuit
			}
		case <-sigs:
			return errQuit
		}
	}
}

// runFrames renders a fixed number of frames to stdout without touching the
// terminal. Together with --seed and --size it gives reproducible output for
// screenshots, GIFs, and golden tests.
func (a *app) runFrames() error {
	dt := 1 / float64(a.cfg.fps)
	for i := 0; i < a.frames; i++ {
		a.m.update(dt)
		a.hue += dt * a.cfg.hueSpeed
		a.pal.update(a.hue, a.cfg.sat)
		if _, err := a.out.Write(a.frame.draw(a.m, a.pal, a.cfg.bold)); err != nil {
			return writeErr(err)
		}
	}
	return a.out.Flush()
}

func (a *app) render() error {
	a.pal.update(a.hue, a.cfg.sat)
	frame := a.frame.draw(a.m, a.pal, a.cfg.bold)
	if _, err := a.out.Write(frame); err != nil {
		return writeErr(err)
	}
	if err := a.out.Flush(); err != nil {
		return writeErr(err)
	}
	return nil
}

// checkResize polls the terminal size. Polling instead of SIGWINCH keeps the
// code portable and costs a single ioctl every 250ms.
func (a *app) checkResize(now time.Time) {
	if now.Sub(a.lastSizeCheck) < 250*time.Millisecond {
		return
	}
	a.lastSizeCheck = now
	w, h := termSize()
	if w == a.m.termW && h == a.m.h {
		return
	}
	a.m.resize(w, h)
	a.frame = newFrameBuf(w, h)
	a.out.WriteString("\x1b[2J")
}

// escTimedOut reports whether a lone escape byte has waited long enough to be
// treated as the Escape key rather than the start of a sequence.
func (a *app) escTimedOut(now time.Time) bool {
	return a.escPending && now.Sub(a.escAt) > 120*time.Millisecond
}

func (a *app) handleKey(b byte) bool {
	if a.inSeq {
		// Swallow the rest of a CSI/SS3 sequence, for example arrow keys.
		if b >= 0x40 && b <= 0x7e {
			a.inSeq = false
		}
		return false
	}
	if a.escPending {
		a.escPending = false
		if b == '[' || b == 'O' {
			a.inSeq = true
			return false
		}
		// ESC followed by an ordinary key is treated as Alt+key: quit.
		return true
	}

	switch b {
	case 'q', 'Q', 0x03:
		return true
	case 0x1b:
		a.escPending = true
		a.escAt = time.Now()
		return false
	case 'p', 'P', ' ':
		a.paused = !a.paused
	case '+', '=':
		a.setSpeed(a.cfg.speed * 1.15)
	case '-', '_':
		a.setSpeed(a.cfg.speed / 1.15)
	case 'r', 'R':
		if a.cfg.hueSpeed != 0 {
			a.cfg.hueSpeed = 0
		} else {
			a.cfg.hueSpeed = 0.12
		}
	case 'l', 'L':
		a.nextLang()
	case '1', '2', '3', '4', '5', '6', '7', '8', '9', '0':
		idx := int(b - '1')
		if b == '0' {
			idx = 9
		}
		if idx >= 0 && idx < len(schemeOrder) {
			a.applyScheme(schemeOrder[idx])
		}
	}
	return false
}

func (a *app) applyScheme(name string) {
	s, ok := schemes[name]
	if !ok {
		return
	}
	a.name = name
	a.cfg.hue = s.hue
	a.cfg.sat = s.sat
	a.cfg.hueSpeed = s.hueSpeed
	a.hue = s.hue
}

func (a *app) applyLang(name string) {
	def, ok := lookupLang(name)
	if !ok {
		return
	}
	a.langName = def.name
	cellW := 1
	if def.wide {
		cellW = 2
	}
	a.m.setChars(def.chars, cellW)
	a.frame = newFrameBuf(a.m.termW, a.m.h)
	a.out.WriteString("\x1b[2J")
}

func (a *app) nextLang() {
	idx := 0
	for i, name := range langOrder {
		if name == a.langName {
			idx = i
			break
		}
	}
	a.applyLang(langOrder[(idx+1)%len(langOrder)])
}

func (a *app) setSpeed(speed float64) {
	speed = math.Max(0.1, math.Min(10, speed))
	ratio := speed / a.cfg.speed
	a.cfg.speed = speed
	a.m.scaleSpeed(ratio)
}

func (a *app) shutdown() {
	if a.closed {
		return
	}
	a.closed = true
	if a.screenActive {
		a.out.WriteString(leaveScreen)
		a.out.Flush()
	}
	a.term.restore()
}

func writeErr(err error) error {
	if errors.Is(err, syscall.EPIPE) {
		return errQuit
	}
	return err
}

func readKeys(ch chan<- byte) {
	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			select {
			case ch <- buf[0]:
			default:
			}
		}
		if err != nil {
			return
		}
	}
}

type termState struct {
	fd   int
	orig *term.State
}

func setupTerminal() (*termState, error) {
	t := &termState{fd: int(os.Stdin.Fd())}
	if !term.IsTerminal(t.fd) {
		return t, nil
	}
	orig, err := term.MakeRaw(t.fd)
	if err != nil {
		// Keep running even without raw mode; Ctrl+C still works.
		return t, nil
	}
	t.orig = orig
	return t, nil
}

func (t *termState) restore() {
	if t.orig != nil {
		_ = term.Restore(t.fd, t.orig)
		t.orig = nil
	}
}

func termSize() (int, int) {
	for _, fd := range []int{int(os.Stdout.Fd()), int(os.Stdin.Fd())} {
		if w, h, err := term.GetSize(fd); err == nil && w > 0 && h > 0 {
			return w, h
		}
	}
	return 80, 24
}

func detectTruecolor() bool {
	if ct := strings.ToLower(os.Getenv("COLORTERM")); strings.Contains(ct, "truecolor") || strings.Contains(ct, "24bit") {
		return true
	}
	t := strings.ToLower(os.Getenv("TERM"))
	return strings.Contains(t, "direct") || strings.Contains(t, "24bit")
}

func parseFlags(args []string) (options, error) {
	var (
		opt        options
		colorName  = "green"
		speedPct   = 100
		densityPct = 100
		fps        = 30
		langName   = "classic"
		chars      = classicChars
		truecolor  = detectTruecolor()
		hueDeg     = -1.0
		hueSpeed   = -1.0
		seed       = int64(-1)
		frames     = 0
		size       = ""
		showVer    bool
	)

	fs := flag.NewFlagSet("matrix", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	fs.StringVar(&colorName, "c", colorName, "color scheme")
	fs.StringVar(&colorName, "color", colorName, "color scheme")
	fs.IntVar(&speedPct, "S", speedPct, "speed percentage")
	fs.IntVar(&speedPct, "speed", speedPct, "speed percentage")
	fs.IntVar(&densityPct, "d", densityPct, "stream density percentage")
	fs.IntVar(&densityPct, "density", densityPct, "stream density percentage")
	fs.IntVar(&fps, "f", fps, "frames per second")
	fs.IntVar(&fps, "fps", fps, "frames per second")
	fs.StringVar(&chars, "C", chars, "glyph set")
	fs.StringVar(&chars, "chars", chars, "glyph set")
	fs.StringVar(&langName, "L", langName, "language / glyph set")
	fs.StringVar(&langName, "lang", langName, "language / glyph set")
	fs.BoolVar(&truecolor, "T", truecolor, "force 24-bit color")
	fs.BoolVar(&truecolor, "truecolor", truecolor, "force 24-bit color")
	fs.BoolVar(&opt.cfg.bold, "b", false, "bold glyphs")
	fs.BoolVar(&opt.cfg.bold, "bold", false, "bold glyphs")
	fs.Float64Var(&hueDeg, "hue", -1, "override base hue in degrees")
	fs.Float64Var(&hueSpeed, "hue-speed", -1, "hue rotation in turns per second")
	fs.Int64Var(&seed, "seed", -1, "random seed for reproducible runs")
	fs.IntVar(&frames, "frames", 0, "render N frames to stdout and exit")
	fs.StringVar(&size, "size", "", "force WxH dimensions for --frames")
	fs.BoolVar(&showVer, "v", false, "print version")
	fs.BoolVar(&showVer, "version", false, "print version")

	if err := fs.Parse(args); err != nil {
		return opt, err
	}
	if showVer {
		fmt.Printf("matrix %s\n", version)
		return opt, errVersion
	}
	if fs.NArg() > 0 {
		return opt, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	s, ok := schemes[colorName]
	if !ok {
		return opt, fmt.Errorf("unknown color scheme %q (try green, rainbow, ...)", colorName)
	}
	opt.name = colorName
	opt.cfg.hue = s.hue
	opt.cfg.sat = s.sat
	opt.cfg.hueSpeed = s.hueSpeed
	if hueDeg >= 0 {
		opt.cfg.hue = hueDeg / 360
	}
	opt.cfg.speed = math.Max(0.1, math.Min(10, float64(speedPct)/100))
	opt.cfg.density = math.Max(0.05, math.Min(3, float64(densityPct)/100))
	opt.cfg.fps = fps
	if opt.cfg.fps < 5 {
		opt.cfg.fps = 5
	}
	if opt.cfg.fps > 240 {
		opt.cfg.fps = 240
	}
	opt.cfg.truecolor = truecolor
	opt.seed = seed
	opt.frames = frames
	if opt.frames < 0 {
		opt.frames = 0
	}
	if size != "" {
		w, h, err := parseSize(size)
		if err != nil {
			return opt, err
		}
		opt.width, opt.height = w, h
	}

	seen := make(map[string]bool, 4)
	fs.Visit(func(f *flag.Flag) { seen[f.Name] = true })
	if seen["hue-speed"] {
		opt.cfg.hueSpeed = hueSpeed
	}
	if opt.frames > 0 && !seen["T"] && !seen["truecolor"] {
		// Captures should look identical everywhere.
		opt.cfg.truecolor = true
	}

	// -C/--chars wins over -L/--lang, and its cell width is inferred from
	// the glyphs so CJK text lines up without an extra flag.
	if seen["C"] || seen["chars"] {
		opt.lang = "custom"
		opt.cfg.chars = splitRunes(chars)
		if len(opt.cfg.chars) == 0 {
			return opt, errors.New("glyph set must not be empty")
		}
		opt.cfg.wide = anyWide(opt.cfg.chars)
		return opt, nil
	}

	def, ok := lookupLang(langName)
	if !ok {
		return opt, fmt.Errorf("unknown language %q (try classic, latin, jp, cn, tw, kr)", langName)
	}
	opt.lang = def.name
	opt.cfg.chars = def.chars
	opt.cfg.wide = def.wide
	return opt, nil
}

func parseSize(s string) (int, int, error) {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == 'x' || r == 'X' || r == '×'
	})
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid size %q (want WxH, e.g. 100x30)", s)
	}
	w, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	h, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	if w < 1 || h < 1 || w > 10000 || h > 10000 {
		return 0, 0, fmt.Errorf("size %q out of range", s)
	}
	return w, h, nil
}

const usageText = `matrix - falling glyph rain for your terminal

Usage:
  matrix [options]

Options:
  -L, --lang NAME     glyph set: classic, latin, jp, cn, tw, kr
                      (default classic; aliases: ja, zh, zh-tw, ko)
  -C, --chars STRING  custom glyph set; overrides --lang
  -c, --color NAME    color scheme: green, red, orange, yellow, cyan,
                      blue, purple, magenta, white, rainbow (default green)
  -S, --speed PCT     speed percentage, 10-1000 (default 100)
  -d, --density PCT   stream density percentage, 5-300 (default 100)
  -f, --fps N         frames per second, 5-240 (default 30)
      --hue DEG       override the scheme's base hue (0-360)
      --hue-speed T   hue rotation in turns per second (default 0)
  -T, --truecolor     force 24-bit color (auto-detected by default)
  -b, --bold          render glyphs in bold
      --seed N        seed the simulation for reproducible output
      --frames N      render N frames to stdout and exit (no TUI)
      --size WxH      force dimensions, e.g. 100x30 (with --frames)
  -v, --version       print version and exit

Keys:
  q, Esc, Ctrl+C      quit
  p, Space            pause / resume
  +, -                speed up / slow down
  1-9, 0              switch color scheme
  l                   cycle language
  r                   toggle rainbow hue rotation
`
