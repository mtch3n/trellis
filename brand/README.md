# Trellis brand assets

The supplied `reference/mark-only-4x.png` is the shape authority. Production assets are in `logo/` and `boards/`. Reference files were not changed.

## Fidelity and verification

- `mark.svg`: one charcoal-filled path, two closed subpaths, 17 cubic Bézier segments, square 800 × 800 viewBox; no filters, masks, embedded raster, or font dependency. Background is transparent for reuse; ivory is supplied by the PNG exports and favicon.
- Hand-authored trace was rendered with `rsvg-convert`, compared side by side, and refined. Dark silhouette intersection-over-union at grayscale threshold 120 improved from 98.95% to **99.42%**. This measures the thresholded silhouette, not an exact pixel match to the shaded source.
- The original raster's shading, soft edge halo and paper texture are intentionally absent. Flat fills use the supplied hex colors.
- `mark-inverse.svg` uses the identical path in ivory on a charcoal square.
- `lockup.svg` follows the reference's proportions and two-line “MAP KNOWLEDGE. / MAKE PROGRESS.” tagline. The reference font is unidentified: Liberation Sans is an approximation, with custom spacing and outlined lettering. The reference panel heading and divider are omitted from the reusable lockup.
- 16px and 32px exports were inspected enlarged using nearest-neighbor display. Both retain two separate lobes under 8-neighbor dark-pixel connectivity at threshold 145 (component areas: 45/50 and 176/194 pixels). No alternate small-size geometry was necessary.
- Built-in image generation created the three presentation layouts and a construction-sheet correction attempt. It introduced shape drift and shading. Final boards were reconstructed as SVG production artwork from those layouts, using exact copies of the master mark path, outlined typography and flat palette fills, then rendered with `rsvg-convert`. These are hybrid imagegen-directed/vector-finished boards, not untouched model output.
- All eight mark instances on the construction sheet and the single mark on each other board use byte-identical master path data. Normal raster antialiasing blends palette colors along edges; there are no gradients or shadows.
- Construction is an observational alignment grid, not a claim about the original designer's geometric method. The illustrated clearspace is a proposed 80 master-coordinate units (10% of the square canvas); the supplied kit does not define clearspace. Ramp labels describe actual 128/32/16px icon canvases at the construction sheet's native resolution.
- `verification/*-imagegen.png` are **unapproved process drafts** containing model drift, retained for provenance. Do not use their logos. The full prompts are in `verification/imagegen-prompts.md`.

## File manifest

Dimensions below are pixel dimensions for PNGs and intrinsic dimensions/viewBoxes for SVGs. Scripts and Markdown have no image dimensions. Existing references are listed separately by their folder.

| File | Dimensions |
| --- | --- |
| [boards/logo-construction.png](boards/logo-construction.png) | 1448 × 1086 |
| [boards/logo-construction.svg](boards/logo-construction.svg) | 1448 × 1086 |
| [boards/og-social.png](boards/og-social.png) | 1200 × 630 |
| [boards/og-social.svg](boards/og-social.svg) | 1200 × 630 |
| [boards/readme-header.png](boards/readme-header.png) | 1800 × 600 |
| [boards/readme-header.svg](boards/readme-header.svg) | 1800 × 600 |
| [logo/favicon.svg](logo/favicon.svg) | 800 × 800 |
| [logo/lockup.svg](logo/lockup.svg) | 660 × 940 |
| [logo/mark-1024.png](logo/mark-1024.png) | 1024 × 1024 |
| [logo/mark-128.png](logo/mark-128.png) | 128 × 128 |
| [logo/mark-16.png](logo/mark-16.png) | 16 × 16 |
| [logo/mark-256.png](logo/mark-256.png) | 256 × 256 |
| [logo/mark-32.png](logo/mark-32.png) | 32 × 32 |
| [logo/mark-48.png](logo/mark-48.png) | 48 × 48 |
| [logo/mark-512.png](logo/mark-512.png) | 512 × 512 |
| [logo/mark-64.png](logo/mark-64.png) | 64 × 64 |
| [logo/mark-inverse.svg](logo/mark-inverse.svg) | 800 × 800 |
| [logo/mark.svg](logo/mark.svg) | 800 × 800 |
| [reference/brand-kit-full.png](reference/brand-kit-full.png) | 1448 × 1086 |
| [reference/mark-only-4x.png](reference/mark-only-4x.png) | 800 × 800 |
| [reference/panel-icon-usage.png](reference/panel-icon-usage.png) | 430 × 160 |
| [reference/panel-palette.png](reference/panel-palette.png) | 640 × 340 |
| [reference/panel-primary-logo.png](reference/panel-primary-logo.png) | 330 × 470 |
| [verification/build-assets.py](verification/build-assets.py) | — |
| [verification/build-boards.py](verification/build-boards.py) | — |
| [verification/construction-imagegen.png](verification/construction-imagegen.png) | 1448 × 1086 |
| [verification/header-imagegen.png](verification/header-imagegen.png) | 2172 × 724 |
| [verification/icon-16-enlarged.png](verification/icon-16-enlarged.png) | 320 × 320 |
| [verification/icon-32-enlarged.png](verification/icon-32-enlarged.png) | 320 × 320 |
| [verification/imagegen-prompts.md](verification/imagegen-prompts.md) | — |
| [verification/lockup-render.png](verification/lockup-render.png) | 660 × 940 |
| [verification/mark-inverse-render.png](verification/mark-inverse-render.png) | 800 × 800 |
| [verification/mark-render-v1.png](verification/mark-render-v1.png) | 800 × 800 |
| [verification/mark-render.png](verification/mark-render.png) | 800 × 800 |
| [verification/small-icons.png](verification/small-icons.png) | 640 × 320 |
| [verification/social-imagegen.png](verification/social-imagegen.png) | 1730 × 909 |
| [verification/trace-comparison-v1.png](verification/trace-comparison-v1.png) | 1600 × 800 |
| [verification/trace-comparison.png](verification/trace-comparison.png) | 1600 × 800 |

## Rebuild

From the repository root, run `python brand/verification/build-assets.py` then `python brand/verification/build-boards.py`. Requires pycairo, Liberation Sans and rsvg-convert. Generated layouts and trace iteration snapshots are retained as evidence; these scripts rebuild the final assets, not the image generation runs.
