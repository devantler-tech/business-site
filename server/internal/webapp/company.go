package webapp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"
)

var addressSlots = [2]string{"__BUSINESS_ADDRESS_LINE_1__", "__BUSINESS_ADDRESS_LINE_2__"}

// attribute checks the parsed DOM, independent of Astro's scoped attributes.
func attribute(node *xhtml.Node, name string) bool {
	for _, value := range node.Attr {
		if value.Key == name {
			return true
		}
	}
	return false
}

// companySlots binds each literal to a single native span on the required page.
func companySlots(data []byte) bool {
	for _, slot := range addressSlots {
		if bytes.Count(data, []byte(slot)) != 1 {
			return false
		}
	}
	doc, err := xhtml.Parse(bytes.NewReader(data))
	if err != nil {
		return false
	}
	var counts [2]int
	valid := true
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		for i, slot := range addressSlots {
			if !strings.Contains(node.Data, slot) {
				continue
			}
			parent := node.Parent
			if node.Type != xhtml.TextNode || node.Data != slot || parent == nil || parent.Data != "span" || parent.Parent == nil || parent.Parent.Data != "address" || !attribute(parent.Parent, "data-company-address") || !attribute(parent.Parent, "data-pagefind-ignore") || parent.FirstChild != node || parent.LastChild != node {
				valid = false
			}
			counts[i]++
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return valid && counts == [2]int{1, 1}
}

// HasCompanySlots refuses an unmaterialized template under the ordinary profile.
func (a *Artifact) HasCompanySlots() bool {
	for _, data := range a.files {
		for _, slot := range addressSlots {
			if bytes.Contains(data, []byte(slot)) {
				return true
			}
		}
	}
	return false
}

// WithCompanyDisclosure copies only the two bound company pages. It never edits
// signed template bytes, mutable files, other routes or the input artifact.
func (a *Artifact) WithCompanyDisclosure(input string) (*Artifact, error) {
	if !utf8.ValidString(input) {
		return nil, ErrArtifact
	}
	input = strings.ReplaceAll(input, "\r\n", "\n")
	for _, r := range input {
		if r != '\n' && (unicode.IsControl(r) || unicode.Is(unicode.Cf, r)) {
			return nil, ErrArtifact
		}
	}
	lines := strings.Split(strings.TrimSpace(input), "\n")
	if len(lines) != 2 {
		return nil, ErrArtifact
	}
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
		if lines[i] == "" || len(utf16.Encode([]rune(lines[i]))) > 160 {
			return nil, ErrArtifact
		}
		for _, slot := range addressSlots {
			if strings.Contains(lines[i], slot) {
				return nil, ErrArtifact
			}
		}
	}
	result := &Artifact{files: make(map[string][]byte, len(a.files))}
	names := make([]string, 0, len(a.files))
	var total int64
	for name, data := range a.files {
		company := name == "company/index.html" || name == "da/company/index.html"
		if company {
			if !companySlots(data) {
				return nil, ErrArtifact
			}
			data = bytes.ReplaceAll(data, []byte(addressSlots[0]), []byte(html.EscapeString(lines[0])))
			data = bytes.ReplaceAll(data, []byte(addressSlots[1]), []byte(html.EscapeString(lines[1])))
		} else {
			for _, slot := range addressSlots {
				if bytes.Contains(data, []byte(slot)) {
					return nil, ErrArtifact
				}
			}
		}
		total += int64(len(data))
		if total > maxArtifactBytes {
			return nil, ErrArtifact
		}
		result.files[name] = data
		names = append(names, name)
	}
	if len(result.files["company/index.html"]) == 0 || len(result.files["da/company/index.html"]) == 0 {
		return nil, ErrArtifact
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		data := result.files[name]
		_, _ = fmt.Fprintf(hash, "%s\x00%d\x00", name, len(data))
		hash.Write(data)
	}
	result.digest = hex.EncodeToString(hash.Sum(nil))
	return result, nil
}
