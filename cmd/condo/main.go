// Command condo is the JSON CLI for the condo SDK. It is a direct-HTTP
// program: it makes no browser, no WebDriver, and no headless-browser
// calls. Output is JSON on stdout; errors are JSON-ish text on stderr.
//
// Authentication for routine use:
//   - setup-auth enrolls credentials once, reading the password from
//     stdin without echo and encrypting it immediately into an encrypted
//     store (see the SDK's CredentialStore). Do NOT copy a password from
//     Muse's Secure Vault into setup-auth: the Vault does not hand a
//     password back, it only fills the browser login flow, so setup-auth
//     is a separate one-time step with a password the user types there.
//   - Other commands use --store-dir to load that encrypted store, or
//     CONDO_USERNAME / CONDO_PASSWORD at runtime only (never from a file
//     the CLI writes). Credentials are never printed or logged.
//
// Routine automation example (daily cron, silent unless new):
//
//	condo check-new-bills --store-dir ~/.condo-store --state ~/condo-state/open-bills.jsonl --update-state
//
// The command prints {"checked_at":..., "new_bills":[...]}. When
// new_bills is empty, a cron wrapper should stay silent.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"condo"

	"golang.org/x/term"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	ctx := context.Background()
	switch os.Args[1] {
	case "tools":
		printJSON(condo.Tools())
	case "unmapped":
		printJSON(condo.UnmappedTools())
	case "list-open-bills":
		runWithClient(ctx, func(c *condo.Client) (any, error) {
			return c.ListOpenBills(ctx, condo.Pagination{All: hasFlag("all")})
		})
	case "list-paid-bills":
		runWithClient(ctx, func(c *condo.Client) (any, error) {
			return c.ListPaidBills(ctx, condo.Pagination{All: hasFlag("all")})
		})
	case "list-documents":
		runWithClient(ctx, func(c *condo.Client) (any, error) { return c.ListDocuments(ctx) })
	case "list-notices":
		runWithClient(ctx, func(c *condo.Client) (any, error) { return c.ListNotices(ctx) })
	case "notice-detail":
		id := argAfter("id")
		if id == "" && len(os.Args) > 2 {
			id = os.Args[2]
		}
		runWithClient(ctx, func(c *condo.Client) (any, error) { return c.NoticeDetail(ctx, id) })
	case "list-reservations":
		runWithClient(ctx, func(c *condo.Client) (any, error) { return c.ListReservationAvailability(ctx) })
	case "list-requests":
		runWithClient(ctx, func(c *condo.Client) (any, error) { return c.ListRequests(ctx) })
	case "list-occurrences":
		runWithClient(ctx, func(c *condo.Client) (any, error) { return c.ListOccurrences(ctx) })
	case "fetch-document-pdf":
		id := argAfter("id")
		if id == "" && len(os.Args) > 2 {
			id = os.Args[2]
		}
		out := argAfter("output")
		runWithClient(ctx, func(c *condo.Client) (any, error) {
			data, err := c.FetchDocumentPDF(ctx, id)
			if err != nil {
				return nil, err
			}
			if out != "" {
				if err := os.WriteFile(out, data, 0o600); err != nil {
					return nil, err
				}
			}
			return map[string]any{"document_id": id, "size_bytes": len(data), "saved_to": out}, nil
		})
	case "preview-request":
		p, err := condo.PreviewRequest(argAfter("title"), argAfter("subject"))
		failIf(err)
		printJSON(p)
	case "create-request":
		p, err := condo.PreviewRequest(argAfter("title"), argAfter("subject"))
		failIf(err)
		runWithClient(ctx, func(c *condo.Client) (any, error) {
			return c.CreateRequest(ctx, p, hasFlag("confirm"))
		})
	case "preview-reservation":
		p, err := condo.PreviewReservation(argAfter("area"), argAfter("date"), argAfter("period"))
		failIf(err)
		printJSON(p)
	case "pre-reserve":
		p, err := condo.PreviewReservation(argAfter("area"), argAfter("date"), argAfter("period"))
		failIf(err)
		runWithClient(ctx, func(c *condo.Client) (any, error) {
			return c.PreReserveReservation(ctx, p, hasFlag("confirm"))
		})
	case "setup-auth":
		setupAuth()
	case "check-new-bills":
		checkNewBills(ctx)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage: condo <command> [flags]
Commands: tools, unmapped, list-open-bills, list-paid-bills, list-documents,
  list-notices, notice-detail, list-reservations, list-requests,
  list-occurrences, fetch-document-pdf, preview-request, create-request,
  preview-reservation, pre-reserve, setup-auth, check-new-bills
Flags: --base-url, --store-dir, --state, --update-state, --all, --confirm,
  --title, --subject, --area, --date, --period, --id, --output, --username, --from-file`)
}

func hasFlag(name string) bool {
	for _, a := range os.Args[2:] {
		if a == "--"+name {
			return true
		}
	}
	return false
}

func argAfter(name string) string {
	args := os.Args[2:]
	for i, a := range args {
		if a == "--"+name && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, "--"+name+"=") {
			return strings.TrimPrefix(a, "--"+name+"=")
		}
	}
	return ""
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func failIf(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func baseURL() string {
	if v := argAfter("base-url"); v != "" {
		return v
	}
	if v := os.Getenv("CONDO_BASE_URL"); v != "" {
		return v
	}
	return condo.DefaultBaseURL
}

func runWithClient(ctx context.Context, fn func(*condo.Client) (any, error)) {
	client, creds, err := clientForRun()
	failIf(err)
	if creds != nil {
		defer creds.Clear()
		defer client.ClearCredentials()
	}
	if creds != nil {
		if err := client.Authenticate(ctx, *creds); err != nil {
			failIf(err)
		}
	}
	out, err := fn(client)
	failIf(err)
	printJSON(out)
}

func clientForRun() (*condo.Client, *condo.Credentials, error) {
	storeDir := argAfter("store-dir")
	if storeDir == "" {
		storeDir = os.Getenv("CONDO_STORE_DIR")
	}
	if storeDir != "" {
		store := condo.NewCredentialStore(storeDir)
		if !store.Exists() {
			return nil, nil, fmt.Errorf("credential store not found in %s; run setup-auth once", storeDir)
		}
		return condo.ClientFromStore(baseURL(), store)
	}
	username := os.Getenv("CONDO_USERNAME")
	password := os.Getenv("CONDO_PASSWORD")
	if username != "" && password != "" {
		creds := &condo.Credentials{Username: username, Password: password}
		client, err := condo.NewClient(condo.Config{BaseURL: baseURL(), Credentials: creds})
		return client, creds, err
	}
	// No credentials: the client can still be constructed, and reads will
	// fail with a structured unauthenticated error if the portal requires
	// sign-in. This keeps the CLI honest about auth state.
	client, err := condo.NewClient(condo.Config{BaseURL: baseURL()})
	return client, nil, err
}

func setupAuth() {
	dir := argAfter("store-dir")
	if dir == "" {
		dir = argAfter("dir")
	}
	if dir == "" {
		fmt.Fprintln(os.Stderr, "setup-auth requires --store-dir")
		os.Exit(2)
	}
	username := argAfter("username")
	if username == "" {
		fmt.Fprintln(os.Stderr, "setup-auth requires --username")
		os.Exit(2)
	}
	// Temp-file seeding path: read once, encrypt immediately, securely
	// delete the source in the same run. See the SDK's seed.go.
	if fromFile := argAfter("from-file"); fromFile != "" {
		store := condo.NewCredentialStore(dir)
		failIf(store.SeedFromFile(username, fromFile))
		fmt.Fprintln(os.Stderr, "credentials enrolled in encrypted store; source file securely deleted and verified gone")
		return
	}
	fmt.Fprint(os.Stderr, "Password (no echo): ")
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	failIf(err)
	defer func() {
		for i := range pw {
			pw[i] = 0
		}
	}()
	if len(pw) == 0 {
		fmt.Fprintln(os.Stderr, "password is required")
		os.Exit(2)
	}
	store := condo.NewCredentialStore(dir)
	failIf(store.Enroll(username, pw))
	fmt.Fprintln(os.Stderr, "credentials enrolled in encrypted store (key and ciphertext are separate files, both 0600)")
}

func checkNewBills(ctx context.Context) {
	statePath := argAfter("state")
	if statePath == "" {
		fmt.Fprintln(os.Stderr, "check-new-bills requires --state <jsonl file>")
		os.Exit(2)
	}
	client, creds, err := clientForRun()
	failIf(err)
	if creds != nil {
		defer creds.Clear()
		defer client.ClearCredentials()
	}
	if creds != nil {
		failIf(client.Authenticate(ctx, *creds))
	}
	current, err := client.ListOpenBills(ctx, condo.Pagination{})
	failIf(err)
	var previous []condo.Bill
	if data, rerr := os.ReadFile(statePath); rerr == nil {
		// Parse JSONL from the state file without importing extra helpers.
		previous, err = condo.UnmarshalBillsJSONL(strings.NewReader(string(data)))
		failIf(err)
	} else if !os.IsNotExist(rerr) {
		failIf(rerr)
	}
	//nolint:govet // previous may be nil; CheckNewBills handles nil.
	newBills := condo.CheckNewBills(current, previous)
	out := map[string]any{
		"checked_at": time.Now().UTC().Format(time.RFC3339),
		"new_bills":  newBills,
		"count":      len(newBills),
	}
	if hasFlag("update-state") {
		records := make([]any, 0, len(current))
		for _, b := range current {
			records = append(records, b)
		}
		data, merr := condo.MarshalJSONL(records)
		failIf(merr)
		if dir := filepath.Dir(statePath); dir != "" && dir != "." {
			failIf(os.MkdirAll(dir, 0o700))
		}
		failIf(os.WriteFile(statePath, data, 0o600))
	}
	printJSON(out)
}
