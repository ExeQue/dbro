"""Generate dbro artwork SVGs with all text converted to paths."""
import sys
from fontTools.ttLib import TTFont
from fontTools.pens.svgPathPen import SVGPathPen

OUT = "/home/exe/Work/dbro/assets"
F = "/usr/share/fonts/noto/"
BLACK, MED, MONO = F + "NotoSans-Black.ttf", F + "NotoSans-Medium.ttf", F + "NotoSansMono-Bold.ttf"

INK = "#1A1214"      # tile / background
INK2 = "#2A1C20"     # raised surface
RED = "#FF4D45"      # cylinder, "bro"
RED_HI = "#FF8A7A"   # lid
CRIMSON = "#B3262E"  # disk band, lid rim
GOLD = "#FFC15E"     # lens glint accent
WHITE = "#FBF5F3"
MUTED = "#BFAEAA"

_fonts = {}


def text(path, s, x, y, size, fill, track=0.0):
    """Return (svg <path>, advance width) for s, baseline at y."""
    f = _fonts.setdefault(path, TTFont(path))
    gs, cmap, upm = f.getGlyphSet(), f.getBestCmap(), f["head"].unitsPerEm
    k = size / upm
    parts, adv = [], 0.0
    for ch in s:
        g = cmap[ord(ch)]
        pen = SVGPathPen(gs)
        gs[g].draw(pen)
        d = pen.getCommands()
        if d:
            parts.append(f'<path transform="translate({x + adv:.2f} {y}) scale({k:.5f} {-k:.5f})" d="{d}"/>')
        adv += gs[g].width * k + track * size
    adv -= track * size
    return f'<g fill="{fill}">{"".join(parts)}</g>', adv


def mark(tile=True):
    """The dbro mark in a 512x512 coordinate space."""
    t = f'<rect width="512" height="512" rx="116" fill="{INK}"/>' if tile else ""
    return t + f"""
  <path d="M100 150 V384 A156 48 0 0 0 412 384 V150 Z" fill="{RED}"/>
  <path d="M100 311 A156 48 0 0 0 412 311 V325 A156 48 0 0 1 100 325 Z" fill="{CRIMSON}"/>
  <ellipse cx="256" cy="150" rx="156" ry="48" fill="{RED_HI}"/>
  <ellipse cx="256" cy="150" rx="156" ry="48" fill="none" stroke="{CRIMSON}" stroke-width="10" opacity=".5"/>
  <path d="M114 204 H398 V224 H114 Z" fill="{INK}"/>
  <path d="M130 212 H246 V246 C246 268 230 280 206 280 H170 C146 280 130 266 130 244 Z" fill="{INK}"/>
  <path d="M266 212 H382 V244 C382 266 366 280 342 280 H306 C282 280 266 268 266 246 Z" fill="{INK}"/>
  <path d="M148 262 L160 262 L184 226 L172 226 Z M168 262 L175 262 L192 236 L185 236 Z" fill="{GOLD}"/>
  <path d="M284 262 L296 262 L320 226 L308 226 Z M304 262 L311 262 L328 236 L321 236 Z" fill="{GOLD}"/>
  <path d="M222 292 Q262 312 298 284" fill="none" stroke="{INK}" stroke-width="15" stroke-linecap="round"/>
"""


def svg(w, h, body, label):
    return (f'<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}" '
            f'viewBox="0 0 {w} {h}" role="img" aria-label="{label}">\n{body}\n</svg>\n')


def wordmark(x, baseline, size):
    db, w1 = text(BLACK, "d", x, baseline, size, WHITE, -0.02)
    bro, w2 = text(BLACK, "bro", x + w1 - size * 0.02, baseline, size, RED, -0.02)
    return db + bro, w1 + w2


def placed_mark(x, y, s):
    return f'<g transform="translate({x} {y}) scale({s / 512})">{mark()}</g>'


# logo
open(f"{OUT}/logo.svg", "w").write(svg(512, 512, mark(), "dbro logo"))

# banner 1280x400, self-contained dark background
W, H = 1280, 400
wm, wmw = wordmark(0, 0, 168)
tag, tagw = text(MED, "Your database bro. A simple SQL browser for Linux.", 0, 0, 34, MUTED)
ms, gap = 236, 56
content_w = ms + gap + max(wmw, tagw)
x0 = (W - content_w) / 2
tx = x0 + ms + gap
banner = (f'<rect width="{W}" height="{H}" rx="28" fill="{INK}"/>'
          f'<circle cx="{W - 90}" cy="70" r="210" fill="{INK2}"/>'
          + placed_mark(x0, (H - ms) / 2, ms)
          + f'<g transform="translate({tx} 226)">{wm}</g>'
          + f'<g transform="translate({tx + 6} 292)">{tag}</g>')
open(f"{OUT}/banner.svg", "w").write(svg(W, H, banner, "dbro: Your database bro. A simple SQL browser for Linux."))

# social preview 1280x640
W, H = 1280, 640
wm, wmw = wordmark(0, 0, 200)
tag, tagw = text(MED, "Your database bro. A simple SQL browser for Linux.", 0, 0, 38, MUTED)
chips_s = "MySQL  ·  MariaDB  ·  PostgreSQL  ·  SQLite  ·  MCP"
chips, chipw = text(MONO, chips_s, 0, 0, 26, RED)
ms, gap = 260, 60
top_w = ms + gap + wmw
x0 = (W - top_w) / 2
social = (f'<rect width="{W}" height="{H}" fill="{INK}"/>'
          f'<circle cx="{W - 60}" cy="40" r="300" fill="{INK2}"/>'
          f'<circle cx="40" cy="{H}" r="200" fill="{INK2}"/>'
          + placed_mark(x0, 80, ms)
          + f'<g transform="translate({x0 + ms + gap} 286)">{wm}</g>'
          + f'<g transform="translate({(W - tagw) / 2} 440)">{tag}</g>'
          + f'<rect x="{(W - chipw) / 2 - 28}" y="482" width="{chipw + 56}" height="54" rx="27" fill="{INK2}"/>'
          + f'<g transform="translate({(W - chipw) / 2} 518)">{chips}</g>')
open(f"{OUT}/social-preview.svg", "w").write(svg(W, H, social, "dbro social preview"))
print("ok")
