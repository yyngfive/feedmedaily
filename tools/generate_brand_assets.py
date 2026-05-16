from __future__ import annotations

import base64
from io import BytesIO
from pathlib import Path

from PIL import Image

ROOT = Path(__file__).resolve().parents[1]
BRAND_DIR = ROOT / "assets" / "branding"
WEB_PUBLIC_DIR = ROOT / "web" / "public"
MASTER_PATH = BRAND_DIR / "feedmedaily-icon-master.png"


def crop_master(image: Image.Image) -> Image.Image:
    image = image.convert("RGBA")
    width, height = image.size
    pixels = image.load()

    left = width
    top = height
    right = -1
    bottom = -1

    for y in range(height):
        for x in range(width):
            r, g, b, a = pixels[x, y]
            if a == 0:
                continue
            # Treat near-white backdrop as empty so we keep only the icon tile.
            if r > 245 and g > 245 and b > 245:
                continue
            left = min(left, x)
            top = min(top, y)
            right = max(right, x)
            bottom = max(bottom, y)

    if right < left or bottom < top:
        raise RuntimeError(f"Could not detect icon bounds in {MASTER_PATH}")

    return image.crop((left, top, right + 1, bottom + 1))


def square_with_padding(image: Image.Image, size: int = 1024, padding: int = 48) -> Image.Image:
    inner = size - padding * 2
    image = image.copy()
    image.thumbnail((inner, inner), Image.Resampling.LANCZOS)

    canvas = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    offset_x = (size - image.width) // 2
    offset_y = (size - image.height) // 2
    canvas.paste(image, (offset_x, offset_y), image)
    return canvas


def write_embedded_svg(png_image: Image.Image, target: Path) -> None:
    buffer = BytesIO()
    png_image.save(buffer, format="PNG")
    encoded = base64.b64encode(buffer.getvalue()).decode("ascii")
    target.write_text(
        (
            '<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1024" '
            'viewBox="0 0 1024 1024">'
            f'<image href="data:image/png;base64,{encoded}" width="1024" height="1024"/>'
            "</svg>"
        ),
        encoding="utf-8",
    )


def main() -> None:
    BRAND_DIR.mkdir(parents=True, exist_ok=True)
    WEB_PUBLIC_DIR.mkdir(parents=True, exist_ok=True)

    if not MASTER_PATH.exists():
        raise FileNotFoundError(f"Missing bitmap master: {MASTER_PATH}")

    master = Image.open(MASTER_PATH)
    icon = square_with_padding(crop_master(master), size=1024, padding=36)

    icon.save(BRAND_DIR / "feedmedaily-icon.png")
    icon.save(
        BRAND_DIR / "feedmedaily.ico",
        sizes=[(256, 256), (128, 128), (64, 64), (48, 48), (32, 32), (16, 16)],
    )
    icon.save(BRAND_DIR / "feedmedaily.icns")

    favicon = icon.resize((64, 64), Image.Resampling.LANCZOS)
    favicon.save(WEB_PUBLIC_DIR / "favicon.png")

    write_embedded_svg(icon, BRAND_DIR / "feedmedaily-icon.svg")
    write_embedded_svg(icon, WEB_PUBLIC_DIR / "favicon.svg")


if __name__ == "__main__":
    main()
