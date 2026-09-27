# ESC/POS and Persian rendering

Backend owns all byte generation. PWA sends validated semantic `invoice` or `receipt`; raw byte injection/control characters are rejected. Rust encoder supports initialize, ASCII text, bold, alignment, font size, line/feed, cut, QR Model 2, Code128 subset B, fixed-width ASCII tables, monochrome raster and drawer pulse. These are internal Rust APIs; only rendered semantic documents are remotely available in v1.

Persian pipeline:

```text
UTF-8 semantic lines
 → cosmic-text font layout
 → rustybuzz Arabic shaping + Unicode BiDi/line breaking
 → Swash glyph rasterization
 → alpha threshold into 1-bit monochrome bitmap
 → GS v 0 raster stripes (≤128 rows each)
 → feed + optional cut
```

Noto Sans Arabic Regular is embedded, including SIL Open Font License in `src-tauri/assets/OFL-NotoSansArabic.txt`. Asset was obtained from `@expo-google-fonts/noto-sans-arabic@0.4.3`, regular TTF, unmodified; use a reviewed font update process. Selected local system fonts may be used by profile name, never by PWA-supplied filesystem paths. Missing family/glyphs produce `FONT_UNAVAILABLE` / `FONT_GLYPH_MISSING` instead of silently outputting tofu.

The renderer wraps to actual dot width and limits output to 4096 rows. It thresholds alpha at 100 (out of 255); it is not grayscale photography dithering. Invoice layout is a simple stacked receipt, with item name then quantity × unit price = line amount; no business accounting is inferred. Paper mm is metadata, width dots controls output. Text scale uses pixels; no native Persian codepage is required. Mixed Persian/Latin numbers and punctuation must pass visual hardware acceptance.

Encoder tests assert initialization/control sequences and raster header/size validation. Full render hardware/screenshot baselines must be added after native compilation is available; the current live test checks actual encoding/transport handoff, not visual typography quality. Font/cosmic-text upgrades can change line wrapping: treat them as output compatibility changes.

Cash drawer is internal-only, fixed conservative example pulse (pin 0, on/off timings). It is **not** called by invoice/test documents. Exposing drawer, arbitrary raster images or QR semantic elements requires an explicit versioned schema and device authorization review.
