//! Device-independent ESC/POS building blocks. Never accept raw bytes from the PWA.
use crate::error::{ AgentError, Result };
#[derive(Default)]
pub struct Encoder(pub Vec<u8>);
impl Encoder {
    pub fn initialize(&mut self) -> &mut Self {
        self.0.extend([0x1b, 0x40]);
        self
    }
    pub fn bold(&mut self, on: bool) -> &mut Self {
        self.0.extend([0x1b, 0x45, on as u8]);
        self
    }
    pub fn align(&mut self, position: u8) -> Result<&mut Self> {
        if position > 2 {
            return Err(invalid());
        }
        self.0.extend([0x1b, 0x61, position]);
        Ok(self)
    }
    pub fn size(&mut self, w: u8, h: u8) -> Result<&mut Self> {
        if !(1..=8).contains(&w) || !(1..=8).contains(&h) {
            return Err(invalid());
        }
        self.0.extend([0x1d, 0x21, ((w - 1) << 4) | (h - 1)]);
        Ok(self)
    }
    pub fn text(&mut self, s: &str) -> Result<&mut Self> {
        if !s.is_ascii() || !crate::protocol::text_ok(s, 8192) {
            return Err(invalid());
        }
        self.0.extend(s.bytes());
        Ok(self)
    }
    pub fn line(&mut self) -> &mut Self {
        self.0.push(10);
        self
    }
    pub fn feed(&mut self, n: u8) -> &mut Self {
        self.0.extend([0x1b, 0x64, n]);
        self
    }
    pub fn cut(&mut self) -> &mut Self {
        self.0.extend([0x1d, 0x56, 0]);
        self
    }
    pub fn drawer(&mut self) -> &mut Self {
        self.0.extend([0x1b, 0x70, 0, 25, 250]);
        self
    }
    pub fn raster(&mut self, width: u16, height: u16, bits: &[u8]) -> Result<&mut Self> {
        let stride = ((width as usize) + 7) / 8;
        if
            width == 0 ||
            width > 832 ||
            height == 0 ||
            height > 4096 ||
            bits.len() != stride * (height as usize)
        {
            return Err(invalid());
        }
        // GS v 0 in bounded stripes for printers with small buffers.
        for (index, chunk) in bits.chunks(stride * 128).enumerate() {
            let rows = ((height as usize) - index * 128).min(128) as u16;
            self.0.extend([
                0x1d,
                0x76,
                0x30,
                0,
                stride as u8,
                (stride >> 8) as u8,
                rows as u8,
                (rows >> 8) as u8,
            ]);
            self.0.extend(chunk);
        }
        Ok(self)
    }
    pub fn qr(&mut self, data: &str) -> Result<&mut Self> {
        if data.len() > 2048 {
            return Err(invalid());
        }
        self.0.extend([
            0x1d, 0x28, 0x6b, 4, 0, 49, 65, 50, 0, 0x1d, 0x28, 0x6b, 3, 0, 49, 67, 5, 0x1d, 0x28, 0x6b,
            3, 0, 49, 69, 48,
        ]);
        let n = (data.len() + 3) as u16;
        self.0.extend([0x1d, 0x28, 0x6b, n as u8, (n >> 8) as u8, 49, 80, 48]);
        self.0.extend(data.bytes());
        self.0.extend([0x1d, 0x28, 0x6b, 3, 0, 49, 81, 48]);
        Ok(self)
    }
    pub fn barcode(&mut self, data: &str) -> Result<&mut Self> {
        if
            data.is_empty() ||
            data.len() > 80 ||
            !data.bytes().all(|b| (32..=126).contains(&b) && b != b'{')
        {
            return Err(invalid());
        }
        self.0.extend([
            0x1d,
            0x68,
            80,
            0x1d,
            0x48,
            2,
            0x1d,
            0x6b,
            73,
            (data.len() + 2) as u8,
            b'{',
            b'B',
        ]);
        self.0.extend(data.bytes());
        Ok(self)
    }
    pub fn table(&mut self, rows: &[Vec<String>], width: usize) -> Result<&mut Self> {
        if width == 0 || width > 64 {
            return Err(invalid());
        }
        for row in rows {
            for cell in row {
                if cell.len() > width {
                    return Err(invalid());
                }
                self.text(&format!("{cell:width$}"))?;
            }
            self.line();
        }
        Ok(self)
    }
}
fn invalid() -> AgentError {
    AgentError::new("ESC_POS_ERROR", "Invalid encoder input")
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn controls() {
        let mut e = Encoder::default();
        e.initialize().bold(true).feed(1).cut();
        assert_eq!(e.0, vec![27, 64, 27, 69, 1, 27, 100, 1, 29, 86, 0]);
    }
    #[test]
    fn raster_size() {
        let mut e = Encoder::default();
        assert!(e.raster(8, 1, &[128]).is_ok());
        assert_eq!(&e.0[..8], &[29, 118, 48, 0, 1, 0, 1, 0]);
        assert!(e.raster(8, 2, &[0]).is_err());
        assert!(e.text("سلام").is_err());
    }
}
