#!/usr/bin/env python3
"""Design preview for MenuVex receipts.

This script mirrors the rules of the real renderer
(`src-tauri/src/print/layout.rs` + `src-tauri/src/print/mod.rs`) so the printed design can be
reviewed and regression-checked without a printer or a Rust toolchain. It shapes and measures
text with HarfBuzz on the *same* bundled font the agent embeds (cosmic-text/rustybuzz does the
equivalent work in Rust) and rasterizes a 1-bit receipt exactly like the ESC/POS `GS v 0` data.

`--design current` reproduces the previous design (`Document::lines()` rendered left aligned,
which is what the shipped agent prints today) so the two layouts can be compared.

The preview is an approximation of the hardware output: it exists to review alignment, columns,
separators and spacing, not to certify a printer model.

Usage:
    pip install uharfbuzz freetype-py pillow
    python3 tools/receipt-preview/preview.py [--out dist/receipt-preview] [--design both]

Writes <out>/invoice.png, <out>/kitchen.png and, with `--design both`, the `*-compare.png`
before/after sheets. Before rendering it checks its invariants (money grouping, planner
labels and sizes against `src-tauri/src/print/layout.rs`, font coverage of every planned
character, and the 4096 row protocol limit) and exits non-zero on a violation.

This is a design review tool: a green run proves the layout rules are consistent, not that a
physical printer produced good paper.
"""

from __future__ import annotations

import argparse
import os
import sys

try:
    import freetype
    import uharfbuzz as hb
    from PIL import Image
except ModuleNotFoundError as missing:  # keep the failure actionable for an operator
    print(f"the receipt preview needs {missing.name!r}; install the requirements first:", file=sys.stderr)
    print("  python3 -m pip install uharfbuzz freetype-py pillow", file=sys.stderr)
    raise SystemExit(2) from missing

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
FONT_PATH = os.path.join(ROOT, "src-tauri", "assets", "NotoSansArabic-Regular.ttf")

# --- rules mirrored from src-tauri/src/print/layout.rs --------------------------------
STORE = 132
TOTAL = 120
DETAIL = 84
LEADING = 1.45
LEGACY_LEADING = 1.5
INK = 100  # of 255
MAX_ROWS = 4096
RULE_GLYPH = "─"  # the previous design used U+2500; it is NOT part of the bundled font


def scaled(size: int, percent: int) -> int:
    return max(8, min(96, size * percent // 100))


def _is_rtl_char(char: str) -> bool:
    code = ord(char)
    return 0x0590 <= code <= 0x08FF or 0xFB1D <= code <= 0xFDFF or 0xFE70 <= code <= 0xFEFF


def is_rtl(text: str) -> bool:
    for c in text:
        if _is_rtl_char(c):
            return True
        if c.isascii() and c.isalpha():
            return False
    return False


def money(value: int) -> str:
    digits = str(value)
    out = []
    for index, c in enumerate(digits):
        if index > 0 and (len(digits) - index) % 3 == 0:
            out.append(",")
        out.append(c)
    return "".join(out)


# --- design plans ----------------------------------------------------------------------
def plan(doc: dict, font_size: int) -> list[dict]:
    """Mirror of `layout::plan` — the new design."""
    if doc["type"] == "invoice":
        return invoice(doc["data"], font_size)
    return receipt(doc["lines"], font_size)


def invoice(data: dict, font_size: int) -> list[dict]:
    items: list[dict] = []
    store = data["storeName"].strip()
    if store:
        items.append({"kind": "text", "text": store, "align": "center", "size": scaled(font_size, STORE), "bold": True})
    order = data["orderNumber"].strip()
    if order:
        items.append({"kind": "text", "text": f"شماره سفارش: {order}", "align": "center", "size": font_size, "bold": False})
    items.append({"kind": "rule"})
    items.append({"kind": "row", "right": "کالا", "left": "مبلغ", "size": font_size, "bold": True})
    items.append({"kind": "rule"})
    for item in data["items"]:
        items.append({"kind": "row", "right": item["name"], "left": money(item["quantity"] * item["unitPrice"]), "size": font_size, "bold": False})
        if item["quantity"] > 1:
            items.append({"kind": "text", "text": f"{money(item['quantity'])} × {money(item['unitPrice'])}", "align": "right", "size": scaled(font_size, DETAIL), "bold": False})
    items.append({"kind": "rule"})
    items.append({"kind": "row", "right": "جمع کل", "left": money(data["total"]), "size": scaled(font_size, TOTAL), "bold": True})
    footer = data["footer"].strip()
    if footer:
        items.append({"kind": "space", "dots": font_size // 2})
        items.append({"kind": "text", "text": footer, "align": "center", "size": scaled(font_size, DETAIL), "bold": False})
    return items


def receipt(lines: list[str], font_size: int) -> list[dict]:
    items: list[dict] = []
    for line in lines:
        text = line.rstrip()
        if not text.strip():
            items.append({"kind": "space", "dots": font_size // 2})
            continue
        items.append({"kind": "text", "text": text, "align": "right" if is_rtl(text) else "left", "size": font_size, "bold": False})
    return items


def plain_lines(doc: dict) -> list[str]:
    """Mirror of `Document::lines()` — the plain text the previous design printed."""
    if doc["type"] == "receipt":
        return list(doc["lines"])
    data = doc["data"]
    lines = [
        data["storeName"],
        f"سفارش: {data['orderNumber']}",
        RULE_GLYPH * 16,
    ]
    for item in data["items"]:
        lines.append(item["name"])
        lines.append(
            f"{item['quantity']} × {item['unitPrice']} = {item['quantity'] * item['unitPrice']}"
        )
    lines.append(RULE_GLYPH * 16)
    lines.append(f"جمع: {data['total']}")
    lines.append(data["footer"])
    return lines


def current_plan(doc: dict, font_size: int) -> list[dict]:
    """The design shipped before the fix: stacked plain text, all lines left aligned."""
    return [
        {"kind": "text", "text": line, "align": "left", "size": font_size, "bold": False}
        for line in plain_lines(doc)
    ]


# --- shaping (HarfBuzz + simple receipt BiDi) ------------------------------------------
class Shaper:
    def __init__(self, path: str) -> None:
        with open(path, "rb") as handle:
            self.data = handle.read()
        self.face = hb.Face(self.data)
        self.font = hb.Font(self.face)
        self.upem = self.face.upem
        self.free = freetype.Face(path)

    def runs(self, text: str) -> list[tuple[str, str]]:
        """Split `text` into visual-order (run_text, direction) pairs.

        Receipt lines are simple: Persian segments, number/Latin segments and their punctuation.
        The real agent runs the full Unicode BiDi algorithm inside cosmic-text; this mirror uses
        the same base-direction rule and lays segments out right-to-left for Persian lines.
        """
        base_rtl = is_rtl(text)
        segments: list[list[str]] = []
        for char in text:
            if _is_rtl_char(char):
                kind = "rtl"
            elif char.isdigit() or (char.isascii() and char.isalpha()):
                kind = "ltr"
            else:
                kind = None
            if kind is None:
                kind = segments[-1][0] if segments else ("rtl" if base_rtl else "ltr")
            if segments and segments[-1][0] == kind:
                segments[-1][1] += char
            else:
                segments.append([kind, char])
        ordered = list(reversed(segments)) if base_rtl else segments
        return [(run, kind) for kind, run in ordered]

    def shape(self, text: str, size: int, direction: str) -> list[tuple[int, float, float]]:
        """Shape one run: [(glyph_id, x_offset, x_advance)] in pixels, visual order."""
        buf = hb.Buffer()
        buf.add_str(text)
        buf.direction = direction
        buf.script = "Arab" if direction == "rtl" else "Latn"
        buf.language = "fa"
        hb.shape(self.font, buf, {"kern": True, "liga": True})
        scale = size / self.upem
        return [
            (info.codepoint, pos.x_offset * scale, pos.x_advance * scale)
            for info, pos in zip(buf.glyph_infos, buf.glyph_positions)
        ]

    def width(self, text: str, size: int) -> float:
        """Natural (unwrapped) width in dots, mirroring `Renderer::measure`."""
        total = 0.0
        for run_text, direction in self.runs(text):
            buf = hb.Buffer()
            buf.add_str(run_text)
            buf.direction = direction
            buf.language = "fa"
            hb.shape(self.font, buf, {"kern": True, "liga": True})
            total += sum(p.x_advance for p in buf.glyph_positions) * size / self.upem
        return total

    def glyph_bitmap(self, glyph_id: int, size: int):
        self.free.set_pixel_sizes(0, size)
        self.free.load_glyph(glyph_id, freetype.FT_LOAD_RENDER)
        bitmap = self.free.glyph.bitmap
        rows = []
        for y in range(bitmap.rows):
            start = y * bitmap.pitch
            rows.append(bitmap.buffer[start : start + bitmap.width])
        return rows, self.free.glyph.bitmap_left, self.free.glyph.bitmap_top

    def ascent(self, size: int) -> int:
        self.free.set_pixel_sizes(0, size)
        return self.free.size.ascender >> 6


# --- raster (mirror of the Rust `Paper`) ----------------------------------------------
class Paper:
    def __init__(self, width: int) -> None:
        self.width = width
        self.stride = (width + 7) // 8
        self.margin = max(2, min(24, round(width * 0.02)))
        self.bits = [0] * (self.stride * MAX_ROWS)
        self.y = 2.0

    def usable(self) -> int:
        return self.width - self.margin * 2

    def plot(self, x: int, y: int) -> None:
        if x < 0 or y < 0 or x >= self.width or y >= MAX_ROWS:
            return
        self.bits[y * self.stride + x // 8] |= 0x80 >> (x % 8)

    def space(self, dots: int) -> None:
        self.y += dots

    def rule(self, thickness: int) -> None:
        self.y += 3.0
        top = round(self.y)
        for row in range(top, min(top + thickness, MAX_ROWS)):
            for x in range(self.margin, self.width - self.margin):
                self.plot(x, row)
        self.y = min(top + thickness, MAX_ROWS) + 3.0

    def image(self) -> Image.Image:
        """Ink is black: ESC/POS raster bits are 1 for a printed dot."""
        height = max(1, min(MAX_ROWS, round(self.y + 0.5)))
        payload = bytes(self.bits[: self.stride * height])
        image = Image.frombytes("1", (self.width, height), payload)
        return image.convert("L").point(lambda value: 255 - value)


class Preview:
    def __init__(self, shaper: Shaper, width: int, font_size: int, design: str) -> None:
        self.shaper = shaper
        self.paper = Paper(width)
        self.font_size = font_size
        self.leading = LEADING if design == "new" else LEGACY_LEADING

    def run(self, doc: dict) -> Image.Image:
        items = plan(doc, self.font_size) if self.leading == LEADING else current_plan(doc, self.font_size)
        for item in items:
            if item["kind"] == "text":
                self.text(item["text"], item["size"], item["align"], item["bold"])
            elif item["kind"] == "row":
                self.row(item["right"], item["left"], item["size"], item["bold"])
            elif item["kind"] == "rule":
                self.paper.rule(max(1, self.font_size // 12))
            else:
                self.paper.space(item["dots"])
        return self.paper.image()

    def text(self, text: str, size: int, align: str, bold: bool) -> None:
        if not text.strip():
            self.paper.space(size // 2)
            return
        self.draw(text, size, align, bold)

    def row(self, right: str, left: str, size: int, bold: bool) -> None:
        if not left:
            self.draw(right, size, "right", bold)
            return
        gap = round(size * 0.75)
        if self.shaper.width(right, size) + self.shaper.width(left, size) + gap <= self.paper.usable():
            top = self.paper.y
            self.draw(right, size, "right", bold)
            after = self.paper.y
            self.paper.y = top
            self.draw(left, size, "left", bold)
            self.paper.y = max(self.paper.y, after)
        else:
            self.draw(right, size, "right", bold)
            self.draw(left, size, "left", bold)

    def draw(self, text: str, size: int, align: str, bold: bool) -> None:
        lines = self.wrap(text, size)
        left_edge = self.paper.margin
        right_edge = self.paper.width - self.paper.margin
        line_height = size * self.leading
        for number, (runs, width) in enumerate(lines):
            top = self.paper.y + number * line_height
            if align == "right":
                x = right_edge - width
            elif align == "center":
                x = (self.paper.width - width) / 2
            else:
                x = left_edge
            x = max(x, left_edge)
            baseline = top + self.shaper.ascent(size)
            pen = x
            for run_text, direction in runs:
                for glyph_id, offset, advance in self.shaper.shape(run_text, size, direction):
                    rows, bitmap_left, bitmap_top = self.shaper.glyph_bitmap(glyph_id, size)
                    for row_index, row in enumerate(rows):
                        for col, coverage in enumerate(row):
                            if coverage < INK:
                                continue
                            px = int(pen + offset + bitmap_left) + col
                            py = int(baseline - bitmap_top) + row_index
                            self.paper.plot(px, py)
                            if bold:
                                self.paper.plot(px + 1, py)
                    pen += advance
        self.paper.y += line_height * len(lines)

    def wrap(self, text: str, size: int) -> list[tuple[list[tuple[str, str]], float]]:
        """Greedy word wrapping mirroring cosmic-text `Wrap::WordOrGlyph` closely enough."""
        limit = self.paper.usable()
        out: list[tuple[list[tuple[str, str]], float]] = []
        for hard in text.split("\n"):
            words = hard.split(" ")
            current: list[str] = []
            for word in words:
                candidate = " ".join(current + [word])
                if current and self.shaper.width(candidate, size) > limit:
                    out.append(self.measure_line(" ".join(current), size))
                    current = [word]
                else:
                    current.append(word)
            out.append(self.measure_line(" ".join(current), size))
        return [line for line in out if line[0]] or [([], 0.0)]

    def measure_line(self, text: str, size: int) -> tuple[list[tuple[str, str]], float]:
        runs = self.shaper.runs(text)
        width = sum(self.shaper.width(run, size) for run, _ in runs)
        return runs, width


SAMPLES: dict[str, dict] = {
    "invoice": {
        "type": "invoice",
        "data": {
            "storeName": "کافه ونک",
            "orderNumber": "1842",
            "items": [
                {"name": "اسپرسو دوبل", "quantity": 2, "unitPrice": 120000},
                {"name": "کیک شکلاتی", "quantity": 1, "unitPrice": 95000},
                {"name": "آب معدنی", "quantity": 3, "unitPrice": 8000},
            ],
            "total": 359000,
            "footer": "با سپاس از خرید شما",
        },
    },
    "kitchen": {
        "type": "receipt",
        "lines": ["میز ۴", "اسپرسو دوبل ×۲", "کیک شکلاتی ×۱", "— بدون شکر —"],
    },
}


def sheet(left: Image.Image, right: Image.Image, gap: int = 24) -> Image.Image:
    """Place the previous and the new design next to each other on one sheet."""
    height = max(left.height, right.height)
    width = left.width + gap + right.width
    canvas = Image.new("L", (width, height), 255)
    canvas.paste(Image.new("L", (left.width, left.height), 255), (0, 0))
    canvas.paste(left, (0, 0))
    canvas.paste(right, (left.width + gap, 0))
    return canvas


# --- invariant checks (CI) -------------------------------------------------------------
LAYOUT_RS = os.path.join(ROOT, "src-tauri", "src", "print", "layout.rs")


class CheckFailure(Exception):
    pass


def require(condition: bool, message: str) -> None:
    if not condition:
        raise CheckFailure(message)


def check_money() -> None:
    for value, expected in [(0, "0"), (999, "999"), (1000, "1,000"), (240000, "240,000"), (9000000000000, "9,000,000,000,000")]:
        require(money(value) == expected, f"money({value}) == {money(value)!r}, expected {expected!r}")


def check_plan(samples: dict[str, dict], font_size: int = 24) -> None:
    items = plan(samples["invoice"], font_size)
    first = items[0]
    require(first["kind"] == "text" and first["align"] == "center", "store name must be centered")
    require(first["size"] == scaled(font_size, STORE) and first["bold"], "store name must be larger and bold")
    require(any(item["kind"] == "rule" for item in items), "invoice must contain a separator rule")
    require(
        {"kind": "row", "right": "کالا", "left": "مبلغ", "size": font_size, "bold": True} in items,
        "invoice must have the right/left column header",
    )
    amounts = {item["left"] for item in items if item["kind"] == "row"}
    require("240,000" in amounts, "line amounts must be grouped")
    total = [item for item in items if item["kind"] == "row" and item["right"] == "جمع کل"]
    require(len(total) == 1, "invoice must have exactly one total row")
    require(total[0]["size"] == scaled(font_size, TOTAL) and total[0]["bold"], "total must be emphasised")
    require(
        all(item["kind"] != "text" or "1 ×" not in item["text"] for item in items),
        "unit price breakdown belongs to quantity > 1 only",
    )
    lines = [line["text"] for line in plan(samples["kitchen"], font_size)]
    require(lines[0].startswith("میز") and lines[1].startswith("اسپرسو"), "receipt lines must keep their order")


def check_layout_source() -> None:
    """The mirror must not drift from the real planner."""
    with open(LAYOUT_RS, encoding="utf-8") as handle:
        source = handle.read()
    for name, value in (("STORE", STORE), ("TOTAL", TOTAL), ("DETAIL", DETAIL)):
        require(
            f"const {name}: u32 = {value};" in source,
            f"layout.rs {name} no longer matches the preview constant {value}",
        )
    for label in ["شماره سفارش: ", "کالا", "مبلغ", "جمع کل"]:
        require(label in source, f"layout.rs no longer contains the label {label!r}")
    require(
        '"────────────────"' not in source,
        "layout.rs must not fall back to a U+2500 separator glyph; draw the rule as pixels",
    )


def check_font_coverage(shaper: Shaper, samples: dict[str, dict], font_size: int = 24) -> None:
    """Every planned character must have a glyph; the old U+2500 design did not."""
    texts: list[str] = []
    for doc in samples.values():
        for item in plan(doc, font_size):
            if item["kind"] == "text":
                texts.append(item["text"])
            elif item["kind"] == "row":
                texts.extend([item["right"], item["left"]])
    for text in texts:
        for char in text:
            if char.isspace():
                continue
            require(
                shaper.free.get_char_index(ord(char)) != 0,
                f"bundled font has no glyph for {char!r} (U+{ord(char):04X}) used by {text!r}",
            )
    require(
        shaper.free.get_char_index(0x2500) == 0,
        "the bundled font now has U+2500: reconsider the pixel rule decision in docs/escpos.md",
    )


def check(shaper: Shaper, samples: dict[str, dict], width: int, font_size: int) -> None:
    check_money()
    check_plan(samples, font_size)
    check_layout_source()
    check_font_coverage(shaper, samples, font_size)
    require(Paper(128).usable() > 0, "the side margins consume the narrowest supported width")
    for name, doc in samples.items():
        drawn = Preview(shaper, width, font_size, "new").run(doc)
        require(
            drawn.height <= MAX_ROWS,
            f"{name} renders {drawn.height} rows, over the {MAX_ROWS} row protocol limit",
        )


def main() -> int:
    parser = argparse.ArgumentParser(description="Render MenuVex receipt design previews")
    parser.add_argument("--out", default=os.path.join(ROOT, "dist", "receipt-preview"))
    parser.add_argument("--width", type=int, default=576, help="printer dots (384 for 58mm)")
    parser.add_argument("--font-size", type=int, default=24)
    parser.add_argument("--design", choices=["current", "new", "both"], default="both")
    parser.add_argument("--scale", type=int, default=2, help="view scale of the saved PNGs")
    args = parser.parse_args()
    if not os.path.exists(FONT_PATH):
        print(f"missing bundled font: {FONT_PATH}", file=sys.stderr)
        return 2
    shaper = Shaper(FONT_PATH)
    try:
        check(shaper, SAMPLES, args.width, args.font_size)
    except CheckFailure as failure:
        print(f"receipt design check failed: {failure}", file=sys.stderr)
        return 1
    except Exception as failure:  # a broken check is a failed check
        print(f"receipt design check errored: {failure!r}", file=sys.stderr)
        return 1
    print("design checks passed (money grouping, planner labels/sizes, font coverage, row limit)")
    os.makedirs(args.out, exist_ok=True)
    for name, doc in SAMPLES.items():
        images = {}
        for design in (["current", "new"] if args.design == "both" else [args.design]):
            paper = Preview(shaper, args.width, args.font_size, design).run(doc)
            images[design] = paper
            rows = paper.height
            state = "ok" if rows <= MAX_ROWS else "TOO TALL"
            suffix = "" if args.design != "both" else f"-{design}"
            target = os.path.join(args.out, f"{name}{suffix}.png")
            view = paper.resize((paper.width * args.scale, paper.height * args.scale), Image.NEAREST)
            view.save(target)
            print(f"{name}{suffix}: {paper.width}x{rows} dots ({state} for the {MAX_ROWS} row limit) -> {target}")
        if args.design == "both":
            combined = sheet(images["current"], images["new"])
            target = os.path.join(args.out, f"{name}-compare.png")
            view = combined.resize((combined.width * args.scale, combined.height * args.scale), Image.NEAREST)
            view.save(target)
            print(f"{name}-compare: previous (left) vs new design (right) -> {target}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
