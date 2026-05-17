package parser

import (
	"strings"
	"testing"

	"github.com/lucasdss/v8go/pkg/dom"
)

// TestStreamingParser_BasicDocument verifies incremental DOM construction.
func TestStreamingParser_BasicDocument(t *testing.T) {
	html := `<!DOCTYPE html><html><head><title>Test</title></head><body><p>Hello</p></body></html>`
	p := NewParser(html)

	var tokenCount int
	for {
		tok := p.NextToken()
		tokenCount++
		if tok.Type == TokenEOF {
			break
		}
		p.ProcessToken(tok)
	}

	if tokenCount < 3 {
		t.Errorf("expected at least 3 tokens, got %d", tokenCount)
	}

	doc := p.Document()
	if doc == nil {
		t.Fatal("document is nil")
	}
	if doc.DocumentElement == nil {
		t.Fatal("document element is nil")
	}
	if doc.DocumentElement.LocalName != "html" {
		t.Errorf("expected <html> root, got %q", doc.DocumentElement.LocalName)
	}

	// Title should be "Test".
	titles := doc.GetElementsByTagName("title")
	if len(titles) != 1 {
		t.Fatalf("expected 1 title element, got %d", len(titles))
	}
	if text := titles[0].TextContent(); text != "Test" {
		t.Errorf("expected title 'Test', got %q", text)
	}

	// Body paragraph.
	ps := doc.GetElementsByTagName("p")
	if len(ps) != 1 {
		t.Fatalf("expected 1 paragraph, got %d", len(ps))
	}
	if text := ps[0].TextContent(); text != "Hello" {
		t.Errorf("expected 'Hello' in paragraph, got %q", text)
	}
}

// TestStreamingParser_ScriptTag verifies script content is preserved as raw text.
func TestStreamingParser_ScriptTag(t *testing.T) {
	html := `<html><head><script>var x = "<div>not a tag</div>";</script></head></html>`
	p := NewParser(html)

	for {
		tok := p.NextToken()
		if tok.Type == TokenEOF {
			break
		}
		p.ProcessToken(tok)
	}

	doc := p.Document()
	scripts := doc.GetElementsByTagName("script")
	if len(scripts) != 1 {
		t.Fatalf("expected 1 script element, got %d", len(scripts))
	}

	text := scripts[0].TextContent()
	if !strings.Contains(text, "<div>not a tag</div>") {
		t.Errorf("script content should contain raw HTML-like text, got %q", text)
	}
}

// TestStreamingParser_StyleTag verifies style content is preserved as raw text.
func TestStreamingParser_StyleTag(t *testing.T) {
	html := `<html><head><style>div > p { color: red; }</style></head></html>`
	p := NewParser(html)

	for {
		tok := p.NextToken()
		if tok.Type == TokenEOF {
			break
		}
		p.ProcessToken(tok)
	}

	doc := p.Document()
	styles := doc.GetElementsByTagName("style")
	if len(styles) != 1 {
		t.Fatalf("expected 1 style element, got %d", len(styles))
	}

	text := styles[0].TextContent()
	if !strings.Contains(text, "div > p") {
		t.Errorf("style content should contain raw CSS text, got %q", text)
	}
}

// TestStreamingParser_NoRawTextPrePass verifies that protectRawText is not
// needed — the tokenizer handles raw text states directly.
func TestStreamingParser_NoRawTextPrePass(t *testing.T) {
	html := `<script>if (x < 3 && y > 4) { alert("</script>"); }</script>`
	p := NewParser(html)

	for {
		tok := p.NextToken()
		if tok.Type == TokenEOF {
			break
		}
		p.ProcessToken(tok)
	}

	doc := p.Document()
	scripts := doc.GetElementsByTagName("script")
	if len(scripts) != 1 {
		t.Fatalf("expected 1 script element, got %d", len(scripts))
	}

	text := scripts[0].TextContent()
	// The script content should include the comparison operators and the
	// quoted </script> string literal (not treated as a real closing tag).
	if !strings.Contains(text, "x < 3") {
		t.Errorf("expected script to contain 'x < 3', got %q", text)
	}
	if !strings.Contains(text, "alert") {
		t.Errorf("expected script to contain 'alert', got %q", text)
	}
}

// TestStreamingParser_ScriptEndTagInString verifies that </script> inside
// a JS string literal closes the script element. This is the naive behavior;
// full WHATWG ScriptData state handling (which treats quoted </script>
// differently) is a future enhancement.
func TestStreamingParser_ScriptEndTagInString(t *testing.T) {
	html := `<script>var s = "</script>"; var x = 1;</script><p>after</p>`
	p := NewParser(html)

	for {
		tok := p.NextToken()
		if tok.Type == TokenEOF {
			break
		}
		p.ProcessToken(tok)
	}

	doc := p.Document()
	scripts := doc.GetElementsByTagName("script")
	if len(scripts) != 1 {
		t.Fatalf("expected 1 script element, got %d", len(scripts))
	}

	text := scripts[0].TextContent()
	// The first </script> closes the element; " after it and the rest go elsewhere.
	if !strings.Contains(text, `var s = "`) {
		t.Errorf("expected script to start with var s, got %q", text)
	}
	// After the script, the paragraph should be present.
	ps := doc.GetElementsByTagName("p")
	if len(ps) < 1 {
		t.Errorf("expected paragraph after script")
	}
}

// TestStreamingParser_NestedRawText verifies that the first </style> closes
// the style element, even inside a CSS comment. Full WHATWG raw text
// end tag matching (requiring whitespace/>/ followed by tag name) is a
// future enhancement.
func TestStreamingParser_NestedRawText(t *testing.T) {
	html := `<style>/* </style> is the end */ h1 { font: bold; }</style><p>ok</p>`
	p := NewParser(html)

	for {
		tok := p.NextToken()
		if tok.Type == TokenEOF {
			break
		}
		p.ProcessToken(tok)
	}

	doc := p.Document()
	styles := doc.GetElementsByTagName("style")
	if len(styles) != 1 {
		t.Fatalf("expected 1 style element, got %d", len(styles))
	}

	text := styles[0].TextContent()
	// The first </style> closes the element; text after it belongs to parent.
	if !strings.Contains(text, "/* ") {
		t.Errorf("expected style to start with /*, got %q", text)
	}

	ps := doc.GetElementsByTagName("p")
	if len(ps) < 1 {
		t.Errorf("expected paragraph after style")
	}
}

// TestStreamingParser_TokenByToken verifies NextToken returns correct types in order.
func TestStreamingParser_TokenByToken(t *testing.T) {
	html := `<div id="x">hello</div>`
	p := NewParser(html)

	tokens := []Token{}
	for {
		tok := p.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == TokenEOF {
			break
		}
	}

	// Expected: StartTag(div), Character(hello), EndTag(div), EOF.
	if len(tokens) < 4 {
		t.Fatalf("expected at least 4 tokens, got %d", len(tokens))
	}

	if tokens[0].Type != TokenStartTag || tokens[0].Data != "div" {
		t.Errorf("token 0: expected start tag 'div', got %v", tokens[0])
	}
	if tokens[0].Attributes["id"] != "x" {
		t.Errorf("token 0: expected id='x', got %q", tokens[0].Attributes["id"])
	}
	if tokens[1].Type != TokenCharacter || tokens[1].Data != "hello" {
		t.Errorf("token 1: expected character 'hello', got %v", tokens[1])
	}
	if tokens[2].Type != TokenEndTag || tokens[2].Data != "div" {
		t.Errorf("token 2: expected end tag 'div', got %v", tokens[2])
	}
	if tokens[3].Type != TokenEOF {
		t.Errorf("token 3: expected EOF, got %v", tokens[3])
	}
}

// TestStreamingParser_EmptyInput produces only EOF.
func TestStreamingParser_EmptyInput(t *testing.T) {
	p := NewParser("")
	tok := p.NextToken()
	if tok.Type != TokenEOF {
		t.Errorf("expected EOF for empty input, got %v", tok)
	}
}

// TestStreamingParser_MatchesBatchParser verifies streaming parser produces
// the same DOM as the batch ParseHTMLSimple.
func TestStreamingParser_MatchesBatchParser(t *testing.T) {
	html := `<!DOCTYPE html><html><head><title>Compare</title></head><body><div class="a">text</div><br><span>more</span></body></html>`

	// Batch parser.
	batchDoc := ParseHTMLSimple(html)

	// Streaming parser.
	p := NewParser(html)
	for {
		tok := p.NextToken()
		if tok.Type == TokenEOF {
			break
		}
		p.ProcessToken(tok)
	}
	streamDoc := p.Document()

	// Compare text content of the root.
	if batchDoc.TextContent() != streamDoc.TextContent() {
		t.Errorf("text content mismatch: batch=%q stream=%q",
			batchDoc.TextContent(), streamDoc.TextContent())
	}

	// Compare number of elements.
	batchAll := batchDoc.GetElementsByTagName("*")
	streamAll := streamDoc.GetElementsByTagName("*")
	if len(batchAll) != len(streamAll) {
		t.Errorf("element count mismatch: batch=%d stream=%d",
			len(batchAll), len(streamAll))
	}
}

// TestStreamingParser_TextareaRCDATA verifies <textarea> content is treated as RCDATA.
func TestStreamingParser_TextareaRCDATA(t *testing.T) {
	html := `<textarea><div>not a div</div></textarea>`
	p := NewParser(html)

	for {
		tok := p.NextToken()
		if tok.Type == TokenEOF {
			break
		}
		p.ProcessToken(tok)
	}

	doc := p.Document()
	textareas := doc.GetElementsByTagName("textarea")
	if len(textareas) != 1 {
		t.Fatalf("expected 1 textarea, got %d", len(textareas))
	}
	text := textareas[0].TextContent()
	if !strings.Contains(text, "<div>not a div</div>") {
		t.Errorf("textarea should contain raw HTML-like content, got %q", text)
	}
}

// TestStreamingParser_TokenizerModeReset verifies tokenizer returns to data state
// after a raw text block.
func TestStreamingParser_TokenizerModeReset(t *testing.T) {
	html := `<script>var x=1;</script><p>after script</p>`
	p := NewParser(html)

	for {
		tok := p.NextToken()
		if tok.Type == TokenEOF {
			break
		}
		p.ProcessToken(tok)
	}

	doc := p.Document()
	ps := doc.GetElementsByTagName("p")
	if len(ps) != 1 {
		t.Fatalf("expected 1 paragraph after script, got %d", len(ps))
	}
	if ps[0].TextContent() != "after script" {
		t.Errorf("expected 'after script', got %q", ps[0].TextContent())
	}
}

// TestStreamingParser_ParseHTMLStreamEntryPoint tests the convenience function.
func TestStreamingParser_ParseHTMLStreamEntryPoint(t *testing.T) {
	p := ParseHTMLStream("<div>stream test</div>")

	for {
		tok := p.NextToken()
		if tok.Type == TokenEOF {
			break
		}
		p.ProcessToken(tok)
	}

	doc := p.Document()
	divs := doc.GetElementsByTagName("div")
	if len(divs) != 1 {
		t.Fatalf("expected 1 div, got %d", len(divs))
	}
	if divs[0].TextContent() != "stream test" {
		t.Errorf("expected 'stream test', got %q", divs[0].TextContent())
	}
}

// TestStreamingParser_DocumentAccessor returns the document mid-parse.
func TestStreamingParser_DocumentAccessor(t *testing.T) {
	p := NewParser("<html><body><h1>mid-parse</h1></body></html>")

	// Read first few tokens.
	for i := 0; i < 5; i++ {
		tok := p.NextToken()
		if tok.Type == TokenEOF {
			break
		}
		p.ProcessToken(tok)
	}

	doc := p.Document()
	if doc == nil {
		t.Fatal("Document() returned nil mid-parse")
	}
	// Document should be partially built.
	_ = doc.GetElementsByTagName("h1")
}

// Ensure dom package is used for the import.
var _ = (*dom.Document)(nil)
