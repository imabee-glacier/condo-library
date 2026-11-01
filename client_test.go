package condo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const loginPage = `<html><body><form action="/auth" method="get"><input type="text" name="email"><input type="password" name="senha"><input type="hidden" name="FL_LOGIN_WEB" value="1"></form></body></html>`
const passwordPage = loginPage
const billsPage = `<html><body><table>
<tr><td>Pago</td><td>10/10/2026</td><td>R$ 250,00</td><td>2026-10</td></tr>
<tr><td>Aberto</td><td>10/11/2026</td><td>R$ 260,00</td><td>2026-11</td></tr>
</table><a href="/cobranca/index?page=2">Mais itens</a></body></html>`
const billsPage2 = `<html><body><table><tr><td>Pago</td><td>10/09/2026</td><td>R$ 240,00</td><td>2026-09</td></tr></table></body></html>`
const docsPage = `<html><body><div data-category="DOCUMENTOS"><a href="/documentos/download?id=doc1">Monthly statement</a> 29/10/2026</div></body></html>`
const noticesPage = `<html><body><ul><li>Notice one 29/10/2026</li><li>Notice two 28/10/2026</li></ul></body></html>`
const noticePage = `<html><body><h1>Notice one</h1><article>Body text 29/10/2026</article></body></html>`
const reservationsPage = `<html><body><div data-area="SALÃO DE FESTAS" data-date="2026-11-02" data-period="full day" data-status="free"></div><div data-area="MUDANÇAS BLOCO 01" data-date="2026-11-02" data-period="morning" data-status="disabled"></div></body></html>`
const emptyRequestsPage = `<html><body><p>Nenhuma solicitação encontrada</p></body></html>`
const emptyOccurrencesPage = `<html><body><p>Nenhuma ocorrência encontrada</p></body></html>`

func mockServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Login page at root.
		if r.URL.Path == "/" {
			_, _ = w.Write([]byte(loginPage))
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/areadocondomino/atual/publico/verificarcondomino", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"200","msg":"","data":"-1"}`))
	})
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "auth requires POST", http.StatusMethodNotAllowed)
			return
		}
		_ = r.ParseForm()
		if r.FormValue("email") == "" || r.FormValue("senha") == "" {
			http.Error(w, "email and senha required", http.StatusBadRequest)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "valid", Path: "/"})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"202","msg":"","data":{}}`))
	})
	mux.HandleFunc("/cobranca/index", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(billsPage2))
			return
		}
		_, _ = w.Write([]byte(billsPage))
	})
	mux.HandleFunc("/impressoes", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(docsPage)) })
	mux.HandleFunc("/comunicados/index/doTipo/documento", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(noticesPage))
	})
	mux.HandleFunc("/comunicados/id/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(noticePage)) })
	mux.HandleFunc("/reservas", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(reservationsPage)) })
	mux.HandleFunc("/tickets", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(emptyRequestsPage)) })
	mux.HandleFunc("/ocorrencias", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(emptyOccurrencesPage)) })
	mux.HandleFunc("/documentos/download", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.4 fake"))
	})
	mux.HandleFunc("/areadocondomino/atual/cobranca/index", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		pagina := r.URL.Query().Get("pagina")
		if pagina == "1" {
			_, _ = w.Write([]byte(`{"status":"200","msg":"","data":[{"id_recebimento_recb":"1","dt_vencimento_recb":"10/10/2026 00:00:00","dt_liquidacao_recb":"10/01/2026 00:00:00","dt_competencia_recb":"09/15/2026 00:00:00","vl_total_recb":"250.00","st_label_recb":"","st_formapagamento_recb":"Boleto"},{"id_recebimento_recb":"2","dt_vencimento_recb":"11/10/2026 00:00:00","dt_liquidacao_recb":"","dt_competencia_recb":"10/15/2026 00:00:00","vl_total_recb":"260.00","st_label_recb":"","st_formapagamento_recb":"Boleto"}]}`))
			return
		}
		if pagina == "2" {
			_, _ = w.Write([]byte(`{"status":"200","msg":"","data":[{"id_recebimento_recb":"3","dt_vencimento_recb":"09/10/2026 00:00:00","dt_liquidacao_recb":"09/01/2026 00:00:00","dt_competencia_recb":"08/15/2026 00:00:00","vl_total_recb":"240.00","st_label_recb":"","st_formapagamento_recb":"Boleto"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"200","msg":"","data":[]}`))
	})
	mux.HandleFunc("/areadocondomino/atual/impressoes/index", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("pagina") == "1" {
			_, _ = w.Write([]byte(`{"status":"200","msg":"","data":[{"id_impressao_fimp":"doc1","st_titulo_fimp":"Monthly statement","dt_criacao_fimp":"10/29/2026 16:18:54","fl_tipodocumento_fimp":"0","st_tipo_etiqueta":""}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"200","msg":"","data":[]}`))
	})
	mux.HandleFunc("/areadocondomino/atual/comunicados/index", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"200","msg":"","data":[{"id_comunicado_com":"n1","st_titulo_com":"Notice one","dt_comunicado_com":"10/29/2026 16:18:54","st_texto_com":"Body text 29/10/2026"},{"id_comunicado_com":"n2","st_titulo_com":"Notice two","dt_comunicado_com":"10/28/2026 00:00:00","st_texto_com":"Body two"}]}`))
	})
	mux.HandleFunc("/areadocondomino/atual/reservas/index", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"200","msg":"","data":[{"st_nome_are":"SALÃO DE FESTAS - DIA INTEIRO","datas":{"11/02/2026":[],"11/03/2026":[]}}]}`))
	})
	mux.HandleFunc("/areadocondomino/atual/tickets/index", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"200","msg":"","data":[]}`))
	})
	mux.HandleFunc("/areadocondomino/atual/ocorrencias/index", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"200","msg":"","data":[]}`))
	})
	mux.HandleFunc("/flaky", func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(billsPage))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &calls
}

func testClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := NewClient(Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAuthenticateSingleForm(t *testing.T) {
	srv, _ := mockServer(t)
	c := testClient(t, srv)
	if err := c.Authenticate(context.Background(), Credentials{Username: "user@example.com", Password: "secret"}); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !c.Authenticated() {
		t.Fatal("expected Authenticated true")
	}
}

func TestListBillsAndPagination(t *testing.T) {
	srv, _ := mockServer(t)
	c := testClient(t, srv)
	ctx := context.Background()
	paid, err := c.ListPaidBills(ctx, Pagination{})
	if err != nil {
		t.Fatal(err)
	}
	if len(paid) != 1 || paid[0].Status != BillStatusPaid {
		t.Fatalf("paid = %+v", paid)
	}
	all, err := c.ListPaidBills(ctx, Pagination{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("paginated paid = %d, want 2", len(all))
	}
	open, err := c.ListOpenBills(ctx, Pagination{})
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].Status != BillStatusOpen || open[0].DueDate != "2026-11-10" {
		t.Fatalf("open = %+v", open)
	}
}

func TestListDocumentsNoticesReservations(t *testing.T) {
	srv, _ := mockServer(t)
	c := testClient(t, srv)
	ctx := context.Background()
	docs, err := c.ListDocuments(ctx)
	if err != nil || len(docs) != 1 || docs[0].ID != "doc1" {
		t.Fatalf("docs = %+v, err %v", docs, err)
	}
	notices, err := c.ListNotices(ctx)
	if err != nil || len(notices) != 2 {
		t.Fatalf("notices = %+v, err %v", notices, err)
	}
	detail, err := c.NoticeDetail(ctx, "n1")
	if err != nil || detail.Title != "Notice one" || detail.Warning == "" {
		t.Fatalf("detail = %+v, err %v", detail, err)
	}
	slots, err := c.ListReservationAvailability(ctx)
	if err != nil || len(slots) != 2 || slots[0].Status != "free" {
		t.Fatalf("slots = %+v, err %v", slots, err)
	}
	reqs, err := c.ListRequests(ctx)
	if err != nil || len(reqs) != 0 {
		t.Fatalf("requests = %+v, err %v", reqs, err)
	}
	occs, err := c.ListOccurrences(ctx)
	if err != nil || len(occs) != 0 {
		t.Fatalf("occurrences = %+v, err %v", occs, err)
	}
	data, err := c.FetchDocumentPDF(ctx, "doc1")
	if err != nil || !strings.HasPrefix(string(data), "%PDF-") {
		t.Fatalf("pdf = %q, err %v", data, err)
	}
}

func TestRetryOnFlakyGET(t *testing.T) {
	srv, calls := mockServer(t)
	c := testClient(t, srv)
	_, err := c.get(context.Background(), "test", "/flaky")
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 2 {
		t.Fatalf("calls = %d, want 2", *calls)
	}
}

func TestCheckNewBills(t *testing.T) {
	prev := []Bill{{ID: "1", DueDate: "2026-10-10", AmountBRL: "250.00"}}
	cur := []Bill{{ID: "1", DueDate: "2026-10-10", AmountBRL: "250.00"}, {ID: "2", DueDate: "2026-11-10", AmountBRL: "260.00", Period: "2026-11", Status: BillStatusOpen}}
	got := CheckNewBills(cur, prev)
	if len(got) != 1 || got[0].ID != "2" {
		t.Fatalf("new = %+v", got)
	}
	if len(CheckNewBills(cur, cur)) != 0 {
		t.Fatal("expected silent empty result when nothing is new")
	}
}

func TestPreviewAndConfirmGates(t *testing.T) {
	srv, _ := mockServer(t)
	c := testClient(t, srv)
	ctx := context.Background()
	p, err := PreviewRequest("Leak", "Hallway")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateRequest(ctx, p, false); !IsCode(err, ErrCodeConfirmationRequired) {
		t.Fatalf("expected confirmation required, got %v", err)
	}
	if _, err := c.CreateRequest(ctx, p, true); !IsCode(err, ErrCodeSubmissionUndiscovered) {
		t.Fatalf("expected undiscovered endpoint, got %v", err)
	}
	rp, err := PreviewReservation("SALÃO DE FESTAS", "2026-11-02", "full day")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.PreReserveReservation(ctx, rp, false); !IsCode(err, ErrCodeConfirmationRequired) {
		t.Fatalf("expected confirmation required for reservation, got %v", err)
	}
	if _, err := c.PreReserveReservation(ctx, rp, true); !IsCode(err, ErrCodeSubmissionUndiscovered) {
		t.Fatalf("expected undiscovered endpoint for reservation, got %v", err)
	}
	if err := ValidateDestructive("p1", "doc1", "doc2", true); err == nil {
		t.Fatal("expected destructive validation to fail on mismatched target")
	}
}

func TestCredentialStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewCredentialStore(dir)
	pw := []byte("s3cret")
	if err := store.Enroll("user@example.com", pw); err != nil {
		t.Fatal(err)
	}
	if !store.Exists() {
		t.Fatal("store should exist")
	}
	creds, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if creds.Username != "user@example.com" || creds.Password != "s3cret" {
		t.Fatal("credential round trip mismatch")
	}
	creds.Clear()
	if creds.Password != "" {
		t.Fatal("Clear should empty password")
	}
}

func TestToolsCatalogAndExecute(t *testing.T) {
	tools := Tools()
	if len(tools) < 10 {
		t.Fatalf("tools = %d, want at least 10", len(tools))
	}
	for _, tool := range tools {
		if tool.Name == "" || tool.Description == "" {
			t.Fatalf("tool missing name/description: %+v", tool)
		}
	}
	srv, _ := mockServer(t)
	c := testClient(t, srv)
	out, err := c.ExecuteTool(context.Background(), "list_notices", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.([]Notice); !ok {
		t.Fatalf("execute tool type = %T", out)
	}
	if _, err := c.ExecuteTool(context.Background(), "pay_bill", nil); !IsCode(err, ErrCodeNotAvailable) {
		t.Fatalf("expected not available for pay_bill, got %v", err)
	}
	if len(UnmappedTools()) == 0 {
		t.Fatal("unmapped tools should be listed")
	}
}

func TestMarshalJSONL(t *testing.T) {
	data, err := MarshalJSONL([]any{Bill{ID: "1", AmountBRL: "250.00"}, Bill{ID: "2", AmountBRL: "260.00"}})
	if err != nil {
		t.Fatal(err)
	}
	bills, err := UnmarshalBillsJSONL(strings.NewReader(string(data)))
	if err != nil || len(bills) != 2 || bills[0].AmountBRL != "250.00" {
		t.Fatalf("jsonl = %s, bills %+v, err %v", data, bills, err)
	}
}
