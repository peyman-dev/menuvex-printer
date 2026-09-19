use std::path::Path;
use rusqlite::{ Connection, params };
use crate::{ config::Config, error::Result };
pub struct Storage {
    pub(crate) db: Connection,
}
impl Storage {
    pub fn open(path: &Path) -> Result<Self> {
        let db = Connection::open(path)?;
        let version: u32 = db.query_row("PRAGMA user_version", [], |r| r.get(0))?;
        if version > 1 {
            return Err(
                crate::error::AgentError::new(
                    "SCHEMA_VERSION_UNSUPPORTED",
                    "Database belongs to a newer Agent; refusing downgrade"
                )
            );
        }
        db.busy_timeout(std::time::Duration::from_secs(5))?;
        db.execute_batch(
            "PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA foreign_keys=ON;
   CREATE TABLE IF NOT EXISTS config (id INTEGER PRIMARY KEY CHECK(id=1), json TEXT NOT NULL);
   CREATE TABLE IF NOT EXISTS jobs (job_id TEXT PRIMARY KEY, printer_id TEXT NOT NULL, status TEXT NOT NULL, attempts INTEGER NOT NULL, created_at INTEGER NOT NULL, next_at INTEGER NOT NULL, document TEXT NOT NULL, profile TEXT NOT NULL, error TEXT);
   CREATE INDEX IF NOT EXISTS jobs_due ON jobs(status,next_at,created_at);
   PRAGMA user_version=1;"
        )?;
        // A crash while printing is ambiguous; do not automatically duplicate physical output.
        db.execute(
            "UPDATE jobs SET status='failed', error=?1 WHERE status='printing'",
            params![serde_json::to_string(&crate::error::AgentError::uncertain())?]
        )?;
        Ok(Self { db })
    }
    pub fn config(&self) -> Result<Config> {
        let raw = self.db.query_row("SELECT json FROM config WHERE id=1", [], |r|
            r.get::<_, String>(0)
        );
        match raw {
            Ok(s) => {
                let c: Config = serde_json::from_str(&s)?;
                c.validate()?;
                Ok(c)
            }
            Err(rusqlite::Error::QueryReturnedNoRows) => Ok(Config::default()),
            Err(e) => Err(e.into()),
        }
    }
    pub fn save_config(&self, c: &Config) -> Result<()> {
        c.validate()?;
        self.db.execute(
            "INSERT INTO config VALUES (1,?1) ON CONFLICT(id) DO UPDATE SET json=excluded.json",
            params![serde_json::to_string(c)?]
        )?;
        Ok(())
    }
}
