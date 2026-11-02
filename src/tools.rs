//! Agent-facing strict tool inputs, matching schemas, and explicit safety metadata.
use crate::client::{required, valid_date};
use crate::{
    Bill, Client, Document, Error, Notice, NoticeDetail, Occurrence, Pagination, Request,
    ReservationSlot, Result, check_new_bills,
};
use schemars::{JsonSchema, schema_for};
use serde::{Deserialize, Serialize, de::DeserializeOwned};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};

#[derive(Serialize, Deserialize, JsonSchema, Clone, Debug, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct RequestPreview {
    pub action: String,
    pub title: String,
    pub subject: String,
    pub preview_id: String,
}
#[derive(Serialize, Deserialize, JsonSchema, Clone, Debug, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct ReservationPreview {
    pub action: String,
    pub area: String,
    pub date: String,
    pub period: String,
    pub preview_id: String,
}
fn fingerprint<T: Serialize>(payload: &T) -> String {
    format!(
        "{:x}",
        Sha256::digest(serde_json::to_vec(payload).expect("serializable preview"))
    )
}
pub fn preview_request(title: &str, subject: &str) -> Result<RequestPreview> {
    let title = title.trim();
    let subject = subject.trim();
    required(title)?;
    required(subject)?;
    Ok(RequestPreview {
        action: "create_request".into(),
        title: title.into(),
        subject: subject.into(),
        preview_id: fingerprint(&("create_request", title, subject)),
    })
}
pub fn preview_reservation(area: &str, date: &str, period: &str) -> Result<ReservationPreview> {
    let area = area.trim();
    let period = period.trim();
    required(area)?;
    required(period)?;
    let parts: Vec<_> = date.split('-').collect();
    let n: Option<Vec<u32>> = parts.iter().map(|p| p.parse().ok()).collect();
    let valid = n.is_some_and(|n| {
        parts.len() == 3
            && parts[0].len() == 4
            && parts[1].len() == 2
            && parts[2].len() == 2
            && valid_date(n[0], n[1], n[2])
    });
    if !valid {
        return Err(Error::Validation("date must be a valid YYYY-MM-DD date"));
    }
    Ok(ReservationPreview {
        action: "pre_reserve_reservation".into(),
        area: area.into(),
        date: date.into(),
        period: period.into(),
        preview_id: fingerprint(&("pre_reserve_reservation", area, date, period)),
    })
}
pub fn create_request(p: &RequestPreview, confirm: bool) -> Result<()> {
    if *p != preview_request(&p.title, &p.subject)? {
        return Err(Error::Validation("preview does not match payload"));
    }
    if !confirm {
        return Err(Error::ConfirmationRequired);
    }
    Err(Error::SubmissionUndiscovered)
}
pub fn pre_reserve_reservation(p: &ReservationPreview, confirm: bool) -> Result<()> {
    if *p != preview_reservation(&p.area, &p.date, &p.period)? {
        return Err(Error::Validation("preview does not match payload"));
    }
    if !confirm {
        return Err(Error::ConfirmationRequired);
    }
    Err(Error::SubmissionUndiscovered)
}
pub fn validate_destructive(
    preview_id: &str,
    target: &str,
    restated_target: &str,
    confirm: bool,
) -> Result<()> {
    required(preview_id)?;
    required(target)?;
    if !confirm {
        return Err(Error::ConfirmationRequired);
    }
    if target != restated_target {
        return Err(Error::Validation("target must match exactly"));
    }
    Ok(())
}
#[derive(Deserialize, JsonSchema)]
#[serde(deny_unknown_fields)]
struct Empty {}
#[derive(Deserialize, JsonSchema)]
#[serde(deny_unknown_fields)]
struct Id {
    id: String,
}
#[derive(Deserialize, JsonSchema)]
#[serde(deny_unknown_fields)]
struct RequestInput {
    title: String,
    subject: String,
}
#[derive(Deserialize, JsonSchema)]
#[serde(deny_unknown_fields)]
struct ReservationInput {
    area: String,
    date: String,
    period: String,
}
#[derive(Deserialize, JsonSchema)]
#[serde(deny_unknown_fields)]
struct ConfirmRequest {
    preview: RequestPreview,
    confirm: bool,
}
#[derive(Deserialize, JsonSchema)]
#[serde(deny_unknown_fields)]
struct ConfirmReservation {
    preview: ReservationPreview,
    confirm: bool,
}
#[derive(Deserialize, JsonSchema)]
#[serde(deny_unknown_fields)]
struct CheckInput {
    #[serde(default)]
    previous: Vec<Bill>,
}
#[derive(Serialize, JsonSchema)]
pub struct PdfInfo {
    pub document_id: String,
    pub size_bytes: usize,
    pub sha256: String,
    pub content_type: String,
}
#[derive(Serialize, JsonSchema)]
struct CheckOutput {
    new_bills: Vec<Bill>,
    count: usize,
}
#[derive(Serialize, Debug)]
pub struct Tool {
    pub name: &'static str,
    pub description: &'static str,
    pub input_schema: Value,
    pub output_schema: Value,
    pub annotations: Annotations,
}
#[derive(Serialize, Debug)]
pub struct Annotations {
    pub read_only: bool,
    pub destructive: bool,
    pub idempotent: bool,
    pub moves_money: bool,
    pub changes_state: bool,
}
fn schema<T: JsonSchema>() -> Value {
    serde_json::to_value(schema_for!(T)).unwrap()
}
fn tool<I: JsonSchema>(
    name: &'static str,
    description: &'static str,
    output: Value,
    changes: bool,
) -> Tool {
    Tool {
        name,
        description,
        input_schema: schema::<I>(),
        output_schema: output,
        annotations: Annotations {
            read_only: !changes,
            destructive: false,
            idempotent: !changes,
            moves_money: false,
            changes_state: changes,
        },
    }
}
pub fn tools() -> Vec<Tool> {
    vec![
        tool::<Pagination>(
            "list_open_bills",
            "List open bills. all=true retrieves every page or fails if incomplete.",
            schema::<Vec<Bill>>(),
            false,
        ),
        tool::<Pagination>(
            "list_paid_bills",
            "List paid bills; money is an exact decimal string.",
            schema::<Vec<Bill>>(),
            false,
        ),
        tool::<Empty>(
            "list_documents",
            "List document metadata without downloading content.",
            schema::<Vec<Document>>(),
            false,
        ),
        tool::<Empty>(
            "list_notices",
            "List notices. Unknown senders are omitted.",
            schema::<Vec<Notice>>(),
            false,
        ),
        tool::<Id>(
            "notice_detail",
            "Read notice from list API without opening its page.",
            schema::<NoticeDetail>(),
            false,
        ),
        tool::<Empty>(
            "list_reservation_availability",
            "List availability slots; unknown values remain unknown.",
            schema::<Vec<ReservationSlot>>(),
            false,
        ),
        tool::<Empty>(
            "list_requests",
            "List requests; an empty list is a valid result.",
            schema::<Vec<Request>>(),
            false,
        ),
        tool::<Empty>(
            "list_occurrences",
            "List occurrences.",
            schema::<Vec<Occurrence>>(),
            false,
        ),
        tool::<Id>(
            "fetch_document_pdf",
            "Explicitly fetch a PDF and return size and SHA-256. No PDF bytes in tool output.",
            schema::<PdfInfo>(),
            false,
        ),
        tool::<CheckInput>(
            "check_new_bills",
            "Fetch all open bills and compare with previous records.",
            schema::<CheckOutput>(),
            false,
        ),
        tool::<RequestInput>(
            "preview_request",
            "Preview a request without sending it.",
            schema::<RequestPreview>(),
            false,
        ),
        tool::<ConfirmRequest>(
            "create_request",
            "Requires an unchanged preview and confirmation. Submission unavailable until endpoint is verified.",
            json!({"type":"null"}),
            true,
        ),
        tool::<ReservationInput>(
            "preview_reservation",
            "Preview a reservation without sending it.",
            schema::<ReservationPreview>(),
            false,
        ),
        tool::<ConfirmReservation>(
            "pre_reserve_reservation",
            "Requires unchanged preview and confirmation. Submission endpoint remains unverified.",
            json!({"type":"null"}),
            true,
        ),
    ]
}
pub fn unmapped_tools() -> [&'static str; 6] {
    [
        "register_visitor",
        "release_visitor",
        "assembly_vote",
        "update_profile",
        "change_payment_method",
        "pay_bill",
    ]
}
fn input<T: DeserializeOwned>(v: Value) -> Result<T> {
    serde_json::from_value(v).map_err(|_| Error::Validation("tool input does not match schema"))
}
fn output<T: Serialize>(v: T) -> Result<Value> {
    serde_json::to_value(v).map_err(|_| Error::Parse)
}
/// Offline tools never construct a client or access credentials.
pub fn execute_offline(name: &str, v: Value) -> Result<Value> {
    match name {
        "preview_request" => {
            let p: RequestInput = input(v)?;
            output(preview_request(&p.title, &p.subject)?)
        }
        "preview_reservation" => {
            let p: ReservationInput = input(v)?;
            output(preview_reservation(&p.area, &p.date, &p.period)?)
        }
        "create_request" => {
            let p: ConfirmRequest = input(v)?;
            create_request(&p.preview, p.confirm)?;
            Ok(Value::Null)
        }
        "pre_reserve_reservation" => {
            let p: ConfirmReservation = input(v)?;
            pre_reserve_reservation(&p.preview, p.confirm)?;
            Ok(Value::Null)
        }
        _ => Err(Error::NotAvailable),
    }
}
impl Client {
    pub fn fetch_document_pdf_info(&mut self, id: &str) -> Result<PdfInfo> {
        let bytes = self.fetch_document_pdf(id)?;
        Ok(PdfInfo {
            document_id: id.into(),
            size_bytes: bytes.len(),
            sha256: format!("{:x}", Sha256::digest(&bytes)),
            content_type: "application/pdf".into(),
        })
    }
    pub fn execute_tool(&mut self, name: &str, v: Value) -> Result<Value> {
        match name {
            "list_open_bills" => output(self.list_open_bills(input(v)?)?),
            "list_paid_bills" => output(self.list_paid_bills(input(v)?)?),
            "list_documents" => {
                let _: Empty = input(v)?;
                output(self.list_documents()?)
            }
            "list_notices" => {
                let _: Empty = input(v)?;
                output(self.list_notices()?)
            }
            "notice_detail" => {
                let p: Id = input(v)?;
                output(self.notice_detail(&p.id)?)
            }
            "list_reservation_availability" => {
                let _: Empty = input(v)?;
                output(self.list_reservation_availability()?)
            }
            "list_requests" => {
                let _: Empty = input(v)?;
                output(self.list_requests()?)
            }
            "list_occurrences" => {
                let _: Empty = input(v)?;
                output(self.list_occurrences()?)
            }
            "fetch_document_pdf" => {
                let p: Id = input(v)?;
                output(self.fetch_document_pdf_info(&p.id)?)
            }
            "check_new_bills" => {
                let p: CheckInput = input(v)?;
                let current = self.list_open_bills(Pagination {
                    all: true,
                    max_pages: None,
                })?;
                let new_bills = check_new_bills(&current, &p.previous);
                output(CheckOutput {
                    count: new_bills.len(),
                    new_bills,
                })
            }
            _ => execute_offline(name, v),
        }
    }
}
impl Error {
    pub fn code(&self) -> &'static str {
        match self {
            Self::Offline => "offline",
            Self::Validation(_) => "validation",
            Self::Unauthenticated => "unauthenticated",
            Self::Transport(_) => "transport",
            Self::Http(_) => "http",
            Self::Api => "api",
            Self::Parse => "parse",
            Self::TooLarge => "response_too_large",
            Self::Io(_) => "io",
            Self::Crypto => "crypto",
            Self::ConfirmationRequired => "confirmation_required",
            Self::SubmissionUndiscovered => "submission_endpoint_undiscovered",
            Self::NotAvailable => "not_available",
        }
    }
    pub fn as_json(&self) -> Value {
        json!({"code":self.code(),"message":self.to_string(),"retryable":matches!(self,Self::Transport(_)|Self::Http(429|502|503|504))})
    }
}
