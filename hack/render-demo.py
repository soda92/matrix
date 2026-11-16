#!/usr/bin/env python3
"""Render a demo GIF from matrix's headless frame output.

matrix can emit a fixed number of frames as ANSI art:

    ./matrix --lang cn --size 96x28 --frames 72 --fps 24 --seed 7

This script parses that stream, rasterizes it with a monospace CJK font, and
writes an animated GIF. It is only needed to regenerate docs/demo.gif; the
matrix binary itself has no Python or font dependency.

Requires: Python 3, Pillow, fontconfig (fc-match), and a monospace CJK font.
On Debian/Ubuntu: python3-pil fonts-noto-cjk fontconfig
"""

import argparse
import os
import re
import shutil
import subprocess
import sys

try:
    from PIL import Image, ImageDraw, ImageFont
except ImportError:
    sys.exit("render-demo.py: Pillow is required (pip install pillow)")

CURSOR = re.compile(rb"\x1b\[(\d+);1H")
COLOR = re.compile(rb"\x1b\[38;2;(\d+);(\d+);(\d+)m")
SGR = re.compile(rb"\x1b\[[0-9;?]*[a-zA-Z]")


def parse_frames(data):
    frames = []
    for chunk in data.split(b"\x1b[?2026h")[1:]:
        chunk = chunk.split(b"\x1b[?2026l")[0]
        rows = {}
        row = -1
        for part in re.split(rb"(\x1b\[\d+;1H)", chunk):
            m = CURSOR.fullmatch(part)
            if m:
                row = int(m.group(1)) - 1
                rows[row] = b""
            elif row >= 0:
                rows[row] += part

        parsed = {}
        for y, segment in rows.items():
            cells = []
            color = (0, 0, 0)
            i = 0
            while i < len(segment):
                if segment[i] == 0x1B:
                    m = COLOR.match(segment, i)
                    if m:
                        color = tuple(int(v) for v in m.groups())
                        i = m.end()
                        continue
                    m = SGR.match(segment, i)
                    if m:
                        i = m.end()
                        continue
                    i += 1
                    continue
                ch = segment[i:].decode("utf-8", "replace")[0]
                i += len(ch.encode())
                cells.append((ch, color))
            parsed[y] = cells
        frames.append(parsed)
    return frames


def resolve_font(family):
    if shutil.which("fc-match") is None:
        return None, 0, family
    try:
        out = subprocess.check_output(
            ["fc-match", "-f", "%{file}\t%{index}\t%{family}", family],
            text=True, stderr=subprocess.DEVNULL,
        ).strip()
    except (OSError, subprocess.CalledProcessError):
        return None, 0, family
    parts = out.split("\t")
    if len(parts) != 3 or not parts[0] or not os.path.exists(parts[0]):
        return None, 0, family
    return parts[0], int(parts[1] or 0), parts[2]


def render(frames, width, height, font, cell_w, cell_h, ascent, bg):
    images = []
    for rows in frames:
        img = Image.new("RGB", (width * cell_w, height * cell_h), bg)
        draw = ImageDraw.Draw(img)
        for y, cells in rows.items():
            if y >= height:
                continue
            x = 0
            baseline = y * cell_h + ascent
            for ch, color in cells:
                if x >= width:
                    break
                if ch != " ":
                    draw.text((x * cell_w, baseline), ch, font=font, fill=color, anchor="ls")
                x += 2 if is_wide(ch) else 1
        images.append(img)
    return images


def is_wide(ch):
    import unicodedata
    return unicodedata.east_asian_width(ch) in ("W", "F")


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--bin", default="./matrix")
    p.add_argument("--out", default="docs/demo.gif")
    p.add_argument("--lang", default="cn")
    p.add_argument("--color", default="green")
    p.add_argument("--size", default="96x28", help="WxH in terminal cells")
    p.add_argument("--frames", type=int, default=72)
    p.add_argument("--fps", type=int, default=24)
    p.add_argument("--seed", type=int, default=7)
    p.add_argument("--speed", type=int, default=70)
    p.add_argument("--font", default="Noto Sans Mono CJK JP")
    p.add_argument("--font-size", type=int, default=16)
    p.add_argument("--background", default="#000000")
    p.add_argument("--preview", help="also write the last frame as a PNG")
    args = p.parse_args()

    try:
        cols, rows = (int(v) for v in args.size.lower().split("x"))
    except ValueError:
        sys.exit(f"render-demo.py: bad --size {args.size!r}, want WxH")

    cmd = [
        args.bin, "--lang", args.lang, "--color", args.color,
        "--size", args.size, "--frames", str(args.frames), "--fps", str(args.fps),
        "--seed", str(args.seed), "--speed", str(args.speed), "--truecolor",
    ]
    data = subprocess.check_output(cmd)
    frames = parse_frames(data)
    if len(frames) != args.frames:
        sys.exit(f"render-demo.py: parsed {len(frames)} frames, expected {args.frames}")

    path, index, family = resolve_font(args.font)
    if path is None:
        sys.exit(f"render-demo.py: font {args.font!r} not found; install a monospace CJK font")
    font = ImageFont.truetype(path, args.font_size, index=index)
    advance = font.getlength("M")
    wide_advance = font.getlength("漢")
    if wide_advance < advance * 1.8:
        print(
            f"render-demo.py: warning: {family} does not look dual-width "
            f"(M={advance:.1f}, CJK={wide_advance:.1f}); glyphs may overlap",
            file=sys.stderr,
        )
    cell_w = round(advance)
    cell_h = round(args.font_size * 1.35)
    ascent, _ = font.getmetrics()

    images = render(frames, cols, rows, font, cell_w, cell_h, ascent, args.background)
    os.makedirs(os.path.dirname(os.path.abspath(args.out)), exist_ok=True)
    duration = max(20, round(1000 / args.fps))
    images[0].save(
        args.out, save_all=True, append_images=images[1:],
        duration=duration, loop=0, optimize=True, disposal=2,
    )

    if args.preview:
        images[-1].save(args.preview)

    size_mb = os.path.getsize(args.out) / (1 << 20)
    print(
        f"wrote {args.out} ({cols * cell_w}x{rows * cell_h}, {len(images)} frames, "
        f"{size_mb:.1f} MiB) using {family} [{path}#{index}]"
    )


if __name__ == "__main__":
    main()
