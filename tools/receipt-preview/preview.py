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
BRAND = 66
TABLE_MIN_DOTS = 464
BRAND_LINE = "POWERED BY MENUVEX.IR"
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


FA_DIGITS = {ord("0") + i: chr(0x06F0 + i) for i in range(10)}


def fa_digits(text: str) -> str:
    return text.translate(FA_DIGITS)


def amount(value: int, currency: str) -> str:
    grouped = fa_digits(money(value))
    currency = currency.strip()
    return f"{grouped} {currency}" if currency else grouped


# --- design plans ----------------------------------------------------------------------
def plan(doc: dict, font_size: int, width_dots: int) -> list[dict]:
    """Mirror of `layout::plan` — the new design."""
    if doc["type"] == "invoice":
        return invoice(doc["data"], font_size, width_dots)
    return receipt(doc["lines"], font_size)


def _text(text: str, align: str, size: int, bold: bool) -> dict:
    return {"kind": "text", "text": text, "align": align, "size": size, "bold": bold}


def _row(right: str, left: str, size: int, bold: bool) -> dict:
    return {"kind": "row", "right": right, "left": left, "size": size, "bold": bold}


TABLE_HEADER = ["شرح کالا", "تعداد", "قیمت واحد", "جمع"]
TABLE_WEIGHTS = [34, 12, 27, 27]
TABLE_ALIGNS = ["right", "center", "center", "left"]


def invoice(data: dict, font_size: int, width_dots: int) -> list[dict]:
    detail = scaled(font_size, DETAIL)
    items: list[dict] = []
    store = data["storeName"].strip()
    if store:
        items.append(_text(store, "center", scaled(font_size, STORE), True))
    address = data.get("address", "").strip()
    if address:
        items.append(_text(address, "center", detail, False))
    phone = data.get("phone", "").strip()
    if phone:
        items.append(_text(f"تلفن: {fa_digits(phone)}", "center", detail, False))
    items.append({"kind": "rule"})
    title = data.get("title", "").strip() or "فاکتور فروش"
    order = data["orderNumber"].strip()
    if order:
        items.append(_row(title, f"فیش {fa_digits(order)}", font_size, True))
    else:
        items.append(_text(title, "center", font_size, True))
    items.append({"kind": "rule"})
    details = 0
    for label, key in [("تاریخ", "date"), ("وضعیت", "status"), ("نوع سفارش", "orderType"), ("میز", "table")]:
        value = data.get(key, "").strip()
        if value:
            items.append(_row(label, fa_digits(value), font_size, False))
            details += 1
    if details:
        items.append({"kind": "rule"})
    if width_dots >= TABLE_MIN_DOTS:
        cells = [
            {"text": text, "align": align, "weight": weight}
            for text, weight, align in zip(TABLE_HEADER, TABLE_WEIGHTS, TABLE_ALIGNS)
        ]
        items.append({"kind": "cells", "cells": cells, "size": detail, "bold": True})
        items.append({"kind": "rule"})
        for item in data["items"]:
            columns = [
                item["name"],
                fa_digits(str(item["quantity"])),
                fa_digits(money(item["unitPrice"])),
                fa_digits(money(item["quantity"] * item["unitPrice"])),
            ]
            cells = [
                {"text": text, "align": align, "weight": weight}
                for text, weight, align in zip(columns, TABLE_WEIGHTS, TABLE_ALIGNS)
            ]
            items.append({"kind": "cells", "cells": cells, "size": font_size, "bold": False})
    else:
        items.append(_row("شرح کالا", "جمع", font_size, True))
        items.append({"kind": "rule"})
        for item in data["items"]:
            items.append(_row(item["name"], fa_digits(money(item["quantity"] * item["unitPrice"])), font_size, False))
            if item["quantity"] > 1:
                items.append(_text(fa_digits(f"{item['quantity']} × {money(item['unitPrice'])}"), "right", detail, False))
    items.append({"kind": "rule"})
    items.append(_row("تعداد اقلام", fa_digits(str(len(data["items"]))), font_size, False))
    if data.get("subtotal") is not None:
        items.append(_row("جمع اقلام", amount(data["subtotal"], data.get("currency", "")), font_size, False))
    items.append({"kind": "rule"})
    items.append(_row("مبلغ قابل پرداخت", amount(data["total"], data.get("currency", "")), scaled(font_size, TOTAL), True))
    note = data.get("note", "").strip()
    if note:
        items.append({"kind": "space", "dots": font_size // 4})
        items.append({"kind": "dashed"})
        items.append({"kind": "space", "dots": font_size // 4})
        items.append(_text(f"یادداشت: {note}", "center", detail, False))
        items.append({"kind": "space", "dots": font_size // 4})
        items.append({"kind": "dashed"})
    footer = data["footer"].strip()
    if footer:
        items.append({"kind": "space", "dots": font_size // 2})
        items.append(_text(footer, "center", detail, True))
    items.append({"kind": "space", "dots": font_size // 2})
    items.append(_text(BRAND_LINE, "center", scaled(font_size, BRAND), False))
    return items


def separator_line(text: str) -> bool:
    marks = set("-_=*‐‑‒–—―﹘﹣－─━═")
    text = text.strip()
    return len(text) >= 3 and all(char in marks for char in text)


def receipt_columns(text: str) -> list[str] | None:
    cells = [cell.strip() for cell in text.split("|")]
    return cells if 2 <= len(cells) <= 4 else None


def receipt(lines: list[str], font_size: int) -> list[dict]:
    items: list[dict] = []
    weights_by_count = {2: [3, 2], 3: [45, 15, 40], 4: [34, 12, 27, 27]}
    for line in lines:
        text = line.rstrip().strip()
        if not text:
            items.append({"kind": "space", "dots": font_size // 2})
        elif separator_line(text):
            items.append({"kind": "rule"})
        elif text.startswith("[center]"):
            centered = text[len("[center]"):].lstrip()
            if centered.startswith(":"):
                centered = centered[1:].lstrip()
            items.append(_text(centered, "center", font_size, False) if centered else {"kind": "space", "dots": font_size // 2})
        elif (columns := receipt_columns(text)) is not None:
            weights = weights_by_count[len(columns)]
            cells = [
                {
                    "text": cell,
                    "align": "right" if index == 0 else "left" if index + 1 == len(columns) else "center",
                    "weight": weights[index],
                }
                for index, cell in enumerate(columns)
            ]
            items.append({"kind": "cells", "cells": cells, "size": font_size, "bold": False})
        else:
            items.append(_text(text, "right" if is_rtl(text) else "left", font_size, False))
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
        base = "rtl" if base_rtl else "ltr"
        # Pass 1: strong classification. Digits (including Persian ۰-۹) are numbers in the
        # UBA: they keep their left-to-right order even inside a right-to-left line.
        kinds: list[str | None] = []
        for char in text:
            if char.isdigit() or (char.isascii() and char.isalpha()):
                kinds.append("ltr")
            elif _is_rtl_char(char):
                kinds.append("rtl")
            else:
                kinds.append(None)
        # Pass 2: resolve neutrals. Whitespace takes the base direction (so number runs
        # reorder as units in RTL lines); other neutrals (، : / ,) join their neighbours
        # when both sides agree, otherwise the base direction.
        resolved: list[str] = []
        for index, (char, kind) in enumerate(zip(text, kinds)):
            if kind is not None:
                resolved.append(kind)
                continue
            if char.isspace():
                resolved.append(base)
                continue
            prev_kind = next((k for k in reversed(kinds[:index]) if k), None)
            next_kind = next((k for k in kinds[index + 1:] if k), None)
            resolved.append(prev_kind if prev_kind == next_kind and prev_kind else base)
        segments: list[list[str]] = []
        for char, kind in zip(text, resolved):
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
        self.separator(thickness, None)

    def dashed(self, thickness: int) -> None:
        self.separator(thickness, (8, 5))

    def separator(self, thickness: int, dash: tuple[int, int] | None) -> None:
        self.y += 3.0
        top = round(self.y)
        for row in range(top, min(top + thickness, MAX_ROWS)):
            for x in range(self.margin, self.width - self.margin):
                if dash and (x - self.margin) % (dash[0] + dash[1]) >= dash[0]:
                    continue
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
        items = plan(doc, self.font_size, self.paper.width) if self.leading == LEADING else current_plan(doc, self.font_size)
        for item in items:
            if item["kind"] == "text":
                self.text(item["text"], item["size"], item["align"], item["bold"])
            elif item["kind"] == "row":
                self.row(item["right"], item["left"], item["size"], item["bold"])
            elif item["kind"] == "cells":
                self.cells(item["cells"], item["size"], item["bold"])
            elif item["kind"] == "rule":
                self.paper.rule(max(1, self.font_size // 12))
            elif item["kind"] == "dashed":
                self.paper.dashed(max(1, self.font_size // 12))
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

    def cells(self, cells: list[dict], size: int, bold: bool) -> None:
        """Mirror of `Renderer::cells`: right-to-left column regions, tallest cell wins."""
        gutter = max(4.0, size / 3)
        weight_sum = sum(max(1, c["weight"]) for c in cells)
        usable = self.paper.usable() - gutter * (len(cells) - 1)
        top = self.paper.y
        bottom = top
        right = float(self.paper.width - self.paper.margin)
        for cell in cells:
            width = usable * max(1, cell["weight"]) / weight_sum
            left = right - width
            self.paper.y = top
            self.draw(cell["text"], size, cell["align"], bold, left, right)
            bottom = max(bottom, self.paper.y)
            right = left - gutter
        self.paper.y = bottom

    def draw(self, text: str, size: int, align: str, bold: bool,
             left_edge: float | None = None, right_edge: float | None = None) -> None:
        if left_edge is None:
            left_edge = float(self.paper.margin)
        if right_edge is None:
            right_edge = float(self.paper.width - self.paper.margin)
        lines = self.wrap(text, size, right_edge - left_edge)
        line_height = size * self.leading
        for number, (runs, width) in enumerate(lines):
            top = self.paper.y + number * line_height
            if align == "right":
                x = right_edge - width
            elif align == "center":
                x = (left_edge + right_edge - width) / 2
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

    def wrap(self, text: str, size: int, limit: float | None = None) -> list[tuple[list[tuple[str, str]], float]]:
        """Greedy word wrapping mirroring cosmic-text `Wrap::WordOrGlyph` closely enough."""
        if limit is None:
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
    # Mirrors the MenuVex app invoice template (without the logo).
    "invoice": {
        "type": "invoice",
        "data": {
            "storeName": "کافه رترو",
            "orderNumber": "10195",
            "title": "فاکتور فروش",
            "address": "زنجان، میدان کوه نورد، کافه رترو",
            "phone": "09362114096",
            "date": "۱۴۰۵/۰۷/۱۲ ۲۱:۴۷",
            "status": "تکمیل‌شده",
            "orderType": "حضوری",
            "items": [
                {"name": "آیس ماچا توت فرنگی", "quantity": 1, "unitPrice": 230000},
                {"name": "اسپرسو دوبل", "quantity": 2, "unitPrice": 120000},
                {"name": "کیک سن سباستین", "quantity": 1, "unitPrice": 287000},
            ],
            "subtotal": 757000,
            "total": 757000,
            "currency": "تومان",
            "note": "بسته شده در پایان روز کاری ۰۰:۰۵",
            "footer": "از خرید شما سپاسگزاریم",
        },
    },
    "kitchen": {
        "type": "receipt",
        "lines": [
            "[center] سفارش آشپزخانه",
            "میز ۴ | اسپرسو دوبل | ×۲",
            "کیک شکلاتی | ۱ | ۲۸۷,۰۰۰",
            "------------------------------",
            "— بدون شکر —",
        ],
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
    items = plan(samples["invoice"], font_size, 576)
    first = items[0]
    require(first["kind"] == "text" and first["align"] == "center", "store name must be centered")
    require(first["size"] == scaled(font_size, STORE) and first["bold"], "store name must be larger and bold")
    require(any(item["kind"] == "rule" for item in items), "invoice must contain a separator rule")
    require(
        {"kind": "row", "right": "فاکتور فروش", "left": "فیش ۱۰۱۹۵", "size": font_size, "bold": True} in items,
        "invoice must have the bold title/slip row",
    )
    tables = [item["cells"] for item in items if item["kind"] == "cells"]
    require(tables and [cell["text"] for cell in tables[0]] == TABLE_HEADER, "wide invoice must start with the 4 column header")
    require(
        [cell["text"] for cell in tables[1]] == ["آیس ماچا توت فرنگی", "۱", "۲۳۰,۰۰۰", "۲۳۰,۰۰۰"],
        "item rows must carry quantity, grouped unit price and line amount in Persian digits",
    )
    total = [item for item in items if item["kind"] == "row" and item["right"] == "مبلغ قابل پرداخت"]
    require(len(total) == 1, "invoice must have exactly one payable row")
    require(total[0]["size"] == scaled(font_size, TOTAL) and total[0]["bold"], "payable amount must be emphasised")
    require(total[0]["left"] == "۷۵۷,۰۰۰ تومان", "payable amount must keep the adapter currency word")
    require(sum(1 for item in items if item["kind"] == "dashed") == 2, "the note must sit between two dashed rules")
    texts = [item["text"] for item in items if item["kind"] == "text"]
    require(texts[-1] == BRAND_LINE, "the brand line must close the invoice")
    narrow = plan(samples["invoice"], font_size, 384)
    require(all(item["kind"] != "cells" for item in narrow), "narrow paper must stack items instead of columns")
    require(
        any(item["kind"] == "text" and item["text"] == "۲ × ۱۲۰,۰۰۰" for item in narrow),
        "narrow paper keeps the quantity × unit price breakdown for quantity > 1",
    )
    minimal = plan({"type": "invoice", "data": {
        "storeName": "کافه", "orderNumber": "1", "footer": "",
        "items": [{"name": "چای", "quantity": 1, "unitPrice": 45000}], "total": 45000,
    }}, font_size, 576)
    require(
        all("تومان" not in (item.get("text", "") + item.get("left", "")) for item in minimal),
        "the agent must never invent a currency word",
    )
    kitchen = plan(samples["kitchen"], font_size, 576)
    require(kitchen[0]["kind"] == "text" and kitchen[0]["align"] == "center", "[center] receipt lines must be centered")
    require(any(item["kind"] == "rule" for item in kitchen), "receipt dash separators must become pixel rules")
    kitchen_rows = [item for item in kitchen if item["kind"] == "cells"]
    require(
        kitchen_rows and [cell["text"] for cell in kitchen_rows[0]["cells"]] == ["میز ۴", "اسپرسو دوبل", "×۲"],
        "pipe-separated receipt text must remain an ordered column row",
    )


def check_layout_source() -> None:
    """The mirror must not drift from the real planner."""
    with open(LAYOUT_RS, encoding="utf-8") as handle:
        source = handle.read()
    for name, value in (("STORE", STORE), ("TOTAL", TOTAL), ("DETAIL", DETAIL), ("BRAND", BRAND)):
        require(
            f"const {name}: u32 = {value};" in source,
            f"layout.rs {name} no longer matches the preview constant {value}",
        )
    require(
        f"const TABLE_MIN_DOTS: u16 = {TABLE_MIN_DOTS};" in source,
        "layout.rs table threshold no longer matches the preview constant",
    )
    for label in ["فاکتور فروش", "فیش ", "شرح کالا", "تعداد", "قیمت واحد", "جمع",
                  "تعداد اقلام", "جمع اقلام", "مبلغ قابل پرداخت", "یادداشت: ", BRAND_LINE]:
        require(label in source, f"layout.rs no longer contains the label {label!r}")
    require(
        '"────────────────"' not in source,
        "layout.rs must not fall back to a U+2500 separator glyph; draw the rule as pixels",
    )
    for fragment in ["fn separator_line", "strip_prefix(\"[center]\")", "Item::Cells"]:
        require(fragment in source, f"layout.rs is missing receipt formatting support {fragment!r}")


def check_font_coverage(shaper: Shaper, samples: dict[str, dict], font_size: int = 24) -> None:
    """Every planned character must have a glyph; the old U+2500 design did not."""
    texts: list[str] = []
    for doc in samples.values():
        for item in plan(doc, font_size, 576):
            if item["kind"] == "text":
                texts.append(item["text"])
            elif item["kind"] == "row":
                texts.extend([item["right"], item["left"]])
            elif item["kind"] == "cells":
                texts.extend(cell["text"] for cell in item["cells"])
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
