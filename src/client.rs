use crate::{
    Bill, Credentials, Document, Error, Notice, NoticeDetail, Occurrence, Request, ReservationSlot,
    Result, Store,
};
use cookie_store::CookieStore;
use reqwest::{Method, blocking::Response};
use reqwest_cookie_store::CookieStoreMutex;
use scraper::{Html, Selector};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::{collections::BTreeMap, io::Read, sync::Arc, time::Duration};
use url::Url;
use zeroize::Zeroizing;

pub const DEFAULT_BASE_URL: &str =
    "https://dinamicacondominiossc.superlogica.net/clients/areadocondomino";
const LIMIT: u64 = 12 * 1024 * 1024;
#[derive(Clone, Copy, Debug, Default, Deserialize, Serialize, schemars::JsonSchema)]
#[serde(deny_unknown_fields)]
pub struct Pagination {
    #[serde(default)]
    pub all: bool,
    #[serde(default)]
    pub max_pages: Option<usize>,
}
impl Pagination {
    fn limit(self) -> Result<usize> {
        let n = self.max_pages.unwrap_or(20);
        if !(1..=100).contains(&n) {
            return Err(Error::Validation("max_pages must be between 1 and 100"));
        }
        Ok(if self.all { n } else { 1 })
    }
}
/// A client is deliberately used through &mut self: one session renewal at a time.
/// A persistent store holds a process lock throughout the client's lifetime.
pub struct Client {
    offline: bool,
    base: Url,
    http: reqwest::blocking::Client,
    cookies: Arc<CookieStoreMutex>,
    store: Option<Store>,
    credentials: Option<Credentials>,
}
impl Client {
    pub fn normalize_base(base: &str) -> Result<Url> {
        let mut u = Url::parse(base).map_err(|_| Error::Validation("invalid base URL"))?;
        if u.host_str().is_none()
            || !u.username().is_empty()
            || u.password().is_some()
            || u.query().is_some()
            || u.fragment().is_some()
        {
            return Err(Error::Validation(
                "base URL must have a host and no credentials, query, or fragment",
            ));
        }
        let loopback = matches!(u.host_str(), Some("localhost" | "127.0.0.1" | "[::1]"));
        if u.scheme() != "https" && !(u.scheme() == "http" && loopback) {
            return Err(Error::Validation(
                "HTTPS is required except for localhost tests",
            ));
        }
        u.set_path(&format!("{}/", u.path().trim_end_matches('/')));
        Ok(u)
    }
    /// Construct a client that refuses all HTTP calls, including authentication.
    pub fn offline(base: &str) -> Result<Self> {
        let mut client = Self::new(base)?;
        client.offline = true;
        Ok(client)
    }
    pub fn new(base: &str) -> Result<Self> {
        Self::build(base, None, None)
    }
    pub fn from_store(base: &str, store: Store) -> Result<Self> {
        Self::build(base, Some(store), None)
    }
    pub fn with_credentials(base: &str, creds: Credentials) -> Result<Self> {
        Self::build(base, None, Some(creds))
    }
    fn build(base: &str, store: Option<Store>, credentials: Option<Credentials>) -> Result<Self> {
        let base = Self::normalize_base(base)?;
        let jar = if let Some(s) = &store {
            match s.load("session.enc", base.as_str())? {
                Some(data) => {
                    cookie_store::serde::json::load(&data[..]).map_err(|_| Error::Crypto)?
                }
                None => CookieStore::default(),
            }
        } else {
            CookieStore::default()
        };
        let cookies = Arc::new(CookieStoreMutex::new(jar));
        let origin = base.origin();
        let http = reqwest::blocking::Client::builder()
            .cookie_provider(cookies.clone())
            .timeout(Duration::from_secs(30))
            .connect_timeout(Duration::from_secs(10))
            .user_agent("condo-library-rust/0.2.0")
            .redirect(reqwest::redirect::Policy::custom(move |a| {
                if a.previous().len() >= 5 {
                    return a.error("redirect limit");
                }
                if a.url().origin() != origin {
                    return a.error("cross-origin redirect refused");
                }
                a.follow()
            }))
            .build()
            .map_err(|_| Error::Transport)?;
        Ok(Self {
            offline: false,
            base,
            http,
            cookies,
            store,
            credentials,
        })
    }
    pub fn base_url(&self) -> &str {
        self.base.as_str()
    }
    fn resolve(&self, path: &str) -> Result<Url> {
        let u = self
            .base
            .join(path)
            .map_err(|_| Error::Validation("invalid endpoint"))?;
        if u.origin() != self.base.origin() || !u.username().is_empty() || u.password().is_some() {
            return Err(Error::Validation("cross-origin endpoint refused"));
        }
        Ok(u)
    }
    fn api_url(&self, controller: &str) -> Result<Url> {
        self.resolve(&format!("/areadocondomino/atual/{controller}/index"))
    }
    fn save_session(&self) -> Result<()> {
        if let Some(s) = &self.store {
            let mut bytes = Zeroizing::new(Vec::new());
            let jar = self.cookies.lock().map_err(|_| Error::Crypto)?;
            cookie_store::serde::json::save_incl_expired_and_nonpersistent(&jar, &mut *bytes)
                .map_err(|_| Error::Crypto)?;
            s.save("session.enc", self.base.as_str(), &bytes)?;
        }
        Ok(())
    }
    pub fn forget_session(&mut self) -> Result<()> {
        self.cookies.lock().map_err(|_| Error::Crypto)?.clear();
        if let Some(s) = &self.store {
            s.clear_session()?;
        }
        Ok(())
    }
    fn response_bytes(mut response: Response, limit: u64) -> Result<(u16, Vec<u8>)> {
        let status = response.status().as_u16();
        let mut bytes = Vec::new();
        (&mut response)
            .take(limit + 1)
            .read_to_end(&mut bytes)
            .map_err(|_| Error::Transport)?;
        if bytes.len() as u64 > limit {
            return Err(Error::TooLarge);
        }
        Ok((status, bytes))
    }
    fn request(
        &self,
        method: Method,
        u: Url,
        form: Option<&BTreeMap<String, String>>,
    ) -> Result<(u16, Vec<u8>)> {
        self.request_bounded(method, u, form, LIMIT)
    }
    fn request_bounded(
        &self,
        method: Method,
        u: Url,
        form: Option<&BTreeMap<String, String>>,
        limit: u64,
    ) -> Result<(u16, Vec<u8>)> {
        if self.offline {
            return Err(Error::Offline);
        }
        // Only safe GETs retry transient failures. Authentication POST is sent once.
        let attempts = if method == Method::GET { 3 } else { 1 };
        for attempt in 0..attempts {
            if attempt > 0 {
                std::thread::sleep(Duration::from_millis(200 << (attempt - 1)));
            }
            let mut req = self
                .http
                .request(method.clone(), u.clone())
                .header("Accept", "application/json")
                .header("X-Requested-With", "XMLHttpRequest");
            if let Some(f) = form {
                req = req.form(f);
            }
            let response = match req.send() {
                Ok(r) => r,
                Err(_) if attempt + 1 < attempts => continue,
                Err(_) => return Err(Error::Transport),
            };
            let status = response.status().as_u16();
            if matches!(status, 429 | 502 | 503 | 504) && attempt + 1 < attempts {
                // Respect Retry-After seconds up to a bounded wait. Long waits are returned to caller.
                if let Some(wait) = response
                    .headers()
                    .get("Retry-After")
                    .and_then(|v| v.to_str().ok())
                    .and_then(|v| v.parse::<u64>().ok())
                {
                    if wait > 5 {
                        return Err(Error::Http(status));
                    }
                    std::thread::sleep(Duration::from_secs(wait));
                }
                continue;
            }
            let result = Self::response_bytes(response, limit);
            self.save_session()?;
            return result;
        }
        Err(Error::Transport)
    }
    fn decode(status: u16, bytes: &[u8]) -> Result<Value> {
        if matches!(status, 401 | 403) || login_html(bytes) {
            return Err(Error::Unauthenticated);
        }
        if !(200..300).contains(&status) {
            return Err(Error::Http(status));
        }
        let env: Value = serde_json::from_slice(bytes).map_err(|_| Error::Parse)?;
        let code = text(&env, "status");
        if matches!(code.as_str(), "401" | "403") {
            return Err(Error::Unauthenticated);
        }
        if !matches!(
            code.as_str(),
            "200" | "201" | "202" | "203" | "204" | "205" | "206"
        ) {
            return Err(Error::Api);
        }
        env.get("data").cloned().ok_or(Error::Parse)
    }
    fn api_get(&mut self, u: Url) -> Result<Value> {
        let (status, bytes) = self.request(Method::GET, u.clone(), None)?;
        match Self::decode(status, &bytes) {
            Err(Error::Unauthenticated) => {
                self.authenticate()?;
                let (status, bytes) = self.request(Method::GET, u, None)?;
                Self::decode(status, &bytes)
            }
            result => result,
        }
    }
    /// Login once using enrolled credentials. No recursion and no password retries.
    pub fn authenticate(&mut self) -> Result<()> {
        if self.offline {
            return Err(Error::Offline);
        }
        let loaded;
        let creds = match &self.credentials {
            Some(c) => c,
            None => {
                loaded = self
                    .store
                    .as_ref()
                    .ok_or(Error::Unauthenticated)?
                    .credentials(self.base.as_str())?;
                &loaded
            }
        };
        let (status, page) = self.request(Method::GET, self.base.clone(), None)?;
        if !(200..300).contains(&status) {
            return Err(Error::Http(status));
        }
        let decoded = match std::str::from_utf8(&page) {
            Ok(s) => std::borrow::Cow::Borrowed(s),
            Err(_) => encoding_rs::WINDOWS_1252.decode(&page).0,
        };
        let html = Html::parse_document(&decoded);
        let forms = Selector::parse("form").unwrap();
        let inputs = Selector::parse("input").unwrap();
        let mut found = None;
        for form in html.select(&forms) {
            let mut fields = BTreeMap::new();
            let mut email = None;
            let mut password = None;
            for input in form.select(&inputs) {
                if let Some(name) = input.value().attr("name") {
                    let kind = input.value().attr("type").unwrap_or("text");
                    if kind == "password" {
                        password = Some(name.to_owned());
                    }
                    if kind == "email" || name == "email" {
                        email = Some(name.to_owned());
                    }
                    if kind == "hidden" {
                        fields.insert(
                            name.to_owned(),
                            input.value().attr("value").unwrap_or("").to_owned(),
                        );
                    }
                }
            }
            if let (Some(email), Some(password)) = (email, password) {
                found = Some((
                    form.value().attr("action").unwrap_or("").to_owned(),
                    fields,
                    email,
                    password,
                ));
                break;
            }
        }
        let (action, mut fields, email, password) = found.ok_or(Error::Unauthenticated)?;
        let auth = self.resolve(&action)?;
        if auth.scheme() != self.base.scheme() {
            return Err(Error::Validation("auth scheme mismatch"));
        }
        let mut verify = if auth.path().ends_with("/publico/auth") {
            auth.join("verificarcondomino").map_err(|_| Error::Parse)?
        } else {
            self.resolve("/areadocondomino/atual/publico/verificarcondomino")?
        };
        verify
            .query_pairs_mut()
            .append_pair("filename", "")
            .append_pair("email", &creds.username)
            .append_pair(
                "hashemail",
                fields.get("hashemail").map(String::as_str).unwrap_or(""),
            );
        let (s, b) = self.request(Method::GET, verify, None)?;
        let data = Self::decode(s, &b)?;
        if value_text(&data) != "-1" {
            return Err(Error::Unauthenticated);
        }
        fields.insert(email, creds.username.clone());
        fields.insert(password, creds.password.clone());
        let posted = self.request(Method::POST, auth, Some(&fields));
        // Zeroize password/username/hidden fields after the request, including errors.
        for v in fields.values_mut() {
            use zeroize::Zeroize;
            v.zeroize();
        }
        let (s, b) = posted?;
        Self::decode(s, &b)?;
        // Prove auth on the JSON API, rather than accepting an arbitrary HTML shell.
        let mut verify = self.api_url("cobranca")?;
        verify
            .query_pairs_mut()
            .append_pair("status", "em-abertas")
            .append_pair("pagina", "1")
            .append_pair("itensPorPagina", "1");
        let (s, b) = self.request(Method::GET, verify, None)?;
        if !Self::decode(s, &b)?.is_array() {
            return Err(Error::Parse);
        }
        self.save_session()
    }
    pub fn list_bills(&mut self, p: Pagination) -> Result<Vec<Bill>> {
        let limit = p.limit()?;
        let mut out = Vec::new();
        let mut seen = std::collections::HashSet::new();
        for page in 1..=limit {
            let mut u = self.api_url("cobranca")?;
            u.query_pairs_mut()
                .append_pair("pagina", &page.to_string())
                .append_pair("itensPorPagina", "50")
                .append_pair("status", "em-abertas");
            let data = self.api_get(u)?;
            let rows = data.as_array().ok_or(Error::Parse)?;
            if rows.is_empty() {
                return Ok(out);
            }
            for row in rows {
                let paid_date = api_date(&text(row, "dt_liquidacao_recb"));
                let competence = api_date(&text(row, "dt_competencia_recb"));
                let bill = Bill {
                    id: text(row, "id_recebimento_recb"),
                    status: if paid_date.is_empty() { "open" } else { "paid" }.into(),
                    due_date: api_date(&text(row, "dt_vencimento_recb")),
                    paid_date,
                    period: if competence.len() == 10 && competence.as_bytes()[4] == b'-' {
                        competence[..7].to_owned()
                    } else {
                        String::new()
                    },
                    amount_brl: text(row, "vl_total_recb"),
                    label: nonempty(
                        text(row, "st_label_recb"),
                        text(row, "st_formapagamento_recb"),
                    ),
                };
                let key = serde_json::to_string(&bill).map_err(|_| Error::Parse)?;
                if !seen.insert(key) {
                    return Err(Error::Validation(
                        "pagination repeated a bill; result is incomplete",
                    ));
                }
                out.push(bill);
            }
            if !p.all {
                return Ok(out);
            }
        }
        Err(Error::Validation(
            "pagination limit reached before an empty page; increase max_pages",
        ))
    }
    pub fn list_open_bills(&mut self, p: Pagination) -> Result<Vec<Bill>> {
        Ok(self
            .list_bills(p)?
            .into_iter()
            .filter(|b| b.status == "open")
            .collect())
    }
    pub fn list_paid_bills(&mut self, p: Pagination) -> Result<Vec<Bill>> {
        Ok(self
            .list_bills(p)?
            .into_iter()
            .filter(|b| b.status == "paid")
            .collect())
    }
    pub fn list_documents(&mut self) -> Result<Vec<Document>> {
        let mut out = Vec::new();
        let mut seen = std::collections::HashSet::new();
        for page in 1..=100 {
            let mut u = self.api_url("impressoes")?;
            u.query_pairs_mut()
                .append_pair("pagina", &page.to_string())
                .append_pair("itensPorPagina", "50")
                .append_pair("doTipo", "documento")
                .append_pair("apenasOutrosDocumentos", "1");
            let data = self.api_get(u)?;
            let rows = data.as_array().ok_or(Error::Parse)?;
            if rows.is_empty() {
                return Ok(out);
            }
            for row in rows {
                let id = text(row, "id_impressao_fimp");
                if !seen.insert(serde_json::to_string(row).map_err(|_| Error::Parse)?) {
                    return Err(Error::Validation("document pagination repeated"));
                }
                out.push(Document {
                    id: optional(id),
                    title: text(row, "st_titulo_fimp"),
                    category: optional(text(row, "st_tipo_etiqueta")),
                    sent_date: optional(api_date(&text(row, "dt_criacao_fimp"))),
                });
            }
        }
        Err(Error::Validation("document pagination limit reached"))
    }
    fn rows(&mut self, controller: &str) -> Result<Vec<Value>> {
        let mut u = self.api_url(controller)?;
        if controller == "comunicados" {
            u.query_pairs_mut().append_pair("doTipo", "documento");
        }
        self.api_get(u)?.as_array().cloned().ok_or(Error::Parse)
    }
    pub fn list_notices(&mut self) -> Result<Vec<Notice>> {
        Ok(self.rows("comunicados")?.iter().map(notice).collect())
    }
    pub fn notice_detail(&mut self, id: &str) -> Result<NoticeDetail> {
        required(id)?;
        for row in self.rows("comunicados")? {
            if text(&row, "id_comunicado_com") == id {
                return Ok(NoticeDetail {
                    notice: notice(&row),
                    body: text(&row, "st_texto_com"),
                    warning: "Fetched from the list API; no notice page opened.".into(),
                });
            }
        }
        Err(Error::Validation("notice not found"))
    }
    pub fn list_reservation_availability(&mut self) -> Result<Vec<ReservationSlot>> {
        let mut out = Vec::new();
        for row in self.rows("reservas")? {
            let name = nonempty(text(&row, "st_nome_are"), text(&row, "titulo_area"));
            let (area, period) = name.rsplit_once(" - ").unwrap_or((&name, ""));
            if let Some(dates) = row.get("datas").and_then(Value::as_object) {
                for (date, slots) in dates {
                    let status = match slots.as_array() {
                        Some(a) if a.is_empty() => "free",
                        Some(_) => "occupied",
                        None => "unknown",
                    };
                    out.push(ReservationSlot {
                        area: optional(area.into()),
                        period: optional(period.into()),
                        date: optional(api_date(date)),
                        status: status.into(),
                    });
                }
            }
        }
        out.sort_by_key(|v| (v.area.clone(), v.date.clone(), v.period.clone()));
        Ok(out)
    }
    pub fn list_requests(&mut self) -> Result<Vec<Request>> {
        Ok(self
            .rows("tickets")?
            .iter()
            .map(|r| {
                let title = nonempty(text(r, "assunto"), text(r, "st_titulo"));
                Request {
                    id: optional(text(r, "id")),
                    title: title.clone(),
                    subject: optional(title),
                }
            })
            .collect())
    }
    pub fn list_occurrences(&mut self) -> Result<Vec<Occurrence>> {
        Ok(self
            .rows("ocorrencias")?
            .iter()
            .map(|r| Occurrence {
                id: optional(text(r, "id_ocorrencia_uoc")),
                title: nonempty(text(r, "st_titulo_uoc"), text(r, "st_titulo")),
            })
            .collect())
    }
    pub fn fetch_document_pdf(&mut self, id: &str) -> Result<Vec<u8>> {
        required(id)?;
        let mut u = self.resolve("documentos/download")?;
        u.query_pairs_mut().append_pair("id", id);
        let (mut status, mut bytes) =
            self.request_bounded(Method::GET, u.clone(), None, 64 * 1024 * 1024)?;
        let envelope_auth = serde_json::from_slice::<Value>(&bytes)
            .ok()
            .is_some_and(|v| matches!(text(&v, "status").as_str(), "401" | "403"));
        if matches!(status, 401 | 403) || login_html(&bytes) || envelope_auth {
            self.authenticate()?;
            (status, bytes) = self.request_bounded(Method::GET, u, None, 64 * 1024 * 1024)?;
        }
        if matches!(status, 401 | 403) || login_html(&bytes) {
            return Err(Error::Unauthenticated);
        }
        if !(200..300).contains(&status) {
            return Err(Error::Http(status));
        }
        if !bytes.starts_with(b"%PDF-") {
            return Err(Error::Parse);
        }
        Ok(bytes)
    }
}
fn login_html(bytes: &[u8]) -> bool {
    let text = String::from_utf8_lossy(bytes).to_lowercase();
    text.contains("areadocondomino_forms_auth_entrarlogar")
        || (text.contains("<form")
            && (text.contains("type=\"password\"") || text.contains("type='password'")))
}
pub(crate) fn required(s: &str) -> Result<()> {
    if s.trim().is_empty() {
        Err(Error::Validation("required string is empty"))
    } else {
        Ok(())
    }
}
fn value_text(v: &Value) -> String {
    match v {
        Value::String(s) => s.trim().to_owned(),
        Value::Number(n) => n.to_string(),
        _ => String::new(),
    }
}
fn text(v: &Value, k: &str) -> String {
    v.get(k).map(value_text).unwrap_or_default()
}
fn nonempty(a: String, b: String) -> String {
    if a.is_empty() { b } else { a }
}
fn optional(value: String) -> Option<String> {
    if value.is_empty() { None } else { Some(value) }
}
fn notice(r: &Value) -> Notice {
    Notice {
        id: optional(text(r, "id_comunicado_com")),
        title: text(r, "st_titulo_com"),
        published_date: optional(api_date(&text(r, "dt_comunicado_com"))),
    }
}
pub fn api_date(s: &str) -> String {
    let s = s.trim();
    let date = s.split_whitespace().next().unwrap_or("");
    let parts: Vec<_> = date.split('/').collect();
    if parts.len() != 3 {
        return s.to_owned();
    }
    let nums: Option<Vec<u32>> = parts.iter().map(|p| p.parse().ok()).collect();
    let Some(n) = nums else { return s.to_owned() };
    let (m, d, y) = if n[0] > 12 {
        (n[1], n[0], n[2])
    } else {
        (n[0], n[1], n[2])
    };
    if !valid_date(y, m, d) {
        return s.to_owned();
    }
    format!("{y:04}-{m:02}-{d:02}")
}
pub(crate) fn valid_date(y: u32, m: u32, d: u32) -> bool {
    let leap = y.is_multiple_of(4) && (!y.is_multiple_of(100) || y.is_multiple_of(400));
    let days = match m {
        2 => {
            if leap {
                29
            } else {
                28
            }
        }
        4 | 6 | 9 | 11 => 30,
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        _ => 0,
    };
    (1..=9999).contains(&y) && d >= 1 && d <= days
}
