/**
 * The Braela mark: a prize wheel.
 *
 * Inline SVG rather than an image file — it is ~700 bytes, scales to any size,
 * and costs no extra request. The geometry matches `app/lib/ui/brand.dart` and
 * `brand/make_icons.py` (which generates the launcher icon), so the three
 * cannot drift.
 *
 * A lightning bolt stood here first. It said "electricity" or "fast" and could
 * have belonged to any app; a logo should say what the product is.
 */

const EMBER = '#8A2C08';
const FACE = '#ffffff';

/**
 * @param size   rendered px
 * @param onDark true on a dark surface (wedges use the lighter amber), false
 *               when sitting on the brand gradient
 */
export function brandMarkSvg(size = 28, onDark = false): string {
  const alt = onDark ? '#D94F12' : EMBER;
  // viewBox 100x100; r = 34.5 matches the painter's 0.345 of the box.
  const c = 50;
  const r = 34.5;
  const step = 360 / 8;

  // Alternating wedges as pie slices, -90deg so a boundary sits under the
  // pointer — the same convention the real wheel uses.
  const wedges: string[] = [];
  for (let i = 1; i < 8; i += 2) {
    const a0 = (i * step - 90) * (Math.PI / 180);
    const a1 = ((i + 1) * step - 90) * (Math.PI / 180);
    const x0 = c + r * Math.cos(a0), y0 = c + r * Math.sin(a0);
    const x1 = c + r * Math.cos(a1), y1 = c + r * Math.sin(a1);
    wedges.push(`M${c} ${c}L${x0.toFixed(2)} ${y0.toFixed(2)}A${r} ${r} 0 0 1 ${x1.toFixed(2)} ${y1.toFixed(2)}Z`);
  }

  const hr = r * 0.34;
  const pw = 9.8, top = c - r - 8, tip = c - r + 7;

  return `<svg viewBox="0 0 100 100" width="${size}" height="${size}" aria-hidden="true" focusable="false">
<circle cx="${c}" cy="${c}" r="${r}" fill="${FACE}"/>
<path d="${wedges.join('')}" fill="${alt}"/>
<circle cx="${c}" cy="${c}" r="${r}" fill="none" stroke="${alt}" stroke-width="3"/>
<circle cx="${c}" cy="${c}" r="${hr.toFixed(2)}" fill="${alt}"/>
<circle cx="${c}" cy="${c}" r="${(hr * 0.42).toFixed(2)}" fill="${FACE}"/>
<path d="M${c} ${tip + 1.2}L${c - pw * 1.2} ${top - 1.2}L${c + pw * 1.2} ${top - 1.2}Z" fill="${alt}"/>
<path d="M${c} ${tip}L${c - pw} ${top}L${c + pw} ${top}Z" fill="${FACE}"/>
</svg>`;
}
