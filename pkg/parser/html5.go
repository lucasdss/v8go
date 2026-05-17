package parser

import (
	"strings"

	htmllib "golang.org/x/net/html"

	"github.com/lucasdss/v8go/pkg/dom"
)

// ParseHTML5 parses HTML using golang.org/x/net/html and returns a
// dom.Document tree.
//
// Parser strategy: gobrowser intentionally uses the maintained HTML5 parser
// from x/net/html for the public ParseHTML path so malformed real-world markup
// follows the same WHATWG HTML tree-construction behavior as other Go tools.
// The custom tokenizer/parser in html.go is retained as an educational,
// test-focused implementation of selected HTML Standard § 12.2 concepts, not
// as the production parser for arbitrary web content.
func ParseHTML5(rawHTML string) *dom.Document {
	doc := dom.NewDocument()
	doc.ContentType = "text/html"
	doc.CharacterSet = "UTF-8"

	// Parse with the standard HTML5 parser.
	htmldoc, err := htmllib.Parse(strings.NewReader(rawHTML))
	if err != nil || htmldoc == nil {
		return doc
	}

	// Convert the html.Node tree to our dom.Document tree.
	convertTree(htmldoc, doc, nil)
	return doc
}

// ParseHTML parses html and returns a *dom.Document using the standards-backed
// x/net/html parser. Use ParseHTMLSimple only when explicitly testing the
// repository's simplified from-scratch parser.
func ParseHTML(html string) *dom.Document {
	return ParseHTML5(html)
}

// convertTree recursively converts an html.Node tree into our dom tree.
func convertTree(src *htmllib.Node, doc *dom.Document, parent *dom.Node) {
	var targetNode *dom.Node

	switch src.Type {
	case htmllib.DocumentNode:
		// Document node — the root.
		targetNode = &doc.Node
		for child := src.FirstChild; child != nil; child = child.NextSibling {
			convertTree(child, doc, targetNode)
		}
		return // already recursed into children above

	case htmllib.ElementNode:
		elem := doc.CreateElement(src.Data)
		// Copy attributes.
		for _, attr := range src.Attr {
			if attr.Namespace == "" {
				elem.SetAttribute(attr.Key, attr.Val)
			}
		}
		if parent != nil {
			_ = parent.AppendChild(&elem.Node)
		}

		// Set DocumentElement.
		if doc.DocumentElement == nil && elem.LocalName == "html" {
			doc.DocumentElement = elem
		}

		targetNode = &elem.Node

	case htmllib.TextNode:
		text := doc.CreateTextNode(src.Data)
		if parent != nil {
			_ = parent.AppendChild(&text.Node)
		}
		targetNode = &text.Node

	case htmllib.CommentNode:
		comment := doc.CreateComment(src.Data)
		if parent != nil {
			_ = parent.AppendChild(&comment.Node)
		}
		targetNode = &comment.Node

	case htmllib.DoctypeNode:
		doctype := doc.CreateDocumentType(src.Data, "", "")
		if parent != nil {
			_ = parent.AppendChild(&doctype.Node)
		}
		doc.Doctype = doctype
		targetNode = &doctype.Node

	case htmllib.RawNode:
		// Raw content (script/style) — treat as text.
		text := doc.CreateTextNode(src.Data)
		if parent != nil {
			_ = parent.AppendChild(&text.Node)
		}
		targetNode = &text.Node

	default:
		if parent != nil {
			targetNode = parent
		} else {
			targetNode = &doc.Node
		}
	}

	// Recursively convert children.
	for child := src.FirstChild; child != nil; child = child.NextSibling {
		convertTree(child, doc, targetNode)
	}
}
