# owl identity pack

Brand mark and favicon assets for owl. The canonical source is
`owl-mark.svg`; everything else is a derived variant.

## Files

| File | Purpose |
|---|---|
| `owl-mark.svg` | Canonical mark, uses `currentColor` so it inherits text colour. Embed inline in HTML where you want the colour to follow the surrounding theme. |
| `owl-mark-light.svg` | Mark with `oklch(0.20 0.008 80)` baked in (owl's `--fg` in light theme). Use over light surfaces in contexts where `currentColor` won't propagate (e.g. README on GitHub light mode, social cards). |
| `owl-mark-dark.svg` | Mark with `oklch(0.94 0.004 80)` baked in (owl's `--fg` in dark theme). Use over dark surfaces in the same contexts. |
| `owl-mark-accent.svg` | Mark with `oklch(0.62 0.13 0)` baked in (owl's `--accent` magenta). Use for emphasis or accent-themed uses. |
| `favicon.svg` | Favicon variant with internal `prefers-color-scheme` media query. Modern browsers render light or dark automatically based on the user's OS theme. |
| `favicon-16.png` | 16×16 raster fallback. Rendered light. |
| `favicon-32.png` | 32×32 raster fallback (standard browser tab size). Rendered light. |
| `favicon-180.png` | 180×180 raster for `apple-touch-icon`. Rendered light. |

## Mark anatomy

Modern, simple strokes. Two-stroke pointed ears (outer vertical, inner
diagonal) sit above two circular eyes; small filled pupils centred in
the eyes; a small filled triangle as the beak between them. ViewBox is
`0 0 32 32`. Stroke width 1.5; round caps and joins.

## Re-rendering the PNGs

If you change `owl-mark.svg`, regenerate the PNG fallbacks. ImageMagick's
SVG renderer does not handle this file (strokes drop out), so we render
through a browser canvas. The recipe lives in commit history; in short:
load the SVG in a headless browser, draw to a `<canvas>` at the target
size, export as PNG via `toDataURL('image/png')`.
