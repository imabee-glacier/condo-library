package condo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Authenticate performs the portal's verified sign-in flow, traced
// from the live login page JavaScript on 2026-11-01:
//
//  1. GET the base login page. It contains a single form with email
//     and senha fields, plus hidden fields (url, CHAVE, idCondominio,
//     FL_LOGIN_WEB=1, hashemail). The password field is hidden by
//     JavaScript until the email is verified — that is the "email
//     first, then password" step seen in the browser, not two HTML
//     forms.
//  2. Call the verificarcondomino API:
//     <api>/publico/verificarcondomino?filename=&email=...&hashemail=
//     It returns JSON {"status":"200","data":"-1"} for a registered
//     resident, "1" for first-access/cadastro, anything else for
//     unregistered. Only "-1" proceeds to a password submit.
//  3. POST the form fields (email, senha, hidden fields) to the
//     form's action, .../publico/auth, as a normal AJAX POST
//     (application/x-www-form-urlencoded, Accept: application/json,
//     X-Requested-With: XMLHttpRequest). A direct GET to that URL
//     returns an empty 200 and no session — POST is required.
//     Success returns JSON status 200-206 with data.
//  4. Verify by GETting /cobranca/index under the client base. If
//     that page is still the login form, authentication failed,
//     even if the auth POST returned 200.
//
// Credentials exist only in memory and are never logged. Auth
// failures are not retried: retrying a failed password could
// lock the account.
func (c *Client) Authenticate(ctx context.Context, creds Credentials) error {
	const op = "Authenticate"
	if strings.TrimSpace(creds.Username) == "" || creds.Password == "" {
		return wrapErr(ErrCodeValidation, op, fmt.Errorf("username and password are required"))
	}
	page, err := c.get(ctx, op, "")
	if err != nil {
		return err
	}
	forms, ferr := parseForms(string(page))
	if ferr != nil {
		return wrapErr(ErrCodeParse, op, ferr)
	}
	var loginForm *Form
	for i := range forms {
		if formField(forms[i], "email") != "" && formField(forms[i], "password") != "" {
			loginForm = &forms[i]
			break
		}
	}
	if loginForm == nil {
		return wrapErr(ErrCodeUnauthenticated, op, fmt.Errorf("no combined email+password login form found on the portal login page"))
	}
	emailField := formField(*loginForm, "email")
	pwField := formField(*loginForm, "password")
	// Hidden fields, as sent by the browser (values are empty on the
	// initial page except FL_LOGIN_WEB=1; they are preserved verbatim).
	hashemail := ""
	if v, ok := loginForm.Fields["hashemail"]; ok {
		hashemail = v
	}
	// Step 2: verificarcondomino.
	verifyURL := verifyURLFromFormAction(loginForm.Action, c.base.String())
	q := url.Values{}
	q.Set("filename", "")
	q.Set("email", creds.Username)
	q.Set("hashemail", hashemail)
	verifyBody, vstatus, verr := c.do(ctx, http.MethodGet, verifyURL+"?"+q.Encode(), nil, "")
	if verr != nil {
		return wrapErr(ErrCodeUnauthenticated, op, fmt.Errorf("verificarcondomino request failed: %w", verr))
	}
	if vstatus < 200 || vstatus >= 300 {
		return wrapErr(ErrCodeUnauthenticated, op, fmt.Errorf("verificarcondomino returned status %d", vstatus))
	}
	var vresp struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
		Msg    string          `json:"msg"`
	}
	if jerr := json.Unmarshal(verifyBody, &vresp); jerr != nil {
		return wrapErr(ErrCodeParse, op, fmt.Errorf("verificarcondomino response is not JSON: %w", jerr))
	}
	verifyData := strings.Trim(string(vresp.Data), `"`)
	switch verifyData {
	case "-1":
		// Registered resident: proceed.
	case "1":
		return wrapErr(ErrCodeUnauthenticated, op, fmt.Errorf("email requires first-access registration (verificarcondomino=1); password login is not the next step"))
	default:
		return wrapErr(ErrCodeUnauthenticated, op, fmt.Errorf("email is not a registered resident (verificarcondomino=%s)", verifyData))
	}
	// Step 3: POST auth.
	values := url.Values{}
	for k, v := range loginForm.Fields {
		values.Set(k, v)
	}
	values.Set(emailField, creds.Username)
	values.Set(pwField, creds.Password)
	authURL := loginForm.Action
	if authURL == "" {
		authURL = c.base.String()
	}
	authBody, astatus, aerr := c.postAuth(ctx, authURL, values)
	if aerr != nil {
		return aerr
	}
	if astatus < 200 || astatus >= 300 {
		return wrapErr(ErrCodeUnauthenticated, op, fmt.Errorf("auth submission returned status %d", astatus))
	}
	var aresp struct {
		Status string `json:"status"`
		Msg    string `json:"msg"`
	}
	if jerr := json.Unmarshal(authBody, &aresp); jerr != nil {
		return wrapErr(ErrCodeUnauthenticated, op, fmt.Errorf("auth response is not JSON (auth may have failed): %w", jerr))
	}
	if aresp.Status != "" {
		// Superlógica JSON statuses 200-206 are success; anything
		// else, including 202's neighbours outside that range, is
		// treated as failure. 202 itself is the observed success.
		s := aresp.Status
		if s != "200" && s != "201" && s != "202" && s != "203" && s != "204" && s != "205" && s != "206" {
			return wrapErr(ErrCodeUnauthenticated, op, fmt.Errorf("auth returned status %s: %s", s, aresp.Msg))
		}
	}
	// Step 4: verify with a real list page.
	verify, verr := c.get(ctx, op, "/cobranca/index")
	if verr != nil {
		return wrapErr(ErrCodeUnauthenticated, op, fmt.Errorf("verification list fetch failed: %w", verr))
	}
	if strings.Contains(string(verify), "Areadocondomino_Forms_Auth_EntrarLogar") {
		return wrapErr(ErrCodeUnauthenticated, op, fmt.Errorf("portal still presents the login form after auth"))
	}
	if strings.Contains(string(verify), "Aplicação inválida") {
		return wrapErr(ErrCodeUnauthenticated, op, fmt.Errorf("portal returned invalid application on verification fetch"))
	}
	c.authed = true
	if c.creds == nil {
		c.creds = &Credentials{Username: creds.Username, Password: creds.Password}
	}
	return nil
}

// verifyURLFromFormAction derives the verificarcondomino API URL
// from the login form's action, e.g.
// .../areadocondomino/atual/publico/auth -> .../areadocondomino/atual/publico/verificarcondomino.
// If the action does not end with /publico/auth, it falls back to
// <scheme>://<host>/areadocondomino/atual/publico/verificarcondomino.
func verifyURLFromFormAction(action, base string) string {
	if strings.HasSuffix(action, "/publico/auth") {
		return strings.TrimSuffix(action, "/auth") + "/verificarcondomino"
	}
	if u, err := url.Parse(base); err == nil && u.Host != "" {
		return u.Scheme + "://" + u.Host + "/areadocondomino/atual/publico/verificarcondomino"
	}
	return action
}

// postAuth POSTs form fields to the auth endpoint with the AJAX
// headers the portal's JavaScript uses. It uses the client's cookie
// jar, so the session established by the login page and by
// verificarcondomino is carried through. It never logs the body.
func (c *Client) postAuth(ctx context.Context, authURL string, values url.Values) ([]byte, int, error) {
	full, err := c.resolve(authURL)
	if err != nil {
		return nil, 0, wrapErr(ErrCodeValidation, "Authenticate", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, full, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, 0, wrapErr(ErrCodeValidation, "Authenticate", err)
	}
	req.Header.Set("User-Agent", c.agent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json, text/javascript, */*")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, wrapErr(ErrCodeHTTP, "Authenticate", err)
	}
	defer resp.Body.Close()
	data, rerr := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if rerr != nil {
		return nil, 0, wrapErr(ErrCodeHTTP, "Authenticate", rerr)
	}
	return data, resp.StatusCode, nil
}

// Authenticated reports whether an Authenticate call has succeeded on
// this client. It does not prove the portal session is still valid.
func (c *Client) Authenticated() bool { return c.authed }

func (c *Client) postForm(ctx context.Context, action string, values url.Values) ([]byte, int, error) {
	full, err := c.resolve(action)
	if err != nil {
		return nil, 0, err
	}
	data, status, err := c.do(ctx, "POST", full, strings.NewReader(values.Encode()), "application/x-www-form-urlencoded")
	if err != nil {
		return nil, 0, err
	}
	return data, status, nil
}

func findPasswordForm(forms []Form) *Form {
	for i := range forms {
		if formField(forms[i], "password") != "" {
			return &forms[i]
		}
	}
	return nil
}
