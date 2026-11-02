#![doc = include_str!("../README.md")]
pub mod client;
pub mod models;
pub use models::*;
pub mod store;
pub mod tools;
pub use client::{Client, DEFAULT_BASE_URL, Pagination};
use serde::{Deserialize, Serialize};
pub use store::{Credentials, Store};

#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("offline: network access is disabled")]
    Offline,
    #[error("validation: {0}")]
    Validation(&'static str),
    #[error("unauthenticated: login required or rejected")]
    Unauthenticated,
    #[error("HTTP request failed ({0})")]
    Transport(&'static str),
    #[error("portal HTTP status {0}")]
    Http(u16),
    #[error("portal API rejected the request")]
    Api,
    #[error("invalid portal response")]
    Parse,
    #[error("response exceeds size limit")]
    TooLarge,
    #[error("private store I/O failed")]
    Io(#[from] std::io::Error),
    #[error("encrypted store could not be authenticated")]
    Crypto,
    #[error("confirmation_required")]
    ConfirmationRequired,
    #[error("submission_endpoint_undiscovered")]
    SubmissionUndiscovered,
    #[error("not_available")]
    NotAvailable,
}
pub type Result<T> = std::result::Result<T, Error>;

#[derive(Clone, Debug, Default, Serialize, Deserialize, schemars::JsonSchema, PartialEq, Eq)]
pub struct Bill {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub id: String,
    #[serde(default)]
    pub status: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub period: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub due_date: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub paid_date: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub amount_brl: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub label: String,
}
impl Bill {
    fn key(&self) -> String {
        if !self.id.is_empty() {
            format!("id:{}", self.id)
        } else {
            serde_json::to_string(&(&self.due_date, &self.label, &self.amount_brl)).unwrap()
        }
    }
}
pub fn check_new_bills(current: &[Bill], previous: &[Bill]) -> Vec<Bill> {
    let mut seen: std::collections::HashSet<_> = previous.iter().map(Bill::key).collect();
    current
        .iter()
        .filter(|b| seen.insert(b.key()))
        .cloned()
        .collect()
}
pub fn marshal_jsonl(bills: &[Bill]) -> Result<Vec<u8>> {
    let mut bytes = Vec::new();
    for bill in bills {
        serde_json::to_writer(&mut bytes, bill).map_err(|_| Error::Parse)?;
        bytes.push(b'\n');
    }
    Ok(bytes)
}
pub fn unmarshal_bills_jsonl(bytes: &[u8]) -> Result<Vec<Bill>> {
    bytes
        .split(|b| *b == b'\n')
        .filter(|l| !l.iter().all(u8::is_ascii_whitespace))
        .map(|l| serde_json::from_slice(l).map_err(|_| Error::Parse))
        .collect()
}
