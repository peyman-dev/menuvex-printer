pub mod escpos;
pub mod queue;
use cosmic_text::{ Attrs, Buffer, Color, Family, FontSystem, Metrics, Shaping, SwashCache };
use crate::{ error::{ AgentError, Result }, printers::Printer, protocol::Document };
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
        let mut encoder = escpos::Encoder::default();
        encoder.initialize();
        let text = doc.lines().join("\n");
        let metrics = Metrics::new(p.font_size as f32, (p.font_size as f32) * 1.5);
        let mut buffer = Buffer::new(&mut self.fonts, metrics);
        let mut b = buffer.borrow_with(&mut self.fonts);
        b.set_size(Some(p.width_dots as f32), None);
        b.set_text(&text, &Attrs::new().family(Family::Name(&p.font_family)), Shaping::Advanced);
        b.shape_until_scroll(true);
        let mut height = 1f32;
        for run in b.layout_runs() {
            height = height.max(run.line_top + run.line_height);
            if run.glyphs.iter().any(|g| g.glyph_id == 0) {
                return Err(
                    AgentError::new(
                        "FONT_GLYPH_MISSING",
                        "Configured fonts cannot render this document"
                    )
                );
            }
        }
        if height > 4096.0 {
            return Err(
                AgentError::new(
                    "INVALID_JOB",
                    "Rendered document exceeds 4096 rows; split the receipt"
                )
            );
        }
        let height = height.ceil() as u16;
        let stride = (p.width_dots as usize) / 8;
        let mut bits = vec![0u8;stride*height as usize];
        b.draw(&mut self.cache, Color::rgb(0, 0, 0), |x, y, w, h, color| {
            if color.a() < 100 {
                return;
            }
            for yy in y.max(0)..(y + (h as i32)).min(height as i32) {
                for xx in x.max(0)..(x + (w as i32)).min(p.width_dots as i32) {
                    bits[(yy as usize) * stride + (xx as usize) / 8] |= 0x80 >> xx % 8;
                }
            }
        });
        encoder.raster(p.width_dots, height, &bits)?;
        encoder.feed(3);
        if p.cut {
            encoder.cut();
        }
        Ok(encoder.0)
    }
}
