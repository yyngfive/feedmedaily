from __future__ import annotations

from pathlib import Path

from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parents[1]
BRAND_DIR = ROOT / "assets" / "branding"
WEB_PUBLIC_DIR = ROOT / "web" / "public"


def rounded_rect(draw: ImageDraw.ImageDraw, box: tuple[int, int, int, int], radius: int, fill):
    draw.rounded_rectangle(box, radius=radius, fill=fill)


def build_icon(size: int = 512) -> Image.Image:
    image = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(image)

    rounded_rect(draw, (24, 24, size - 24, size - 24), 104, "#0F766E")
    draw.ellipse((size - 188, 78, size - 74, 192), fill=(98, 229, 214, 36))
    draw.ellipse((70, size - 190, 210, size - 50), fill=(217, 255, 249, 18))

    for radius, color in [(72, "#9BF7EA"), (116, "#57E2D1"), (160, "#E2FFFA")]:
        draw.arc(
            (64, 144, 64 + radius * 2, 144 + radius * 2),
            start=270,
            end=360,
            fill=color,
            width=18,
        )
    draw.ellipse((128, 286, 164, 322), fill="#E2FFFA")

    rounded_rect(draw, (208, 114, 374, 352), 26, "#F7FEFD")
    rounded_rect(draw, (232, 156, 346, 170), 7, "#D5E8EA")
    rounded_rect(draw, (232, 190, 328, 204), 7, "#D5E8EA")
    rounded_rect(draw, (232, 234, 346, 252), 9, (15, 118, 110, 42))
    rounded_rect(draw, (232, 268, 346, 286), 9, (15, 118, 110, 42))
    rounded_rect(draw, (232, 302, 312, 320), 9, (15, 118, 110, 42))

    star = [
        (361, 136),
        (370, 154),
        (389, 157),
        (375, 171),
        (378, 190),
        (361, 181),
        (344, 190),
        (347, 171),
        (333, 157),
        (352, 154),
    ]
    draw.polygon(star, fill="#F6D264")

    return image


def main() -> None:
    BRAND_DIR.mkdir(parents=True, exist_ok=True)
    WEB_PUBLIC_DIR.mkdir(parents=True, exist_ok=True)

    icon = build_icon(512)
    icon.save(BRAND_DIR / "feedmedaily-icon.png")
    icon.save(
        BRAND_DIR / "feedmedaily.ico",
        sizes=[(256, 256), (128, 128), (64, 64), (48, 48), (32, 32), (16, 16)],
    )
    icon.resize((64, 64), Image.Resampling.LANCZOS).save(WEB_PUBLIC_DIR / "favicon.png")


if __name__ == "__main__":
    main()
