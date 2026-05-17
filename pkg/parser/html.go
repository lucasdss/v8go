// Package parser implements the HTML parsing algorithm as specified in the
// WHATWG HTML Living Standard § 12 "The HTML syntax":
// https://html.spec.whatwg.org/multipage/parsing.html
//
// The tokenizer is a state machine that converts a byte stream into a sequence
// of tokens (DOCTYPE, start tag, end tag, comment, character, EOF).  The tree
// construction stage consumes those tokens and builds a DOM tree.
package parser

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/lucasdss/v8go/pkg/dom"
)

// ----------------------------- Token types -----------------------------------

// TokenType identifies the kind of HTML token.
type TokenType int

const (
	// TokenError is emitted when the tokenizer encounters an unrecoverable error.
	TokenError TokenType = iota
	// TokenDoctype is a <!DOCTYPE ...> token.
	TokenDoctype
	// TokenStartTag is an opening tag token.
	TokenStartTag
	// TokenEndTag is a closing tag token.
	TokenEndTag
	// TokenSelfClosingTag is a self-closing start tag (e.g. <br />).
	TokenSelfClosingTag
	// TokenComment is a <!-- ... --> comment.
	TokenComment
	// TokenCharacter is one or more character data characters.
	TokenCharacter
	// TokenEOF signals end of input.
	TokenEOF
)

// Token represents a single HTML token.
type Token struct {
	Type       TokenType
	Data       string            // tag name, comment text, or character data
	Attributes map[string]string // for start/end tag tokens
	// Doctype fields
	DoctypeName     string
	DoctypePublicID string
	DoctypeSystemID string
	SelfClosing     bool
}

// ----------------------------- Tokenizer -------------------------------------

// tokenizer state constants follow the WHATWG HTML Standard § 12.2.
type tokenizerState int

const (
	stateData                  tokenizerState = iota
	stateTagOpen                              // after "<"
	stateEndTagOpen                           // after "</"
	stateTagName                              // reading tag name
	stateSelfClosingStartTag                  // after "/" in start tag
	stateBeforeAttrName                       // before attribute name
	stateAttrName                             // reading attribute name
	stateAfterAttrName                        // after attribute name, before "="
	stateBeforeAttrValue                      // after "="
	stateAttrValueDoubleQuoted                // reading double-quoted attr value
	stateAttrValueSingleQuoted                // reading single-quoted attr value
	stateAttrValueUnquoted                    // reading unquoted attr value
	stateAfterAttrValueQuoted                 // after closing attr quote
	stateMarkupDeclarationOpen                // after "<!"
	stateCommentStart                         // after "<!--"
	stateCommentStartDash                     // after "<!---"
	stateComment                              // inside comment
	stateCommentEndDash                       // after "-" inside comment
	stateCommentEnd                           // after "--" inside comment
	stateDOCTYPE                              // reading DOCTYPE
	stateBeforeDOCTYPEName
	stateDOCTYPEName
	stateAfterDOCTYPEName
	stateBogusComment
	// Raw text states (WHATWG § 12.2.5).
	stateRCDATA       // <title>, <textarea> — character references decoded
	stateRAWTEXT      // <style> — no character references
	stateScriptData   // <script> — special handling for <!--, </script>, etc.
)

// Tokenizer converts an HTML input string into a stream of tokens.
type Tokenizer struct {
	input    []rune
	pos      int
	state    tokenizerState
	tokens   []Token
	// returnState is used when transitioning back from raw text states.
	returnState tokenizerState
	// lastStartTag is the most recent start tag name (lowercase), used for
	// matching end tags in raw text states.
	lastStartTag string
}

// NewTokenizer creates a Tokenizer for the given HTML input.
func NewTokenizer(input string) *Tokenizer {
	return &Tokenizer{input: []rune(input)}
}

// AppendInput appends additional runes to the tokenizer input, enabling
// incremental/streaming tokenization. After appending, callers can resume
// calling nextToken() to consume the new data.
func (t *Tokenizer) AppendInput(data []rune) {
	t.input = append(t.input, data...)
}

// peek returns the rune at the current position without advancing.
func (t *Tokenizer) peek() (rune, bool) {
	if t.pos >= len(t.input) {
		return 0, false
	}
	return t.input[t.pos], true
}

// consume advances pos and returns the consumed rune.
func (t *Tokenizer) consume() (rune, bool) {
	if t.pos >= len(t.input) {
		return 0, false
	}
	r := t.input[t.pos]
	t.pos++
	return r, true
}

// consumeIf advances only if the next sequence equals s (case-insensitive).
func (t *Tokenizer) consumeIf(s string) bool {
	runes := []rune(s)
	if t.pos+len(runes) > len(t.input) {
		return false
	}
	for i, r := range runes {
		if unicodeToLower(t.input[t.pos+i]) != unicodeToLower(r) {
			return false
		}
	}
	t.pos += len(runes)
	return true
}

func unicodeToLower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

// Tokenize runs the full tokenization and returns all tokens.
func (t *Tokenizer) Tokenize() []Token {
	t.tokens = nil
	t.state = stateData
	for {
		tok := t.nextToken()
		t.tokens = append(t.tokens, tok)
		if tok.Type == TokenEOF || tok.Type == TokenError {
			break
		}
	}
	return t.tokens
}

// SetTextMode switches the tokenizer to a raw text mode (RCDATA, RAWTEXT,
// or ScriptData). This is called by the tree construction stage after emitting
// a start tag for <title>, <textarea>, <style>, or <script>.
func (t *Tokenizer) SetTextMode(mode tokenizerState, tagName string) {
	t.returnState = stateData
	t.lastStartTag = tagName
	t.state = mode
}

// peekAhead returns up to n runes starting at pos without advancing.
func (t *Tokenizer) peekAhead(n int) []rune {
	if t.pos+n > len(t.input) {
		return t.input[t.pos:]
	}
	return t.input[t.pos : t.pos+n]
}

// rawTextLessThanSign handles '<' in raw text states. The '<' has already
// been consumed by the caller. If a matching closing tag is found, it flushes
// accumulated chars and returns an end tag token. Otherwise it emits '<' as
// a character and returns false.
func (t *Tokenizer) rawTextLessThanSign(charBuf *strings.Builder, closeTag string) (Token, bool) {
	// Check for '/'.
	r, ok := t.peek()
	if !ok || r != '/' {
		charBuf.WriteRune('<')
		return Token{}, false
	}
	t.pos++ // consume '/'

	// Check if tag name matches (case-insensitive).
	tagRunes := []rune(closeTag)
	for i, tr := range tagRunes {
		r, ok := t.peek()
		if !ok || unicodeToLower(r) != unicodeToLower(tr) {
			// Not expected closing tag — emit '<' + '/' + partial name as chars.
			charBuf.WriteRune('<')
			charBuf.WriteRune('/')
			for j := 0; j < i; j++ {
				charBuf.WriteRune(tagRunes[j])
			}
			return Token{}, false
		}
		t.pos++
		_ = i
	}

	// After tag name, expect whitespace, '>', or '/'.
	r, ok = t.peek()
	if ok && r != '>' && r != '/' && !isWhitespace(r) {
		// Extra characters after tag name — not our closing tag.
		charBuf.WriteRune('<')
		charBuf.WriteRune('/')
		for _, tr := range tagRunes {
			charBuf.WriteRune(tr)
		}
		return Token{}, false
	}

	// Found matching closing tag! Skip to '>' and emit end tag.
	// Skip optional whitespace and attributes (simplified: just skip to '>').
	for {
		r, ok := t.peek()
		if !ok || r == '>' {
			break
		}
		t.pos++
	}
	if r, ok := t.peek(); ok && r == '>' {
		t.pos++ // consume '>'
	}

	// Return accumulated character data first, then the caller will emit
	// the end tag on the next call. But we need to signal the end tag.
	// Set state back to data so the caller's loop handles it correctly.
	t.state = stateData

	// Flush any accumulated character data.
	if charBuf.Len() > 0 {
		tok := Token{Type: TokenCharacter, Data: charBuf.String()}
		charBuf.Reset()
		return tok, true
	}

	// No chars to flush — signal that raw text mode ended.
	// The parser will get the end tag from the regular flow (but we already
	// consumed it). Instead, return the end tag token directly.
	return Token{Type: TokenEndTag, Data: closeTag}, true
}

// nextToken runs the state machine until one token is produced.
func (t *Tokenizer) nextToken() Token {
	var charBuf strings.Builder

	flushChars := func() (Token, bool) {
		if charBuf.Len() > 0 {
			tok := Token{Type: TokenCharacter, Data: charBuf.String()}
			charBuf.Reset()
			return tok, true
		}
		return Token{}, false
	}

	for {
		switch t.state {
		case stateData:
			r, ok := t.consume()
			if !ok {
				if tok, had := flushChars(); had {
					return tok
				}
				return Token{Type: TokenEOF}
			}
			if r == '<' {
				if tok, had := flushChars(); had {
					t.pos-- // put '<' back
					return tok
				}
				t.state = stateTagOpen
			} else {
				charBuf.WriteRune(r)
			}

		case stateTagOpen:
			r, ok := t.peek()
			if !ok {
				charBuf.WriteRune('<')
				t.state = stateData
				continue
			}
			switch {
			case r == '!':
				t.pos++
				t.state = stateMarkupDeclarationOpen
			case r == '/':
				t.pos++
				t.state = stateEndTagOpen
			case isASCIIAlpha(r):
				tok := t.readTag(false)
				t.state = stateData
				return tok
			case r == '?':
				t.state = stateBogusComment
			default:
				charBuf.WriteRune('<')
				t.state = stateData
			}

		case stateEndTagOpen:
			r, ok := t.peek()
			if !ok {
				charBuf.WriteString("</")
				t.state = stateData
				continue
			}
			if isASCIIAlpha(r) {
				tok := t.readTag(true)
				t.state = stateData
				return tok
			}
			t.state = stateBogusComment

		case stateMarkupDeclarationOpen:
			if t.consumeIf("--") {
				t.state = stateCommentStart
			} else if t.consumeIf("DOCTYPE") || t.consumeIf("doctype") {
				t.state = stateBeforeDOCTYPEName
			} else {
				t.state = stateBogusComment
			}

		case stateCommentStart, stateComment, stateCommentStartDash,
			stateCommentEndDash, stateCommentEnd:
			tok := t.readComment()
			return tok

		case stateBeforeDOCTYPEName:
			for {
				r, ok := t.peek()
				if !ok {
					break
				}
				if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
					t.pos++
					continue
				}
				break
			}
			t.state = stateDOCTYPEName
			tok := t.readDoctype()
			return tok

		case stateDOCTYPEName, stateDOCTYPE, stateAfterDOCTYPEName:
			tok := t.readDoctype()
			return tok

		case stateBogusComment:
			tok := t.readBogusComment()
			return tok

		// ── Raw text states ──────────────────────────────────────────
		case stateRCDATA, stateRAWTEXT, stateScriptData:
			r, ok := t.consume()
			if !ok {
				if tok, had := flushChars(); had {
					return tok
				}
				return Token{Type: TokenEOF}
			}
			if r == '<' {
				closeTag := t.lastStartTag
				if tok, emitted := t.rawTextLessThanSign(&charBuf, closeTag); emitted {
					return tok
				}
			} else {
				charBuf.WriteRune(r)
			}

		default:
			t.state = stateData
		}
	}
}

// readTag parses a start tag or end tag starting after "<" or "</".
// isEnd is true for end tags.
func (t *Tokenizer) readTag(isEnd bool) Token {
	var name strings.Builder
	attrs := make(map[string]string)

	// Read tag name.
	for {
		r, ok := t.peek()
		if !ok {
			break
		}
		if r == '>' || r == '/' || isWhitespace(r) {
			break
		}
		t.pos++
		name.WriteRune(unicodeToLower(r))
	}

	selfClosing := false

	// Read attributes.
	for {
		// Skip whitespace.
		for {
			r, ok := t.peek()
			if !ok {
				break
			}
			if !isWhitespace(r) {
				break
			}
			t.pos++
		}

		r, ok := t.peek()
		if !ok {
			break
		}
		if r == '>' {
			t.pos++
			break
		}
		if r == '/' {
			t.pos++
			next, nok := t.peek()
			if nok && next == '>' {
				t.pos++
				selfClosing = true
			}
			break
		}

		// Read attribute name.
		var attrName strings.Builder
		for {
			r, ok = t.peek()
			if !ok {
				break
			}
			if r == '=' || r == '>' || isWhitespace(r) {
				break
			}
			t.pos++
			attrName.WriteRune(unicodeToLower(r))
		}

		an := attrName.String()
		if an == "" {
			// Skip malformed attribute.
			if r2, ok2 := t.peek(); ok2 && r2 != '>' {
				t.pos++
			}
			continue
		}

		// Skip whitespace around "=".
		for {
			r, ok = t.peek()
			if !ok || !isWhitespace(r) {
				break
			}
			t.pos++
		}
		if r, ok = t.peek(); !ok || r != '=' {
			attrs[an] = ""
			continue
		}
		t.pos++ // consume "="

		// Skip whitespace after "=".
		for {
			r, ok = t.peek()
			if !ok || !isWhitespace(r) {
				break
			}
			t.pos++
		}

		// Read attribute value.
		var attrVal strings.Builder
		if r, ok = t.peek(); ok {
			switch r {
			case '"', '\'':
				quote := r
				t.pos++
				for {
					ch, ok2 := t.consume()
					if !ok2 || ch == quote {
						break
					}
					attrVal.WriteRune(ch)
				}
			default:
				// Unquoted.
				for {
					ch, ok2 := t.peek()
					if !ok2 || isWhitespace(ch) || ch == '>' {
						break
					}
					t.pos++
					attrVal.WriteRune(ch)
				}
			}
		}
		attrs[an] = attrVal.String()
	}

	tokType := TokenStartTag
	if isEnd {
		tokType = TokenEndTag
	} else if selfClosing {
		tokType = TokenSelfClosingTag
	}

	return Token{
		Type:        tokType,
		Data:        name.String(),
		Attributes:  attrs,
		SelfClosing: selfClosing,
	}
}

// readComment parses a comment starting after "<!--".
func (t *Tokenizer) readComment() Token {
	var buf strings.Builder
	for {
		r, ok := t.consume()
		if !ok {
			return Token{Type: TokenComment, Data: buf.String()}
		}
		if r == '-' {
			r2, ok2 := t.peek()
			if ok2 && r2 == '-' {
				t.pos++
				r3, ok3 := t.peek()
				if ok3 && r3 == '>' {
					t.pos++
					t.state = stateData
					return Token{Type: TokenComment, Data: buf.String()}
				}
				buf.WriteRune('-')
				buf.WriteRune('-')
				continue
			}
		}
		buf.WriteRune(r)
	}
}

// readDoctype parses a DOCTYPE declaration.
func (t *Tokenizer) readDoctype() Token {
	// Skip whitespace.
	for {
		r, ok := t.peek()
		if !ok {
			break
		}
		if !isWhitespace(r) {
			break
		}
		t.pos++
	}

	var name strings.Builder
	for {
		r, ok := t.peek()
		if !ok || r == '>' || isWhitespace(r) {
			break
		}
		t.pos++
		name.WriteRune(unicodeToLower(r))
	}

	var publicID, systemID string

	// Check for PUBLIC or SYSTEM keywords.
	for {
		r, ok := t.peek()
		if !ok || r == '>' {
			break
		}
		if isWhitespace(r) {
			t.pos++
			continue
		}
		if t.consumeIf("PUBLIC") || t.consumeIf("public") {
			for {
				r2, ok2 := t.peek()
				if !ok2 || !isWhitespace(r2) {
					break
				}
				t.pos++
			}
			publicID = t.readDoctypeID()
			for {
				r2, ok2 := t.peek()
				if !ok2 || !isWhitespace(r2) {
					break
				}
				t.pos++
			}
			systemID = t.readDoctypeID()
		} else if t.consumeIf("SYSTEM") || t.consumeIf("system") {
			for {
				r2, ok2 := t.peek()
				if !ok2 || !isWhitespace(r2) {
					break
				}
				t.pos++
			}
			systemID = t.readDoctypeID()
		}
		break
	}

	// Consume until ">".
	for {
		r, ok := t.consume()
		if !ok || r == '>' {
			break
		}
	}

	t.state = stateData
	return Token{
		Type:            TokenDoctype,
		DoctypeName:     name.String(),
		DoctypePublicID: publicID,
		DoctypeSystemID: systemID,
	}
}

func (t *Tokenizer) readDoctypeID() string {
	r, ok := t.peek()
	if !ok {
		return ""
	}
	if r != '"' && r != '\'' {
		return ""
	}
	quote := r
	t.pos++
	var buf strings.Builder
	for {
		ch, ok2 := t.consume()
		if !ok2 || ch == quote {
			break
		}
		buf.WriteRune(ch)
	}
	return buf.String()
}

// readBogusComment reads until ">" and returns a comment token.
func (t *Tokenizer) readBogusComment() Token {
	var buf strings.Builder
	for {
		r, ok := t.consume()
		if !ok || r == '>' {
			break
		}
		buf.WriteRune(r)
	}
	t.state = stateData
	return Token{Type: TokenComment, Data: buf.String()}
}

func isASCIIAlpha(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
}

// ----------------------------- Tree Construction ----------------------------

// voidElements is the set of HTML void elements that must not have end tags.
// See HTML Standard § 12.1.2 "Elements".
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true,
	"embed": true, "hr": true, "img": true, "input": true,
	"link": true, "meta": true, "param": true, "source": true,
	"track": true, "wbr": true,
}

// rawTextElements are elements whose content is treated as raw text
// (script, style).  Currently tracked for future parser enhancements.
//
var rawTextElements = map[string]bool{
	"script": true, "style": true,
}

// rcdataElements are elements whose content is RCDATA (character references
// are decoded but tags are not parsed).
var rcdataElements = map[string]bool{
	"title": true, "textarea": true,
}

// PreloadURL represents a subresource URL discovered during incremental
// HTML parsing. It is sent to a channel so network fetches can start before
// the full DOM tree is built.
type PreloadURL struct {
	URL  string // raw attribute value (unresolved)
	Type string // "css", "js", "img"
}

// Parser constructs a DOM Document from a stream of HTML tokens.
// It implements a simplified version of the tree-construction algorithm
// defined in HTML Standard § 12.2.6.
type Parser struct {
	tokenizer    *Tokenizer
	doc          *dom.Document
	openElements []*dom.Element // stack of open elements
	headElement  *dom.Element
	bodyElement  *dom.Element
	framesetOK   bool

	// Streaming parse support.
	rawBuf    bytes.Buffer   // accumulated raw HTML bytes
	preloadCh chan PreloadURL // channel for discovered subresource URLs
	finished  bool           // true after Finish() — no more data expected
}

// ParseHTMLSimple parses html using the custom tokenizer-based parser.
// This is a simplified parser for testing. For production use, see ParseHTML5.
func ParseHTMLSimple(html string) *dom.Document {
	p := &Parser{
		tokenizer:  NewTokenizer(html),
		doc:        dom.NewDocument(),
		framesetOK: true,
	}
	p.parse()
	return p.doc
}

func (p *Parser) parse() {
	for {
		tok := p.tokenizer.nextToken()
		p.processToken(tok)
		if tok.Type == TokenEOF || tok.Type == TokenError {
			break
		}
	}
}

func (p *Parser) processToken(tok Token) {
	switch tok.Type {
	case TokenDoctype:
		p.handleDoctype(tok)
	case TokenStartTag:
		p.handleStartTag(tok)
	case TokenEndTag:
		p.handleEndTag(tok)
	case TokenSelfClosingTag:
		p.handleSelfClosingTag(tok)
	case TokenCharacter:
		p.handleCharacter(tok)
	case TokenComment:
		p.handleComment(tok)
	case TokenEOF:
		// Stop parsing.
	}
}

func (p *Parser) handleDoctype(tok Token) {
	dt := p.doc.CreateDocumentType(tok.DoctypeName, tok.DoctypePublicID, tok.DoctypeSystemID)
	_ = p.doc.Node.AppendChild(&dt.Node)
	p.doc.Doctype = dt
}

func (p *Parser) handleStartTag(tok Token) {
	elem := p.doc.CreateElement(tok.Data)
	for k, v := range tok.Attributes {
		elem.SetAttribute(k, v)
	}

	p.insertElement(elem)

	// For raw text elements (script, style), switch tokenizer to the
	// appropriate text mode so content is treated as raw characters.
	if rawTextElements[tok.Data] {
		switch tok.Data {
		case "script":
			p.tokenizer.SetTextMode(stateScriptData, "script")
		case "style":
			p.tokenizer.SetTextMode(stateRAWTEXT, "style")
		}
	}
	if rcdataElements[tok.Data] {
		p.tokenizer.SetTextMode(stateRCDATA, tok.Data)
	}

	// Track implicit head/body.
	switch tok.Data {
	case "html":
		if p.doc.DocumentElement == nil {
			p.doc.DocumentElement = elem
		}
	case "head":
		p.headElement = elem
	case "body":
		p.bodyElement = elem
	}

	// Void elements are immediately popped.
	if voidElements[tok.Data] {
		p.popCurrentElement()
	}
}

func (p *Parser) handleSelfClosingTag(tok Token) {
	elem := p.doc.CreateElement(tok.Data)
	for k, v := range tok.Attributes {
		elem.SetAttribute(k, v)
	}
	p.insertElement(elem)
	p.popCurrentElement()
}

func (p *Parser) handleEndTag(tok Token) {
	// Pop open elements until we find the matching element.
	for i := len(p.openElements) - 1; i >= 0; i-- {
		if p.openElements[i].LocalName == tok.Data {
			p.openElements = p.openElements[:i]
			// Reset tokenizer to data state after closing raw text elements.
			if rawTextElements[tok.Data] || rcdataElements[tok.Data] {
				p.tokenizer.state = stateData
			}
			return
		}
	}
}

func (p *Parser) handleCharacter(tok Token) {
	text := p.doc.CreateTextNode(tok.Data)
	_ = p.currentNode().AppendChild(&text.Node)
}

func (p *Parser) handleComment(tok Token) {
	comment := p.doc.CreateComment(tok.Data)
	_ = p.currentNode().AppendChild(&comment.Node)
}

// insertElement appends elem to the current node and pushes it onto the open-
// element stack.  Callers are responsible for popping void elements immediately.
func (p *Parser) insertElement(elem *dom.Element) {
	_ = p.currentNode().AppendChild(&elem.Node)
	p.openElements = append(p.openElements, elem)
}

// currentNode returns the current insertion point (top of the open-element stack,
// or the document if the stack is empty).
func (p *Parser) currentNode() *dom.Node {
	if len(p.openElements) == 0 {
		return &p.doc.Node
	}
	return &p.openElements[len(p.openElements)-1].Node
}

// popCurrentElement removes the top element from the stack.
func (p *Parser) popCurrentElement() {
	if len(p.openElements) > 0 {
		p.openElements = p.openElements[:len(p.openElements)-1]
	}
}

// ── Streaming API ──────────────────────────────────────────────────────

// NewParser creates a streaming HTML parser that emits tokens incrementally.
// Call NextToken() to pull tokens one at a time, process them with
// ProcessToken(), and check Document() for the result.
func NewParser(html string) *Parser {
	return &Parser{
		tokenizer:  NewTokenizer(html),
		doc:        dom.NewDocument(),
		framesetOK: true,
	}
}

// NewStreamingParser creates a Parser for incremental (chunked) HTML parsing.
// The provided doc will be populated as chunks arrive via Write().
// Call PreloadCh() to obtain the channel that receives discovered subresource
// URLs during incremental parsing.
func NewStreamingParser(doc *dom.Document) *Parser {
	return &Parser{
		tokenizer:  NewTokenizer(""),
		doc:        doc,
		framesetOK: true,
		preloadCh:  make(chan PreloadURL, 50),
	}
}

// PreloadCh returns the channel on which discovered subresource URLs are sent
// during incremental parsing. The channel is closed by Finish().
func (p *Parser) PreloadCh() chan PreloadURL {
	return p.preloadCh
}

// NextToken returns the next token from the tokenizer. Returns TokenEOF when
// input is exhausted. Callers should call ProcessToken() with each token to
// build the DOM incrementally.
func (p *Parser) NextToken() Token {
	return p.tokenizer.nextToken()
}

// ProcessToken feeds a token to the tree construction algorithm. Call this
// with each token returned by NextToken() to build the DOM incrementally.
func (p *Parser) ProcessToken(tok Token) {
	p.processToken(tok)
}

// Tokenizer returns the underlying tokenizer so callers can switch text modes
// if they are doing their own tree construction.
func (p *Parser) Tokenizer() *Tokenizer {
	return p.tokenizer
}

// Document returns the document being built.
func (p *Parser) Document() *dom.Document {
	return p.doc
}

// ParseHTMLStream parses HTML incrementally through a parser that emits
// tokens one at a time. This is the primary streaming entry point.
// Call p.NextToken() and p.ProcessToken() in a loop until TokenEOF.
func ParseHTMLStream(html string) *Parser {
	p := NewParser(html)
	return p
}

// Write feeds a chunk of raw HTML bytes into the streaming parser.
// It appends the data to the internal buffer, extends the tokenizer input,
// and incrementally tokenizes all newly available tokens.  Subresource URLs
// discovered in <link>, <img>, and <script> tags are sent to the channel
// returned by PreloadCh().
func (p *Parser) Write(data []byte) {
	if len(data) == 0 {
		return
	}
	// Accumulate raw bytes for Finish().
	p.rawBuf.Write(data)

	// Append to tokenizer input as runes and resume tokenization.
	runes := []rune(string(data))
	p.tokenizer.AppendInput(runes)
	p.parseChunk()

	// Scan the raw chunk for preload URLs using simple regex matching.
	// This runs in parallel with tokenization and discovers URLs before the
	// full DOM tree is built.
	if p.preloadCh != nil {
		p.scanPreloadURLs(data)
	}
}

// Finish signals that no more data will be written and completes the parse.
// It drains any remaining tokens and closes the preload channel.
func (p *Parser) Finish() {
	p.finished = true
	// Drain any remaining tokens.
	p.parseChunk()
	// Close the preload channel so consumers know scanning is complete.
	if p.preloadCh != nil {
		close(p.preloadCh)
	}
}

// parseChunk tokenizes all currently available input without blocking.
// It stops when the tokenizer exhausts its input (returns TokenEOF).
func (p *Parser) parseChunk() {
	for {
		tok := p.tokenizer.nextToken()
		p.processToken(tok)
		if tok.Type == TokenEOF {
			break
		}
	}
}

// preloadTagPatterns matches <link>, <img>, and <script> open tags and
// captures the relevant URL attribute (href or src).
var (
	preloadLinkRE   = regexp.MustCompile(`(?i)<link[^>]*\srel\s*=\s*["']?stylesheet["']?[^>]*\shref\s*=\s*["']([^"']+)["']`)
	preloadLinkRE2  = regexp.MustCompile(`(?i)<link[^>]*\shref\s*=\s*["']([^"']+)["'][^>]*\srel\s*=\s*["']?stylesheet["']?`)
	preloadScriptRE = regexp.MustCompile(`(?i)<script[^>]*\ssrc\s*=\s*["']([^"']+)["']`)
	preloadImgRE    = regexp.MustCompile(`(?i)<img[^>]*\ssrc\s*=\s*["']([^"']+)["']`)
)

// scanPreloadURLs scans a chunk of HTML for subresource URLs and sends them
// to the preload channel.
func (p *Parser) scanPreloadURLs(chunk []byte) {
	chunkStr := string(chunk)

	// <link rel="stylesheet" href="...">
	for _, re := range []*regexp.Regexp{preloadLinkRE, preloadLinkRE2} {
		for _, m := range re.FindAllStringSubmatch(chunkStr, -1) {
			if len(m) > 1 && m[1] != "" {
				select {
				case p.preloadCh <- PreloadURL{URL: m[1], Type: "css"}:
				default:
					// Channel full — drop to avoid blocking the parser.
				}
			}
		}
	}

	// <script src="...">
	for _, m := range preloadScriptRE.FindAllStringSubmatch(chunkStr, -1) {
		if len(m) > 1 && m[1] != "" {
			select {
			case p.preloadCh <- PreloadURL{URL: m[1], Type: "js"}:
			default:
			}
		}
	}

	// <img src="...">
	for _, m := range preloadImgRE.FindAllStringSubmatch(chunkStr, -1) {
		if len(m) > 1 && m[1] != "" {
			select {
			case p.preloadCh <- PreloadURL{URL: m[1], Type: "img"}:
			default:
			}
		}
	}
}
