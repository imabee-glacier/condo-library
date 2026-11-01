package condo

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var periodRe = regexp.MustCompile(`\b(\d{4})-(\d{2})\b`)
var periodBR = regexp.MustCompile(`\b(\d{2})/(\d{4})\b`)

func periodFromText(text string) string {
	if m := periodRe.FindString(text); m != "" {
		return m
	}
	if m := periodBR.FindStringSubmatch(text); m != nil {
		return m[2] + "-" + m[1]
	}
	return ""
}

func parseBills(src string) ([]Bill, error) {
	rows, hrefs, err := tableRows(src)
	if err != nil {
		return nil, err
	}
	var out []Bill
	for i, cells := range rows {
		joined := strings.Join(cells, " ")
		b := Bill{
			ID:        idFromHref(hrefs[i]),
			Status:    statusFromText(joined),
			AmountBRL: normalizeAmount(joined),
			Label:     joined,
			Period:    periodFromText(joined),
		}
		dates := allDates(joined)
		if len(dates) > 0 {
			b.DueDate = dates[0]
		}
		if len(dates) > 1 {
			b.PaidDate = dates[1]
		}
		out = append(out, b)
	}
	if len(out) == 0 {
		// Fallback: list items.
		texts, hrefs2, err := listItems(src)
		if err != nil {
			return nil, err
		}
		for i, t := range texts {
			dates := allDates(t)
			b := Bill{ID: idFromHref(hrefs2[i]), Status: statusFromText(t), AmountBRL: normalizeAmount(t), Label: t, Period: periodFromText(t)}
			if len(dates) > 0 {
				b.DueDate = dates[0]
			}
			if len(dates) > 1 {
				b.PaidDate = dates[1]
			}
			out = append(out, b)
		}
	}
	return out, nil
}

func parseDocuments(src string) ([]Document, error) {
	doc, err := parseDoc(src)
	if err != nil {
		return nil, err
	}
	var out []Document
	// Anchor-first: every download link is a document.
	for _, a := range findNodes(doc, "a") {
		href := attr(a, "href")
		if !strings.Contains(href, "/documentos/download") {
			continue
		}
		title := textOf(a)
		if strings.TrimSpace(title) == "" {
			continue
		}
		d := Document{ID: idFromHref(href), Title: title}
		// Walk up to a parent row/div for category and date.
		for p := a.Parent; p != nil && p.Type == html.ElementNode; p = p.Parent {
			txt := textOf(p)
			d.SentDate = normalizeDate(txt)
			if cat := attr(p, "data-category"); cat != "" {
				d.Category = cat
			}
			if d.SentDate != "" {
				break
			}
			if p.Data == "tr" || p.Data == "li" {
				break
			}
		}
		out = append(out, d)
	}
	if len(out) > 0 {
		return out, nil
	}
	rows, hrefs, err := tableRows(src)
	if err != nil {
		return nil, err
	}
	for i, cells := range rows {
		joined := strings.Join(cells, " ")
		cat := ""
		if len(cells) > 0 {
			cat = cells[0]
		}
		out = append(out, Document{ID: idFromHref(hrefs[i]), Title: joined, Category: cat, SentDate: normalizeDate(joined)})
	}
	return out, nil
}

func parseNotices(src string) ([]Notice, error) {
	rows, hrefs, err := tableRows(src)
	if err != nil {
		return nil, err
	}
	var out []Notice
	for i, cells := range rows {
		joined := strings.Join(cells, " ")
		out = append(out, Notice{ID: idFromHref(hrefs[i]), Title: joined, PublishedDate: normalizeDate(joined)})
	}
	if len(out) > 0 {
		return out, nil
	}
	texts, hrefs2, err := listItems(src)
	if err != nil {
		return nil, err
	}
	for i, t := range texts {
		out = append(out, Notice{ID: idFromHref(hrefs2[i]), Title: t, PublishedDate: normalizeDate(t)})
	}
	return out, nil
}

func parseNoticeDetail(src, id string) (NoticeDetail, error) {
	doc, err := parseDoc(src)
	if err != nil {
		return NoticeDetail{}, err
	}
	title := ""
	for _, tag := range []string{"h1", "h2", "h3"} {
		nodes := findNodes(doc, tag)
		if len(nodes) > 0 {
			title = textOf(nodes[0])
			break
		}
	}
	body := ""
	for _, tag := range []string{"article", "main"} {
		nodes := findNodes(doc, tag)
		if len(nodes) > 0 {
			body = textOf(nodes[0])
			break
		}
	}
	if body == "" {
		body = textOf(doc)
	}
	return NoticeDetail{Notice: Notice{ID: id, Title: title, PublishedDate: normalizeDate(body)}, Body: body}, nil
}

func parseReservationSlots(src string) ([]ReservationSlot, error) {
	doc, err := parseDoc(src)
	if err != nil {
		return nil, err
	}
	var out []ReservationSlot
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if area := attr(n, "data-area"); area != "" {
				status := attr(n, "data-status")
				if status == "" {
					status = "free"
				}
				out = append(out, ReservationSlot{Area: area, Date: normalizeDate(attr(n, "data-date") + " " + textOf(n)), Period: attr(n, "data-period"), Status: status})
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(doc)
	if len(out) > 0 {
		return out, nil
	}
	// Fallback: table rows with area-like text.
	rows, _, err := tableRows(src)
	if err != nil {
		return nil, err
	}
	for _, cells := range rows {
		joined := strings.Join(cells, " ")
		if joined == "" {
			continue
		}
		status := "free"
		lower := strings.ToLower(joined)
		if strings.Contains(lower, "indispon") || strings.Contains(lower, "disabled") || strings.Contains(lower, "ocupado") {
			status = "disabled"
		}
		out = append(out, ReservationSlot{Area: cells[0], Date: normalizeDate(joined), Status: status})
	}
	return out, nil
}

func parseRequests(src string) ([]Request, error) {
	rows, hrefs, err := tableRows(src)
	if err != nil {
		return nil, err
	}
	var out []Request
	for i, cells := range rows {
		joined := strings.Join(cells, " ")
		title := joined
		if len(cells) > 0 {
			title = cells[0]
		}
		out = append(out, Request{ID: idFromHref(hrefs[i]), Title: title, Subject: joined})
	}
	return out, nil
}

func parseOccurrences(src string) ([]Occurrence, error) {
	rows, hrefs, err := tableRows(src)
	if err != nil {
		return nil, err
	}
	var out []Occurrence
	for i, cells := range rows {
		joined := strings.Join(cells, " ")
		title := joined
		if len(cells) > 0 {
			title = cells[0]
		}
		out = append(out, Occurrence{ID: idFromHref(hrefs[i]), Title: title})
	}
	return out, nil
}
