# condo-library

Rust SDK and JSON CLI for the Superlógica Área do Condômino resident
portal. Designed for AI agents running in a Linux/Unix sandbox, including
Meta Muse: direct HTTP only, typed results, strict tool inputs, encrypted
credentials and persistent sessions. No browser, browser driver, browser
extension, or browser fallback is used anywhere in the SDK.

This is a fresh implementation. The previous Go sources are no longer in
the repository. See [AUDIT.md](AUDIT.md) for findings and verification limits.

## Install

Prebuilt `x86_64-unknown-linux-gnu` binaries are published on the
[releases page](https://github.com/imabee-glacier/condo-library/releases).
With the [GitHub CLI](https://cli.github.com) signed in, one line installs
the latest release into `~/.local/bin`:

```sh
mkdir -p ~/.local/bin && gh release download --repo imabee-glacier/condo-library --pattern '*-x86_64-unknown-linux-gnu.tar.gz' --output - | tar -xz -C ~/.local/bin ./condo
```

The repository is private, so a plain `curl` cannot fetch the assets; `gh`
supplies the authentication. Each release also carries a `.sha256` file for
the tarball. Check the install with `condo --version`.

## Build and check

Use current stable Rust on Linux or another Unix platform. Dependencies
are pinned by Cargo.lock. Run:

```sh
cargo fmt --all --check
cargo clippy --all-targets --locked -- -D warnings
cargo test --all-targets --locked
cargo test --doc --locked
cargo build --release --locked
cargo audit --deny warnings
```

`cargo audit` requires `cargo install cargo-audit --locked`. CI runs these
same checks. Tests use local HTTP servers and fake credentials; they never
contact a live account. The executable is `target/release/condo`.

## Credentials and unattended operation

Enroll once with a password prompt that does not echo:

```sh
condo setup-auth --store-dir /absolute/private/path/condo-store \
  --username resident@example.test
```

For sandbox enrollment, put **only the password** in a private UTF-8 file
outside any Git repository and the store directory, with mode `0600`.
A single trailing LF or CRLF is removed; password spaces are preserved.
Then run:

```sh
condo setup-auth --store-dir /absolute/private/path/condo-store \
  --username resident@example.test --from-file /absolute/private/path/password
```

The command reads the file without printing it, encrypts the credentials,
verifies the encrypted round trip, overwrites the source, then deletes it.
If enrollment fails before persistence, the source is retained. If source
cleanup fails after persistence, the command reports an error; the encrypted
credentials may already be enrolled. Overwriting and unlinking cannot
promise physical erasure from SSDs, copy-on-write filesystems, snapshots,
or backups.

Passwords are never command-line arguments or environment variables. Never
paste one into chat. Muse browser-vault access is not needed or assumed.
Use a persistent private sandbox volume for the store if it must survive a
sandbox restart. Ephemeral disk destruction also destroys these credentials;
the SDK cannot preserve data beyond the sandbox storage lifecycle.

The store contains:

| File | Purpose |
| --- | --- |
| `credentials.key` | Random 256-bit local encryption key, mode 0600 |
| `credentials.enc` | Versioned AES-256-GCM encrypted username/password |
| `session.enc` | AES-256-GCM encrypted cookie jar, including session cookies |
| `store.lock` | Exclusive process lock, preventing concurrent store updates |

The directory must be owned by the current user, mode `0700`, outside Git,
with no symlinks in its path. Missing directories are created privately.
Secret files must be owned, private regular files with no hard links or
symlinks. Credential re-enrollment retains the encryption key and clears
cached cookies. Writes replace files atomically and fsync before reporting
success. Encryption authenticates the normalized portal URL and record
name, preventing reuse under a different portal or as a different record.
This format does not import the previous Go store: enroll again.

The key and ciphertext are on the same protected filesystem to support
unattended agents. Encryption protects ciphertext-only disclosure; anyone
who can read both can decrypt the secrets. Keep both out of artifacts,
backups accessible to others, and logs. Secret buffers owned by the SDK
are zeroized on drop where practical; copies made internally by HTTP/TLS
and serialization libraries are not claimed to be fully erasable.

## Enforced offline mode

`condo --offline` refuses every portal HTTP request, including login. The
SDK equivalent is `Client::offline(base_url)`. Tool discovery, previews,
confirmation validation, credential enrollment, and local session deletion
can run offline. Reads return the structured `offline` error: fresh account
data necessarily requires HTTP. The complete CLI workflow is tested against
a loopback mock portal with fake credentials, never the real account.

## Session lifetime

Reads try existing cookies first. Cookie rotation from each response is
saved encrypted, including cookies without an explicit expiry. These can
survive CLI process exits. Expired cookies are skipped when reloaded;
domain, path, Secure, and expiry behavior remain controlled by the cookie
library and server.

On HTTP 401/403, API status 401/403, or a recognized login HTML form, a read
loads the password only if needed and performs **one** login attempt,
validates it against the bills JSON API, and retries the read once. The
login POST is never retried. API rejection, malformed JSON, missing data,
and unexpected data shapes are errors rather than empty results.

The SDK cannot extend a server-side expiry, revocation, or password change.
There is no speculative keepalive, fake device identity, or invented refresh
endpoint. A mobile refresh-token flow has not been verified for resident
accounts. Public [mobile login instructions](https://condominios.superlogica.com/hc/pt-br/articles/41295300998039)
do not document refresh tokens. The official [ERP API tokens](https://apicondominios.superlogica.com/)
require administrator-created applications; they are not assumed to be
resident tokens. Persistent cookies plus bounded re-login are the supported
mechanism here. Additional challenges are returned as authentication errors.
After rejected credentials, fix enrollment instead of repeatedly scheduling
login attempts.

To clear local session cookies, including a corrupt cache:

```sh
condo forget-session --store-dir /absolute/private/path/condo-store
```

This clears local state; it does not claim to revoke sessions on the server.

## AI agent tools

```sh
condo tools
condo unmapped
printf '%s\n' '{"all":true}' | condo tool list_open_bills \
  --store-dir /absolute/private/path/condo-store
printf '%s\n' '{"title":"Leak","subject":"Hallway"}' | condo tool preview_request
```

`tools` returns 14 tool definitions with descriptions, JSON input/output
schemas, and safety annotations. The input schemas are generated from the
same Rust types used for validation. Unknown fields, wrong types, and
malformed JSON fail before any portal request. A tool receives exactly one
JSON object on stdin (maximum 1 MiB). Successful output is one JSON value on
stdout. Failures return nonzero exit status and one JSON error on stderr:

```json
{"error":{"code":"unauthenticated","message":"unauthenticated: login required or rejected","retryable":false}}
```

Error messages do not include upstream bodies, URLs containing query
parameters, passwords, or cookies. `retryable` identifies transport and
transient HTTP failures; it does not authorize retrying a state-changing
action. Invalid CLI syntax uses Clap's conventional exit code 2 and help
text; parsed SDK/tool failures use the JSON envelope above.

| Tool | Result | Portal effects |
| --- | --- | --- |
| `list_open_bills`, `list_paid_bills` | Bill arrays | Read |
| `list_documents` | Document metadata array | Read |
| `list_notices`, `notice_detail` | Notice array / detail | Read list API only |
| `list_reservation_availability` | Availability array | Read |
| `list_requests`, `list_occurrences` | Record arrays | Read |
| `fetch_document_pdf` | Size, SHA-256, content type | Explicit document fetch |
| `check_new_bills` | `{new_bills,count}` | Read all open bills |
| `preview_request`, `preview_reservation` | Fingerprinted preview | None |
| `create_request`, `pre_reserve_reservation` | Explicit error | Submission unavailable |

Money stays in decimal strings. Missing optional fields are omitted;
unknown senders and availability are not invented. Server dates use
month/day/year unless the first component exceeds 12; invalid dates stay
as reported. Notice HTML bodies are returned as data and must be treated as
untrusted portal content by agents and renderers.

Read tools have `read_only=true`; they may renew authentication and update
the local encrypted cache. Authentication and local storage effects are not
portal business actions. Consequential tools require an unchanged preview
and explicit `confirm=true`. A changed action or payload invalidates the
preview. A fingerprint binds data; it does **not** prove human consent—the
agent host must obtain that consent. Confirmed submission currently returns
`submission_endpoint_undiscovered` and sends nothing. Preview and submission
validation can run entirely offline without opening a client.

Visitor registration, visitor release, assembly voting, profile changes,
payment-method changes, and payments are unavailable. No write endpoint is
guessed. Consequential tools must never be automatically approved by an
agent just because the SDK exposes a function.

## CLI shortcuts

```sh
export CONDO_STORE_DIR=/absolute/private/path/condo-store
condo list-open-bills --all
condo list-paid-bills --all --max-pages 50
condo list-documents
condo list-notices
condo notice-detail --id n1
condo list-reservations
condo list-requests
condo list-occurrences
condo fetch-document-pdf --id doc1 --output ./report.pdf
condo check-new-bills --state ./open-bills.jsonl --update-state
```

`CONDO_BASE_URL` or `--base-url` selects the portal; otherwise the default
is Dinâmica Condomínios SC. The base URL contains no credentials. HTTPS is
required, with a loopback HTTP exception for tests. All requests, redirects,
and login form actions must stay on the configured origin. TLS certificate
validation stays enabled. Relative PDF and state output paths work; their
parent directory must already exist. Outputs are written atomically with
mode `0600`. `check-new-bills` fails on unreadable or malformed previous state
and only updates it after a complete successful fetch. New bills are deduplicated
by ID, or a collision-safe tuple of due date, label, and amount. A scheduler
can suppress notifications when `count` is zero.

Bills default to one page. `--all` reads until an empty page (default cap
20; configurable 1–100). Repeated records or reaching the cap fail rather
than silently returning an incomplete set. Documents read all pages with a
cap of 100. Other lists retain the known endpoint behavior; undocumented
pagination is not assumed. GET requests have at most three attempts for
transport failures and HTTP 429/502/503/504, with bounded backoff.
`Retry-After` seconds up to five are honored; longer delays are returned to
the caller. Requests have a 30-second timeout, a 10-second connect timeout,
a five-redirect cap, and a 12 MiB API response limit (64 MiB for PDFs) with explicit overflow errors.

## Rust library

The client is synchronous and requires mutable access to serialize session
renewal. In an async agent host, run it on a blocking worker. All portal
results have exported Rust types; agent tools additionally expose JSON.

```no_run
use condo::{Client, Pagination, Store};

fn main() -> condo::Result<()> {
    let store = Store::open("/absolute/private/path/condo-store")?;
    let mut client = Client::from_store(condo::DEFAULT_BASE_URL, store)?;
    let bills = client.list_open_bills(Pagination { all: true, max_pages: None })?;
    println!("{} open bills", bills.len());
    Ok(())
}
```

`Client::with_credentials` supports in-memory credentials for embedding.
`Client::new` can use public endpoints without credentials. `from_store`
loads encrypted cookies immediately, and loads credentials only on session
rejection. Keep separate stores for different portals/accounts. The process
lock is held until the client/store drops; a concurrent command gets a
structured busy error rather than racing cookie rotation.
