package parser_test

import (
	"testing"

	"github.com/lucasdss/v8go/pkg/dom"
	"github.com/lucasdss/v8go/pkg/parser"
)

// ─────────────────────────────── Tokenizer ────────────────────────────────

func TestTokenizer_EOF(t *testing.T) {
	toks := parser.NewTokenizer("").Tokenize()
	if len(toks) == 0 || toks[len(toks)-1].Type != parser.TokenEOF {
		t.Error("expected EOF token for empty input")
	}
}

func TestTokenizer_DocType(t *testing.T) {
	toks := parser.NewTokenizer("<!DOCTYPE html>").Tokenize()
	found := false
	for _, tok := range toks {
		if tok.Type == parser.TokenDoctype {
			found = true
			if tok.DoctypeName != "html" {
				t.Errorf("DOCTYPE name: got %q, want %q", tok.DoctypeName, "html")
			}
		}
	}
	if !found {
		t.Error("expected a DOCTYPE token")
	}
}

func TestTokenizer_StartTag_Simple(t *testing.T) {
	toks := parser.NewTokenizer("<div>").Tokenize()
	found := false
	for _, tok := range toks {
		if tok.Type == parser.TokenStartTag && tok.Data == "div" {
			found = true
		}
	}
	if !found {
		t.Error("expected a start tag token for <div>")
	}
}

func TestTokenizer_EndTag(t *testing.T) {
	toks := parser.NewTokenizer("</p>").Tokenize()
	found := false
	for _, tok := range toks {
		if tok.Type == parser.TokenEndTag && tok.Data == "p" {
			found = true
		}
	}
	if !found {
		t.Error("expected an end tag token for </p>")
	}
}

func TestTokenizer_SelfClosingTag(t *testing.T) {
	toks := parser.NewTokenizer("<br/>").Tokenize()
	found := false
	for _, tok := range toks {
		if tok.Type == parser.TokenSelfClosingTag && tok.Data == "br" {
			found = true
		}
	}
	if !found {
		t.Error("expected a self-closing tag token for <br/>")
	}
}

func TestTokenizer_Attributes_DoubleQuoted(t *testing.T) {
	toks := parser.NewTokenizer(`<a href="https://example.com">`).Tokenize()
	var startTag *parser.Token
	for i := range toks {
		if toks[i].Type == parser.TokenStartTag && toks[i].Data == "a" {
			startTag = &toks[i]
			break
		}
	}
	if startTag == nil {
		t.Fatal("expected start tag for <a>")
	}
	if startTag.Attributes["href"] != "https://example.com" {
		t.Errorf("href attr: got %q, want %q", startTag.Attributes["href"], "https://example.com")
	}
}

func TestTokenizer_Attributes_SingleQuoted(t *testing.T) {
	toks := parser.NewTokenizer(`<a href='https://example.com'>`).Tokenize()
	var startTag *parser.Token
	for i := range toks {
		if toks[i].Type == parser.TokenStartTag && toks[i].Data == "a" {
			startTag = &toks[i]
			break
		}
	}
	if startTag == nil {
		t.Fatal("expected start tag for <a>")
	}
	if startTag.Attributes["href"] != "https://example.com" {
		t.Errorf("href attr: got %q, want %q", startTag.Attributes["href"], "https://example.com")
	}
}

func TestTokenizer_Attributes_Unquoted(t *testing.T) {
	toks := parser.NewTokenizer(`<input type=text>`).Tokenize()
	var startTag *parser.Token
	for i := range toks {
		if toks[i].Type == parser.TokenStartTag && toks[i].Data == "input" {
			startTag = &toks[i]
			break
		}
	}
	if startTag == nil {
		t.Fatal("expected start tag for <input>")
	}
	if startTag.Attributes["type"] != "text" {
		t.Errorf("type attr: got %q, want text", startTag.Attributes["type"])
	}
}

func TestTokenizer_Attributes_Boolean(t *testing.T) {
	toks := parser.NewTokenizer(`<input disabled>`).Tokenize()
	var startTag *parser.Token
	for i := range toks {
		if toks[i].Type == parser.TokenStartTag && toks[i].Data == "input" {
			startTag = &toks[i]
			break
		}
	}
	if startTag == nil {
		t.Fatal("expected start tag for <input>")
	}
	if _, ok := startTag.Attributes["disabled"]; !ok {
		t.Error("expected disabled attribute")
	}
}

func TestTokenizer_Comment(t *testing.T) {
	toks := parser.NewTokenizer("<!-- a comment -->").Tokenize()
	found := false
	for _, tok := range toks {
		if tok.Type == parser.TokenComment && tok.Data == " a comment " {
			found = true
		}
	}
	if !found {
		t.Error("expected comment token")
	}
}

func TestTokenizer_CharacterData(t *testing.T) {
	toks := parser.NewTokenizer("Hello").Tokenize()
	found := false
	for _, tok := range toks {
		if tok.Type == parser.TokenCharacter && tok.Data == "Hello" {
			found = true
		}
	}
	if !found {
		t.Error("expected character token")
	}
}

func TestTokenizer_MixedContent(t *testing.T) {
	toks := parser.NewTokenizer("<p>Hello</p>").Tokenize()
	types := make([]parser.TokenType, 0)
	for _, tok := range toks {
		if tok.Type != parser.TokenEOF {
			types = append(types, tok.Type)
		}
	}
	// Expect: StartTag, Character, EndTag
	if len(types) < 3 {
		t.Fatalf("expected at least 3 tokens, got %d: %v", len(types), types)
	}
	if types[0] != parser.TokenStartTag {
		t.Errorf("token[0]: got %v, want StartTag", types[0])
	}
	if types[1] != parser.TokenCharacter {
		t.Errorf("token[1]: got %v, want Character", types[1])
	}
	if types[2] != parser.TokenEndTag {
		t.Errorf("token[2]: got %v, want EndTag", types[2])
	}
}

func TestTokenizer_Malformed_UnclosedTag(_ *testing.T) {
	// Should not panic; malformed input must be handled gracefully.
	toks := parser.NewTokenizer("<div").Tokenize()
	_ = toks // just checking it doesn't panic
}

func TestTokenizer_Malformed_NestedBrackets(_ *testing.T) {
	toks := parser.NewTokenizer("<div<span>").Tokenize()
	_ = toks
}

func TestTokenizer_DOCTYPE_WithPublicID(t *testing.T) {
	input := `<!DOCTYPE html PUBLIC "-//W3C//DTD HTML 4.01//EN" "http://www.w3.org/TR/html4/strict.dtd">`
	toks := parser.NewTokenizer(input).Tokenize()
	for _, tok := range toks {
		if tok.Type == parser.TokenDoctype {
			if tok.DoctypePublicID != "-//W3C//DTD HTML 4.01//EN" {
				t.Errorf("PublicID: got %q", tok.DoctypePublicID)
			}
			return
		}
	}
	t.Error("expected DOCTYPE token")
}

// ─────────────────────────────── Tree Construction ────────────────────────

func TestParseHTML_Empty(t *testing.T) {
	doc := parser.ParseHTML("")
	if doc == nil {
		t.Fatal("ParseHTML returned nil document")
	}
}

func TestParseHTML_SimpleDocument(t *testing.T) {
	html := `<!DOCTYPE html><html><head><title>Test</title></head><body><p>Hello</p></body></html>`
	doc := parser.ParseHTML(html)

	if doc.Doctype == nil {
		t.Error("expected document to have a doctype")
	}

	ps := doc.GetElementsByTagName("p")
	if len(ps) == 0 {
		t.Error("expected at least one <p> element")
	}
}

func TestParseHTML_Title(t *testing.T) {
	html := `<html><head><title>My Page</title></head><body></body></html>`
	doc := parser.ParseHTML(html)

	titles := doc.GetElementsByTagName("title")
	if len(titles) == 0 {
		t.Error("expected <title> element")
	}
}

func TestParseHTML_Attributes(t *testing.T) {
	html := `<a href="https://example.com" class="link external">Click</a>`
	doc := parser.ParseHTML(html)

	anchors := doc.GetElementsByTagName("a")
	if len(anchors) == 0 {
		t.Fatal("expected <a> element")
	}
	a := anchors[0]
	if a.GetAttribute("href") != "https://example.com" {
		t.Errorf("href: got %q", a.GetAttribute("href"))
	}
	if a.GetAttribute("class") != "link external" {
		t.Errorf("class: got %q", a.GetAttribute("class"))
	}
}

func TestParseHTML_VoidElements(t *testing.T) {
	html := `<div><br/><img src="test.png"/></div>`
	doc := parser.ParseHTML(html)

	// <br> and <img> should be in the tree but should not swallow subsequent siblings.
	divs := doc.GetElementsByTagName("div")
	if len(divs) == 0 {
		t.Fatal("expected <div>")
	}
	children := divs[0].Children()
	if len(children) < 2 {
		t.Errorf("expected at least 2 children of div, got %d", len(children))
	}
}

func TestParseHTML_NestedElements(t *testing.T) {
	html := `<ul><li>one</li><li>two</li><li>three</li></ul>`
	doc := parser.ParseHTML(html)

	lis := doc.GetElementsByTagName("li")
	// HTML5 parser creates implied elements; expect at least 3.
	if len(lis) < 3 {
		t.Errorf("expected at least 3 <li> elements, got %d", len(lis))
	}
}

func TestParseHTML_Comments(t *testing.T) {
	html := `<div><!-- This is a comment --><p>text</p></div>`
	doc := parser.ParseHTML(html)
	// Should not panic and should produce a document.
	ps := doc.GetElementsByTagName("p")
	if len(ps) == 0 {
		t.Error("expected <p> element")
	}
}

func TestParseHTML_MalformedMissingClosingTag(t *testing.T) {
	// Should not panic.
	doc := parser.ParseHTML(`<div><p>unclosed`)
	if doc == nil {
		t.Error("ParseHTML returned nil for malformed input")
	}
}

func TestParseHTML_DeepNesting(t *testing.T) {
	// Build a deeply nested structure. The HTML5 parser may auto-correct
	// nesting, so we just verify it doesn't crash and produces elements.
	var sb string
	depth := 50
	for i := 0; i < depth; i++ {
		sb += "<div>"
	}
	sb += "content"
	for i := 0; i < depth; i++ {
		sb += "</div>"
	}
	doc := parser.ParseHTML(sb)
	divs := doc.GetElementsByTagName("div")
	// The HTML5 parser handles deep nesting differently; just check it worked.
	if len(divs) == 0 {
		t.Error("deep nesting: expected at least some divs")
	}
}

// ─────────────────────────────── CSS Tokenizer ────────────────────────────

func TestCSSTokenizer_Ident(t *testing.T) {
	toks := parser.NewCSSTokenizer("color").TokenizeCSS()
	if len(toks) == 0 || toks[0].Type != parser.CSSTokenIdent {
		t.Errorf("expected ident token, got %v", toks)
	}
	if toks[0].Value != "color" {
		t.Errorf("ident value: got %q, want color", toks[0].Value)
	}
}

func TestCSSTokenizer_Number(t *testing.T) {
	toks := parser.NewCSSTokenizer("42").TokenizeCSS()
	if len(toks) == 0 || toks[0].Type != parser.CSSTokenNumber {
		t.Errorf("expected number token, got %v", toks)
	}
}

func TestCSSTokenizer_Dimension(t *testing.T) {
	toks := parser.NewCSSTokenizer("16px").TokenizeCSS()
	if len(toks) == 0 || toks[0].Type != parser.CSSTokenDimension {
		t.Errorf("expected dimension token, got %v", toks)
	}
	if toks[0].Unit != "px" {
		t.Errorf("dimension unit: got %q, want px", toks[0].Unit)
	}
}

func TestCSSTokenizer_Percentage(t *testing.T) {
	toks := parser.NewCSSTokenizer("50%").TokenizeCSS()
	if len(toks) == 0 || toks[0].Type != parser.CSSTokenPercentage {
		t.Errorf("expected percentage token, got %v", toks)
	}
}

func TestCSSTokenizer_String_DoubleQuoted(t *testing.T) {
	toks := parser.NewCSSTokenizer(`"hello"`).TokenizeCSS()
	if len(toks) == 0 || toks[0].Type != parser.CSSTokenString {
		t.Errorf("expected string token")
	}
	if toks[0].Value != "hello" {
		t.Errorf("string value: got %q, want hello", toks[0].Value)
	}
}

func TestCSSTokenizer_Hash(t *testing.T) {
	toks := parser.NewCSSTokenizer("#ff0000").TokenizeCSS()
	if len(toks) == 0 || toks[0].Type != parser.CSSTokenHash {
		t.Errorf("expected hash token")
	}
	if toks[0].Value != "ff0000" {
		t.Errorf("hash value: got %q, want ff0000", toks[0].Value)
	}
}

func TestCSSTokenizer_At(t *testing.T) {
	toks := parser.NewCSSTokenizer("@media").TokenizeCSS()
	if len(toks) == 0 || toks[0].Type != parser.CSSTokenAt {
		t.Errorf("expected at-keyword token")
	}
	if toks[0].Value != "media" {
		t.Errorf("at value: got %q, want media", toks[0].Value)
	}
}

func TestCSSTokenizer_Comments_Skipped(t *testing.T) {
	toks := parser.NewCSSTokenizer("/* comment */ color").TokenizeCSS()
	// Comments are consumed and the ident follows.
	hasIdent := false
	for _, tok := range toks {
		if tok.Type == parser.CSSTokenIdent && tok.Value == "color" {
			hasIdent = true
		}
	}
	if !hasIdent {
		t.Error("comment should be skipped; expected ident token after comment")
	}
}

// ─────────────────────────────── CSS Parser ───────────────────────────────

func TestParseCSS_EmptyInput(t *testing.T) {
	ss := parser.ParseCSS("")
	if ss == nil {
		t.Fatal("ParseCSS returned nil")
	}
	if len(ss.Rules) != 0 {
		t.Errorf("expected 0 rules for empty input, got %d", len(ss.Rules))
	}
}

func TestParseCSS_SimpleRule(t *testing.T) {
	css := `body { color: red; font-size: 16px; }`
	ss := parser.ParseCSS(css)
	if len(ss.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(ss.Rules))
	}
	rule := ss.Rules[0]
	if rule.Selector != "body" {
		t.Errorf("selector: got %q, want body", rule.Selector)
	}
	if len(rule.Declarations) != 2 {
		t.Errorf("declarations: got %d, want 2", len(rule.Declarations))
	}
}

func TestParseCSS_MultipleRules(t *testing.T) {
	css := `h1 { font-size: 32px; } p { margin: 0; }`
	ss := parser.ParseCSS(css)
	if len(ss.Rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(ss.Rules))
	}
}

func TestParseCSS_AtRule_Import(t *testing.T) {
	css := `@import url("style.css");`
	ss := parser.ParseCSS(css)
	if len(ss.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(ss.Rules))
	}
	if ss.Rules[0].Type != "import" {
		t.Errorf("rule type: got %q, want import", ss.Rules[0].Type)
	}
}

func TestParseCSS_AtRule_Media(t *testing.T) {
	css := `@media screen { body { display: block; } }`
	ss := parser.ParseCSS(css)
	if len(ss.Rules) == 0 {
		t.Fatal("expected at least one rule")
	}
	mediaRule := ss.Rules[0]
	if mediaRule.Type != "media" {
		t.Errorf("rule type: got %q, want media", mediaRule.Type)
	}
}

func TestParseCSS_ImportantDeclaration(t *testing.T) {
	css := `p { color: red !important; }`
	ss := parser.ParseCSS(css)
	if len(ss.Rules) == 0 {
		t.Fatal("expected rule")
	}
	decl := ss.Rules[0].Declarations[0]
	if !decl.Important {
		t.Error("expected !important flag")
	}
}

func TestParseCSS_SelectorList(t *testing.T) {
	css := `h1, h2, h3 { font-weight: bold; }`
	ss := parser.ParseCSS(css)
	if len(ss.Rules) == 0 {
		t.Fatal("expected rule")
	}
}

func TestParseCSS_MalformedInput(_ *testing.T) {
	// Should not panic.
	ss := parser.ParseCSS(`{{{ malformed }}}`)
	_ = ss
}

// ─────────────────────────── Tokenizer Token type String ──────────────────

// Ensure the DOM integration round-trips via ParseHTML correctly.
func TestParseHTML_DOMIntegration(t *testing.T) {
	html := `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8"></head><body><main id="content"><p class="intro">Hello</p></main></body></html>`
	doc := parser.ParseHTML(html)

	mains := doc.GetElementsByTagName("main")
	if len(mains) == 0 {
		t.Fatal("expected <main> element")
	}
	mainEl := mains[0]
	if mainEl.ID() != "content" {
		t.Errorf("main id: got %q, want content", mainEl.ID())
	}

	ps := doc.GetElementsByTagName("p")
	if len(ps) == 0 {
		t.Fatal("expected <p> element")
	}
	if !containsClass(ps[0], "intro") {
		t.Error("expected class intro on <p>")
	}
}

func containsClass(el *dom.Element, cls string) bool {
	for _, c := range el.ClassList() {
		if c == cls {
			return true
		}
	}
	return false
}
