//! Printed receipt design.
//!
//! The design is decided here as an ordered list of drawing [`Item`]s; the renderer turns them
//! into a monochrome raster. No font access happens in this module, so the layout is
//! unit-testable without a `FontSystem`.
//!
//! Persian receipts are read right to left: Persian text hangs on the **right** margin and money
//! on the **left** margin of the same row. Amounts are grouped (`240000` → `240,000`) for
//! readability only; the value itself is never converted or recalculated, see `docs/escpos.md`.

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
    /// Full width separator line, drawn as pixels (never as font glyphs).
    Rule,
    /// Vertical whitespace in dots.
    Space(u16),
}

/// Size of the store name, percent of the local font size.
const STORE: u32 = 132;
/// Size of the total row, percent of the local font size.
const TOTAL: u32 = 120;
/// Size of secondary detail lines, percent of the local font size.
const DETAIL: u32 = 84;

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

/// Build the printed design of `doc`.
pub fn plan(doc: &Document, font_size: u16) -> Vec<Item> {
    match doc {
        Document::Invoice { data } => invoice(data, font_size),
        Document::Receipt { lines } => receipt(lines, font_size),
    }
}

/// Persian invoice: store header, order number, item rows with the amount on the left margin,
/// emphasised total and an optional centered footer.
fn invoice(data: &InvoiceData, font_size: u16) -> Vec<Item> {
    let mut out = Vec::new();
    let store = data.store_name.trim();
    if !store.is_empty() {
        out.push(Item::Text(Line {
            text: store.to_string(),
            align: Align::Center,
            size: scaled(font_size, STORE),
            bold: true,
        }));
    }
    let order = data.order_number.trim();
    if !order.is_empty() {
        out.push(Item::Text(Line {
            text: format!("شماره سفارش: {order}"),
            align: Align::Center,
            size: font_size,
            bold: false,
        }));
    }
    out.push(Item::Rule);
    out.push(Item::Row {
        right: "کالا".into(),
        left: "مبلغ".into(),
        size: font_size,
        bold: true,
    });
    out.push(Item::Rule);
    for item in &data.items {
        out.push(Item::Row {
            right: item.name.clone(),
            left: money(item.quantity as u64 * item.unit_price),
            size: font_size,
            bold: false,
        });
        if item.quantity > 1 {
            out.push(Item::Text(Line {
                text: format!(
                    "{} × {}",
                    money(item.quantity as u64),
                    money(item.unit_price)
                ),
                align: Align::Right,
                size: scaled(font_size, DETAIL),
                bold: false,
            }));
        }
    }
    out.push(Item::Rule);
    out.push(Item::Row {
        right: "جمع کل".into(),
        left: money(data.total),
        size: scaled(font_size, TOTAL),
        bold: true,
    });
    let footer = data.footer.trim();
    if !footer.is_empty() {
        out.push(Item::Space(font_size / 2));
        out.push(Item::Text(Line {
            text: footer.to_string(),
            align: Align::Center,
            size: scaled(font_size, DETAIL),
            bold: false,
        }));
    }
    out
}

/// Free-form receipt (kitchen/bar ticket, test print): every supplied line is printed as it is,
/// hanging on the margin of its own writing direction — exactly like the Legacy Windows agent.
fn receipt(lines: &[String], font_size: u16) -> Vec<Item> {
    let mut out = Vec::new();
    for line in lines {
        let text = line.trim_end();
        if text.trim().is_empty() {
            out.push(Item::Space(font_size / 2));
            continue;
        }
        out.push(Item::Text(Line {
            text: text.to_string(),
            align: if is_rtl(text) { Align::Right } else { Align::Left },
            size: font_size,
            bold: false,
        }));
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::protocol::InvoiceItem;

    fn invoice_data() -> InvoiceData {
        InvoiceData {
            store_name: "کافه ونک".into(),
            order_number: "1842".into(),
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
            footer: "با سپاس از خرید شما".into(),
        }
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
    fn direction_is_detected_from_the_first_strong_character() {
        assert!(is_rtl("قهوه"));
        assert!(is_rtl("۲ عدد چای"));
        assert!(!is_rtl("Latte"));
        assert!(!is_rtl("2 × 120,000"));
        assert!(!is_rtl(""));
    }

    #[test]
    fn invoice_has_header_columns_and_total() {
        let items = plan(&Document::Invoice { data: invoice_data() }, 24);
        assert_eq!(
            items.first(),
            Some(&Item::Text(Line {
                text: "کافه ونک".into(),
                align: Align::Center,
                size: scaled(24, STORE),
                bold: true,
            }))
        );
        assert!(items.contains(&Item::Rule));
        assert!(items.contains(&Item::Row {
            right: "کالا".into(),
            left: "مبلغ".into(),
            size: 24,
            bold: true,
        }));
        // The first item shares one row with its grouped amount.
        assert!(items.contains(&Item::Row {
            right: "اسپرسو دوبل".into(),
            left: "240,000".into(),
            size: 24,
            bold: false,
        }));
        // Quantity > 1 adds a secondary `quantity × unit price` line.
        assert!(items.contains(&Item::Text(Line {
            text: "2 × 120,000".into(),
            align: Align::Right,
            size: scaled(24, DETAIL),
            bold: false,
        })));
        // Quantity 1 adds no secondary line and no extra row.
        assert!(!items.iter().any(|item| matches!(item, Item::Text(line) if line.text.starts_with("1 ×"))));
        // Total is emphasised and carries the authoritative value from the adapter.
        assert!(items.contains(&Item::Row {
            right: "جمع کل".into(),
            left: "285,000".into(),
            size: scaled(24, TOTAL),
            bold: true,
        }));
        assert_eq!(
            items.last(),
            Some(&Item::Text(Line {
                text: "با سپاس از خرید شما".into(),
                align: Align::Center,
                size: scaled(24, DETAIL),
                bold: false,
            }))
        );
    }

    #[test]
    fn invoice_without_footer_and_order_number_stays_compact() {
        let mut data = invoice_data();
        data.footer = "   ".into();
        data.order_number = String::new();
        let items = plan(&Document::Invoice { data }, 24);
        assert!(!items.iter().any(|item| matches!(item, Item::Text(line) if line.text.contains("سفارش"))));
        assert!(!items.iter().any(|item| matches!(item, Item::Space(_))));
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
        let items = plan(&doc, 20);
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
}
