package engine

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// # HTML sanitization

// allowedElements is the Matrix-spec-compatible set of permitted HTML elements.
// https://spec.matrix.org/v1.16/client-server-api/#mroommessage-msgtypes
var allowedElements = map[string]bool{
	"font": true, "del": true, "strike": true, "s": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"blockquote": true, "p": true, "a": true,
	"ul": true, "ol": true, "li": true,
	"sup": true, "sub": true, "b": true, "i": true, "u": true,
	"strong": true, "em": true,
	"code": true, "hr": true, "br": true, "div": true,
	"table": true, "thead": true, "tbody": true,
	"tr": true, "th": true, "td": true, "caption": true,
	"pre": true, "span": true, "img": true,
	"details": true, "summary": true,
}

// allowedAttrs is the per-element attribute allowlist (excluding data-mx-* which
// are always permitted on any element).
var allowedAttrs = map[string]map[string]bool{
	"font": {"color": true, "face": true, "data-mx-bg-color": true, "data-mx-color": true},
	"span": {"data-mx-bg-color": true, "data-mx-color": true, "data-mx-spoiler": true},
	"a":    {"name": true, "target": true, "href": true},
	"img":  {"width": true, "height": true, "alt": true, "title": true, "src": true},
	"ol":   {"start": true},
	"li":   {"value": true},
	"code": {"class": true},
	"td":   {"colspan": true, "rowspan": true},
	"th":   {"colspan": true, "rowspan": true},
}

// voidElements are self-closing HTML elements that must not have an end tag.
var voidElements = map[string]bool{
	"br": true, "hr": true, "img": true,
}

// SanitizeHTML strips disallowed elements and attributes from an HTML string
// intended for use as formatted_body in a Matrix m.room.message event.
//
// Disallowed elements are replaced by their child content (stripped, not dropped).
// Disallowed attributes are silently removed.
// Dangerous URL schemes (e.g. javascript:) are removed from href and src.
func SanitizeHTML(input string) string {
	nodes, err := html.ParseFragment(strings.NewReader(input), &html.Node{
		Type:     html.ElementNode,
		Data:     "body",
		DataAtom: atom.Body,
	})
	if err != nil || len(nodes) == 0 {
		return html.EscapeString(input)
	}
	var sb strings.Builder
	for _, n := range nodes {
		sanitizeNode(&sb, n)
	}
	return sb.String()
}

func sanitizeNode(sb *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		sb.WriteString(html.EscapeString(n.Data))

	case html.ElementNode:
		tag := strings.ToLower(n.Data)
		if allowedElements[tag] {
			sb.WriteByte('<')
			sb.WriteString(tag)
			writeAllowedAttrs(sb, tag, n.Attr)
			sb.WriteByte('>')
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				sanitizeNode(sb, c)
			}
			if !voidElements[tag] {
				sb.WriteString("</")
				sb.WriteString(tag)
				sb.WriteByte('>')
			}
		} else {
			// Strip the disallowed element but keep its children.
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				sanitizeNode(sb, c)
			}
		}

	default:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			sanitizeNode(sb, c)
		}
	}
}

func writeAllowedAttrs(sb *strings.Builder, tag string, attrs []html.Attribute) {
	permitted := allowedAttrs[tag]
	for _, a := range attrs {
		key := strings.ToLower(a.Key)
		// data-mx-* custom Matrix attributes are allowed on any element.
		if strings.HasPrefix(key, "data-mx-") {
			writeAttr(sb, key, a.Val)
			continue
		}
		if !permitted[key] {
			continue
		}
		val := a.Val
		if key == "href" || key == "src" {
			if !isSafeURL(val) {
				continue
			}
		}
		writeAttr(sb, key, val)
	}
}

func writeAttr(sb *strings.Builder, key, val string) {
	sb.WriteByte(' ')
	sb.WriteString(html.EscapeString(key))
	sb.WriteString(`="`)
	sb.WriteString(html.EscapeString(val))
	sb.WriteByte('"')
}

// isSafeURL returns true for URL schemes that are safe to include in Matrix HTML.
func isSafeURL(u string) bool {
	lower := strings.ToLower(strings.TrimSpace(u))
	return strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "matrix:") ||
		strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(lower, "mxc://") ||
		strings.HasPrefix(lower, "#")
}
