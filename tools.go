package condo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ToolAnnotation describes an operation's safety properties for an AI
// agent, in the style of professional agent-tool catalogs (for example
// MCP tool annotations). It is metadata for the caller, not a substitute
// for the preview+confirm gate in actions.go.
type ToolAnnotation struct {
	ReadOnly     bool `json:"read_only"`
	Destructive  bool `json:"destructive"`
	Idempotent   bool `json:"idempotent"`
	MovesMoney   bool `json:"moves_money"`
	ChangesState bool `json:"changes_state"`
}

// Tool is one agent-facing operation: name, description, JSON schemas,
// and safety annotations. Schemas are JSON Schema objects (type object).
type Tool struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"input_schema"`
	OutputSchema map[string]any `json:"output_schema"`
	Annotations  ToolAnnotation `json:"annotations"`
}

func objSchema(props map[string]any, required []string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

// Tools returns the full agent-tool catalog. Unmapped features are
// deliberately absent, rather than listed with guessed endpoints; see
// UnmappedTools for what is explicitly unavailable.
func Tools() []Tool {
	readOnly := ToolAnnotation{ReadOnly: true, Idempotent: true}
	consequential := ToolAnnotation{ReadOnly: false, Idempotent: false, ChangesState: true}
	return []Tool{
		{Name: "list_open_bills", Description: "List open Boletos (bills) for the resident unit. Returns an empty list when none are open.", InputSchema: objSchema(map[string]any{"all": boolProp("Follow More-items pagination.")}, nil), OutputSchema: objSchema(map[string]any{"bills": map[string]any{"type": "array"}}, nil), Annotations: readOnly},
		{Name: "list_paid_bills", Description: "List paid Boletos for the resident unit.", InputSchema: objSchema(map[string]any{"all": boolProp("Follow More-items pagination.")}, nil), OutputSchema: objSchema(map[string]any{"bills": map[string]any{"type": "array"}}, nil), Annotations: readOnly},
		{Name: "list_documents", Description: "List Documentos metadata (title, category, sent date). Content is not fetched.", InputSchema: objSchema(nil, nil), OutputSchema: objSchema(map[string]any{"documents": map[string]any{"type": "array"}}, nil), Annotations: readOnly},
		{Name: "list_notices", Description: "List Comunicados (notices) rows. Sender is unknown unless the row states it.", InputSchema: objSchema(nil, nil), OutputSchema: objSchema(map[string]any{"notices": map[string]any{"type": "array"}}, nil), Annotations: readOnly},
		{Name: "notice_detail", Description: "Fetch one notice in full. Warning: opening a notice may change its read state.", InputSchema: objSchema(map[string]any{"id": strProp("Notice id.")}, []string{"id"}), OutputSchema: objSchema(map[string]any{"notice": map[string]any{"type": "object"}}, nil), Annotations: readOnly},
		{Name: "list_reservation_availability", Description: "List Reservas availability grid slots.", InputSchema: objSchema(nil, nil), OutputSchema: objSchema(map[string]any{"slots": map[string]any{"type": "array"}}, nil), Annotations: readOnly},
		{Name: "list_requests", Description: "List Solicitações (requests), or an empty list.", InputSchema: objSchema(nil, nil), OutputSchema: objSchema(map[string]any{"requests": map[string]any{"type": "array"}}, nil), Annotations: readOnly},
		{Name: "list_occurrences", Description: "List Ocorrências (occurrences), or an empty list.", InputSchema: objSchema(nil, nil), OutputSchema: objSchema(map[string]any{"occurrences": map[string]any{"type": "array"}}, nil), Annotations: readOnly},
		{Name: "fetch_document_pdf", Description: "Fetch the official PDF for a document, only on explicit request. Returns metadata (size, SHA-256), not PDF bytes in JSON.", InputSchema: objSchema(map[string]any{"id": strProp("Document id.")}, []string{"id"}), OutputSchema: objSchema(map[string]any{"document_id": strProp("Document id."), "size_bytes": map[string]any{"type": "integer"}, "sha256": strProp("SHA-256 of the PDF.")}, nil), Annotations: readOnly},
		{Name: "check_new_bills", Description: "Compare current open bills against a previous list and return only new bills. Silent (empty list) when nothing is new.", InputSchema: objSchema(map[string]any{"previous": map[string]any{"type": "array", "description": "Previously seen bills."}}, nil), OutputSchema: objSchema(map[string]any{"new_bills": map[string]any{"type": "array"}}, nil), Annotations: readOnly},
		{Name: "preview_request", Description: "Preview a new Solicitação without sending it. Always call this before create_request.", InputSchema: objSchema(map[string]any{"title": strProp("Request title."), "subject": strProp("Request subject.")}, []string{"title", "subject"}), OutputSchema: objSchema(map[string]any{"preview": map[string]any{"type": "object"}}, nil), Annotations: ToolAnnotation{ReadOnly: true, Idempotent: true}},
		{Name: "create_request", Description: "Create a Solicitação from a preview, only with confirm=true matching that preview. The submission endpoint is currently undiscovered, so a confirmed call returns an undiscovered-endpoint error and sends nothing.", InputSchema: objSchema(map[string]any{"preview": map[string]any{"type": "object"}, "confirm": boolProp("Must be true, with the preview shown to the user.")}, []string{"preview", "confirm"}), OutputSchema: objSchema(map[string]any{"preview": map[string]any{"type": "object"}}, nil), Annotations: consequential},
		{Name: "preview_reservation", Description: "Preview a reservation without making it. Always call this before pre_reserve_reservation.", InputSchema: objSchema(map[string]any{"area": strProp("Area, e.g. party hall or moving block."), "date": strProp("Date, YYYY-MM-DD."), "period": strProp("Period, e.g. morning, afternoon, or full day.")}, []string{"area", "date", "period"}), OutputSchema: objSchema(map[string]any{"preview": map[string]any{"type": "object"}}, nil), Annotations: ToolAnnotation{ReadOnly: true, Idempotent: true}},
		{Name: "pre_reserve_reservation", Description: "Pre-reserve a reservation from a preview, only with confirm=true matching that preview. The submission endpoint is currently undiscovered, so a confirmed call returns an undiscovered-endpoint error and makes no reservation.", InputSchema: objSchema(map[string]any{"preview": map[string]any{"type": "object"}, "confirm": boolProp("Must be true, with the preview shown to the user.")}, []string{"preview", "confirm"}), OutputSchema: objSchema(map[string]any{"preview": map[string]any{"type": "object"}}, nil), Annotations: consequential},
	}
}

// UnmappedTools lists operations that are explicitly NOT available, so an
// agent does not invent them: visitors, assembly voting, profile editing,
// payment-method change, and paying a charge.
func UnmappedTools() []string {
	return []string{
		"register_visitor", "release_visitor",
		"assembly_vote",
		"update_profile", "change_payment_method", "pay_bill",
	}
}

// ExecuteTool dispatches a tool by name from raw JSON input. It is a
// convenience for CLI and agent harnesses; library callers should prefer
// the typed Client methods.
func (c *Client) ExecuteTool(ctx context.Context, name string, input json.RawMessage) (any, error) {
	switch name {
	case "list_open_bills":
		var in struct {
			All bool `json:"all"`
		}
		_ = json.Unmarshal(input, &in)
		return c.ListOpenBills(ctx, Pagination{All: in.All})
	case "list_paid_bills":
		var in struct {
			All bool `json:"all"`
		}
		_ = json.Unmarshal(input, &in)
		return c.ListPaidBills(ctx, Pagination{All: in.All})
	case "list_documents":
		return c.ListDocuments(ctx)
	case "list_notices":
		return c.ListNotices(ctx)
	case "notice_detail":
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, wrapErr(ErrCodeValidation, "ExecuteTool", err)
		}
		return c.NoticeDetail(ctx, in.ID)
	case "list_reservation_availability":
		return c.ListReservationAvailability(ctx)
	case "list_requests":
		return c.ListRequests(ctx)
	case "list_occurrences":
		return c.ListOccurrences(ctx)
	case "fetch_document_pdf":
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, wrapErr(ErrCodeValidation, "ExecuteTool", err)
		}
		return c.FetchDocumentPDFInfo(ctx, in.ID)
	case "check_new_bills":
		var in struct {
			Previous []Bill `json:"previous"`
			Current  []Bill `json:"current"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, wrapErr(ErrCodeValidation, "ExecuteTool", err)
		}
		if in.Current == nil {
			current, err := c.ListOpenBills(ctx, Pagination{})
			if err != nil {
				return nil, err
			}
			in.Current = current
		}
		return CheckNewBills(in.Current, in.Previous), nil
	case "preview_request":
		var in struct {
			Title   string `json:"title"`
			Subject string `json:"subject"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, wrapErr(ErrCodeValidation, "ExecuteTool", err)
		}
		return PreviewRequest(in.Title, in.Subject)
	case "create_request":
		var in struct {
			Preview RequestPreview `json:"preview"`
			Confirm bool           `json:"confirm"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, wrapErr(ErrCodeValidation, "ExecuteTool", err)
		}
		return c.CreateRequest(ctx, in.Preview, in.Confirm)
	case "preview_reservation":
		var in struct {
			Area   string `json:"area"`
			Date   string `json:"date"`
			Period string `json:"period"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, wrapErr(ErrCodeValidation, "ExecuteTool", err)
		}
		return PreviewReservation(in.Area, in.Date, in.Period)
	case "pre_reserve_reservation":
		var in struct {
			Preview ReservationPreview `json:"preview"`
			Confirm bool               `json:"confirm"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, wrapErr(ErrCodeValidation, "ExecuteTool", err)
		}
		return c.PreReserveReservation(ctx, in.Preview, in.Confirm)
	default:
		if strings.TrimSpace(name) == "" {
			return nil, wrapErr(ErrCodeValidation, "ExecuteTool", fmt.Errorf("tool name is required"))
		}
		return nil, wrapErr(ErrCodeNotAvailable, "ExecuteTool", fmt.Errorf("tool %s is not available", name))
	}
}
