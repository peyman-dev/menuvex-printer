# Receipt design preview

Renders the printed design of `invoice` and `receipt` documents to PNG **without a printer**, so a
layout change can be reviewed and regression-checked before it reaches a café. It mirrors
`src-tauri/src/print/layout.rs` and `src-tauri/src/print/mod.rs` (1-bit raster, same bundled Noto
Sans Arabic, same percentages) with HarfBuzz + FreeType; cosmic-text/rustybuzz does the equivalent
work inside the agent.

```sh
python3 -m pip install uharfbuzz freetype-py pillow
npm run preview:receipt                     # writes dist/receipt-preview/*.png
npm run preview:receipt -- --width 384      # 58mm paper
npm run preview:receipt -- --design new     # only the current design
```

Outputs: `invoice-*.png`, `kitchen-*.png` and `invoice-compare.png` / `kitchen-compare.png`
(previous design on the left, current design on the right). Ink is black; a printed dot is a black
pixel, and one image pixel is one printer dot before the 2x zoom.

The script **checks its invariants before rendering** and exits non-zero if one is violated:

- money grouping (`240000` → `240,000`, shown as Persian digits `۲۴۰,۰۰۰` with the
  adapter-supplied currency word only) and the planner labels/sizes/emphasis of the app-template
  design (slip row, four column table on 80mm, stacked items on 58mm, dashed note frame, brand
  line) against the real `layout.rs` (both must not drift apart),
- free-form receipt hints (`[center]`, two-to-four `|` columns and separator runs) map to centered text, bounded cells and pixel rules, and every planned character has a glyph in the bundled font,
- every sample stays inside the 4096 row protocol limit.

A free-form receipt can use `----------------` for a pixel rule, `[center] text` for a centered line,
`item | quantity | amount` for right-to-left columns, and an empty line for spacing. Test tickets
also print the configured paper millimeters/dots; this is a profile check, not physical-width sensing.

This is a review tool, not a hardware test: it proves the layout rules are consistent, not that a
specific printer produced good paper. Physical acceptance (alignment, darkness, cutter, paper
feed) still happens on the real model — see [`docs/hardware-testing.md`](../../docs/hardware-testing.md).
