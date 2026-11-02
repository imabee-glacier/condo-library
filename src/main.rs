use clap::{Parser, Subcommand};
use condo::tools;
use condo::{
    Client, Credentials, Error, Pagination, Result, Store, check_new_bills, marshal_jsonl,
    unmarshal_bills_jsonl,
};
use serde_json::{Value, json};
use std::{io::Read, path::PathBuf};
use zeroize::Zeroizing;

#[derive(Parser)]
#[command(
    name = "condo",
    version = env!("CONDO_VERSION"),
    about = "Direct HTTP resident portal SDK for AI agents"
)]
struct Args {
    /// Refuse every portal HTTP call. Offline previews and local setup still work.
    #[arg(long, global = true)]
    offline: bool,
    #[arg(long,global=true,env="CONDO_BASE_URL",default_value=condo::DEFAULT_BASE_URL)]
    base_url: String,
    #[arg(long, global = true, env = "CONDO_STORE_DIR")]
    store_dir: Option<PathBuf>,
    #[command(subcommand)]
    command: Command,
}
#[derive(Subcommand)]
enum Command {
    Tools,
    Unmapped,
    /// Read strict JSON input from stdin and run an agent tool.
    Tool {
        name: String,
    },
    SetupAuth {
        #[arg(long)]
        username: String,
        #[arg(long)]
        from_file: Option<PathBuf>,
    },
    ListOpenBills {
        #[arg(long)]
        all: bool,
        #[arg(long)]
        max_pages: Option<usize>,
    },
    ListPaidBills {
        #[arg(long)]
        all: bool,
        #[arg(long)]
        max_pages: Option<usize>,
    },
    ListDocuments,
    ListNotices,
    NoticeDetail {
        #[arg(long)]
        id: String,
    },
    ListReservations,
    ListRequests,
    ListOccurrences,
    FetchDocumentPdf {
        #[arg(long)]
        id: String,
        #[arg(long)]
        output: Option<PathBuf>,
    },
    PreviewRequest {
        #[arg(long)]
        title: String,
        #[arg(long)]
        subject: String,
    },
    /// Pass the preview JSON on stdin; --confirm is required. Endpoint remains unavailable.
    CreateRequest {
        #[arg(long)]
        confirm: bool,
    },
    PreviewReservation {
        #[arg(long)]
        area: String,
        #[arg(long)]
        date: String,
        #[arg(long)]
        period: String,
    },
    PreReserve {
        #[arg(long)]
        confirm: bool,
    },
    CheckNewBills {
        #[arg(long)]
        state: PathBuf,
        #[arg(long)]
        update_state: bool,
    },
    /// Delete locally cached session cookies. Does not claim server-side revocation.
    ForgetSession,
}
fn stdin_json() -> Result<Value> {
    let mut bytes = Vec::new();
    std::io::stdin()
        .take(1024 * 1024 + 1)
        .read_to_end(&mut bytes)?;
    if bytes.len() > 1024 * 1024 {
        return Err(Error::TooLarge);
    }
    serde_json::from_slice(&bytes).map_err(|_| Error::Validation("stdin must contain valid JSON"))
}
fn client(a: &Args) -> Result<Client> {
    if a.offline {
        return Client::offline(&a.base_url);
    }
    if let Some(dir) = &a.store_dir {
        Client::from_store(&a.base_url, Store::open(dir)?)
    } else {
        Client::new(&a.base_url)
    }
}
fn run(a: &Args) -> Result<Value> {
    match &a.command {
        Command::Tools => return Ok(json!(tools::tools())),
        Command::ForgetSession => {
            let store = Store::open(
                a.store_dir
                    .as_ref()
                    .ok_or(Error::Validation("--store-dir is required"))?,
            )?;
            store.clear_session()?;
            return Ok(json!({"forgotten":true}));
        }
        Command::Unmapped => return Ok(json!(tools::unmapped_tools())),
        Command::SetupAuth {
            username,
            from_file,
        } => {
            let base = Client::normalize_base(&a.base_url)?;
            let store = Store::open(
                a.store_dir
                    .as_ref()
                    .ok_or(Error::Validation("--store-dir is required"))?,
            )?;
            if let Some(path) = from_file {
                store.seed_from_file(base.as_str(), username, path)?;
            } else {
                let password = Zeroizing::new(rpassword::prompt_password("Password (no echo): ")?);
                store.enroll(
                    base.as_str(),
                    &Credentials {
                        username: username.clone(),
                        password: password.to_string(),
                    },
                )?;
            }
            return Ok(json!({"enrolled":true,"source_deleted":from_file.is_some()}));
        }
        Command::PreviewRequest { title, subject } => {
            return Ok(json!(tools::preview_request(title, subject)?));
        }
        Command::PreviewReservation { area, date, period } => {
            return Ok(json!(tools::preview_reservation(area, date, period)?));
        }
        Command::CreateRequest { confirm } => {
            let p = serde_json::from_value(stdin_json()?)
                .map_err(|_| Error::Validation("invalid preview"))?;
            tools::create_request(&p, *confirm)?;
            return Ok(Value::Null);
        }
        Command::PreReserve { confirm } => {
            let p = serde_json::from_value(stdin_json()?)
                .map_err(|_| Error::Validation("invalid preview"))?;
            tools::pre_reserve_reservation(&p, *confirm)?;
            return Ok(Value::Null);
        }
        Command::Tool { name } => {
            let value = stdin_json()?;
            if matches!(
                name.as_str(),
                "preview_request"
                    | "preview_reservation"
                    | "create_request"
                    | "pre_reserve_reservation"
            ) {
                return tools::execute_offline(name, value);
            }
            // Validate the name before opening credentials or making network calls.
            if !tools::tools().iter().any(|t| t.name == name) {
                return Err(Error::NotAvailable);
            }
            return client(a)?.execute_tool(name, value);
        }
        _ => {}
    }
    let mut c = client(a)?;
    match &a.command {
        Command::ListOpenBills { all, max_pages } => Ok(json!(c.list_open_bills(Pagination {
            all: *all,
            max_pages: *max_pages
        })?)),
        Command::ListPaidBills { all, max_pages } => Ok(json!(c.list_paid_bills(Pagination {
            all: *all,
            max_pages: *max_pages
        })?)),
        Command::ListDocuments => Ok(json!(c.list_documents()?)),
        Command::ListNotices => Ok(json!(c.list_notices()?)),
        Command::NoticeDetail { id } => Ok(json!(c.notice_detail(id)?)),
        Command::ListReservations => Ok(json!(c.list_reservation_availability()?)),
        Command::ListRequests => Ok(json!(c.list_requests()?)),
        Command::ListOccurrences => Ok(json!(c.list_occurrences()?)),
        Command::FetchDocumentPdf { id, output } => {
            use sha2::{Digest, Sha256};
            let bytes = c.fetch_document_pdf(id)?;
            if let Some(path) = output {
                condo::store::write_private(path, &bytes)?;
            }
            Ok(
                json!({"document_id":id,"size_bytes":bytes.len(),"sha256":format!("{:x}",Sha256::digest(&bytes)),"content_type":"application/pdf"}),
            )
        }
        Command::CheckNewBills {
            state,
            update_state,
        } => {
            let previous = match std::fs::read(state) {
                Ok(bytes) => unmarshal_bills_jsonl(&bytes)?,
                Err(e) if e.kind() == std::io::ErrorKind::NotFound => Vec::new(),
                Err(e) => return Err(e.into()),
            };
            let current = c.list_open_bills(Pagination {
                all: true,
                max_pages: None,
            })?;
            let new_bills = check_new_bills(&current, &previous);
            if *update_state {
                condo::store::write_private(state, &marshal_jsonl(&current)?)?;
            }
            Ok(json!({"new_bills":new_bills,"count":new_bills.len()}))
        }
        Command::ForgetSession => {
            c.forget_session()?;
            Ok(json!({"forgotten":true}))
        }
        _ => Err(Error::NotAvailable),
    }
}
fn main() {
    let a = Args::parse();
    let value = match run(&a) {
        Ok(v) => v,
        Err(e) => {
            eprintln!("{}", json!({"error":e.as_json()}));
            std::process::exit(1);
        }
    };
    // One JSON value on stdout per invocation; diagnostics never include secrets.
    if serde_json::to_writer(std::io::stdout().lock(), &value).is_err() {
        std::process::exit(1);
    }
    println!();
}
