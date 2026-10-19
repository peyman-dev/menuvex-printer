//! Printed receipt design.
//!
//! The design is decided here as an ordered list of drawing [`Item`]s; the renderer turns them
//! into a monochrome raster. No font access happens in this module, so the layout is
//! unit-testable without a `FontSystem`.
//!
//! Invoices follow the MenuVex app template (without the logo): centered store header, a titled
//! slip row, label/value detail rows, an items table, the emphasised payable amount, an optional
//! note between dashed rules and the brand line. Persian receipts are read right to left:
//! Persian text hangs on the **right** margin and amounts on the **left** margin of the same
//! row. Amounts are grouped (`240000` → `۲۴۰,۰۰۰`) and shown in Persian digits for readability
//! only; the value itself is never converted or recalculated, see `docs/escpos.md`.

use crate::protocol::{Document, InvoiceData};

/// Horizontal placement of a line inside the printable width.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Align {
    Right,
    Center,
    Left,
}

/// One line of text. `text` may contain `\n` for a hard line break.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Line {
    pub text: String,
    pub align: Align,
    /// Font size in dots.
    pub size: u16,
    pub bold: bool,
}

/// One column of a [`Item::Cells`] table row.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Cell {
    pub text: String,
    pub align: Align,
    /// Relative column width; the renderer divides the printable width by the weight sum.
    pub weight: u16,
}

/// A drawing instruction produced by [`plan`].
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Item {
    Text(Line),
    /// Two cells on one row: `right` hangs on the right margin, `left` on the left margin. The
    /// renderer stacks them on two rows when they cannot share one.
    Row {
        right: String,
        left: String,
        size: u16,
        bold: bool,
    },
    /// A table row. Cells are listed in reading order and laid out right-to-left: the first
    /// cell takes the rightmost column. Text wraps inside its own column.
    Cells {
        cells: Vec<Cell>,
        size: u16,
        bold: bool,
    },
    /// Full width separator line, drawn as pixels (never as font glyphs).
    Rule,
    /// Full width dashed separator, drawn as pixels.
    Dashed,
    /// Vertical whitespace in dots.
    Space(u16),
}

/// Size of the store name, percent of the local font size.
const STORE: u32 = 132;
/// Size of the total row, percent of the local font size.
const TOTAL: u32 = 120;
/// Size of secondary detail lines, percent of the local font size.
const DETAIL: u32 = 84;
/// Size of the brand line, percent of the local font size.
const BRAND: u32 = 66;
/// Minimum printable dots for the four column items table; narrower paper stacks the items.
const TABLE_MIN_DOTS: u16 = 464;
/// Brand line printed under every invoice, same as the MenuVex app template.
const BRAND_LINE: &str = "POWERED BY MENUVEX.IR";

/// Scale a base font size, clamped to sane dot sizes.
pub fn scaled(size: u16, percent: u32) -> u16 {
    ((size as u32 * percent) / 100).clamp(8, 96) as u16
}

/// True when the first strong character of `text` is right-to-left (Persian/Arabic script).
///
/// Lines without any strong character (pure numbers, punctuation) are treated as left-to-right,
/// which keeps expressions such as `2 × 120,000` in their written order.
pub fn is_rtl(text: &str) -> bool {
    for c in text.chars() {
        let code = c as u32;
        if matches!(code, 0x0590..=0x08FF | 0xFB1D..=0xFDFF | 0xFE70..=0xFEFF) {
            return true;
        }
        if c.is_ascii_alphabetic() {
            return false;
        }
    }
    false
}

/// Map ASCII digits to Persian digits (`۰`–`۹`). Presentation only.
pub fn fa_digits(text: &str) -> String {
    text.chars()
        .map(|c| match c {
            '0'..='9' => {
                char::from_u32(0x06f0 + (c as u32 - '0' as u32)).unwrap_or(c)
            }
            _ => c,
        })
        .collect()
}

/// Group digits in threes: `240000` → `240,000`. Presentation only.
pub fn money(value: u64) -> String {
    let digits = value.to_string();
    let mut out = String::with_capacity(digits.len() + digits.len() / 3);
    for (index, c) in digits.chars().enumerate() {
        if index > 0 && (digits.len() - index) % 3 == 0 {
            out.push(',');
        }
        out.push(c);
    }
    out
}

/// Grouped amount in Persian digits, with the adapter supplied currency word when present.
/// The agent never invents a currency.
pub fn amount(value: u64, currency: &str) -> String {
    let grouped = fa_digits(&money(value));
    let currency = currency.trim();
    if currency.is_empty() {
        grouped
    } else {
        format!("{grouped} {currency}")
    }
}

/// Build the printed design of `doc` for a paper of `width_dots` printable dots.
pub fn plan(doc: &Document, font_size: u16, width_dots: u16) -> Vec<Item> {
    match doc {
        Document::Invoice { data } => invoice(data, font_size, width_dots),
        Document::Receipt { lines } => receipt(lines, font_size),
        // Raw ESC/POS already contains the final printer bytes and deliberately bypasses layout.
        // `Renderer::encode` returns those bytes before calling this function; keeping this arm a
        // no-op also makes direct callers incapable of accidentally re-rendering raw data.
        Document::Escpos { .. } => Vec::new(),
    }
}

fn text(text: impl Into<String>, align: Align, size: u16, bold: bool) -> Item {
    Item::Text(Line { text: text.into(), align, size, bold })
}

fn row(right: impl Into<String>, left: impl Into<String>, size: u16, bold: bool) -> Item {
    Item::Row { right: right.into(), left: left.into(), size, bold }
}

/// Persian invoice following the app template without the logo: centered store header with
/// address and phone, the slip row, label/value details, the items table (stacked on narrow
/// paper), counters, the emphasised payable amount, note and brand line.
fn invoice(data: &InvoiceData, font_size: u16, width_dots: u16) -> Vec<Item> {
    let detail = scaled(font_size, DETAIL);
    let mut out = Vec::new();
    let store = data.store_name.trim();
    if !store.is_empty() {
        out.push(text(store, Align::Center, scaled(font_size, STORE), true));
    }
    let address = data.address.trim();
    if !address.is_empty() {
        out.push(text(address, Align::Center, detail, false));
    }
    let phone = data.phone.trim();
    if !phone.is_empty() {
        out.push(text(
            format!("تلفن: {}", fa_digits(phone)),
            Align::Center,
            detail,
            false,
        ));
    }
    out.push(Item::Rule);
    let title = data.title.trim();
    let title = if title.is_empty() { "فاکتور فروش" } else { title };
    let order = data.order_number.trim();
    if order.is_empty() {
        out.push(text(title, Align::Center, font_size, true));
    } else {
        out.push(row(title, format!("فیش {}", fa_digits(order)), font_size, true));
    }
    out.push(Item::Rule);
    let mut details = 0;
    for (label, value) in [
        ("تاریخ", &data.date),
        ("وضعیت", &data.status),
        ("نوع سفارش", &data.order_type),
        ("میز", &data.table),
    ] {
        let value = value.trim();
        if !value.is_empty() {
            out.push(row(label, fa_digits(value), font_size, false));
            details += 1;
        }
    }
    if details > 0 {
        out.push(Item::Rule);
    }
    if width_dots >= TABLE_MIN_DOTS {
        table_items(data, font_size, &mut out);
    } else {
        stacked_items(data, font_size, &mut out);
    }
    out.push(Item::Rule);
    out.push(row(
        "تعداد اقلام",
        fa_digits(&data.items.len().to_string()),
        font_size,
        false,
    ));
    if let Some(subtotal) = data.subtotal {
        out.push(row("جمع اقلام", amount(subtotal, &data.currency), font_size, false));
    }
    out.push(Item::Rule);
    out.push(row(
        "مبلغ قابل پرداخت",
        amount(data.total, &data.currency),
        scaled(font_size, TOTAL),
        true,
    ));
    let note = data.note.trim();
    if !note.is_empty() {
        out.push(Item::Space(font_size / 4));
        out.push(Item::Dashed);
        out.push(Item::Space(font_size / 4));
        out.push(text(format!("یادداشت: {note}"), Align::Center, detail, false));
        out.push(Item::Space(font_size / 4));
        out.push(Item::Dashed);
    }
    let footer = data.footer.trim();
    if !footer.is_empty() {
        out.push(Item::Space(font_size / 2));
        out.push(text(footer, Align::Center, detail, true));
    }
    out.push(Item::Space(font_size / 2));
    out.push(text(BRAND_LINE, Align::Center, scaled(font_size, BRAND), false));
    out
}

/// Wide paper (80 mm): four column table like the app template — item, quantity, unit price
/// and line amount. Line amounts come straight from `quantity × unitPrice` of the adapter
/// payload; nothing else is derived.
fn table_items(data: &InvoiceData, font_size: u16, out: &mut Vec<Item>) {
    let detail = scaled(font_size, DETAIL);
    let header = ["شرح کالا", "تعداد", "قیمت واحد", "جمع"];
    let weights = [34u16, 12, 27, 27];
    let aligns = [Align::Right, Align::Center, Align::Center, Align::Left];
    out.push(Item::Cells {
        cells: header
            .iter()
            .zip(weights)
            .zip(aligns)
            .map(|((text, weight), align)| Cell { text: (*text).into(), align, weight })
            .collect(),
        size: detail,
        bold: true,
    });
    out.push(Item::Rule);
    for item in &data.items {
        let columns = [
            item.name.clone(),
            fa_digits(&item.quantity.to_string()),
            fa_digits(&money(item.unit_price)),
            fa_digits(&money(item.quantity as u64 * item.unit_price)),
        ];
        out.push(Item::Cells {
            cells: columns
                .into_iter()
                .zip(weights)
                .zip(aligns)
                .map(|((text, weight), align)| Cell { text, align, weight })
                .collect(),
            size: font_size,
            bold: false,
        });
    }
}

/// Narrow paper (58 mm): the four columns cannot fit, so every item prints as a name/amount
/// row plus a `quantity × unit price` detail line underneath.
fn stacked_items(data: &InvoiceData, font_size: u16, out: &mut Vec<Item>) {
    let detail = scaled(font_size, DETAIL);
    out.push(row("شرح کالا", "جمع", font_size, true));
    out.push(Item::Rule);
    for item in &data.items {
        out.push(row(
            item.name.clone(),
            fa_digits(&money(item.quantity as u64 * item.unit_price)),
            font_size,
            false,
        ));
        if item.quantity > 1 {
            out.push(text(
                fa_digits(&format!("{} × {}", item.quantity, money(item.unit_price))),
                Align::Right,
                detail,
                false,
            ));
        }
    }
}

/// A standalone line made only from separator marks is drawn as a rule, not sent to the font.
/// This includes common ASCII and box-drawing characters typed by receipt templates.
fn separator_line(text: &str) -> bool {
    let mut chars = text.trim().chars();
    let Some(first) = chars.next() else {
        return false;
    };
    let is_mark = |c: char| {
        matches!(
            c,
            '-' | '_' | '=' | '*' | '‐' | '‑' | '‒' | '–' | '—' | '―' | '﹘' | '﹣' | '－' |
            '─' | '━' | '═'
        )
    };
    is_mark(first) && text.trim().chars().count() >= 3 && chars.all(is_mark)
}

fn columns(text: &str) -> Option<Vec<String>> {
    let cells = text.split('|').map(str::trim).map(str::to_owned).collect::<Vec<_>>();
    (2..=4).contains(&cells.len()).then_some(cells)
}

fn column_weights(count: usize) -> &'static [u16] {
    match count {
        2 => &[3, 2],
        3 => &[45, 15, 40],
        4 => &[34, 12, 27, 27],
        _ => &[],
    }
}

/// Free-form receipt (kitchen/bar ticket, test print). Templates may use `---` for a pixel rule,
/// `a | b | c` for a right-to-left column row, and `[center] text` for a centered line. Ordinary
/// text keeps hanging alignment based on its writing direction.
fn receipt(lines: &[String], font_size: u16) -> Vec<Item> {
    let mut out = Vec::new();
    for line in lines {
        let source = line.trim_end();
        let text = source.trim();
        if text.is_empty() {
            out.push(Item::Space(font_size / 2));
        } else if separator_line(text) {
            out.push(Item::Rule);
        } else if let Some(centered) = text.strip_prefix("[center]") {
            let mut centered = centered.trim_start();
            if let Some(without_colon) = centered.strip_prefix(':') {
                centered = without_colon.trim_start();
            }
            if centered.is_empty() {
                out.push(Item::Space(font_size / 2));
            } else {
                out.push(Item::Text(Line {
                    text: centered.to_owned(),
                    align: Align::Center,
                    size: font_size,
                    bold: false,
                }));
            }
        } else if let Some(cells) = columns(text) {
            let weights = column_weights(cells.len());
            out.push(Item::Cells {
                cells: cells
                    .into_iter()
                    .enumerate()
                    .map(|(index, text)| Cell {
                        text,
                        align: if index == 0 {
                            Align::Right
                        } else if index + 1 == weights.len() {
                            Align::Left
                        } else {
                            Align::Center
                        },
                        weight: weights[index],
                    })
                    .collect(),
                size: font_size,
                bold: false,
            });
        } else {
            out.push(Item::Text(Line {
                text: text.to_owned(),
                align: if is_rtl(text) { Align::Right } else { Align::Left },
                size: font_size,
                bold: false,
            }));
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::protocol::InvoiceItem;

    fn invoice_data() -> InvoiceData {
        InvoiceData {
            store_name: "کافه رترو".into(),
            order_number: "10195".into(),
            items: vec![
                InvoiceItem {
                    name: "اسپرسو دوبل".into(),
                    quantity: 2,
                    unit_price: 120_000,
                },
                InvoiceItem {
                    name: "چای".into(),
                    quantity: 1,
                    unit_price: 45_000,
                },
            ],
            total: 285_000,
            footer: "از خرید شما سپاسگزاریم".into(),
            title: String::new(),
            address: "زنجان، میدان کوه نورد، کافه رترو".into(),
            phone: "09362114096".into(),
            date: "۱۴۰۵/۰۷/۱۲ ۲۱:۴۷".into(),
            status: "تکمیل‌شده".into(),
            order_type: "حضوری".into(),
            table: "3".into(),
            note: "بسته شده در پایان روز کاری".into(),
            currency: "تومان".into(),
            subtotal: Some(285_000),
        }
    }

    fn texts(items: &[Item]) -> Vec<String> {
        items
            .iter()
            .filter_map(|item| match item {
                Item::Text(line) => Some(line.text.clone()),
                _ => None,
            })
            .collect()
    }

    #[test]
    fn money_groups_thousands() {
        assert_eq!(money(0), "0");
        assert_eq!(money(999), "999");
        assert_eq!(money(1000), "1,000");
        assert_eq!(money(240000), "240,000");
        assert_eq!(money(9_000_000_000_000), "9,000,000,000,000");
    }

    #[test]
    fn amounts_use_persian_digits_and_adapter_currency_only() {
        assert_eq!(fa_digits("230,000"), "۲۳۰,۰۰۰");
        assert_eq!(amount(230_000, "تومان"), "۲۳۰,۰۰۰ تومان");
        assert_eq!(amount(230_000, ""), "۲۳۰,۰۰۰");
        assert_eq!(amount(230_000, "  "), "۲۳۰,۰۰۰");
    }

    #[test]
    fn direction_is_detected_from_the_first_strong_character() {
        assert!(is_rtl("قهوه"));
        assert!(is_rtl("۲ عدد چای"));
        assert!(!is_rtl("Latte"));
        assert!(!is_rtl("2 × 120,000"));
        assert!(!is_rtl(""));
    }

    #[test]
    fn wide_invoice_follows_the_app_template() {
        let items = plan(&Document::Invoice { data: invoice_data() }, 24, 576);
        assert_eq!(
            items.first(),
            Some(&Item::Text(Line {
                text: "کافه رترو".into(),
                align: Align::Center,
                size: scaled(24, STORE),
                bold: true,
            }))
        );
        let lines = texts(&items);
        assert!(lines.contains(&"زنجان، میدان کوه نورد، کافه رترو".to_string()));
        assert!(lines.contains(&"تلفن: ۰۹۳۶۲۱۱۴۰۹۶".to_string()));
        // Default title with the slip number in Persian digits.
        assert!(items.contains(&Item::Row {
            right: "فاکتور فروش".into(),
            left: "فیش ۱۰۱۹۵".into(),
            size: 24,
            bold: true,
        }));
        // Detail rows keep label right and value left.
        assert!(items.contains(&Item::Row {
            right: "وضعیت".into(),
            left: "تکمیل‌شده".into(),
            size: 24,
            bold: false,
        }));
        assert!(items.contains(&Item::Row {
            right: "میز".into(),
            left: "۳".into(),
            size: 24,
            bold: false,
        }));
        // Four column table: header then one row per item with the line amount.
        let tables: Vec<_> = items
            .iter()
            .filter_map(|item| match item {
                Item::Cells { cells, .. } => Some(cells.iter().map(|c| c.text.clone()).collect::<Vec<_>>()),
                _ => None,
            })
            .collect();
        assert_eq!(tables[0], vec!["شرح کالا", "تعداد", "قیمت واحد", "جمع"]);
        assert_eq!(tables[1], vec!["اسپرسو دوبل", "۲", "۱۲۰,۰۰۰", "۲۴۰,۰۰۰"]);
        assert_eq!(tables[2], vec!["چای", "۱", "۴۵,۰۰۰", "۴۵,۰۰۰"]);
        // Counters and subtotal come from the payload; nothing is derived.
        assert!(items.contains(&Item::Row {
            right: "تعداد اقلام".into(),
            left: "۲".into(),
            size: 24,
            bold: false,
        }));
        assert!(items.contains(&Item::Row {
            right: "جمع اقلام".into(),
            left: "۲۸۵,۰۰۰ تومان".into(),
            size: 24,
            bold: false,
        }));
        assert!(items.contains(&Item::Row {
            right: "مبلغ قابل پرداخت".into(),
            left: "۲۸۵,۰۰۰ تومان".into(),
            size: scaled(24, TOTAL),
            bold: true,
        }));
        // Note sits between dashed rules; brand line closes the slip.
        assert!(lines.contains(&"یادداشت: بسته شده در پایان روز کاری".to_string()));
        assert_eq!(items.iter().filter(|i| matches!(i, Item::Dashed)).count(), 2);
        assert_eq!(lines.last().map(String::as_str), Some(BRAND_LINE));
    }

    #[test]
    fn narrow_invoice_stacks_items_instead_of_columns() {
        let items = plan(&Document::Invoice { data: invoice_data() }, 24, 384);
        assert!(!items.iter().any(|item| matches!(item, Item::Cells { .. })));
        assert!(items.contains(&Item::Row {
            right: "اسپرسو دوبل".into(),
            left: "۲۴۰,۰۰۰".into(),
            size: 24,
            bold: false,
        }));
        // Quantity > 1 adds a secondary `quantity × unit price` line.
        assert!(items.contains(&Item::Text(Line {
            text: "۲ × ۱۲۰,۰۰۰".into(),
            align: Align::Right,
            size: scaled(24, DETAIL),
            bold: false,
        })));
        // Quantity 1 adds no secondary line.
        assert!(!items.iter().any(
            |item| matches!(item, Item::Text(line) if line.text.starts_with("۱ ×"))
        ));
    }

    #[test]
    fn minimal_invoice_stays_compact_and_backwards_compatible() {
        let data = InvoiceData {
            store_name: "کافه ونک".into(),
            order_number: "1842".into(),
            items: vec![InvoiceItem { name: "چای".into(), quantity: 1, unit_price: 45_000 }],
            total: 45_000,
            footer: String::new(),
            title: String::new(),
            address: String::new(),
            phone: String::new(),
            date: String::new(),
            status: String::new(),
            order_type: String::new(),
            table: String::new(),
            note: String::new(),
            currency: String::new(),
            subtotal: None,
        };
        let items = plan(&Document::Invoice { data }, 24, 576);
        let lines = texts(&items);
        // No invented data: no currency word, no subtotal row, no note, no detail rows.
        assert!(!lines.iter().any(|l| l.contains("تومان")));
        assert!(!items.iter().any(
            |item| matches!(item, Item::Row { right, .. } if right == "جمع اقلام")
        ));
        assert!(!items.iter().any(|item| matches!(item, Item::Dashed)));
        assert!(!items.iter().any(
            |item| matches!(item, Item::Row { right, .. } if right == "تاریخ")
        ));
        // Total amount prints bare because no currency was supplied.
        assert!(items.contains(&Item::Row {
            right: "مبلغ قابل پرداخت".into(),
            left: "۴۵,۰۰۰".into(),
            size: scaled(24, TOTAL),
            bold: true,
        }));
    }

    #[test]
    fn raw_escpos_bypasses_semantic_layout() {
        let doc = Document::Escpos { commands: vec![27, 64], data: String::new() };
        assert!(plan(&doc, 20, 576).is_empty());
    }

    #[test]
    fn receipt_lines_keep_order_and_follow_their_direction() {
        let doc = Document::Receipt {
            lines: vec![
                "آزمون چاپ فارسی".into(),
                "Test print".into(),
                String::new(),
            ],
        };
        let items = plan(&doc, 20, 576);
        assert_eq!(
            items,
            vec![
                Item::Text(Line {
                    text: "آزمون چاپ فارسی".into(),
                    align: Align::Right,
                    size: 20,
                    bold: false,
                }),
                Item::Text(Line {
                    text: "Test print".into(),
                    align: Align::Left,
                    size: 20,
                    bold: false,
                }),
                Item::Space(10),
            ]
        );
    }

    #[test]
    fn receipt_templates_render_pixel_rules_columns_and_centered_lines() {
        let doc = Document::Receipt {
            lines: vec![
                "[center] Kitchen ticket".into(),
                "میز ۴ | اسپرسو دوبل | ۲۴۰,۰۰۰".into(),
                "--------------------------------".into(),
                "Tea | 1 | 45,000".into(),
            ],
        };
        let items = plan(&doc, 20, 384);
        assert_eq!(
            items[0],
            Item::Text(Line {
                text: "Kitchen ticket".into(),
                align: Align::Center,
                size: 20,
                bold: false,
            })
        );
        assert!(matches!(items[1], Item::Cells { ref cells, .. }
            if cells.iter().map(|cell| cell.align).collect::<Vec<_>>()
                == vec![Align::Right, Align::Center, Align::Left]
                && cells[0].weight == 45 && cells[2].weight == 40));
        assert_eq!(items[2], Item::Rule);
        assert!(matches!(items[3], Item::Cells { ref cells, .. }
            if cells.iter().map(|cell| cell.text.as_str()).collect::<Vec<_>>()
                == vec!["Tea", "1", "45,000"]));
    }
}
