#!/usr/bin/env python3
"""
Generates the Braela launcher icons.

The mark is a PRIZE WHEEL, not a lightning bolt: alternating wedges, a hub, and
a pointer at twelve o'clock. A logo should say what the product is at 48px in a
crowded launcher, and a bolt says "electricity" or "fast" — it could belong to
any app.

Drawn with primitives rather than shipped as artwork so there is one definition
of the shape, and it can be regenerated at any size without an asset pipeline.
Supersampled 8x and downscaled, which is how you get clean curves out of PIL.
"""

import math
from PIL import Image, ImageDraw

AMBER_HI = (255, 138, 61)
AMBER_LO = (217, 79, 18)
WHITE = (255, 255, 255)
INK = (24, 16, 20)
# A deep burnt amber for the alternate wedges. Pure black against white read
# as a hazard symbol rather than a prize wheel; staying inside the brand's own
# warm range keeps it a game.
EMBER = (138, 44, 8)

SS = 8  # supersample factor


def rounded_gradient_bg(size, radius_frac=0.22):
    """Amber diagonal gradient on a rounded square — the brand's own surface."""
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    grad = Image.new("RGBA", (size, size))
    px = grad.load()
    for y in range(size):
        for x in range(size):
            t = (x + y) / (2 * size - 2)
            px[x, y] = (
                round(AMBER_HI[0] + (AMBER_LO[0] - AMBER_HI[0]) * t),
                round(AMBER_HI[1] + (AMBER_LO[1] - AMBER_HI[1]) * t),
                round(AMBER_HI[2] + (AMBER_LO[2] - AMBER_HI[2]) * t),
                255,
            )
    mask = Image.new("L", (size, size), 0)
    ImageDraw.Draw(mask).rounded_rectangle(
        [0, 0, size - 1, size - 1], radius=int(size * radius_frac), fill=255
    )
    img.paste(grad, (0, 0), mask)
    return img


def wheel_mark(size, wedges=8, fg=WHITE, alt=EMBER, pointer=True):
    """The wheel itself, transparent background.

    A DISC with a RIM, not loose wedges. An earlier version drew alternating
    white wedges straight onto the background, and without a bounding rim they
    read as a fan's blades rather than a wheel. The rim is the single element
    that makes the shape unambiguous, so it is drawn last and drawn solid.

    Two tones only. Alternating white against a semi-transparent white sat too
    close to the amber behind it and closed up into a blob at 48px.
    """
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    c = size / 2
    r = size * 0.345

    box = [c - r, c - r, c + r, c + r]

    # Solid face first, so every wedge sits inside a disc.
    d.ellipse(box, fill=fg)

    step = 360 / wedges
    for i in range(wedges):
        # -90 so a wedge boundary sits under the pointer, matching the real
        # wheel's geometry where segment 1 begins at twelve o'clock.
        if i % 2:
            a0 = i * step - 90
            d.pieslice(box, a0, a0 + step, fill=alt)

    # The rim. Without this the wedges read as blades.
    rim = max(2, int(size * 0.030))
    d.ellipse(box, outline=alt, width=rim)

    # Hub: dark disc, light centre — a wheel turns about something.
    hr = r * 0.34
    d.ellipse([c - hr, c - hr, c + hr, c + hr], fill=alt)
    dr = hr * 0.42
    d.ellipse([c - dr, c - dr, c + dr, c + dr], fill=fg)

    if pointer:
        # Deliberately chunky. A fine pointer is the first thing to disappear
        # at launcher size, and without it this is just a pie chart.
        pw = size * 0.098
        top = c - r - size * 0.080
        tip = c - r + size * 0.070
        d.polygon([(c, tip + size * 0.012), (c - pw * 1.20, top - size * 0.012),
                   (c + pw * 1.20, top - size * 0.012)], fill=alt)
        d.polygon([(c, tip), (c - pw, top), (c + pw, top)], fill=fg)
    return img


def icon(size):
    big = size * SS
    bg = rounded_gradient_bg(big)
    mark = wheel_mark(big)
    bg.alpha_composite(mark)
    return bg.resize((size, size), Image.LANCZOS)


if __name__ == "__main__":
    import sys, os

    out = sys.argv[1] if len(sys.argv) > 1 else "."
    # Android launcher densities.
    for name, px in [
        ("mdpi", 48), ("hdpi", 72), ("xhdpi", 96),
        ("xxhdpi", 144), ("xxxhdpi", 192),
    ]:
        p = os.path.join(out, f"mipmap-{name}")
        os.makedirs(p, exist_ok=True)
        icon(px).save(os.path.join(p, "ic_launcher.png"))
        print(f"  mipmap-{name}/ic_launcher.png  {px}x{px}")

    # Play Store listing and the web favicon fallback.
    icon(512).save(os.path.join(out, "icon-512.png"))
    wheel_mark(512, fg=WHITE, alt=EMBER).save(
        os.path.join(out, "mark-512.png")
    )
    print("  icon-512.png, mark-512.png")
