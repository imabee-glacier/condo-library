package condo

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// Config configures a Client. Only BaseURL and HTTPClient are required in
// the sense that sensible defaults are supplied when they are absent.
// Credentials, when present, are used only in memory for re-login.
type Config struct {
	BaseURL        string
	HTTPClient     *http.Client
	UserAgent      string
	MaxRetries     int
	RetryBaseDelay time.Duration
	Credentials    *Credentials
}

// Client is a direct-HTTP client for the resident portal. It performs no
// browser automation and holds no browser state.
type Client struct {
	base     *url.URL
	http     *http.Client
	agent    string
	retries  int
	retryGap time.Duration
	creds    *Credentials
	authed   bool // an Authenticate attempt has succeeded at least once
}

// NewClient validates config and returns a ready client. It does not sign
// in; call Authenticate, or supply an already-authenticated HTTPClient.
func NewClient(cfg Config) (*Client, error) {
	baseRaw := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseRaw == "" {
		baseRaw = DefaultBaseURL
	}
	base, err := url.Parse(baseRaw)
	if err != nil {
		return nil, wrapErr(ErrCodeValidation, "NewClient", err)
	}
	if base.Scheme != "https" && base.Scheme != "http" {
		return nil, wrapErr(ErrCodeValidation, "NewClient", fmt.Errorf("base URL must use http or https"))
	}
	hc := cfg.HTTPClient
	if hc == nil {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return nil, wrapErr(ErrCodeValidation, "NewClient", err)
		}
		hc = &http.Client{Jar: jar, Timeout: 30 * time.Second}
	} else if hc.Jar == nil {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return nil, wrapErr(ErrCodeValidation, "NewClient", err)
		}
		hc.Jar = jar
	}
	agent := cfg.UserAgent
	if agent == "" {
		agent = "condo-library-go/" + Version
	}
	retries := cfg.MaxRetries
	if retries < 0 {
		retries = 0
	}
	if retries == 0 && cfg.MaxRetries == 0 {
		retries = 3
	}
	gap := cfg.RetryBaseDelay
	if gap <= 0 {
		gap = 200 * time.Millisecond
	}
	return &Client{base: base, http: hc, agent: agent, retries: retries, retryGap: gap, creds: cfg.Credentials}, nil
}

// BaseURL returns the configured portal base, without any credential.
func (c *Client) BaseURL() string { return c.base.String() }

// ClearCredentials releases any in-memory credentials held by the client.
func (c *Client) ClearCredentials() {
	if c.creds != nil {
		c.creds.Clear()
	}
	c.creds = nil
}

func (c *Client) resolve(path string) (string, error) {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path, nil
	}
	if path == "" || path == "/" {
		return c.base.String(), nil
	}
	// Paths in this SDK are relative to the portal base, e.g.
	// "/cobranca/index" under "https://host/clients/areadocondomino"
	// must become "https://host/clients/areadocondomino/cobranca/index",
	// NOT "https://host/cobranca/index". url.ResolveReference with a
	// leading-slash path would drop the base path, which is what caused
	// the portal to return {"msg":"Aplicação inválida","status":400}.
	// That bug is fixed here by joining against the base path.
	trimmed := strings.TrimPrefix(path, "/")
	basePath := strings.Trim(c.base.Path, "/")
	if basePath != "" && (trimmed == basePath || strings.HasPrefix(trimmed, basePath+"/")) {
		// Already a full host-root path under the base; use it as-is.
		out := *c.base
		if i := strings.Index(trimmed, "?"); i >= 0 {
			out.Path = "/" + trimmed[:i]
			out.RawQuery = trimmed[i+1:]
		} else {
			out.Path = "/" + trimmed
			out.RawQuery = ""
		}
		return out.String(), nil
	}
	pathPart := trimmed
	query := ""
	if i := strings.Index(trimmed, "?"); i >= 0 {
		pathPart = trimmed[:i]
		query = trimmed[i+1:]
	}
	out := *c.base
	if basePath == "" {
		out.Path = "/" + pathPart
	} else {
		out.Path = "/" + basePath + "/" + pathPart
	}
	out.RawQuery = query
	return out.String(), nil
}

func isRetryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable || code == http.StatusGatewayTimeout
}

// do performs an HTTP request with retries for safe idempotent GETs only.
// It never logs headers, cookies, or bodies.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader, contentType string) ([]byte, int, error) {
	full, err := c.resolve(path)
	if err != nil {
		return nil, 0, wrapErr(ErrCodeValidation, "do", err)
	}
	attempts := 1
	if method == http.MethodGet {
		attempts = c.retries + 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, 0, wrapErr(ErrCodeValidation, "do", ctx.Err())
			case <-time.After(c.retryGap * time.Duration(1<<uint(attempt-1))):
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, full, body)
		if err != nil {
			return nil, 0, wrapErr(ErrCodeValidation, "do", err)
		}
		req.Header.Set("User-Agent", c.agent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/pdf,*/*")
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if method == http.MethodGet && isRetryableStatus(resp.StatusCode) && attempt < attempts-1 {
			lastErr = fmt.Errorf("portal returned status %d", resp.StatusCode)
			continue
		}
		return data, resp.StatusCode, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("request failed")
	}
	return nil, 0, wrapErr(ErrCodeHTTP, "do", lastErr)
}

// get returns a page body, re-authenticating once if the portal reports
// unauthenticated and credentials are available in memory.
func (c *Client) get(ctx context.Context, op, path string) ([]byte, error) {
	data, status, err := c.do(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		if c.creds != nil && c.creds.Username != "" && c.creds.Password != "" {
			if aerr := c.Authenticate(ctx, *c.creds); aerr == nil {
				data, status, err = c.do(ctx, http.MethodGet, path, nil, "")
				if err != nil {
					return nil, err
				}
			}
		}
	}
	if status < 200 || status >= 300 {
		return nil, wrapErr(ErrCodeHTTP, op, fmt.Errorf("portal returned status %d", status))
	}
	return data, nil
}

// getPaginated fetches a list page and, when p.All is true, follows
// "Mais itens" / "More items" links up to p.maxPages().
func (c *Client) getPaginated(ctx context.Context, op, path string, p Pagination) ([][]byte, error) {
	pages := [][]byte{}
	seen := map[string]bool{}
	next := path
	for i := 0; i < p.maxPages(); i++ {
		if seen[next] {
			break
		}
		seen[next] = true
		data, err := c.get(ctx, op, next)
		if err != nil {
			return nil, err
		}
		pages = append(pages, data)
		if !p.All {
			break
		}
		link := findMoreItemsLink(string(data))
		if link == "" {
			break
		}
		next = link
	}
	return pages, nil
}

func findMoreItemsLink(html string) string {
	// The link text is searched case-insensitively; the href is taken from
	// the parsed document in parse.go via a dedicated helper.
	return moreItemsLinkFromHTML(html)
}

// fetchPDF fetches a document PDF. It is only called from FetchDocumentPDF,
// never as a default for list data.
func (c *Client) fetchPDF(ctx context.Context, path string) ([]byte, error) {
	data, status, err := c.do(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, wrapErr(ErrCodeHTTP, "FetchDocumentPDF", fmt.Errorf("portal returned status %d", status))
	}
	if len(data) < 5 || string(data[:5]) != "%PDF-" {
		return nil, wrapErr(ErrCodeParse, "FetchDocumentPDF", fmt.Errorf("response is not a PDF"))
	}
	return data, nil
}
