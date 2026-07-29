package events

import (
	"bytes"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var dropTags = map[string]struct{}{
	"style": {},
	"link":  {},
	"meta":  {},
	"xml":   {},
}

// sanitizeEventHTML cleans CMS HTML for article.event_content.description:
//   - remove style attributes on any element (name match is case-insensitive)
//   - unwrap <font> (keep children)
//   - delete <style>/<link>/<meta>/<xml> including their contents
//   - rename <h1> to <h2>, preserving attributes and content
func sanitizeEventHTML(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}

	nodes, err := html.ParseFragment(strings.NewReader(raw), &html.Node{
		Type:     html.ElementNode,
		Data:     "div",
		DataAtom: atom.Div,
	})
	if err != nil {
		// Parse errors are rare but can happen on exported content.
		// Even if we can't build a DOM, still do best-effort removal of <style>/<xml>.
		return removeStyleAndXMLBlocks(raw)
	}

	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, n := range nodes {
		root.AppendChild(n)
	}
	sanitizeNode(root)

	var buf bytes.Buffer
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&buf, c); err != nil {
			return raw
		}
	}
	out := buf.String()
	// Parser 不会解析 HTML 注释内部内容；Word 的條件註解可能把 <style>/<xml> 包在 <!-- ... --> 內，
	// 因此此处再做一次“字符串级”删块，确保注释里的字样也被清掉。
	out = removeStyleAndXMLBlocks(out)
	return out
}

func sanitizeNode(n *html.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.ElementNode {
			name := strings.ToLower(c.Data)
			if _, drop := dropTags[name]; drop {
				n.RemoveChild(c)
				c = next
				continue
			}
			if name == "font" {
				unwrapElement(c)
				c = next
				continue
			}
			if name == "h1" {
				c.Data = "h2"
				c.DataAtom = atom.H2
			}
			stripStyleAttr(c)
			sanitizeNode(c)
		}
		c = next
	}
}

func stripStyleAttr(n *html.Node) {
	if len(n.Attr) == 0 {
		return
	}
	kept := n.Attr[:0]
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, "style") {
			continue
		}
		kept = append(kept, a)
	}
	n.Attr = kept
}

// unwrapElement moves children of n to n's parent, then removes n.
func unwrapElement(n *html.Node) {
	parent := n.Parent
	if parent == nil {
		return
	}
	// Sanitize descendants first so nested <font> etc. are cleaned before move.
	sanitizeNode(n)
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		n.RemoveChild(c)
		parent.InsertBefore(c, n)
		c = next
	}
	parent.RemoveChild(n)
}

var (
	reStyleBlock   = regexp.MustCompile(`(?is)<\s*style\b[^>]*>.*?<\s*/\s*style\s*>`)
	reXMLBlock     = regexp.MustCompile(`(?is)<\s*xml\b[^>]*>.*?<\s*/\s*xml\s*>`)
	reEmptyComment = regexp.MustCompile(`(?s)<!--\s*-->`)
	// Fallback for non-paired / malformed blocks inside comments.
	// The scanner checks only opening tags (<style / <xml), so removing open/close tags is sufficient.
	reStyleOpen  = regexp.MustCompile(`(?is)<\s*style\b[^>]*>`)
	reStyleClose = regexp.MustCompile(`(?is)<\s*/\s*style\s*>`)
	reStyleStart = regexp.MustCompile(`(?is)<\s*style\b`)
	reXMLOpen    = regexp.MustCompile(`(?is)<\s*xml\b[^>]*>`)
	reXMLClose   = regexp.MustCompile(`(?is)<\s*/\s*xml\s*>`)
	reXMLStart   = regexp.MustCompile(`(?is)<\s*xml\b`)
)

func removeStyleAndXMLBlocks(s string) string {
	s = reStyleBlock.ReplaceAllString(s, "")
	s = reXMLBlock.ReplaceAllString(s, "")
	// Fallback: if the comment contains <style>/<xml> but without a complete closing block,
	// the block regex won't match. Remove the tag markers themselves to eliminate <style / <xml substrings.
	s = reStyleOpen.ReplaceAllString(s, "")
	s = reStyleClose.ReplaceAllString(s, "")
	// If the tag start is malformed and missing '>', remove only '<style' prefix.
	s = reStyleStart.ReplaceAllString(s, "")
	s = reXMLOpen.ReplaceAllString(s, "")
	s = reXMLClose.ReplaceAllString(s, "")
	s = reXMLStart.ReplaceAllString(s, "")
	s = reEmptyComment.ReplaceAllString(s, "")
	return s
}
