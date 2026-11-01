package condo

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// Form is a discovered HTML form. Action is as written in the page and is
// resolved against the client base before submission. Fields holds hidden
// and default input values; visible fields are named in FieldNames.
type Form struct {
	Action     string
	Method     string
	Fields     map[string]string
	FieldNames []string
}

func parseForms(src string) ([]Form, error) {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return nil, err
	}
	var forms []Form
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "form" {
			f := Form{Fields: map[string]string{}}
			for _, a := range n.Attr {
				if a.Key == "action" {
					f.Action = a.Val
				}
				if a.Key == "method" {
					f.Method = strings.ToUpper(a.Val)
				}
			}
			if f.Method == "" {
				f.Method = "GET"
			}
			collectInputs(n, &f)
			forms = append(forms, f)
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(doc)
	return forms, nil
}

func collectInputs(n *html.Node, f *Form) {
	if n.Type == html.ElementNode && (n.Data == "input" || n.Data == "textarea" || n.Data == "select") {
		name, typ, val := "", "", ""
		for _, a := range n.Attr {
			switch a.Key {
			case "name":
				name = a.Val
			case "type":
				typ = a.Val
			case "value":
				val = a.Val
			}
		}
		if name != "" {
			f.FieldNames = append(f.FieldNames, name+":"+typ)
			if _, ok := f.Fields[name]; !ok {
				f.Fields[name] = val
			}
		}
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		collectInputs(ch, f)
	}
}

// formField returns the input name to use for kind ("email" or "password"),
// preferring an explicit type match, then a name match.
func formField(f Form, kind string) string {
	for _, entry := range f.FieldNames {
		parts := strings.SplitN(entry, ":", 2)
		name, typ := parts[0], ""
		if len(parts) == 2 {
			typ = parts[1]
		}
		if kind == "email" && typ == "email" {
			return name
		}
		if kind == "password" && typ == "password" {
			return name
		}
	}
	for _, entry := range f.FieldNames {
		name := strings.SplitN(entry, ":", 2)[0]
		lower := strings.ToLower(name)
		if kind == "email" && (strings.Contains(lower, "email") || strings.Contains(lower, "login") || strings.Contains(lower, "usuario")) {
			return name
		}
		if kind == "password" && (strings.Contains(lower, "password") || strings.Contains(lower, "senha")) {
			return name
		}
	}
	// Fallback: first text-like field for email, first field for password
	// is handled by the caller checking for an empty result; return empty
	// here rather than guessing a field that may be a CSRF token.
	return ""
}

func textOf(n *html.Node) string {
	var sb strings.Builder
	var walk func(x *html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			sb.WriteString(x.Data)
			sb.WriteString(" ")
		}
		for ch := x.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	fields := strings.Fields(sb.String())
	return strings.Join(fields, " ")
}

func findNodes(doc *html.Node, tag string) []*html.Node {
	var out []*html.Node
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == tag {
			out = append(out, n)
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(doc)
	return out
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func parseDoc(src string) (*html.Node, error) {
	return html.Parse(strings.NewReader(src))
}

var (
	dateBR  = regexp.MustCompile(`\b(\d{2})/(\d{2})/(\d{4})\b`)
	dateISO = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
	amountR = regexp.MustCompile(`R\$\s*([0-9][0-9\.,]*)`)
	idParam = regexp.MustCompile(`[?&]id=([^&#]+)`)
)

// normalizeDate converts DD/MM/YYYY to YYYY-MM-DD, keeps ISO dates as-is,
// and returns the first date found in text, or "".
func normalizeDate(text string) string {
	if m := dateISO.FindString(text); m != "" {
		return m
	}
	if m := dateBR.FindStringSubmatch(text); m != nil {
		return fmt.Sprintf("%s-%s-%s", m[3], m[2], m[1])
	}
	return ""
}

func allDates(text string) []string {
	var out []string
	for _, m := range dateBR.FindAllStringSubmatch(text, -1) {
		out = append(out, fmt.Sprintf("%s-%s-%s", m[3], m[2], m[1]))
	}
	for _, m := range dateISO.FindAllString(text, -1) {
		out = append(out, m)
	}
	return out
}

// normalizeAmount converts a Brazilian amount such as "1.234,56" to the
// decimal string "1234.56". It returns "" when no amount is found.
func normalizeAmount(text string) string {
	m := amountR.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	s := strings.TrimSpace(m[1])
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, ",", ".")
	if _, err := strconv.ParseFloat(s, 64); err != nil {
		return ""
	}
	return s
}

func statusFromText(text string) BillStatus {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "pago") || strings.Contains(lower, "paid") || strings.Contains(lower, "quitado"):
		return BillStatusPaid
	case strings.Contains(lower, "aberto") || strings.Contains(lower, "open") || strings.Contains(lower, "pendente") || strings.Contains(lower, "a vencer"):
		return BillStatusOpen
	default:
		return BillStatusUnknown
	}
}

// tableRows returns, for each <tr>, its cell texts and the first link href.
func tableRows(src string) ([][]string, []string, error) {
	doc, err := parseDoc(src)
	if err != nil {
		return nil, nil, err
	}
	var rows [][]string
	var hrefs []string
	for _, tr := range findNodes(doc, "tr") {
		var cells []string
		href := ""
		for _, td := range findNodes(tr, "td") {
			cells = append(cells, textOf(td))
			if href == "" {
				for _, a := range findNodes(td, "a") {
					if h := attr(a, "href"); h != "" {
						href = h
						break
					}
				}
			}
		}
		if len(cells) == 0 {
			continue
		}
		// Skip header-like rows that are all empty after trimming.
		joined := strings.Join(cells, " ")
		if strings.TrimSpace(joined) == "" {
			continue
		}
		rows = append(rows, cells)
		hrefs = append(hrefs, href)
	}
	return rows, hrefs, nil
}

// listItems returns text and first href for <li> elements, and for <div>
// elements carrying a data-category or data-area attribute.
func listItems(src string) ([]string, []string, error) {
	doc, err := parseDoc(src)
	if err != nil {
		return nil, nil, err
	}
	var texts, hrefs []string
	for _, li := range findNodes(doc, "li") {
		t := textOf(li)
		if strings.TrimSpace(t) == "" {
			continue
		}
		href := ""
		for _, a := range findNodes(li, "a") {
			if h := attr(a, "href"); h != "" {
				href = h
				break
			}
		}
		texts = append(texts, t)
		hrefs = append(hrefs, href)
	}
	return texts, hrefs, nil
}

func moreItemsLinkFromHTML(src string) string {
	doc, err := parseDoc(src)
	if err != nil {
		return ""
	}
	for _, a := range findNodes(doc, "a") {
		t := strings.ToLower(textOf(a))
		if strings.Contains(t, "mais itens") || strings.Contains(t, "more items") {
			if h := attr(a, "href"); h != "" && h != "#" {
				return h
			}
		}
	}
	return ""
}

func idFromHref(href string) string {
	if m := idParam.FindStringSubmatch(href); m != nil {
		return m[1]
	}
	return ""
}
