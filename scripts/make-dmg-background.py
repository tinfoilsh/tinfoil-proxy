#!/usr/bin/env python3
"""Generate the Tinfoil Proxy DMG background (dark teal + diamond texture)."""
from __future__ import annotations

from pathlib import Path

from PIL import Image, ImageDraw

BASE_COLOR = (0, 68, 67)
STROKE_RGBA = (255, 255, 255, 18)
BASE_WIDTH = 540
BASE_HEIGHT = 380
CELL_BASE = 8


def make(width: int, height: int, cell: int, out: Path) -> None:
    img = Image.new("RGB", (width, height), BASE_COLOR).convert("RGBA")
    overlay = Image.new("RGBA", (width, height), (0, 0, 0, 0))
    draw = ImageDraw.Draw(overlay)
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
    Image.alpha_composite(img, overlay).convert("RGB").save(out, "PNG", optimize=True)


def main() -> None:
    assets = Path(__file__).resolve().parent.parent / "assets"
    make(BASE_WIDTH, BASE_HEIGHT, CELL_BASE, assets / "dmg-background.png")
    make(BASE_WIDTH * 2, BASE_HEIGHT * 2, CELL_BASE * 2, assets / "dmg-background@2x.png")
    print(f"Wrote {assets / 'dmg-background.png'} and dmg-background@2x.png")


if __name__ == "__main__":
    main()
