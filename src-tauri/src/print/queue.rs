use serde::{ Serialize, Deserialize };
use rusqlite::{ params, OptionalExtension };
use crate::{
    config::storage::Storage,
    error::{ AgentError, Result },
    printers::{ Printer, Transport },
    protocol::{ Document, valid_id },
};
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Job {
    pub job_id: String,
    pub printer_id: String,
    pub status: String,
    pub attempts: u32,
    pub created_at: i64,
    pub next_at: i64,
    pub error: Option<AgentError>,
}
#[derive(Debug)]
pub struct Work {
    pub job: Job,
    pub printer: Printer,
    pub document: Document,
}
pub fn now() -> i64 {
    std::time::SystemTime
        ::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs() as i64
}
pub fn backoff(attempt: u32) -> i64 {
    (1i64 << attempt.min(6)).min(60)
}
fn read_job(r: &rusqlite::Row<'_>) -> rusqlite::Result<Job> {
    let error: Option<String> = r.get(6)?;
    Ok(Job {
        job_id: r.get(0)?,
        printer_id: r.get(1)?,
        status: r.get(2)?,
        attempts: r.get(3)?,
        created_at: r.get(4)?,
        next_at: r.get(5)?,
        error: error.and_then(|s| serde_json::from_str(&s).ok()),
    })
}
const COLS: &str = "job_id,printer_id,status,attempts,created_at,next_at,error";
impl Storage {
    pub fn job(&self, id: &str) -> Result<Job> {
        self.db
            .query_row(&format!("SELECT {COLS} FROM jobs WHERE job_id=?1"), [id], read_job)
            .optional()?
            .ok_or_else(|| AgentError::new("JOB_NOT_FOUND", "Job ID not found"))
    }
    pub fn queue(&self) -> Result<Vec<Job>> {
        let mut s = self.db.prepare(
            &format!(
                "SELECT {COLS} FROM jobs ORDER BY CASE WHEN status IN ('queued','printing') THEN 0 ELSE 1 END,created_at DESC,job_id LIMIT 500"
            )
        )?;
        let rows = s.query_map([], read_job)?;
        Ok(rows.collect::<std::result::Result<Vec<_>, _>>()?)
    }
    pub fn enqueue(&mut self, id: &str, p: &Printer, d: &Document) -> Result<Job> {
        if !valid_id(id) {
            return Err(AgentError::new("INVALID_JOB", "Invalid job ID"));
        }
        p.validate()?;
        d.validate()?;
        let json = serde_json::to_string(d)?;
        let profile = serde_json::to_string(p)?;
        let tx = self.db.transaction()?;
        let old: Option<(String, String)> = tx
            .query_row("SELECT printer_id,document FROM jobs WHERE job_id=?1", [id], |r|
                Ok((r.get(0)?, r.get(1)?))
            )
            .optional()?;
        if let Some((printer, document)) = old {
            if printer != p.id || document != json {
                return Err(
                    AgentError::new(
                        "JOB_ID_CONFLICT",
                        "This job ID already belongs to a different request"
                    )
                );
            }
        } else {
            let count: i64 = tx.query_row(
                "SELECT COUNT(*) FROM jobs WHERE status IN ('queued','printing')",
                [],
                |r| r.get(0)
            )?;
            if count >= 256 {
                return Err(
                    AgentError::new("QUEUE_FULL", "Resolve pending jobs before submitting more")
                );
            }
            tx.execute(
                "INSERT INTO jobs VALUES (?1,?2,'queued',0,?3,?3,?4,?5,NULL)",
                params![id, p.id, now(), json, profile]
            )?;
        }
        tx.commit()?;
        self.job(id)
    }
    pub fn claim(&mut self, t: i64) -> Result<Option<Work>> {
        let tx = self.db.transaction()?;
        let row: Option<(String, String, String)> = tx
            .query_row(
                "SELECT job_id,profile,document FROM jobs WHERE status='queued' AND next_at<=?1 AND NOT EXISTS (SELECT 1 FROM jobs prior WHERE prior.printer_id=jobs.printer_id AND prior.status IN ('queued','printing') AND (prior.created_at<jobs.created_at OR (prior.created_at=jobs.created_at AND prior.rowid<jobs.rowid))) ORDER BY created_at,rowid LIMIT 1",
                [t],
                |r| Ok((r.get(0)?, r.get(1)?, r.get(2)?))
            )
            .optional()?;
        let Some((id, p, d)) = row else {
            return Ok(None);
        };
        let printer = serde_json::from_str(&p)?;
        let document = serde_json::from_str(&d)?;
        tx.execute(
            "UPDATE jobs SET status='printing', attempts=attempts+1,error=NULL WHERE job_id=?1",
            [&id]
        )?;
        tx.commit()?;
        Ok(Some(Work { job: self.job(&id)?, printer, document }))
    }
    pub fn finish(&self, id: &str, result: Result<()>, max: u32, t: i64) -> Result<Job> {
        let j = self.job(id)?;
        if j.status != "printing" {
            return Err(AgentError::new("QUEUE_ERROR", "Job is not claimed"));
        }
        let (status, error, next) = match result {
            Ok(()) => ("completed", None, t),
            Err(e) => {
                let retry = e.retryable && !e.uncertain && j.attempts < max;
                (
                    if retry { "queued" } else { "failed" },
                    Some(serde_json::to_string(&e)?),
                    t + backoff(j.attempts),
                )
            }
        };
        self.db.execute(
            "UPDATE jobs SET status=?2,error=?3,next_at=?4 WHERE job_id=?1",
            params![id, status, error, next]
        )?;
        self.job(id)
    }
    pub fn cancel(&self, id: &str) -> Result<Job> {
        let j = self.job(id)?;
        if j.status == "cancelled" {
            return Ok(j);
        }
        if j.status != "queued" {
            return Err(AgentError::new("JOB_NOT_CANCELLABLE", "Only queued jobs can be cancelled"));
        }
        self.db.execute("UPDATE jobs SET status='cancelled' WHERE job_id=?1", [id])?;
        self.job(id)
    }
}
pub fn deliver(transport: &dyn Transport, printer: &Printer, bytes: &[u8]) -> Result<()> {
    for copy in 0..printer.copies {
        if let Err(e) = transport.send(printer, bytes) {
            return Err(if copy > 0 { AgentError::uncertain() } else { e });
        }
    }
    Ok(())
}
#[cfg(test)]
mod tests {
    use super::*;
    use crate::printers::Connection;
    fn printer() -> Printer {
        Printer {
            id: "p1".into(),
            name: "Test".into(),
            connection: Connection::Network { host: "192.168.1.50".into(), port: 9100 },
            paper_mm: 80,
            width_dots: 576,
            copies: 1,
            cut: true,
            font_family: "Noto Sans Arabic".into(),
            font_size: 24,
        }
    }
    fn doc() -> Document {
        Document::Receipt { lines: vec!["unit test".into()] }
    }
    #[test]
    fn persistent_dedup_retry_and_crash() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("q.db");
        {
            let mut s = Storage::open(&path).unwrap();
            s.enqueue("order:1", &printer(), &doc()).unwrap();
            s.enqueue("order:1", &printer(), &doc()).unwrap();
            assert_eq!(s.queue().unwrap().len(), 1);
            let w = s.claim(now()).unwrap().unwrap();
            assert_eq!(w.job.attempts, 1);
            s.finish("order:1", Err(AgentError::retry("PRINTER_OFFLINE")), 3, now()).unwrap();
            assert!(s.claim(now()).unwrap().is_none());
            s.claim(now() + 10)
                .unwrap()
                .unwrap();
        }
        let s = Storage::open(&path).unwrap();
        let j = s.job("order:1").unwrap();
        assert_eq!(j.status, "failed");
        assert!(j.error.unwrap().uncertain);
    }
    #[test]
    fn completed_never_reprints_and_conflicts() {
        let d = tempfile::tempdir().unwrap();
        let mut s = Storage::open(&d.path().join("q.db")).unwrap();
        s.enqueue("id", &printer(), &doc()).unwrap();
        s.claim(now()).unwrap();
        s.finish("id", Ok(()), 3, now()).unwrap();
        assert_eq!(s.enqueue("id", &printer(), &doc()).unwrap().status, "completed");
        assert!(s.claim(now()).unwrap().is_none());
        assert!(
            s
                .enqueue("id", &printer(), &(Document::Receipt { lines: vec!["different".into()] }))
                .is_err()
        );
    }
    #[test]
    fn retry_limit_and_cancel() {
        let d = tempfile::tempdir().unwrap();
        let mut s = Storage::open(&d.path().join("q.db")).unwrap();
        s.enqueue("id", &printer(), &doc()).unwrap();
        s.claim(now()).unwrap();
        assert_eq!(
            s.finish("id", Err(AgentError::retry("PRINTER_OFFLINE")), 1, now()).unwrap().status,
            "failed"
        );
        s.enqueue("cancel", &printer(), &doc()).unwrap();
        assert_eq!(s.cancel("cancel").unwrap().status, "cancelled");
        assert_eq!(backoff(100), 60);
    }
    struct TestTransport;
    impl Transport for TestTransport {
        fn send(&self, _: &Printer, b: &[u8]) -> Result<()> {
            assert_eq!(&b[..2], &[27, 64]);
            Ok(())
        }
        fn status(&self, _: &Printer) -> String {
            "online".into()
        }
    }
    #[test]
    fn protocol_to_test_transport() {
        let request = crate::protocol
            ::parse(
                r#"{"version":1,"requestId":"r1","type":"print","jobId":"order:1","printerId":"p1","document":{"type":"receipt","lines":["test"]}}"#
            )
            .unwrap();
        let crate::protocol::Command::Print { job_id, document, .. } = request.command else {
            panic!()
        };
        let dir = tempfile::tempdir().unwrap();
        let mut s = Storage::open(&dir.path().join("q.db")).unwrap();
        s.enqueue(&job_id, &printer(), &document).unwrap();
        let w = s.claim(now()).unwrap().unwrap();
        let mut e = crate::print::escpos::Encoder::default();
        e.initialize();
        for l in w.document.lines() {
            e.text(&l).unwrap().line();
        }
        let result = deliver(&TestTransport, &w.printer, &e.0);
        assert_eq!(s.finish(&job_id, result, 3, now()).unwrap().status, "completed");
    }
}
