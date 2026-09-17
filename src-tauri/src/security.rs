use base64::{ engine::general_purpose::STANDARD, Engine };
use hmac::{ Hmac, Mac };
use sha2::Sha256;
use rand::RngCore;
use zeroize::Zeroize;
use crate::error::{ AgentError, Result };
pub fn origin_allowed(origin: &str) -> bool {
    matches!(origin, "https://menuvex.ir" | "https://www.menuvex.ir") ||
        (cfg!(debug_assertions) &&
            matches!(origin, "http://localhost:5173" | "http://127.0.0.1:5173"))
}
pub fn random_secret() -> String {
    let mut bytes = [0u8; 32];
    rand::rngs::OsRng.fill_bytes(&mut bytes);
    let s = STANDARD.encode(bytes);
    bytes.zeroize();
    s
}
pub struct Secret {
    key: Vec<u8>,
}
impl Drop for Secret {
    fn drop(&mut self) {
        self.key.zeroize();
    }
}
impl Secret {
    pub fn load() -> Result<Self> {
        let entry = keyring::Entry
            ::new("ir.menuvex.printer", "pairing-v1")
            .map_err(|_| storage_error())?;
        let mut encoded = match entry.get_password() {
            Ok(s) => s,
            Err(keyring::Error::NoEntry) => {
                let s = random_secret();
                entry.set_password(&s).map_err(|_| storage_error())?;
                s
            }
            Err(_) => {
                return Err(storage_error());
            }
        };
        let key = STANDARD.decode(&encoded).map_err(|_| storage_error())?;
        encoded.zeroize();
        if key.len() != 32 {
            return Err(storage_error());
        }
        Ok(Self { key })
    }
    pub fn reveal(&self) -> String {
        STANDARD.encode(&self.key)
    }
    pub fn rotate(&mut self) -> Result<()> {
        let mut encoded = random_secret();
        keyring::Entry
            ::new("ir.menuvex.printer", "pairing-v1")
            .map_err(|_| storage_error())?
            .set_password(&encoded)
            .map_err(|_| storage_error())?;
        self.key.zeroize();
        self.key = STANDARD.decode(&encoded).map_err(|_| storage_error())?;
        encoded.zeroize();
        Ok(())
    }
    pub fn server_proof(&self, nonce: &str, origin: &str) -> String {
        let mut mac = Hmac::<Sha256>::new_from_slice(&self.key).expect("HMAC accepts any key size");
        mac.update(format!("menuvex-print-agent:server:v1\n{nonce}\n{origin}").as_bytes());
        STANDARD.encode(mac.finalize().into_bytes())
    }
    pub fn verify(&self, nonce: &str, origin: &str, proof: &str) -> bool {
        let Ok(signature) = STANDARD.decode(proof) else {
            return false;
        };
        let Ok(mut mac) = Hmac::<Sha256>::new_from_slice(&self.key) else {
            return false;
        };
        mac.update(format!("menuvex-print-agent:v1\n{nonce}\n{origin}").as_bytes());
        mac.verify_slice(&signature).is_ok()
    }
}
fn storage_error() -> AgentError {
    AgentError::new(
        "SECURE_STORAGE_UNAVAILABLE",
        "Unlock the OS keychain / Secret Service. Plaintext fallback is disabled."
    )
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn origins() {
        assert!(origin_allowed("https://menuvex.ir"));
        assert!(!origin_allowed("https://menuvex.ir.evil.test"));
        assert!(!origin_allowed("null"));
    }
    #[test]
    fn proofs_are_nonce_bound() {
        let s = Secret { key: vec![5;32] };
        let mut mac = Hmac::<Sha256>::new_from_slice(&s.key).unwrap();
        mac.update(b"menuvex-print-agent:v1\nnonce\nhttps://menuvex.ir");
        let proof = STANDARD.encode(mac.finalize().into_bytes());
        assert!(s.verify("nonce", "https://menuvex.ir", &proof));
        assert!(!s.verify("other", "https://menuvex.ir", &proof));
    }
}
#[cfg(test)]
pub(crate) fn test_secret() -> Secret {
    Secret { key: vec![7;32] }
}
