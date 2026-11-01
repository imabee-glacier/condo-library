# condo-library-go

A proper Go SDK for the Superlógica Área do Condômino resident portal
(Dinâmica Condomínios SC), version 0.1.0.

It is a direct-HTTP SDK, not a browser-automation wrapper. At runtime it
makes ordinary HTTP requests to the portal's JSON API
(`https://<host>/areadocondomino/atual/<controller>/index`, traced live
2026-11-01) and returns JSON-serialisable results. There is no browser,
no headless browser, and no WebDriver in the runtime path. A browser was
used only once, to map the portal; it is never used by this library.
The client HTML pages are shells; their data is loaded by JavaScript
from that API, so HTML table parsing is not the read path.

## What it does

- **Data-first, JSON-first.** Every read returns structured JSON. PDFs
  are only fetched when the caller explicitly asks for the official
  document (`FetchDocumentPDF` / `fetch-document-pdf`).
- **JSONL tracking, like the audit repo.** Records are encoded one JSON
  object per line, money is a decimal string (never binary floating
  point), and a missing field means "not reported", not zero.
- **Autonomous routine checks, silent unless new.** See
  `check-new-bills` below.
- **AI-safe actions.** Every consequential action is preview-first and
  gated. See Safety tiers.

## Authentication — read this first

**Secure Vault limitation, stated plainly:** Muse's Secure Vault does
NOT hand a stored password back to an SDK, to Muse, or to any tool. It
only fills the password in the browser login flow. This SDK therefore
cannot read a password out of the Secure Vault, and no code here tries
to. Do not design around that false assumption, and do not claim
Vault-to-SDK direct use works.

For autonomous, browser-free operation the SDK uses its own encrypted
credential store, enrolled once in a separate one-time user step:

```
condo setup-auth --store-dir ~/.condo-store --username <your-email>
# Password is read from stdin without echo, encrypted immediately,
# and never stored in plain text.
```

### Seeding from a temporary file (user-approved alternative)

```
# Create the temp file OUTSIDE any git repository, with 0600, e.g.:
umask 077
mktemp /tmp/condo-pass.XXXXXX   # then edit that file to contain only the password
chmod 600 /tmp/condo-pass.XXXXXX

condo setup-auth --store-dir ~/.condo-store --username <your-email> \
  --from-file /tmp/condo-pass.XXXXXX
```

The command reads the file once, in memory only, never prints, logs,
or returns its contents, and never copies it to chat, memory, or any
other file. It encrypts it immediately into the store, then securely
deletes the source in the same run: overwrite with random bytes,
unlink, and verify it is gone. It reports only success and deletion
verification, never the password or its length.

It refuses, leaving the file untouched, if the file is a symlink, lives
inside a git repository, or has permissions more open than 0600. The
temp file should live outside any repo, be created with 0600, and be
deleted by this command, not left sitting around. If seeding fails
before encryption, the file is left untouched so a failed attempt does
not destroy the only copy.

### Local-only alternative

The user can build the same binary on their own machine and run
`setup-auth` there, so the password never reaches Muse at all. In that
mode Muse only verifies that an encrypted store exists (for example,
the user reports that the store files are present); routine checks
that need the password then run on the user's own machine, not by
Muse, because Muse has no access to a store that stays local. That
trade-off is intentional: no password reaches Muse, and no remote use
of that local store is claimed.

Store layout, both files `0600`, directory `0700`, both gitignored and
excluded from artifacts:

- `credentials.key` — 32 random bytes, the AES-256 key, kept separate
  from the ciphertext.
- `credentials.enc` — nonce + AES-256-GCM ciphertext of
  `{username, password}`.

Plaintext exists only in memory and is never logged. **Do not run
setup-auth by copying a password from the Secure Vault** — that is
impossible by design — and do not ask for a password in chat. Setup is
a separate one-time step after the SDK is built; it is not run as part
of building or testing this SDK, and no password is enrolled here.

After setup, routine commands use the store without user input:

```
condo list-open-bills --store-dir ~/.condo-store
```

If a session expires, the SDK re-logs in automatically from the
in-memory credentials loaded from the store. If a long-lived or
refreshable token is later discovered on the portal, support for reusing
and refreshing it can be added, still without persisting it by default.
No such token is assumed today.

For one-off use without a store, the CLI also accepts
`CONDO_USERNAME` / `CONDO_PASSWORD` at runtime only. The CLI never
writes those values anywhere.

## Usage

Go library:

```go
package main

import (
    "context"
    "condo"
)

func main() {
    client, _ := condo.NewClient(condo.Config{BaseURL: condo.DefaultBaseURL})
    // Supply credentials at runtime, in memory only, e.g. from the
    // encrypted store: condo.NewCredentialStore(dir).Load()
    _ = client.Authenticate(context.Background(), condo.Credentials{
        Username: "user@example.com", Password: "…",
    })
    bills, _ := client.ListOpenBills(context.Background(), condo.Pagination{})
    _ = bills
}
```

CLI (JSON on stdout, errors on stderr):

```
condo tools
condo list-open-bills --store-dir ~/.condo-store
condo list-paid-bills --store-dir ~/.condo-store --all
condo list-documents --store-dir ~/.condo-store
condo list-notices --store-dir ~/.condo-store
condo notice-detail --id <id> --store-dir ~/.condo-store
condo list-reservations --store-dir ~/.condo-store
condo list-requests --store-dir ~/.condo-store
condo list-occurrences --store-dir ~/.condo-store
condo fetch-document-pdf --id <id> --output doc.pdf --store-dir ~/.condo-store
condo preview-request --title "…" --subject "…"
condo create-request --title "…" --subject "…" --confirm --store-dir ~/.condo-store
condo preview-reservation --area "SALÃO DE FESTAS" --date 2026-11-02 --period "full day"
condo pre-reserve --area "SALÃO DE FESTAS" --date 2026-11-02 --period "full day" --confirm --store-dir ~/.condo-store
```

## Routine automation: check-new-bills

```
condo check-new-bills --store-dir ~/.condo-store \
  --state ~/condo-state/open-bills.jsonl --update-state
```

Output is JSON, for example:

```json
{"checked_at": "2026-11-01T00:00:00Z", "new_bills": [], "count": 0}
```

Each new bill includes period, due date, total (`amount_brl`), and
status. When `new_bills` is empty, a cron wrapper should stay silent.
Daily cron example:

```
# Daily, silent unless a new bill arrives; report in the condo side chat only then.
0 9 * * * condo check-new-bills --store-dir ~/.condo-store --state ~/condo-state/open-bills.jsonl --update-state > /tmp/condo-check.json 2>/tmp/condo-check.err || true
# Notify only if the JSON count is greater than 0.
```

No user website visit and no user input per check, after the one-time
setup-auth step. `--update-state` writes the current open-bill list as
JSONL (0600) so the next run is silent unless something changes.

## Tool catalog

| Tool | Read-only | Changes state | Notes |
|---|---|---|---|
| list_open_bills | yes | no | Empty list when none open |
| list_paid_bills | yes | no | `--all` follows More-items pagination |
| list_documents | yes | no | Metadata only, no content |
| list_notices | yes | no | Sender unknown unless the row states it |
| notice_detail | yes | no | Opening may change read state |
| list_reservation_availability | yes | no | Grid slots |
| list_requests | yes | no | Empty list when none |
| list_occurrences | yes | no | Empty list when none |
| fetch_document_pdf | yes | no | Metadata (size, SHA-256) in JSON; bytes via library/CLI file output only |
| check_new_bills | yes | no | Only new bills vs previous JSONL state |
| preview_request | yes | no | Always before create_request |
| create_request | no | yes | Preview + confirm; see Limitations |
| preview_reservation | yes | no | Always before pre_reserve_reservation |
| pre_reserve_reservation | no | yes | Preview + confirm; see Limitations |

Explicitly **not available** (not in the resident menu, not guessed):
`register_visitor`, `release_visitor`, `assembly_vote`,
`update_profile`, `change_payment_method`, `pay_bill`.

Each tool in code carries a name, description, JSON input/output
schema, and safety annotations (`read_only`, `destructive`,
`idempotent`, `moves_money`, `changes_state`).

## Safety tiers

- **Read:** no confirmation needed.
- **Consequential** (request, reservation, and any future pay/vote/
  change): preview first, then explicit `confirm=true` matching that
  preview. Without confirmation the call returns the preview and a
  `confirmation_required` error and sends nothing.
- **Destructive:** preview + `confirm=true` + the caller re-stating
  the exact target (see `ValidateDestructive`).

Having a function in the library does not mean an agent should run it
on its own. Payments, votes, and payment-method changes would still
require explicit user approval each time.

## Limitations — what is proven vs undiscovered

Proven by live, read-only execution on 2026-11-01 (no reservation,
request, or payment was made):

- Sign-in: login page form (email + senha, hidden fields) ->
  `publico/verificarcondomino` API (returns -1 for a registered
  resident) -> POST `publico/auth` with AJAX headers -> verified by
  fetching `/cobranca/index`. No code or CAPTCHA was presented.
  A direct GET to the auth URL returns an empty 200 and no session;
  POST is required. Path joining must preserve the client base path;
  joining to the host root returns `Aplicação inválida` 400.
- Menu sections present: Boletos, Documentos, Comunicados, Reservas,
  Solicitações, Ocorrências.
- JSON API reads, all validated live: bills
  (`cobranca/index`, status parameter required, classified by
  `dt_liquidacao_recb`), documents
  (`impressoes/index?doTipo=documento&apenasOutrosDocumentos=1`),
  notices (`comunicados/index?doTipo=documento`), reservations
  (`reservas/index`, sub-areas with a datas map), requests
  (`tickets/index`, empty), occurrences (`ocorrencias/index`, empty).
  Notice detail is from the list API, so it does not open a notice
  page or change read state. Document PDF:
  `/documentos/download?id={id}` (validated live, metadata only by
  default).

Undiscovered / unverified — the SDK marks these explicitly and does
not guess them:
- **No request submission endpoint.** The `/tickets` "New request"
  modal is `javascript:void(0)`; a confirmed `CreateRequest` returns
  `submission_endpoint_undiscovered` and sends nothing.
- **No reservation submission endpoint.** The `/reservas`
  pre-reserve dialog is `javascript:void(0)`; a confirmed
  `PreReserveReservation` returns `submission_endpoint_undiscovered`
  and makes no reservation.
- **No payment endpoint.** Open charges were 0 in the mapping and paid
  rows are JavaScript-driven, so whether a bill row opens an HTML
  second-copy detail or a PDF, and any payment endpoint, are unresolved.
- **Notice sender split unverified.** Individual notices were not
  opened (to avoid changing read state), so administrator vs síndico
  sender is unknown.
- **Live reservation / request / payment not executed.** The SDK has
  not made a live reservation, request, or payment. Do not claim
  those work until their endpoints are discovered with user approval.

## Development

Requires Go 1.27 (toolchain at `~/workspace/tools/go/bin/go` in this
workspace). One external dependency, `golang.org/x/net/html`, is used
only for HTML parsing; everything else is standard library.

```sh
export PATH=$HOME/workspace/tools/go/bin:$PATH
gofmt -l .
go vet ./...
go test ./...
go build -o condo ./cmd/condo
```

Tests use a local mock HTTP server with fake API JSON fixtures and
never hit the live portal. Live validation is a separate, explicit
step, as above.
