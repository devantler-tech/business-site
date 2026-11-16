package webapp

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"
)

// OrderTerms copies the complete EN/DA terms from the admitted serving snapshot,
// filling the model form with its already-confined trader disclosure. It never
// fetches a mutable website or exposes private partial content on failure (#654).
func (a *Artifact) OrderTerms() ([2]string, error) {
	var result [2]string
	if a == nil {
		return result, ErrArtifact
	}
	for i, prefix := range []string{"", "da/"} {
		terms, err := markedDocument(a.files[prefix+"terms/index.html"], "main", "data-legal-page", "terms")
		if err != nil {
			return [2]string{}, ErrArtifact
		}
		address, err := markedDocument(a.files[prefix+"company/index.html"], "address", "data-company-address", "")
		if err != nil {
			return [2]string{}, ErrArtifact
		}
		text, ok := documentText(terms)
		postal, addressOK := documentText(address)
		placeholder := []string{"[postal address on the company information page]", "[postadressen under virksomhedsoplysninger]"}[i]
		if !ok || !addressOK || strings.Contains(postal, "__BUSINESS_ADDRESS_") || strings.Count(text, placeholder) != 1 {
			return [2]string{}, ErrArtifact
		}
		postal = strings.Join(strings.Split(postal, "\n\n"), ", ")
		result[i] = strings.Replace(text, placeholder, postal, 1)
	}
	return result, nil
}

// markedDocument requires one exact semantic root, rather than including site
// navigation or silently accepting a missing or duplicate document.
func markedDocument(data []byte, tag, key, value string) (*xhtml.Node, error) {
	if len(data) == 0 || len(data) > 1<<20 || !utf8.Valid(data) {
		return nil, ErrArtifact
	}
	doc, err := xhtml.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, ErrArtifact
	}
	var found *xhtml.Node
	count := 0
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Data == tag {
			for _, attr := range n.Attr {
				if attr.Key == key && attr.Val == value {
					count++
					found = n
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	if count != 1 {
		return nil, ErrArtifact
	}
	return found, nil
}

// documentText retains every paragraph/list item and link destination in plain
// text. Active content and control characters refuse the whole document.
func documentText(root *xhtml.Node) (string, bool) {
	var out strings.Builder
	valid := true
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			for _, r := range n.Data {
				if (unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t') || unicode.Is(unicode.Cf, r) {
					valid = false
				}
			}
			out.WriteString(" " + strings.Join(strings.Fields(n.Data), " ") + " ")
			return
		}
		block := false
		if n.Type == xhtml.ElementNode {
			switch n.Data {
			case "script", "style", "iframe", "object", "embed", "form", "input", "button":
				valid = false
				return
			case "h1", "h2", "h3", "p", "li", "br", "address":
				block = true
			case "span":
				block = n.Parent != nil && n.Parent.Data == "address"
			}
		}
		if block {
			out.WriteByte('\n')
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if n.Type == xhtml.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					out.WriteString(" (" + attr.Val + ") ")
				}
			}
		}
		if block {
			out.WriteByte('\n')
		}
	}
	walk(root)
	var lines []string
	for _, line := range strings.Split(out.String(), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	text := strings.Join(lines, "\n\n")
	return text, valid && text != "" && len(text) <= 65536
}
