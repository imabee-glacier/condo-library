# Audit and Rust replacement

Audit date: 2026-11-01. Baseline: Git commit
`85cfd4d7d002e32d23f5a12deff4c8f645ab60df` (the prior Go implementation).
Sources were reviewed locally; Go tests and vet passed before replacement.
Passing those tests did not establish correct session recovery or live behavior.
The repository now contains only the new Rust implementation.

## Findings and disposition

| Finding in the Go baseline | Impact | Rust implementation |
| --- | --- | --- |
| `cmd/condo/main.go` authenticates unconditionally for every credential-backed invocation | Extra password use and loss of session continuity | Reads restore encrypted cookies before attempting login; credentials load only after rejection |
| `reads.go:apiGet` bypasses `client.go:get` re-authentication | Actual JSON read path cannot recover an expired session | Shared JSON read path detects rejection, renews once, retries once |
| API envelope status ignored; absent `data` becomes `[]` | Failures can look like no bills/documents | Rejects unsuccessful statuses, missing data, malformed JSON, and wrong data shapes |
| `get` calls `Authenticate`, which calls `get` | Recursion possible when authentication pages also reject requests | Login uses a non-renewing transport path; failed password POST never retries |
| `client.go:resolve` allows arbitrary absolute URLs and default redirects follow other origins | Cookies or submitted credentials can leave the expected origin | Form actions, requests, redirects restricted to configured origin; HTTPS required outside loopback |
| Auth success checked against weak HTML string absence | An unrelated shell may appear authenticated | Login must succeed against a real JSON bills read |
| `store.go:Enroll` replaces key before ciphertext as separate writes | Interrupted enrollment can destroy ability to decrypt existing credentials | Stable key, authenticated versioned records, atomic ciphertext replacement |
| Existing store directory/file permissions not enforced on load | A previously insecure store may expose credentials | Ownership/type/mode/link checks and rejection of unsafe stores |
| No process-level store synchronization | Parallel agents can lose updates | Exclusive lock held across a complete operation/client lifetime |
| `Credentials.Clear` reassigns immutable Go strings | Claimed memory wiping is inaccurate | Owned Rust secret strings/buffers zeroize on drop; third-party copies explicitly excluded from guarantee |
| `seed.go` promises secure deletion | SSD/COW/snapshot erasure not provable | Best-effort overwrite/unlink only; explicit limitation and preserved source on pre-enrollment failure |
| `io.LimitReader` truncates without overflow detection | Partial responses can be accepted or misdiagnosed | Reads limit+1 bytes and reports an explicit size error |
| PDF ID concatenated into query | Query injection/incorrect IDs | URL query encoding |
| `ExecuteTool` ignores some JSON decode errors and schemas wrap results that are actually bare arrays | Agent contracts diverge from runtime | Strict serde input types, generated schemas, typed output models, schema/response tests |
| Pagination stops silently at configured cap | Automation may update state from incomplete data | Full-pagination mode fails if no terminating empty page appears; repeated records fail |
| Routine checks fetch only the first page | Missed newly posted bills | `check-new-bills` fetches all pages and updates state atomically only after success |
| Reservation parsing interprets non-array values as free | Unknown availability looks bookable | Non-array values produce `unknown` |
| Preview validation does not fully bind the action and normalized payload | Confirmation can be ambiguous | Full payload/action equality and SHA-256 fingerprints; host remains responsible for obtaining consent |

No write endpoint, payment flow, refresh token, mobile device identity, or
unsupported feature was invented. Request/reservation submission remains
explicitly unavailable, even with confirmation.

## Session investigation

The baseline describes a resident web form flow through
`publico/verificarcondomino`, `publico/auth`, then a bills read. The new
client implements that HTTP flow and preserves hidden fields. Those
baseline live claims are not accepted as new live validation.

The vendor's [mobile login guide](https://condominios.superlogica.com/hc/pt-br/articles/41295300998039)
describes registered resident accounts, but does not document a refresh-token
contract. The [official ERP API](https://apicondominios.superlogica.com/)
requires App/Access tokens created by an administrator. There is no evidence
here that those are interchangeable with a resident/mobile session.

An initial unauthenticated request returned HTTP 403. A later direct-HTTP
request using the SDK user agent succeeded, allowing inspection of the
public login HTML and its linked `custom` JavaScript bundle. The form uses
`email`, `senha`, hidden fields including `FL_LOGIN_WEB`, and the same-origin
`publico/auth` action. The JavaScript checks `verificarcondomino` for `-1`
before submission. The login HTML is Latin-1 rather than UTF-8; the Rust
parser handles that encoding. The inspected form/custom/spa/inline web
assets expose no refresh-token flow. This is evidence about those web
assets only, not a reverse engineering of the mobile app.

No mobile refresh endpoint or server session TTL is established. Cookie
persistence and one bounded password re-login are implemented and mock-tested;
neither can override server-side expiration, account policy, or sandbox disk
deletion.

## Verification

Local integration tests cover encrypted cookie reuse across client restarts
without a credential file, cookie rotation, API/HTTP/HTML authentication
rejection, failed-password single attempt, recursion prevention, hostile
forms and redirects, transient GET retries, typed reads, query-safe PDF
fetches, exact decimal strings, pagination completeness, JSONL state,
strict tool inputs, preview tampering, encrypted record tampering/context
binding, file permissions, symlinks/hardlinks, source cleanup, process locking,
CLI JSON errors, enforced offline behavior with zero requests, and the
complete CLI enrollment/read/state/PDF/session-deletion workflow using only
fake credentials on a loopback server.

Required repository checks are formatting, warnings-as-errors Clippy,
all-target tests, documentation tests, release build, and RustSec dependency
audit with warnings denied. CI repeats these checks.

Final local results: formatting passed; Clippy passed with warnings as errors;
23 integration tests passed; the README library example passed as a documentation
test; the optimized release build passed; RustSec audit passed with warnings
denied (293 locked dependencies). The release CLI loaded all 14 tool definitions
offline and rejected an offline list operation with its expected structured error.

## Authorized live read-only validation

The owner subsequently authorized sign-in and account reads, explicitly excluding
reservation submissions, condominium requests, and messages. Direct-HTTP sign-in,
open and paid bill pagination, documents, notices, notice detail from the list API,
reservation availability, requests, and occurrences all succeeded. An official
PDF initially exceeded the generic 12 MiB response cap; PDF downloads now have a
separate 64 MiB cap, with regression coverage retaining the smaller API cap. The
same live PDF then downloaded successfully. Document bytes were not saved.

Session reuse was verified in a fresh CLI process while the encrypted credential
file was temporarily unavailable; it was restored in a finally block. This proves
that the saved session supported a subsequent read without a password login.
Routine bill tracking was checked twice using private local state; the second run
reported no new bills. Credentials, cookies, state, and private response contents
remain outside the repository. No reservation, condominium request, message,
payment, or other business action was submitted. Mobile refresh tokens and the
server's maximum session lifetime remain unverified.
