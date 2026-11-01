package condo

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// This file implements reads against the portal's JSON API, traced
// live on 2026-11-01. The client HTML pages are shells: bills, documents,
// notices and reservations are loaded by JavaScript from
// https://<host>/areadocondomino/atual/<controller>/index, not from
// the HTML tables. The earlier HTML parsers returned empty lists
// against live HTML, so they are no longer the primary path.
//
// API base is derived from the client base host, not hard-coded to a
// licence: <scheme>://<host>/areadocondomino/atual
//
// Authenticated session (cookies from Authenticate) is carried by
// the client's cookie jar. All calls here are GETs and change no
// portal state, except NoticeDetail, which is implemented from the
// list API so it does not open a notice page and change read state.

func (c *Client) apiBase() string {
	scheme := c.base.Scheme
	if scheme == "" {
		scheme = "https"
	}
	return scheme + "://" + c.base.Host + "/areadocondomino/atual"
}

// apiGet fetches an API URL and unmarshals the standard envelope:
// {"status":"200","msg":"","data":...,"session":...}
func (c *Client) apiGet(ctx context.Context, op, url string) (json.RawMessage, error) {
	body, status, err := c.do(ctx, "GET", url, nil, "")
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, wrapErr(ErrCodeHTTP, op, fmt.Errorf("portal returned status %d", status))
	}
	var env struct {
		Status string          `json:"status"`
		Msg    string          `json:"msg"`
		Data   json.RawMessage `json:"data"`
	}
	if jerr := json.Unmarshal(body, &env); jerr != nil {
		return nil, wrapErr(ErrCodeParse, op, fmt.Errorf("API response is not JSON: %w", jerr))
	}
	if env.Data == nil {
		return json.RawMessage("[]"), nil
	}
	return env.Data, nil
}

// parseAPIDate converts a server date to ISO YYYY-MM-DD.
// Server API dates are M/D/Y (e.g. 10/29/2026), with an optional
// time suffix. If the first component is >12, it is D/M/Y instead.
// Empty input returns "".
func parseAPIDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Drop time suffix.
	if i := strings.Index(s, " "); i >= 0 {
		s = s[:i]
	}
	if iso := normalizeDate(s); iso != "" && strings.Contains(s, "-") {
		return iso
	}
	parts := strings.Split(s, "/")
	if len(parts) != 3 {
		return s
	}
	a, err1 := strconv.Atoi(parts[0])
	b, err2 := strconv.Atoi(parts[1])
	y, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return s
	}
	var month, day int
	if a > 12 {
		// D/M/Y
		day, month = a, b
	} else {
		// M/D/Y (server format)
		month, day = a, b
	}
	return fmt.Sprintf("%04d-%02d-%02d", y, month, day)
}

// parseAPIPeriod converts a competence date to YYYY-MM.
func parseAPIPeriod(s string) string {
	d := parseAPIDate(s)
	if len(d) >= 7 {
		return d[:7]
	}
	return ""
}

func asString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		// JSON numbers: keep integer form when whole.
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

// --- Bills ---

type apiBill struct {
	ID          string `json:"id_recebimento_recb"`
	Due         string `json:"dt_vencimento_recb"`
	Liquidacao  string `json:"dt_liquidacao_recb"`
	Competencia string `json:"dt_competencia_recb"`
	Total       string `json:"vl_total_recb"`
	Label       string `json:"st_label_recb"`
	Forma       string `json:"st_formapagamento_recb"`
}

func billFromAPI(b apiBill) Bill {
	status := BillStatusOpen
	paid := ""
	if strings.TrimSpace(b.Liquidacao) != "" {
		status = BillStatusPaid
		paid = parseAPIDate(b.Liquidacao)
	}
	label := b.Label
	if label == "" {
		label = b.Forma
	}
	return Bill{
		ID:        b.ID,
		Status:    status,
		Period:    parseAPIPeriod(b.Competencia),
		DueDate:   parseAPIDate(b.Due),
		PaidDate:  paid,
		AmountBRL: b.Total,
		Label:     label,
	}
}

func (c *Client) listBillsAPI(ctx context.Context, p Pagination) ([]Bill, error) {
	const op = "ListBills"
	// The API returns 0 rows without a status parameter, and all rows
	// when one is present, regardless of its value. Status is therefore
	// not used as the filter; classification is by dt_liquidacao_recb.
	// Fetch pages of 50 until a short page, up to maxPages.
	var out []Bill
	page := 1
	for {
		url := fmt.Sprintf("%s/cobranca/index?pagina=%d&itensPorPagina=50&status=em-abertas", c.apiBase(), page)
		raw, err := c.apiGet(ctx, op, url)
		if err != nil {
			return nil, err
		}
		var items []apiBill
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, wrapErr(ErrCodeParse, op, err)
		}
		for _, it := range items {
			out = append(out, billFromAPI(it))
		}
		if !p.All || len(items) == 0 {
			break
		}
		page++
		if page > p.maxPages() {
			break
		}
	}
	if out == nil {
		out = []Bill{}
	}
	return out, nil
}

func (c *Client) ListOpenBills(ctx context.Context, p Pagination) ([]Bill, error) {
	all, err := c.listBillsAPI(ctx, p)
	if err != nil {
		return nil, err
	}
	var out []Bill
	for _, b := range all {
		if b.Status == BillStatusOpen {
			out = append(out, b)
		}
	}
	if out == nil {
		out = []Bill{}
	}
	return out, nil
}

func (c *Client) ListPaidBills(ctx context.Context, p Pagination) ([]Bill, error) {
	all, err := c.listBillsAPI(ctx, p)
	if err != nil {
		return nil, err
	}
	var out []Bill
	for _, b := range all {
		if b.Status == BillStatusPaid {
			out = append(out, b)
		}
	}
	if out == nil {
		out = []Bill{}
	}
	return out, nil
}

func (c *Client) ListBills(ctx context.Context, p Pagination) ([]Bill, error) {
	return c.listBillsAPI(ctx, p)
}

// --- Documents ---

type apiDocument struct {
	ID       interface{} `json:"id_impressao_fimp"`
	Title    string      `json:"st_titulo_fimp"`
	Created  string      `json:"dt_criacao_fimp"`
	Tipo     interface{} `json:"fl_tipodocumento_fimp"`
	Etiqueta string      `json:"st_tipo_etiqueta"`
}

func (c *Client) ListDocuments(ctx context.Context) ([]Document, error) {
	const op = "ListDocuments"
	// doTipo=documento, apenasOutrosDocumentos=1 is the URL found in
	// the live Documentos page grid configuration. Paginate until an
	// empty page, so counts above 50 are not truncated.
	out := make([]Document, 0)
	for page := 1; page <= 20; page++ {
		url := fmt.Sprintf("%s/impressoes/index?itensPorPagina=50&pagina=%d&doTipo=documento&apenasOutrosDocumentos=1", c.apiBase(), page)
		raw, err := c.apiGet(ctx, op, url)
		if err != nil {
			return nil, err
		}
		var items []apiDocument
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, wrapErr(ErrCodeParse, op, err)
		}
		if len(items) == 0 {
			break
		}
		for _, it := range items {
			out = append(out, Document{
				ID:       asString(it.ID),
				Title:    it.Title,
				Category: it.Etiqueta,
				SentDate: parseAPIDate(it.Created),
			})
		}
	}
	return out, nil
}

// --- Notices ---

type apiNotice struct {
	ID          interface{} `json:"id_comunicado_com"`
	Title       string      `json:"st_titulo_com"`
	Date        string      `json:"dt_comunicado_com"`
	Body        string      `json:"st_texto_com"`
	Visualizado interface{} `json:"visualizado"`
}

func (c *Client) listNoticesAPI(ctx context.Context) ([]apiNotice, error) {
	const op = "ListNotices"
	url := c.apiBase() + "/comunicados/index?doTipo=documento"
	raw, err := c.apiGet(ctx, op, url)
	if err != nil {
		return nil, err
	}
	var items []apiNotice
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, wrapErr(ErrCodeParse, op, err)
	}
	return items, nil
}

func (c *Client) ListNotices(ctx context.Context) ([]Notice, error) {
	items, err := c.listNoticesAPI(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Notice, 0, len(items))
	for _, it := range items {
		out = append(out, Notice{
			ID:            asString(it.ID),
			Title:         it.Title,
			PublishedDate: parseAPIDate(it.Date),
		})
	}
	return out, nil
}

// NoticeDetail is implemented from the list API, so it does not open
// a notice page and does not change read state on the portal.
func (c *Client) NoticeDetail(ctx context.Context, id string) (NoticeDetail, error) {
	const op = "NoticeDetail"
	if strings.TrimSpace(id) == "" {
		return NoticeDetail{}, wrapErr(ErrCodeValidation, op, fmt.Errorf("notice id is required"))
	}
	items, err := c.listNoticesAPI(ctx)
	if err != nil {
		return NoticeDetail{}, err
	}
	for _, it := range items {
		if asString(it.ID) == id {
			return NoticeDetail{
				Notice: Notice{
					ID:            id,
					Title:         it.Title,
					PublishedDate: parseAPIDate(it.Date),
				},
				Body:    it.Body,
				Warning: "Detail is from the list API; no notice page was opened, so read state was not changed by this call.",
			}, nil
		}
	}
	return NoticeDetail{}, wrapErr(ErrCodeValidation, op, fmt.Errorf("notice id %s not found in list", id))
}

// --- Reservations ---

func (c *Client) ListReservationAvailability(ctx context.Context) ([]ReservationSlot, error) {
	const op = "ListReservationAvailability"
	url := c.apiBase() + "/reservas/index"
	raw, err := c.apiGet(ctx, op, url)
	if err != nil {
		return nil, err
	}
	var items []map[string]interface{}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, wrapErr(ErrCodeParse, op, err)
	}
	var out []ReservationSlot
	for _, it := range items {
		name := asString(it["st_nome_are"])
		if name == "" {
			name = asString(it["titulo_area"])
		}
		name = strings.TrimSpace(name)
		// Group headers have no datas map; skip them.
		datas, ok := it["datas"].(map[string]interface{})
		if !ok {
			continue
		}
		// Name is "AREA - Period" for sub-areas.
		area, period := name, ""
		if i := strings.LastIndex(name, " - "); i >= 0 {
			area = strings.TrimSpace(name[:i])
			period = strings.TrimSpace(name[i+3:])
		}
		for dateStr, v := range datas {
			status := "free"
			if arr, ok := v.([]interface{}); ok && len(arr) > 0 {
				status = "occupied"
			}
			out = append(out, ReservationSlot{
				Area:   area,
				Date:   parseAPIDate(dateStr),
				Period: period,
				Status: status,
			})
		}
	}
	if out == nil {
		out = []ReservationSlot{}
	}
	return out, nil
}

// --- Requests / Occurrences ---

func (c *Client) ListRequests(ctx context.Context) ([]Request, error) {
	const op = "ListRequests"
	url := c.apiBase() + "/tickets/index"
	raw, err := c.apiGet(ctx, op, url)
	if err != nil {
		return nil, err
	}
	var items []map[string]interface{}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, wrapErr(ErrCodeParse, op, err)
	}
	out := make([]Request, 0, len(items))
	for _, it := range items {
		// Field names are grid-dependent; keep title/subject only
		// when present, never invent them.
		title := asString(it["assunto"])
		if title == "" {
			title = asString(it["st_titulo"])
		}
		out = append(out, Request{
			ID:      asString(it["id"]),
			Title:   title,
			Subject: title,
		})
	}
	return out, nil
}

func (c *Client) ListOccurrences(ctx context.Context) ([]Occurrence, error) {
	const op = "ListOccurrences"
	url := c.apiBase() + "/ocorrencias/index"
	raw, err := c.apiGet(ctx, op, url)
	if err != nil {
		return nil, err
	}
	var items []map[string]interface{}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, wrapErr(ErrCodeParse, op, err)
	}
	out := make([]Occurrence, 0, len(items))
	for _, it := range items {
		title := asString(it["st_titulo_uoc"])
		if title == "" {
			title = asString(it["st_titulo"])
		}
		out = append(out, Occurrence{
			ID:    asString(it["id_ocorrencia_uoc"]),
			Title: title,
		})
	}
	return out, nil
}

// FetchDocumentPDF fetches the official PDF for a document, only when
// the caller explicitly asks for it. It returns the raw PDF bytes.
func (c *Client) FetchDocumentPDF(ctx context.Context, id string) ([]byte, error) {
	if strings.TrimSpace(id) == "" {
		return nil, wrapErr(ErrCodeValidation, "FetchDocumentPDF", fmt.Errorf("document id is required"))
	}
	return c.fetchPDF(ctx, "/documentos/download?id="+id)
}

// DocumentPDFInfo returns metadata for a PDF without returning its bytes
// in a JSON tool response: size and SHA-256, so a caller can verify it.
type DocumentPDFInfo struct {
	DocumentID  string `json:"document_id"`
	SizeBytes   int    `json:"size_bytes"`
	SHA256      string `json:"sha256"`
	ContentType string `json:"content_type"`
}

// FetchDocumentPDFInfo fetches a PDF and returns only its metadata.
func (c *Client) FetchDocumentPDFInfo(ctx context.Context, id string) (DocumentPDFInfo, error) {
	data, err := c.FetchDocumentPDF(ctx, id)
	if err != nil {
		return DocumentPDFInfo{}, err
	}
	sumBytes := sha256.Sum256(data)
	sum := fmt.Sprintf("%x", sumBytes)
	return DocumentPDFInfo{DocumentID: id, SizeBytes: len(data), SHA256: sum, ContentType: "application/pdf"}, nil
}

// CheckNewBills compares current open bills against previously seen bills
// and returns only bills not previously seen. It performs no file I/O;
// the CLI wraps it with JSONL state loading and saving. A bill is
// identified by ID when present, otherwise by due date + label + amount.
// This is the core of the routine-automation mode: silent when empty.
func CheckNewBills(current, previous []Bill) []Bill {
	seen := map[string]bool{}
	for _, b := range previous {
		seen[billKey(b)] = true
	}
	var out []Bill
	for _, b := range current {
		if !seen[billKey(b)] {
			out = append(out, b)
		}
	}
	if out == nil {
		out = []Bill{}
	}
	return out
}

func billKey(b Bill) string {
	if b.ID != "" {
		return "id:" + b.ID
	}
	return "due:" + b.DueDate + "|label:" + b.Label + "|amount:" + b.AmountBRL
}
