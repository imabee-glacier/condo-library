package condo

// Version is the SDK version, following semantic versioning.
const Version = "0.1.0"

// DefaultBaseURL is the portal base for Dinâmica Condomínios SC.
// It is a host and path only; it carries no credential or token.
const DefaultBaseURL = "https://dinamicacondominiossc.superlogica.net/clients/areadocondomino"

// BillStatus values are normalised from the portal's Portuguese labels.
// "unknown" means the row did not state a recognisable status; it does not
// mean unpaid or paid.
type BillStatus string

const (
	BillStatusOpen    BillStatus = "open"
	BillStatusPaid    BillStatus = "paid"
	BillStatusUnknown BillStatus = "unknown"
)

// Bill is one row from the Boletos list. AmountBRL is a decimal string
// ("123.45") when the row reports one, and empty when it does not. Dates
// are ISO YYYY-MM-DD when they can be normalised, otherwise the reported
// text is kept as-is, and empty when absent.
type Bill struct {
	ID        string     `json:"id,omitempty"`
	Status    BillStatus `json:"status"`
	Period    string     `json:"period,omitempty"`
	DueDate   string     `json:"due_date,omitempty"`
	PaidDate  string     `json:"paid_date,omitempty"`
	AmountBRL string     `json:"amount_brl,omitempty"`
	Label     string     `json:"label,omitempty"`
}

// Document is one item from the Documentos list. Content is never included:
// use FetchDocumentPDF only when the official PDF itself is needed.
type Document struct {
	ID       string `json:"id,omitempty"`
	Title    string `json:"title"`
	Category string `json:"category,omitempty"`
	SentDate string `json:"sent_date,omitempty"`
}

// Notice is one row from the Comunicados list. Sender is a pointer so that
// an unverified sender is nil (unknown), not an empty-string claim.
type Notice struct {
	ID            string  `json:"id,omitempty"`
	Title         string  `json:"title"`
	PublishedDate string  `json:"published_date,omitempty"`
	Sender        *string `json:"sender,omitempty"`
}

// NoticeDetail is the full notice page. Opening a notice may change its
// read state on the portal; callers should only fetch a detail when they
// intend that effect.
type NoticeDetail struct {
	Notice
	Body    string `json:"body,omitempty"`
	Warning string `json:"warning"`
}

// ReservationSlot is one cell of the Reservas availability grid.
type ReservationSlot struct {
	Area   string `json:"area"`
	Date   string `json:"date,omitempty"`
	Period string `json:"period,omitempty"`
	Status string `json:"status"` // "free", "disabled", "pre_reservable", or as reported
}

// Request is one row from Solicitações. Empty results are an empty slice,
// not an error.
type Request struct {
	ID      string `json:"id,omitempty"`
	Title   string `json:"title"`
	Subject string `json:"subject,omitempty"`
	Status  string `json:"status,omitempty"`
}

// Occurrence is one row from Ocorrências.
type Occurrence struct {
	ID     string `json:"id,omitempty"`
	Title  string `json:"title"`
	Status string `json:"status,omitempty"`
}

// Pagination controls list retrieval. All=false returns one page.
// All=true follows "Mais itens" / "More items" links up to MaxPages.
type Pagination struct {
	All      bool `json:"all"`
	MaxPages int  `json:"max_pages,omitempty"`
}

func (p Pagination) maxPages() int {
	if p.MaxPages > 0 {
		return p.MaxPages
	}
	return 20
}

// Credentials are held in memory only. The SDK never logs them, never
// writes them to disk, and never returns them in an error. Call Clear to
// release them when a client is no longer needed.
type Credentials struct {
	Username string
	Password string
}

// Clear zeroes the stored credential strings in memory.
func (c *Credentials) Clear() {
	if c == nil {
		return
	}
	c.Username = ""
	c.Password = ""
}
