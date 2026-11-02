use condo::tools::{self, preview_request, preview_reservation};
use condo::{Bill, Client, Credentials, Error, Pagination, Store};
use serde_json::{Value, json};
use std::{
    fs,
    os::unix::fs::PermissionsExt,
    sync::{
        Arc,
        atomic::{AtomicUsize, Ordering},
    },
    thread,
};
use tiny_http::{Header, Response, Server};

struct Mock {
    base: String,
    login: Arc<AtomicUsize>,
    requests: Arc<AtomicUsize>,
    _thread: thread::JoinHandle<()>,
}
fn mock(mode: &'static str) -> Mock {
    let server = Server::http("127.0.0.1:0").unwrap();
    let base = format!("http://{}/clients/areadocondomino/", server.server_addr());
    let login = Arc::new(AtomicUsize::new(0));
    let logins = login.clone();
    let requests = Arc::new(AtomicUsize::new(0));
    let count = requests.clone();
    let handle = thread::spawn(move || {
        while let Ok(Some(mut req)) = server.recv_timeout(std::time::Duration::from_secs(2)) {
            count.fetch_add(1, Ordering::SeqCst);
            let path = req.url().to_owned();
            let cookie = req
                .headers()
                .iter()
                .find(|h| h.field.equiv("Cookie"))
                .map(|h| h.value.as_str())
                .unwrap_or("")
                .to_owned();
            let mut response = if path == "/clients/areadocondomino/" {
                let action = if mode == "hostile_form" {
                    "https://evil.invalid/auth"
                } else {
                    "/areadocondomino/atual/publico/auth"
                };
                {
                    let mut html = format!("<form action='{action}'><input name='email'><input type='password' name='senha'><input type='hidden' name='csrf' value='nonce'></form>").into_bytes();
                    if mode == "latin1" {
                        html.extend_from_slice(b"<p>Ol\xe1</p>");
                    }
                    Response::from_data(html)
                }
            } else if path.contains("publico/verificarcondomino") {
                Response::from_string(json!({"status":200,"data":"-1"}).to_string())
            } else if path.ends_with("publico/auth") {
                logins.fetch_add(1, Ordering::SeqCst);
                assert_eq!(req.method().as_str(), "POST");
                let mut body = String::new();
                req.as_reader().read_to_string(&mut body).unwrap();
                assert!(body.contains("senha=fake-password"));
                assert!(body.contains("csrf=nonce"));
                if mode == "bad_password" {
                    Response::from_string(
                        json!({"status":"401","data":null,"msg":"fake-password"}).to_string(),
                    )
                } else {
                    Response::from_string(json!({"status":"202","data":{}}).to_string())
                        .with_header(
                            Header::from_bytes("Set-Cookie", "sid=valid; Path=/; HttpOnly")
                                .unwrap(),
                        )
                }
            } else if mode == "redirect" {
                Response::from_string("")
                    .with_status_code(302)
                    .with_header(Header::from_bytes("Location", "https://evil.invalid/").unwrap())
            } else if mode == "invalid_api" {
                Response::from_string(json!({"status":"500","msg":"fake-password"}).to_string())
            } else if mode == "missing_data" {
                Response::from_string(json!({"status":"200"}).to_string())
            } else if mode == "malformed" {
                Response::from_string("garbage")
            } else if mode == "transient" && count.load(Ordering::SeqCst) == 1 {
                Response::from_string("").with_status_code(503)
            } else if mode == "always_expired"
                || (mode != "public"
                    && mode != "transient"
                    && mode != "repeat"
                    && mode != "pages"
                    && !cookie.contains("sid=valid")
                    && !cookie.contains("sid=rotated"))
            {
                if mode == "html_expiry" {
                    Response::from_string("<form><input type='password'></form>")
                } else if mode == "http_expiry" {
                    Response::from_string("").with_status_code(401)
                } else {
                    Response::from_string(json!({"status":"401","data":[]}).to_string())
                }
            } else if path.contains("documentos/download") {
                assert!(path.contains("id=a%26b%3D1"));
                Response::from_string("%PDF-1.4 fake")
            } else {
                let data = if path.contains("cobranca") {
                    if (mode == "pages" && path.contains("pagina=3"))
                        || (mode != "pages" && mode != "repeat" && path.contains("pagina=2"))
                    {
                        json!([])
                    } else {
                        json!([{"id_recebimento_recb":if path.contains("pagina=2") && mode=="pages" {"2"} else {"1"},"vl_total_recb":"9007199254740993.01","dt_vencimento_recb":"11/10/2026","dt_competencia_recb":"10/01/2026"}])
                    }
                } else if path.contains("impressoes") {
                    if path.contains("pagina=2") {
                        json!([])
                    } else {
                        json!([{"id_impressao_fimp":123,"st_titulo_fimp":"Report","dt_criacao_fimp":"10/29/2026"}])
                    }
                } else if path.contains("comunicados") {
                    json!([{"id_comunicado_com":"n1","st_titulo_com":"Notice","st_texto_com":"Body"}])
                } else if path.contains("reservas") {
                    json!([{"st_nome_are":"Hall - Morning","datas":{"11/02/2026":[],"11/03/2026":[1],"11/04/2026":null}}])
                } else if path.contains("tickets") {
                    json!([{"id":"r1","assunto":"Leak"}])
                } else if path.contains("ocorrencias") {
                    json!([{"id_ocorrencia_uoc":"o1","st_titulo_uoc":"Noise"}])
                } else {
                    json!([])
                };
                Response::from_string(json!({"status":"200","data":data}).to_string()).with_header(
                    Header::from_bytes("Set-Cookie", "sid=rotated; Path=/; HttpOnly").unwrap(),
                )
            };
            response.add_header(Header::from_bytes("Content-Type", "application/json").unwrap());
            req.respond(response).unwrap();
        }
    });
    Mock {
        base,
        login,
        requests,
        _thread: handle,
    }
}
fn creds() -> Credentials {
    Credentials {
        username: "fake@example.test".into(),
        password: "fake-password".into(),
    }
}
fn store(dir: &std::path::Path, base: &str) -> Store {
    if dir.exists() {
        fs::set_permissions(dir, fs::Permissions::from_mode(0o700)).unwrap();
    }
    let s = Store::open(dir).unwrap();
    s.enroll(Client::normalize_base(base).unwrap().as_str(), &creds())
        .unwrap();
    s
}
#[test]
fn encrypted_session_survives_restart_without_password_login() {
    let m = mock("session");
    let dir = tempfile::tempdir().unwrap();
    let s = store(dir.path(), &m.base);
    let mut c = Client::from_store(&m.base, s).unwrap();
    assert_eq!(
        c.list_open_bills(Pagination::default()).unwrap()[0].amount_brl,
        "9007199254740993.01"
    );
    assert_eq!(m.login.load(Ordering::SeqCst), 1);
    drop(c);
    let bytes = fs::read(dir.path().join("session.enc")).unwrap();
    assert!(!String::from_utf8_lossy(&bytes).contains("rotated"));
    // Removing credentials demonstrates that session reuse does not even load the password.
    fs::remove_file(dir.path().join("credentials.enc")).unwrap();
    let mut c = Client::from_store(&m.base, Store::open(dir.path()).unwrap()).unwrap();
    assert_eq!(c.list_open_bills(Pagination::default()).unwrap().len(), 1);
    assert_eq!(m.login.load(Ordering::SeqCst), 1);
    c.forget_session().unwrap();
    assert!(!dir.path().join("session.enc").exists());
}
#[test]
fn expiry_signals_renew_once() {
    for mode in ["session", "html_expiry", "http_expiry", "latin1"] {
        let m = mock(mode);
        let mut c = Client::with_credentials(&m.base, creds()).unwrap();
        assert_eq!(c.list_notices().unwrap().len(), 1);
        assert_eq!(m.login.load(Ordering::SeqCst), 1);
    }
}
#[test]
fn rejected_password_is_never_retried_and_errors_are_redacted() {
    let m = mock("bad_password");
    let mut c = Client::with_credentials(&m.base, creds()).unwrap();
    let e = c.list_notices().unwrap_err();
    assert!(matches!(e, Error::Unauthenticated));
    assert_eq!(m.login.load(Ordering::SeqCst), 1);
    assert!(!e.as_json().to_string().contains("fake-password"));
}
#[test]
fn still_expired_does_not_recurse() {
    let m = mock("always_expired");
    let mut c = Client::with_credentials(&m.base, creds()).unwrap();
    assert!(matches!(c.list_notices(), Err(Error::Unauthenticated)));
    assert_eq!(m.login.load(Ordering::SeqCst), 1);
}
#[test]
fn api_failures_are_never_empty_lists() {
    for mode in ["invalid_api", "missing_data", "malformed"] {
        let m = mock(mode);
        let mut c = Client::new(&m.base).unwrap();
        assert!(c.list_notices().is_err());
        assert_eq!(m.login.load(Ordering::SeqCst), 0);
    }
}
#[test]
fn hostile_form_and_redirect_refused() {
    for mode in ["hostile_form", "redirect"] {
        let m = mock(mode);
        let mut c = Client::with_credentials(&m.base, creds()).unwrap();
        assert!(c.list_notices().is_err());
        assert_eq!(m.login.load(Ordering::SeqCst), 0);
    }
}
#[test]
fn transient_get_retries() {
    let m = mock("transient");
    let mut c = Client::new(&m.base).unwrap();
    assert_eq!(c.list_notices().unwrap().len(), 1);
    assert_eq!(m.requests.load(Ordering::SeqCst), 2);
}
#[test]
fn pagination_is_complete_or_errors() {
    let m = mock("pages");
    let mut c = Client::new(&m.base).unwrap();
    assert_eq!(
        c.list_bills(Pagination {
            all: true,
            max_pages: Some(3)
        })
        .unwrap()
        .len(),
        2
    );
    assert!(
        c.list_bills(Pagination {
            all: true,
            max_pages: Some(2)
        })
        .is_err()
    );
    assert!(
        c.list_bills(Pagination {
            all: true,
            max_pages: Some(0)
        })
        .is_err()
    );
    let m = mock("repeat");
    let mut c = Client::new(&m.base).unwrap();
    assert!(
        c.list_bills(Pagination {
            all: true,
            max_pages: Some(3)
        })
        .is_err()
    );
}
#[test]
fn normalized_reads_and_pdf() {
    let m = mock("public");
    let mut c = Client::new(&m.base).unwrap();
    assert_eq!(
        c.list_documents().unwrap()[0].sent_date.as_deref(),
        Some("2026-10-29")
    );
    assert_eq!(c.notice_detail("n1").unwrap().body, "Body");
    assert!(c.notice_detail("").is_err());
    let slots = c.list_reservation_availability().unwrap();
    assert_eq!(
        slots.iter().map(|s| s.status.as_str()).collect::<Vec<_>>(),
        vec!["free", "occupied", "unknown"]
    );
    assert_eq!(c.list_requests().unwrap()[0].title, "Leak");
    assert_eq!(c.list_occurrences().unwrap()[0].title, "Noise");
    assert_eq!(c.fetch_document_pdf_info("a&b=1").unwrap().size_bytes, 13);
}
#[test]
fn encrypted_store_roundtrip_tampering_and_context_binding() {
    let dir = tempfile::tempdir().unwrap();
    let s = store(dir.path(), "https://portal.test/");
    let base = "https://portal.test/";
    assert_eq!(s.credentials(base).unwrap().password, "fake-password");
    assert!(s.credentials("https://other.test/").is_err());
    let path = dir.path().join("credentials.enc");
    let mut bytes = fs::read(&path).unwrap();
    assert!(!String::from_utf8_lossy(&bytes).contains("fake-password"));
    let n = bytes.len();
    bytes[n - 1] ^= 1;
    fs::write(&path, bytes).unwrap();
    assert!(s.credentials(base).is_err());
    assert_eq!(
        fs::metadata(&path).unwrap().permissions().mode() & 0o777,
        0o600
    );
}
#[test]
fn store_reenrollment_and_process_lock() {
    let dir = tempfile::tempdir().unwrap();
    let s = store(dir.path(), "https://portal.test/");
    let key = fs::read(dir.path().join("credentials.key")).unwrap();
    assert!(Store::open(dir.path()).is_err());
    s.enroll("https://portal.test/", &creds()).unwrap();
    assert_eq!(key, fs::read(dir.path().join("credentials.key")).unwrap());
    drop(s);
    // Tests run in parallel threads, and a concurrently spawned CLI process holds an inherited
    // copy of the lock descriptor until it execs, so the release can lag the drop briefly.
    let mut reopened = Store::open(dir.path());
    for _ in 0..200 {
        if reopened.is_ok() {
            break;
        }
        thread::sleep(std::time::Duration::from_millis(10));
        reopened = Store::open(dir.path());
    }
    assert!(reopened.is_ok());
}
#[test]
fn store_rejects_unsafe_files_and_missing_key() {
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("private");
    let s = store(&path, "https://portal.test/");
    drop(s);
    fs::set_permissions(
        path.join("credentials.key"),
        fs::Permissions::from_mode(0o644),
    )
    .unwrap();
    assert!(Store::open(&path).is_err());
    fs::remove_file(path.join("credentials.key")).unwrap();
    assert!(Store::open(&path).is_err());
    let linked = dir.path().join("linked");
    std::os::unix::fs::symlink(&path, &linked).unwrap();
    assert!(Store::open(linked).is_err());
}
#[test]
fn seed_import_deletes_source_only_after_encryption() {
    let dir = tempfile::tempdir().unwrap();
    let s = Store::open(dir.path().join("store")).unwrap();
    let source = dir.path().join("password");
    fs::write(&source, b"fake-password\r\n").unwrap();
    fs::set_permissions(&source, fs::Permissions::from_mode(0o600)).unwrap();
    s.seed_from_file("https://portal.test/", "fake@example.test", &source)
        .unwrap();
    assert!(!source.exists());
    assert_eq!(
        s.credentials("https://portal.test/").unwrap().password,
        "fake-password"
    );
}
#[test]
fn seed_refuses_public_git_symlink_and_hardlink_sources() {
    let dir = tempfile::tempdir().unwrap();
    let s = Store::open(dir.path().join("store")).unwrap();
    let source = dir.path().join("password");
    fs::write(&source, b"fake-password").unwrap();
    fs::set_permissions(&source, fs::Permissions::from_mode(0o644)).unwrap();
    assert!(s.seed_from_file("x", "u", &source).is_err());
    assert!(source.exists());
    fs::set_permissions(&source, fs::Permissions::from_mode(0o600)).unwrap();
    let link = dir.path().join("link");
    std::os::unix::fs::symlink(&source, &link).unwrap();
    assert!(s.seed_from_file("x", "u", &link).is_err());
    fs::remove_file(&link).unwrap();
    fs::hard_link(&source, &link).unwrap();
    assert!(s.seed_from_file("x", "u", &source).is_err());
    fs::remove_file(&link).unwrap();
    fs::create_dir(dir.path().join(".git")).unwrap();
    assert!(s.seed_from_file("x", "u", &source).is_err());
    assert!(source.exists());
}
#[test]
fn strict_tools_and_fail_closed_previews() {
    let mut c = Client::new("https://portal.test/").unwrap();
    assert!(
        c.execute_tool("list_open_bills", json!({"all":"yes"}))
            .is_err()
    );
    assert!(
        c.execute_tool("list_notices", json!({"unexpected":true}))
            .is_err()
    );
    assert!(c.execute_tool("pay_bill", json!({})).is_err());
    let p = preview_request("Leak", "Hallway").unwrap();
    assert!(matches!(
        tools::create_request(&p, false),
        Err(Error::ConfirmationRequired)
    ));
    assert!(matches!(
        tools::create_request(&p, true),
        Err(Error::SubmissionUndiscovered)
    ));
    let mut changed = p.clone();
    changed.title = "Altered".into();
    assert!(matches!(
        tools::create_request(&changed, true),
        Err(Error::Validation(_))
    ));
    changed = p;
    changed.action = "pay_bill".into();
    assert!(tools::create_request(&changed, true).is_err());
    assert!(preview_reservation("Hall", "2026-02-30", "Day").is_err());
    let p = preview_reservation("Hall", "2028-02-29", "Day").unwrap();
    assert!(matches!(
        tools::pre_reserve_reservation(&p, true),
        Err(Error::SubmissionUndiscovered)
    ));
}
#[test]
fn jsonl_exact_money_and_deduplication() {
    let bill = Bill {
        id: "1".into(),
        amount_brl: "9007199254740993.01".into(),
        ..Default::default()
    };
    let records = vec![bill.clone(), bill.clone()];
    let bytes = condo::marshal_jsonl(&records).unwrap();
    assert_eq!(condo::unmarshal_bills_jsonl(&bytes).unwrap(), records);
    assert_eq!(condo::check_new_bills(&records, &[]).len(), 1);
    assert!(condo::check_new_bills(&records, &[bill]).is_empty());
    assert!(condo::unmarshal_bills_jsonl(b"invalid").is_err());
}
#[test]
fn urls_dates_and_tool_catalog() {
    for url in [
        "http://portal.test/",
        "https://user:pass@portal.test/",
        "https://portal.test/?x=1",
        "file:///tmp/a",
    ] {
        assert!(Client::new(url).is_err());
    }
    assert_eq!(condo::client::api_date("11/10/2026 00:00:00"), "2026-11-10");
    assert_eq!(condo::client::api_date("29/10/2026"), "2026-10-29");
    assert_eq!(condo::client::api_date("02/30/2026"), "02/30/2026");
    assert_eq!(tools::tools().len(), 14);
    for tool in tools::tools() {
        assert!(tool.input_schema.is_object());
        assert!(tool.output_schema.is_object());
    }
}
#[test]
fn cli_emits_structured_errors_and_offline_previews() {
    let bin = env!("CARGO_BIN_EXE_condo");
    let output = std::process::Command::new(bin)
        .args(["preview-request", "--title", "Leak", "--subject", "Hallway"])
        .output()
        .unwrap();
    assert!(output.status.success());
    let p: Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(p["action"], "create_request");
    let output = std::process::Command::new(bin)
        .args([
            "preview-reservation",
            "--area",
            "Hall",
            "--date",
            "2026-02-30",
            "--period",
            "Day",
        ])
        .output()
        .unwrap();
    assert!(!output.status.success());
    let error: Value = serde_json::from_slice(&output.stderr).unwrap();
    assert_eq!(error["error"]["code"], "validation");
}

#[test]
fn tool_schemas_validate_actual_responses() {
    let m = mock("public");
    let mut c = Client::new(&m.base).unwrap();
    for (name, input) in [
        ("list_open_bills", json!({"all":true})),
        ("list_paid_bills", json!({})),
        ("list_documents", json!({})),
        ("list_notices", json!({})),
        ("notice_detail", json!({"id":"n1"})),
        ("list_reservation_availability", json!({})),
        ("list_requests", json!({})),
        ("list_occurrences", json!({})),
        ("fetch_document_pdf", json!({"id":"a&b=1"})),
        ("check_new_bills", json!({"previous":[]})),
        (
            "preview_request",
            json!({"title":"Leak","subject":"Hallway"}),
        ),
        (
            "preview_reservation",
            json!({"area":"Hall","date":"2026-11-02","period":"Day"}),
        ),
    ] {
        let tool = tools::tools().into_iter().find(|t| t.name == name).unwrap();
        let validator = jsonschema::validator_for(&tool.input_schema).unwrap();
        assert!(validator.is_valid(&input), "input schema for {name}");
        let result = c.execute_tool(name, input).unwrap();
        let validator = jsonschema::validator_for(&tool.output_schema).unwrap();
        assert!(
            validator.is_valid(&result),
            "output schema for {name}: {result}"
        );
    }
}

#[test]
fn output_replaces_symlink_without_following_and_state_remains_private() {
    let dir = tempfile::tempdir().unwrap();
    let target = dir.path().join("target");
    let output = dir.path().join("output");
    fs::write(&target, b"original").unwrap();
    std::os::unix::fs::symlink(&target, &output).unwrap();
    condo::store::write_private(&output, b"new output").unwrap();
    assert_eq!(fs::read(&target).unwrap(), b"original");
    assert_eq!(fs::read(&output).unwrap(), b"new output");
    assert_eq!(
        fs::metadata(&output).unwrap().permissions().mode() & 0o777,
        0o600
    );
}

#[test]
fn offline_mode_blocks_all_network_even_authentication() {
    let m = mock("public");
    let mut c = Client::offline(&m.base).unwrap();
    assert!(matches!(c.authenticate(), Err(Error::Offline)));
    for (name, input) in [
        ("list_open_bills", json!({})),
        ("list_paid_bills", json!({})),
        ("list_documents", json!({})),
        ("list_notices", json!({})),
        ("notice_detail", json!({"id":"n1"})),
        ("list_reservation_availability", json!({})),
        ("list_requests", json!({})),
        ("list_occurrences", json!({})),
        ("fetch_document_pdf", json!({"id":"doc1"})),
        ("check_new_bills", json!({})),
    ] {
        assert!(
            matches!(c.execute_tool(name, input), Err(Error::Offline)),
            "{name}"
        );
    }
    assert!(
        c.execute_tool("preview_request", json!({"title":"Leak","subject":"Hall"}))
            .is_ok()
    );
    let output = std::process::Command::new(env!("CARGO_BIN_EXE_condo"))
        .args(["--offline", "--base-url", &m.base, "list-notices"])
        .output()
        .unwrap();
    assert!(!output.status.success());
    assert_eq!(
        serde_json::from_slice::<Value>(&output.stderr).unwrap()["error"]["code"],
        "offline"
    );
    assert_eq!(m.requests.load(Ordering::SeqCst), 0);
}

#[test]
fn cli_end_to_end_local_only_with_fake_credentials() {
    let m = mock("session");
    let dir = tempfile::tempdir().unwrap();
    let store = dir.path().join("store");
    let password = dir.path().join("password");
    fs::write(&password, b"fake-password\n").unwrap();
    fs::set_permissions(&password, fs::Permissions::from_mode(0o600)).unwrap();
    let run = |args: &[&str]| {
        let output = std::process::Command::new(env!("CARGO_BIN_EXE_condo"))
            .arg("--base-url")
            .arg(&m.base)
            .arg("--store-dir")
            .arg(&store)
            .args(args)
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        serde_json::from_slice::<Value>(&output.stdout).unwrap()
    };
    assert_eq!(
        run(&[
            "setup-auth",
            "--username",
            "fake@example.test",
            "--from-file",
            password.to_str().unwrap()
        ])["enrolled"],
        true
    );
    assert!(!password.exists());
    assert_eq!(m.requests.load(Ordering::SeqCst), 0);
    for command in [
        "list-open-bills",
        "list-paid-bills",
        "list-documents",
        "list-notices",
        "list-reservations",
        "list-requests",
        "list-occurrences",
    ] {
        assert!(run(&[command]).is_array());
    }
    assert_eq!(m.login.load(Ordering::SeqCst), 1);
    assert_eq!(run(&["notice-detail", "--id", "n1"])["body"], "Body");
    let pdf = dir.path().join("report.pdf");
    assert_eq!(
        run(&[
            "fetch-document-pdf",
            "--id",
            "a&b=1",
            "--output",
            pdf.to_str().unwrap()
        ])["size_bytes"],
        13
    );
    assert!(fs::read(pdf).unwrap().starts_with(b"%PDF-"));
    let state = dir.path().join("bills.jsonl");
    assert_eq!(
        run(&[
            "check-new-bills",
            "--state",
            state.to_str().unwrap(),
            "--update-state"
        ])["count"],
        1
    );
    assert_eq!(
        run(&[
            "check-new-bills",
            "--state",
            state.to_str().unwrap(),
            "--update-state"
        ])["count"],
        0
    );
    let before = m.requests.load(Ordering::SeqCst);
    run(&["forget-session"]);
    assert_eq!(m.requests.load(Ordering::SeqCst), before);
    assert!(!store.join("session.enc").exists());
}

#[test]
fn pdf_download_accepts_large_documents_without_relaxing_api_limit() {
    let server = Server::http("127.0.0.1:0").unwrap();
    let base = format!("http://{}/", server.server_addr());
    let worker = thread::spawn(move || {
        for _ in 0..2 {
            let req = server.recv().unwrap();
            let mut bytes = vec![b' '; 13 * 1024 * 1024];
            bytes[..5].copy_from_slice(b"%PDF-");
            req.respond(Response::from_data(bytes)).unwrap();
        }
    });
    let mut client = Client::new(&base).unwrap();
    assert_eq!(
        client.fetch_document_pdf("large").unwrap().len(),
        13 * 1024 * 1024
    );
    assert!(matches!(client.list_notices(), Err(Error::TooLarge)));
    worker.join().unwrap();
}
