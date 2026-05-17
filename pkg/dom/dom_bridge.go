package dom

import "strings"

// ────────────────────── classList helpers ──────────────────────

// ClassListAdd adds a class token if not already present.
func (e *Element) ClassListAdd(cls string) {
	current := e.ClassList()
	for _, c := range current {
		if c == cls {
			return
		}
	}
	current = append(current, cls)
	e.SetAttribute("class", strings.Join(current, " "))
}

// ClassListRemove removes a class token if present.
func (e *Element) ClassListRemove(cls string) {
	current := e.ClassList()
	var out []string
	for _, c := range current {
		if c != cls {
			out = append(out, c)
		}
	}
	if out == nil {
		e.SetAttribute("class", "")
	} else {
		e.SetAttribute("class", strings.Join(out, " "))
	}
}

// ClassListToggle toggles a class token; returns true if added, false if removed.
func (e *Element) ClassListToggle(cls string) bool {
	if e.ClassListContains(cls) {
		e.ClassListRemove(cls)
		return false
	}
	e.ClassListAdd(cls)
	return true
}

// ClassListContains returns true if the class token is present.
func (e *Element) ClassListContains(cls string) bool {
	for _, c := range e.ClassList() {
		if c == cls {
			return true
		}
	}
	return false
}

// ParentElement returns the parent element, or nil if the parent is not an element.
func (e *Element) ParentElement() *Element {
	if e.parentNode == nil {
		return nil
	}
	if e.parentNode.nodeType == ElementNode {
		if parent, ok := e.parentNode.self.(*Element); ok {
			return parent
		}
	}
	return nil
}

// ────────────────────── innerHTML / outerHTML ──────────────────────

// InnerHTML serializes children to an HTML string.
func (e *Element) InnerHTML() string {
	var b strings.Builder
	for c := e.firstChild; c != nil; c = c.nextSibling {
		serializeNodeGo(c, &b)
	}
	return b.String()
}

// OuterHTML serializes the element and all its children to an HTML string.
func (e *Element) OuterHTML() string {
	var b strings.Builder
	serializeElementGo(e, &b)
	return b.String()
}

// SetInnerHTML parses HTML markup and replaces all children of this element.
func (e *Element) SetInnerHTML(html string) error {
	for c := e.firstChild; c != nil; {
		next := c.nextSibling
		_ = e.removeChild(c)
		c = next
	}
	if html == "" {
		return nil
	}
	fragment := ParseHTMLFragment(html, e.ownerDocument)
	for c := fragment.firstChild; c != nil; {
		next := c.nextSibling
		_ = e.AppendChild(c)
		c = next
	}
	return nil
}

func serializeNodeGo(n *Node, b *strings.Builder) {
	switch n.nodeType {
	case ElementNode:
		if elem, ok := n.self.(*Element); ok {
			serializeElementGo(elem, b)
		}
	case TextNode:
		if t, ok := n.self.(*Text); ok {
			b.WriteString(escapeHTMLGo(t.Data))
		}
	case CommentNode:
		if c, ok := n.self.(*Comment); ok {
			b.WriteString("<!--")
			b.WriteString(c.Data)
			b.WriteString("-->")
		}
	case DocumentNode:
		for c := n.firstChild; c != nil; c = c.nextSibling {
			serializeNodeGo(c, b)
		}
	}
}

func serializeElementGo(e *Element, b *strings.Builder) {
	b.WriteByte('<')
	b.WriteString(e.LocalName)
	for _, a := range e.attributes {
		b.WriteByte(' ')
		b.WriteString(a.LocalName)
		b.WriteString(`="`)
		b.WriteString(escapeHTMLGo(a.Value))
		b.WriteByte('"')
	}
	if isVoidElementGo(e.LocalName) {
		b.WriteString(" />")
		return
	}
	b.WriteByte('>')
	for c := e.firstChild; c != nil; c = c.nextSibling {
		serializeNodeGo(c, b)
	}
	b.WriteString("</")
	b.WriteString(e.LocalName)
	b.WriteByte('>')
}

func escapeHTMLGo(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isVoidElementGo(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input",
		"link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}

// ────────────────────── style helpers ──────────────────────

// StyleProperty returns the value for an inline style property (from the style attribute).
func (e *Element) StyleProperty(prop string) string {
	styleAttr := e.GetAttribute("style")
	if styleAttr == "" {
		return ""
	}
	return parseStylePropertyGo(styleAttr, prop)
}

// SetStyleProperty sets an inline style property in the element's style attribute.
func (e *Element) SetStyleProperty(prop, value string) {
	styleAttr := e.GetAttribute("style")
	newStyle := setStylePropertyGo(styleAttr, prop, value)
	e.SetAttribute("style", newStyle)
}

func parseStylePropertyGo(style, prop string) string {
	prop = strings.ToLower(prop)
	for _, decl := range strings.Split(style, ";") {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		parts := strings.SplitN(decl, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.ToLower(strings.TrimSpace(parts[0])) == prop {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

// ────────────────────── CSS Selector matching ──────────────────────

// cssSelector represents a parsed CSS selector.
type cssSelector struct {
	Tag       string
	Class     string
	ID        string
	AttrName  string
	AttrValue string
}

// QuerySelector returns the first descendant element matching the CSS selector.
// Supports basic selectors: tag, .class, #id, [attr], [attr=value], and combinations.
func (e *Element) QuerySelector(selector string) *Element {
	return querySelectorFrom(&e.Node, selector)
}

// QuerySelectorAll returns all descendant elements matching the CSS selector.
func (e *Element) QuerySelectorAll(selector string) []*Element {
	return querySelectorAllFrom(&e.Node, selector)
}

// QuerySelector returns the first element matching the selector from the document root.
func (d *Document) QuerySelector(selector string) *Element {
	return querySelectorFrom(&d.Node, selector)
}

// QuerySelectorAll returns all elements matching the selector from the document root.
func (d *Document) QuerySelectorAll(selector string) []*Element {
	return querySelectorAllFrom(&d.Node, selector)
}

func parseSimpleSelector(sel string) cssSelector {
	s := cssSelector{}
	remain := strings.TrimSpace(sel)
	for remain != "" {
		switch remain[0] {
		case '.':
			end := 1
			for end < len(remain) && remain[end] != '.' && remain[end] != '#' && remain[end] != '[' {
				end++
			}
			s.Class = remain[1:end]
			remain = remain[end:]
		case '#':
			end := 1
			for end < len(remain) && remain[end] != '.' && remain[end] != '#' && remain[end] != '[' {
				end++
			}
			s.ID = remain[1:end]
			remain = remain[end:]
		case '[':
			end := 1
			for end < len(remain) && remain[end] != ']' {
				end++
			}
			if end < len(remain) {
				attrBody := remain[1:end]
				remain = remain[end+1:]
				if eq := strings.IndexByte(attrBody, '='); eq >= 0 {
					s.AttrName = strings.TrimSpace(attrBody[:eq])
					val := strings.TrimSpace(attrBody[eq+1:])
					if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') {
						val = val[1 : len(val)-1]
					}
					s.AttrValue = val
				} else {
					s.AttrName = strings.TrimSpace(attrBody)
				}
			} else {
				remain = ""
			}
		default:
			end := 0
			for end < len(remain) && remain[end] != '.' && remain[end] != '#' && remain[end] != '[' {
				end++
			}
			s.Tag = strings.ToLower(remain[:end])
			remain = remain[end:]
		}
	}
	return s
}

func (s cssSelector) matches(elem *Element) bool {
	if s.Tag != "" && elem.LocalName != s.Tag {
		return false
	}
	if s.Class != "" {
		found := false
		for _, c := range elem.ClassList() {
			if c == s.Class {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if s.ID != "" && elem.ID() != s.ID {
		return false
	}
	if s.AttrName != "" {
		if s.AttrValue != "" {
			if elem.GetAttribute(s.AttrName) != s.AttrValue {
				return false
			}
		} else {
			if !elem.HasAttribute(s.AttrName) {
				return false
			}
		}
	}
	return true
}

func querySelectorFrom(n *Node, selector string) *Element {
	sel := parseSimpleSelector(selector)
	return matchFirst(n, sel)
}

func matchFirst(n *Node, sel cssSelector) *Element {
	for c := n.firstChild; c != nil; c = c.nextSibling {
		if c.nodeType == ElementNode {
			if elem, ok := c.self.(*Element); ok {
				if sel.matches(elem) {
					return elem
				}
				if found := matchFirst(c, sel); found != nil {
					return found
				}
			}
		} else {
			if found := matchFirst(c, sel); found != nil {
				return found
			}
		}
	}
	return nil
}

func querySelectorAllFrom(n *Node, selector string) []*Element {
	sel := parseSimpleSelector(selector)
	var results []*Element
	matchAll(n, sel, &results)
	return results
}

func matchAll(n *Node, sel cssSelector, results *[]*Element) {
	for c := n.firstChild; c != nil; c = c.nextSibling {
		if c.nodeType == ElementNode {
			if elem, ok := c.self.(*Element); ok {
				if sel.matches(elem) {
					*results = append(*results, elem)
				}
				matchAll(c, sel, results)
			}
		} else {
			matchAll(c, sel, results)
		}
	}
}

func setStylePropertyGo(style, prop, value string) string {
	prop = strings.ToLower(prop)
	if value == "" {
		var keep []string
		for _, decl := range strings.Split(style, ";") {
			decl = strings.TrimSpace(decl)
			if decl == "" {
				continue
			}
			parts := strings.SplitN(decl, ":", 2)
			if len(parts) != 2 {
				keep = append(keep, decl)
				continue
			}
			if strings.ToLower(strings.TrimSpace(parts[0])) != prop {
				keep = append(keep, decl)
			}
		}
		return strings.TrimSuffix(strings.Join(keep, "; "), "; ")
	}
	found := false
	var newParts []string
	for _, decl := range strings.Split(style, ";") {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		parts := strings.SplitN(decl, ":", 2)
		if len(parts) != 2 {
			newParts = append(newParts, decl)
			continue
		}
		if strings.ToLower(strings.TrimSpace(parts[0])) == prop {
			newParts = append(newParts, prop+": "+value)
			found = true
		} else {
			newParts = append(newParts, decl)
		}
	}
	if !found {
		newParts = append(newParts, prop+": "+value)
	}
	result := strings.Join(newParts, "; ")
	return strings.TrimSuffix(result, "; ")
}
