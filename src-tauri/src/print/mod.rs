//! Semantic document → monochrome raster → ESC/POS bytes.
//!
//! Persian text is shaped with cosmic-text (rustybuzz BiDi/Arabic shaping + Swash rasterization)
//! and the *design* comes from [`layout`]: right margin for Persian, left margin for amounts,
//! emphasised header/total and pixel separators. Font selection is local-only.
use cosmic_text::{Attrs, Buffer, Color, Family, FontSystem, Metrics, Shaping, SwashCache, Wrap};
use crate::{ error::{ AgentError, Result }, printers::Printer, protocol::Document };
use layout::{ Align, Item, Line };
pub mod escpos;
pub mod layout;
pub mod queue;
/// Alpha coverage (of 255) above which a pixel becomes a black dot.
const INK: u8 = 100;
/// Row limit of one raster document (`docs/protocol.md`).
const MAX_ROWS: u16 = 4096;
/// Baseline-to-baseline distance as a multiple of the font size.
const LEADING: f32 = 1.45;
/// Rustybuzz shaping + Unicode BiDi + Swash rasterization. Font selection is local-only.
pub struct Renderer {
    fonts: FontSystem,
    cache: SwashCache,
}
impl Default for Renderer {
    fn default() -> Self {
        {
            let mut fonts = FontSystem::new();
            fonts
                .db_mut()
                .load_font_data(include_bytes!("../../assets/NotoSansArabic-Regular.ttf").to_vec());
            Self { fonts, cache: SwashCache::new() }
        }
    }
}
/// Monochrome paper under construction. Rows are added as the design is drawn.
struct Paper {
    width: u16,
    stride: usize,
    margin: u16,
    bits: Vec<u8>,
    height: u16,
    y: f32,
}
impl Paper {
    fn new(width: u16) -> Self {
        Self {
            width,
            stride: (width as usize).div_ceil(8),
            margin: (((width as f32) * 0.02).round() as u16).clamp(2, 24),
            bits: Vec::new(),
            height: 0,
            y: 2.0,
        }
    }
    /// Printable width between both margins.
    fn usable(&self) -> u16 {
        self.width - self.margin * 2
    }
    fn used(&self) -> u16 {
        (self.y.ceil() as u16).max(1)
    }
    fn ensure(&mut self, rows: u16) {
        if rows > self.height {
            self.height = rows.min(MAX_ROWS);
            self.bits.resize(self.stride * self.height as usize, 0);
        }
    }
    fn space(&mut self, dots: u16) {
        self.y += dots as f32;
    }
    /// Separator drawn as real pixels, so the design never depends on a font having `─`.
    fn rule(&mut self, thickness: u16) {
        self.y += 3.0;
        let top = self.y.round() as u16;
        let bottom = top.saturating_add(thickness).min(MAX_ROWS);
        self.ensure(bottom);
        let stride = self.stride;
        for row in top..bottom {
            for x in self.margin..(self.width - self.margin) {
                self.bits[row as usize * stride + x as usize / 8] |= 0x80u8 >> ((x as usize % 8) as u8);
            }
        }
        self.y = bottom as f32 + 3.0;
    }
}
/// Alignment offset of the layout run that owns pixel row `y`.
fn band_offset(y: i32, bands: &[(i32, i32, i32)]) -> i32 {
    let mut fallback = bands[0].2;
    let mut distance = i32::MAX;
    for &(top, bottom, x) in bands {
        if y >= top && y < bottom {
            return x;
        }
        let gap = if y < top { top - y } else { y - bottom + 1 };
        if gap < distance {
            distance = gap;
            fallback = x;
        }
    }
    fallback
}
impl Renderer {
    pub fn encode(&mut self, p: &Printer, doc: &Document) -> Result<Vec<u8>> {
        p.validate()?;
        doc.validate()?;
        if
            !self.fonts
                .db()
                .faces()
                .any(|f| f.families.iter().any(|(name, _)| name == &p.font_family))
        {
            return Err(
                AgentError::new(
                    "FONT_UNAVAILABLE",
                    "Install the configured font, e.g. Noto Sans Arabic, before printing"
                )
            );
        }
        let attrs = Attrs::new().family(Family::Name(&p.font_family));
        let mut paper = Paper::new(p.width_dots);
        for item in layout::plan(doc, p.font_size) {
            match item {
                Item::Text(line) => self.text(&attrs, &mut paper, &line)?,
                Item::Row { right, left, size, bold } =>
                    self.row(&attrs, &mut paper, &right, &left, size, bold)?,
                Item::Rule => paper.rule((p.font_size / 12).max(1)),
                Item::Space(dots) => paper.space(dots),
            }
        }
        let height = paper.used();
        if height > MAX_ROWS {
            return Err(
                AgentError::new(
                    "INVALID_JOB",
                    "Rendered document exceeds 4096 rows; split the receipt"
                )
            );
        }
        paper.ensure(height);
        let mut encoder = escpos::Encoder::default();
        encoder.initialize();
        encoder.raster(p.width_dots, height, &paper.bits[..paper.stride * height as usize])?;
        encoder.feed(3);
        if p.cut {
            encoder.cut();
        }
        Ok(encoder.0)
    }
    fn text(&mut self, attrs: &Attrs, paper: &mut Paper, line: &Line) -> Result<()> {
        if line.text.trim().is_empty() {
            paper.space(line.size / 2);
            return Ok(());
        }
        self.draw(attrs, paper, &line.text, line.size, line.align, line.bold)
    }
    /// One item row: description on the right margin, amount on the left margin. Too wide cells
    /// keep both values but stack them on two rows.
    fn row(
        &mut self,
        attrs: &Attrs,
        paper: &mut Paper,
        right: &str,
        left: &str,
        size: u16,
        bold: bool
    ) -> Result<()> {
        if left.is_empty() {
            return self.draw(attrs, paper, right, size, Align::Right, bold);
        }
        let gap = ((size as f32) * 0.75).round() as u16;
        let right_width = self.measure(attrs, right, size)?;
        let left_width = self.measure(attrs, left, size)?;
        if right_width + left_width + (gap as f32) <= (paper.usable() as f32) {
            let top = paper.y;
            self.draw(attrs, paper, right, size, Align::Right, bold)?;
            let after_right = paper.y;
            paper.y = top;
            self.draw(attrs, paper, left, size, Align::Left, bold)?;
            paper.y = paper.y.max(after_right);
            Ok(())
        } else {
            self.draw(attrs, paper, right, size, Align::Right, bold)?;
            self.draw(attrs, paper, left, size, Align::Left, bold)
        }
    }
    /// Natural width of `text` when it is not wrapped by the paper width.
    fn measure(&mut self, attrs: &Attrs, text: &str, size: u16) -> Result<f32> {
        let metrics = Metrics::new(size as f32, (size as f32) * LEADING);
        let mut buffer = Buffer::new(&mut self.fonts, metrics);
        let mut b = buffer.borrow_with(&mut self.fonts);
        b.set_wrap(Wrap::WordOrGlyph);
        b.set_size(None, None);
        b.set_text(text, attrs, Shaping::Advanced);
        b.shape_until_scroll(true);
        let mut width = 0f32;
        for run in b.layout_runs() {
            width = width.max(run.line_w);
        }
        Ok(width)
    }
    /// Shape one block and paint it at the current cursor, then advance the cursor.
    fn draw(
        &mut self,
        attrs: &Attrs,
        paper: &mut Paper,
        text: &str,
        size: u16,
        align: Align,
        bold: bool
    ) -> Result<()> {
        let metrics = Metrics::new(size as f32, (size as f32) * LEADING);
        let mut buffer = Buffer::new(&mut self.fonts, metrics);
        let mut b = buffer.borrow_with(&mut self.fonts);
        b.set_wrap(Wrap::WordOrGlyph);
        b.set_size(Some(paper.usable() as f32), None);
        b.set_text(text, attrs, Shaping::Advanced);
        b.shape_until_scroll(true);
        let left_edge = paper.margin as f32;
        let right_edge = (paper.width - paper.margin) as f32;
        let mut bands: Vec<(i32, i32, i32)> = Vec::new();
        let mut block_height = 0f32;
        for run in b.layout_runs() {
            if run.glyphs.iter().any(|glyph| glyph.glyph_id == 0) {
                return Err(
                    AgentError::new(
                        "FONT_GLYPH_MISSING",
                        "Configured fonts cannot render this document"
                    )
                );
            }
            let x = match align {
                Align::Right => (right_edge - run.line_w).max(left_edge),
                Align::Center => (((paper.width as f32) - run.line_w) / 2.0).max(left_edge),
                Align::Left => left_edge,
            };
            bands.push((
                run.line_top as i32,
                (run.line_top + run.line_height).ceil() as i32,
                x.round() as i32,
            ));
            block_height = block_height.max(run.line_top + run.line_height);
        }
        if bands.is_empty() {
            paper.space(size);
            return Ok(());
        }
        let top = paper.y.round() as i32;
        paper.y += block_height;
        let needed = paper.y.ceil() as u16;
        paper.ensure(needed);
        let width = paper.width as i32;
        let rows = paper.height as i32;
        let stride = paper.stride;
        let bits = &mut paper.bits;
        b.draw(&mut self.cache, Color::rgb(0, 0, 0), |x, y, _w, _h, color| {
            if color.a() < INK {
                return;
            }
            let dx = band_offset(y, &bands);
            let py = y + top;
            if py < 0 || py >= rows {
                return;
            }
            for shift in 0..=if bold { 1 } else { 0 } {
                let px = x + dx + shift;
                if px < 0 || px >= width {
                    continue;
                }
                bits[py as usize * stride + px as usize / 8] |= 0x80u8 >> ((px as usize % 8) as u8);
            }
        });
        Ok(())
    }
}
