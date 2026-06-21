#!/usr/bin/env python3
"""Generate the Tinfoil Proxy DMG background (dark teal + diamond texture)."""
from __future__ import annotations

from pathlib import Path

from PIL import Image, ImageDraw

BASE_COLOR = (255, 252, 249)
STROKE_RGBA = (0, 0, 0, 13)
ARROW_RGBA = (0, 68, 67, 110)
BASE_WIDTH = 540
BASE_HEIGHT = 380
CELL_BASE = 8
ARROW_CENTER = (270, 175)
ARROW_LENGTH = 90
ARROW_THICKNESS = 10
ARROW_HEAD_WIDTH = 32
ARROW_HEAD_HEIGHT = 38


def draw_diamond_grid(draw: ImageDraw.ImageDraw, width: int, height: int, cell: int) -> None:
    half = cell // 2
    for x in range(-cell, width + cell, cell):
        for y in range(-cell, height + cell, cell):
            pts = [
                (x + half, y),
                (x + cell, y + half),
                (x + half, y + cell),
                (x, y + half),
                (x + half, y),
            ]
            draw.line(pts, fill=STROKE_RGBA, width=1)


def draw_arrow(draw: ImageDraw.ImageDraw, scale: int) -> None:
    cx, cy = ARROW_CENTER[0] * scale, ARROW_CENTER[1] * scale
    length = ARROW_LENGTH * scale
    thickness = ARROW_THICKNESS * scale
    head_w = ARROW_HEAD_WIDTH * scale
    head_h = ARROW_HEAD_HEIGHT * scale
    shaft_left = cx - length // 2
    shaft_right = cx + length // 2 - head_w
    draw.rectangle(
        [shaft_left, cy - thickness // 2, shaft_right, cy + thickness // 2],
        fill=ARROW_RGBA,
    )
    head_tip = (cx + length // 2, cy)
    head_top = (shaft_right, cy - head_h // 2)
    head_bottom = (shaft_right, cy + head_h // 2)
    draw.polygon([head_top, head_tip, head_bottom], fill=ARROW_RGBA)


def make(width: int, height: int, cell: int, scale: int, out: Path) -> None:
    img = Image.new("RGB", (width, height), BASE_COLOR).convert("RGBA")
    overlay = Image.new("RGBA", (width, height), (0, 0, 0, 0))
    draw = ImageDraw.Draw(overlay)
    draw_diamond_grid(draw, width, height, cell)
    draw_arrow(draw, scale)
    Image.alpha_composite(img, overlay).convert("RGB").save(out, "PNG", optimize=True)


def main() -> None:
    assets = Path(__file__).resolve().parent.parent / "assets"
    make(BASE_WIDTH, BASE_HEIGHT, CELL_BASE, 1, assets / "dmg-background.png")
    make(BASE_WIDTH * 2, BASE_HEIGHT * 2, CELL_BASE * 2, 2, assets / "dmg-background@2x.png")
    print(f"Wrote {assets / 'dmg-background.png'} and dmg-background@2x.png")


if __name__ == "__main__":
    main()
