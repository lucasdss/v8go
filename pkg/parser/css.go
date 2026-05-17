// Package parser implements the HTML and CSS parsing engines for the browser.
package parser

import (
	"fmt"
	"strconv"
	"strings"
)

// ----------------------------- CSS Token types -------------------------------

// CSSTokenType identifies the type of a CSS token.
// See CSS Syntax § 4 "Tokenization".
type CSSTokenType int

// CSSTokenType constants.
const (
	CSSTokenIdent      CSSTokenType = iota // identifier
	CSSTokenFunction                       // function-token: ident(
	CSSTokenAt                             // @keyword
	CSSTokenHash                           // #name
	CSSTokenString                         // "…" or '…'
	CSSTokenURL                            // url(…)
	CSSTokenDelim                          // single character delimiter
	CSSTokenNumber                         // number
	CSSTokenPercentage                     // number%
	CSSTokenDimension                      // number<ident>
	CSSTokenWhitespace                     // whitespace
	CSSTokenColon                          // :
	CSSTokenSemicolon                      // ;
	CSSTokenComma                          // ,
	CSSTokenLBracket                       // [
	CSSTokenRBracket                       // ]
	CSSTokenLParen                         // (
	CSSTokenRParen                         // )
	CSSTokenLBrace                         // {
	CSSTokenRBrace                         // }
	CSSTokenCDO                            // <!--
	CSSTokenCDC                            // -->
	CSSTokenEOF                            // end of input
	CSSTokenBadString                      // unclosed string
)

// CSSToken is a single CSS token.
type CSSToken struct {
	Type  CSSTokenType
	Value string // raw text value
	Unit  string // for dimension tokens
}

// ----------------------------- CSS Tokenizer ---------------------------------

// CSSTokenizer tokenizes a CSS string following CSS Syntax Level 3.
type CSSTokenizer struct {
	input []rune
	pos   int
}

// NewCSSTokenizer creates a tokenizer for the given CSS source.
func NewCSSTokenizer(input string) *CSSTokenizer {
	return &CSSTokenizer{input: []rune(input)}
}

func (t *CSSTokenizer) peek() (rune, bool) {
	if t.pos >= len(t.input) {
		return 0, false
	}
	return t.input[t.pos], true
}

func (t *CSSTokenizer) consume() (rune, bool) {
	if t.pos >= len(t.input) {
		return 0, false
	}
	r := t.input[t.pos]
	t.pos++
	return r, true
}

// TokenizeCSS returns all tokens from the CSS input.
func (t *CSSTokenizer) TokenizeCSS() []CSSToken {
	var tokens []CSSToken
	for {
		tok := t.nextCSSToken()
		tokens = append(tokens, tok)
		if tok.Type == CSSTokenEOF {
			break
		}
	}
	return tokens
}

func (t *CSSTokenizer) nextCSSToken() CSSToken {
	r, ok := t.peek()
	if !ok {
		return CSSToken{Type: CSSTokenEOF}
	}

	// Whitespace.
	if isWSRune(r) {
		return t.consumeWhitespace()
	}

	// Comments.
	if r == '/' {
		if t.pos+1 < len(t.input) && t.input[t.pos+1] == '*' {
			t.consumeComment()
			return t.nextCSSToken()
		}
	}

	switch r {
	case '"', '\'':
		return t.consumeString(r)
	case '#':
		t.pos++
		return t.consumeHash()
	case '(':
		t.pos++
		return CSSToken{Type: CSSTokenLParen, Value: "("}
	case ')':
		t.pos++
		return CSSToken{Type: CSSTokenRParen, Value: ")"}
	case '[':
		t.pos++
		return CSSToken{Type: CSSTokenLBracket, Value: "["}
	case ']':
		t.pos++
		return CSSToken{Type: CSSTokenRBracket, Value: "]"}
	case '{':
		t.pos++
		return CSSToken{Type: CSSTokenLBrace, Value: "{"}
	case '}':
		t.pos++
		return CSSToken{Type: CSSTokenRBrace, Value: "}"}
	case ':':
		t.pos++
		return CSSToken{Type: CSSTokenColon, Value: ":"}
	case ';':
		t.pos++
		return CSSToken{Type: CSSTokenSemicolon, Value: ";"}
	case ',':
		t.pos++
		return CSSToken{Type: CSSTokenComma, Value: ","}
	case '@':
		t.pos++
		ident := t.consumeIdent()
		return CSSToken{Type: CSSTokenAt, Value: ident}
	case '<':
		if t.pos+3 < len(t.input) &&
			string(t.input[t.pos:t.pos+4]) == "<!--" {
			t.pos += 4
			return CSSToken{Type: CSSTokenCDO, Value: "<!--"}
		}
		t.pos++
		return CSSToken{Type: CSSTokenDelim, Value: "<"}
	case '-':
		// Could be a number, ident, or custom property (--name).
		if t.pos+1 < len(t.input) {
			next := t.input[t.pos+1]
			if next == '-' {
				// Custom property name (--*).
				t.pos += 2
				return CSSToken{Type: CSSTokenIdent, Value: "--" + t.consumeIdent()}
			}
			if next >= '0' && next <= '9' {
				return t.consumeNumeric()
			}
			if isIdentStart(next) {
				return t.consumeIdentOrFunction()
			}
		}
		t.pos++
		return CSSToken{Type: CSSTokenDelim, Value: "-"}
	default:
		if r >= '0' && r <= '9' {
			return t.consumeNumeric()
		}
		if isIdentStart(r) {
			return t.consumeIdentOrFunction()
		}
		t.pos++
		return CSSToken{Type: CSSTokenDelim, Value: string(r)}
	}
}

func (t *CSSTokenizer) consumeWhitespace() CSSToken {
	var buf strings.Builder
	for {
		r, ok := t.peek()
		if !ok || !isWSRune(r) {
			break
		}
		buf.WriteRune(r)
		t.pos++
	}
	return CSSToken{Type: CSSTokenWhitespace, Value: buf.String()}
}

func (t *CSSTokenizer) consumeComment() {
	t.pos += 2 // consume "/*"
	for t.pos < len(t.input)-1 {
		if t.input[t.pos] == '*' && t.input[t.pos+1] == '/' {
			t.pos += 2
			return
		}
		t.pos++
	}
	t.pos = len(t.input)
}

func (t *CSSTokenizer) consumeString(quote rune) CSSToken {
	t.pos++ // opening quote
	var buf strings.Builder
	for {
		r, ok := t.consume()
		if !ok || r == '\n' {
			return CSSToken{Type: CSSTokenBadString, Value: buf.String()}
		}
		if r == quote {
			return CSSToken{Type: CSSTokenString, Value: buf.String()}
		}
		if r == '\\' {
			escaped, ok2 := t.consume()
			if ok2 {
				buf.WriteRune(escaped)
			}
			continue
		}
		buf.WriteRune(r)
	}
}

func (t *CSSTokenizer) consumeHash() CSSToken {
	var buf strings.Builder
	for {
		r, ok := t.peek()
		if !ok || (!isIdentChar(r) && r != '-') {
			break
		}
		buf.WriteRune(r)
		t.pos++
	}
	return CSSToken{Type: CSSTokenHash, Value: buf.String()}
}

func (t *CSSTokenizer) consumeIdent() string {
	var buf strings.Builder
	for {
		r, ok := t.peek()
		if !ok || !isIdentChar(r) {
			break
		}
		buf.WriteRune(r)
		t.pos++
	}
	return buf.String()
}

func (t *CSSTokenizer) consumeIdentOrFunction() CSSToken {
	ident := t.consumeIdent()
	if r, ok := t.peek(); ok && r == '(' {
		t.pos++
		return CSSToken{Type: CSSTokenFunction, Value: ident}
	}
	return CSSToken{Type: CSSTokenIdent, Value: ident}
}

func (t *CSSTokenizer) consumeNumeric() CSSToken {
	var buf strings.Builder
	// Leading sign.
	if r, ok := t.peek(); ok && (r == '+' || r == '-') {
		buf.WriteRune(r)
		t.pos++
	}
	for {
		r, ok := t.peek()
		if !ok || r < '0' || r > '9' {
			break
		}
		buf.WriteRune(r)
		t.pos++
	}
	// Decimal part.
	if r, ok := t.peek(); ok && r == '.' {
		if t.pos+1 < len(t.input) && t.input[t.pos+1] >= '0' && t.input[t.pos+1] <= '9' {
			buf.WriteRune(r)
			t.pos++
			for {
				r2, ok2 := t.peek()
				if !ok2 || r2 < '0' || r2 > '9' {
					break
				}
				buf.WriteRune(r2)
				t.pos++
			}
		}
	}

	num := buf.String()

	// Check for dimension or percentage.
	if r, ok := t.peek(); ok {
		if r == '%' {
			t.pos++
			return CSSToken{Type: CSSTokenPercentage, Value: num + "%"}
		}
		if isIdentStart(r) {
			unit := t.consumeIdent()
			return CSSToken{Type: CSSTokenDimension, Value: num, Unit: unit}
		}
	}
	return CSSToken{Type: CSSTokenNumber, Value: num}
}

func isWSRune(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' }
func isIdentStart(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r > 0x80
}
func isIdentChar(r rune) bool { return isIdentStart(r) || (r >= '0' && r <= '9') || r == '-' }

// ----------------------------- Media & Feature Query Evaluation --------------

// ViewportDimensions holds the browser viewport size in CSS pixels.
type ViewportDimensions struct {
	Width  float64
	Height float64
}

// EvaluateMediaQuery evaluates a CSS @media query against the given viewport.
// Supported queries:
//   - "all", "" → always true
//   - "screen" → true (screen media)
//   - "print" → false (no print support)
//   - "(min-width: Npx)" → true if vp.Width >= N
//   - "(max-width: Npx)" → true if vp.Width <= N
//   - "only screen and (min-width: ...)" → strip "only" prefix and evaluate
//   - Combinations with "and"
func EvaluateMediaQuery(query string, vp ViewportDimensions) bool {
	query = strings.TrimSpace(query)
	if query == "" || query == "all" {
		return true
	}

	// Strip "only" prefix (CSS3: "only screen and ...").
	lower := strings.ToLower(query)
	if strings.HasPrefix(lower, "only ") {
		query = query[5:]
		lower = lower[5:]
	}

	// Split on " and " to evaluate each condition.
	parts := splitMediaQuery(lower)
	mediaTypeOK := false
	allConditionsOK := true

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Media type keywords.
		switch part {
		case "all":
			mediaTypeOK = true
			continue
		case "screen":
			mediaTypeOK = true
			continue
		case "print":
			return false // no print support
		case "not":
			// "not" negates the whole query — simplified: if present, return false.
			return false
		}

		// Feature queries: (min-width: Npx), (max-width: Npx), etc.
		if strings.HasPrefix(part, "(") && strings.HasSuffix(part, ")") {
			feature := part[1 : len(part)-1]
			if !evaluateMediaFeature(feature, vp) {
				allConditionsOK = false
			}
			continue
		}
	}

	if !mediaTypeOK && !strings.Contains(lower, "(") {
		// Unknown media type with no feature queries — default to false.
		return false
	}

	return allConditionsOK
}

// splitMediaQuery splits a media query on " and " while respecting parentheses.
func splitMediaQuery(query string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(query); i++ {
		switch query[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && i+5 <= len(query) && strings.ToLower(query[i:i+5]) == " and " {
			parts = append(parts, query[start:i])
			start = i + 5
			i += 4
		}
	}
	parts = append(parts, query[start:])
	return parts
}

// evaluateMediaFeature evaluates a single media feature like "min-width: 768px".
func evaluateMediaFeature(feature string, vp ViewportDimensions) bool {
	parts := strings.SplitN(feature, ":", 2)
	if len(parts) != 2 {
		return false
	}
	name := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])

	switch name {
	case "min-width":
		w := extractPx(value)
		return w <= 0 || vp.Width >= w
	case "max-width":
		w := extractPx(value)
		return w <= 0 || vp.Width <= w
	case "min-height":
		h := extractPx(value)
		return h <= 0 || vp.Height >= h
	case "max-height":
		h := extractPx(value)
		return h <= 0 || vp.Height <= h
	case "width":
		w := extractPx(value)
		return w <= 0 || vp.Width == w
	case "height":
		h := extractPx(value)
		return h <= 0 || vp.Height == h
	case "orientation":
		if value == "landscape" {
			return vp.Width > vp.Height
		}
		if value == "portrait" {
			return vp.Height >= vp.Width
		}
		return false
	default:
		// Unknown features default to true (permissive).
		return true
	}
}

// extractPx parses a pixel value from a CSS dimension string like "768px" or "768".
func extractPx(s string) float64 {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "px")
	s = strings.TrimSuffix(s, "PX")
	var val float64
	if _, err := fmt.Sscanf(s, "%f", &val); err != nil {
		return 0
	}
	return val
}

// EvaluateSupports checks if a CSS @supports condition is supported by this browser.
// Known supported features:
//   - display: flex
//   - border-radius
//   - box-shadow
//   - rgba() color function
//   - calc()
// Returns true for unknown features (permissive default).
func EvaluateSupports(condition string) bool {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return true
	}

	// "not (...)" — negate the inner condition.
	if strings.HasPrefix(strings.ToLower(condition), "not ") {
		inner := strings.TrimSpace(condition[4:])
		inner = strings.TrimPrefix(inner, "(")
		inner = strings.TrimSuffix(inner, ")")
		return !EvaluateSupports(inner)
	}

	inner := condition
	inner = strings.TrimPrefix(inner, "(")
	inner = strings.TrimSuffix(inner, ")")
	inner = strings.TrimSpace(inner)

	lower := strings.ToLower(inner)

	// Property: value checks.
	if strings.Contains(lower, "display") && strings.Contains(lower, "flex") {
		return true
	}
	if strings.Contains(lower, "display") && strings.Contains(lower, "grid") {
		return true
	}
	if strings.Contains(lower, "border-radius") {
		return true
	}
	if strings.Contains(lower, "box-shadow") {
		return true
	}
	if strings.Contains(lower, "rgba") {
		return true
	}
	if strings.Contains(lower, "calc") {
		return true
	}
	if strings.Contains(lower, "transform") {
		return true
	}

	// Permissive default: assume feature is supported.
	return true
}

// ----------------------------- CSSOM -----------------------------------------

// StyleSheet represents a parsed CSS stylesheet.
// See CSSOM § 6 "CSS style sheets".
type StyleSheet struct {
	// Href is the URL from which the stylesheet was loaded, if any.
	Href string
	// Rules is the list of CSS rules in declaration order.
	Rules []*CSSRule
	// MediaQuery is the media attribute value from a <link> element,
	// e.g. "screen and (min-width: 768px)".  Empty means "all".
	MediaQuery string
}

// CSSRule represents a single CSS rule (qualified rule or at-rule).
// See CSSOM § 6.1.
type CSSRule struct {
	// Type is the rule type ("style", "import", "media", "keyframes", etc.).
	Type string
	// Selector holds the selector text for qualified rules.
	Selector string
	// Declarations holds the property→value map for style rules.
	Declarations []*CSSDeclaration
	// AtText is the text following the "@" for at-rules (e.g. "media screen").
	AtText string
	// ChildRules holds nested rules (e.g. inside @media).
	ChildRules []*CSSRule
}

// CSSDeclaration is a property–value pair with an optional !important flag.
// See CSSOM § 4.
type CSSDeclaration struct {
	Property  string
	Value     string
	Important bool
}

// String returns a CSS serialization of the declaration.
func (d *CSSDeclaration) String() string {
	if d.Important {
		return fmt.Sprintf("%s: %s !important", d.Property, d.Value)
	}
	return fmt.Sprintf("%s: %s", d.Property, d.Value)
}

// isDeclarationAtRule returns true for at-rules whose block contains
// CSS declarations rather than nested style rules.
func isDeclarationAtRule(ruleType string) bool {
	switch strings.ToLower(ruleType) {
	case "font-face", "keyframes", "-webkit-keyframes",
		"page", "counter-style", "font-feature-values",
		"property", "scroll-timeline":
		return true
	}
	return false
}

// ParseCSS parses a CSS stylesheet and returns a *StyleSheet.
func ParseCSS(input string) *StyleSheet {
	p := &cssParser{tokens: NewCSSTokenizer(input).TokenizeCSS()}
	return p.parseStyleSheet()
}

// cssParser consumes a CSS token stream.
type cssParser struct {
	tokens []CSSToken
	pos    int
}

func (p *cssParser) peek() (CSSToken, bool) {
	for p.pos < len(p.tokens) {
		if p.tokens[p.pos].Type != CSSTokenWhitespace {
			return p.tokens[p.pos], true
		}
		p.pos++
	}
	return CSSToken{Type: CSSTokenEOF}, false
}

func (p *cssParser) consume() (CSSToken, bool) {
	// Skip whitespace.
	for p.pos < len(p.tokens) && p.tokens[p.pos].Type == CSSTokenWhitespace {
		p.pos++
	}
	if p.pos >= len(p.tokens) {
		return CSSToken{Type: CSSTokenEOF}, false
	}
	tok := p.tokens[p.pos]
	p.pos++
	return tok, true
}

func (p *cssParser) parseStyleSheet() *StyleSheet {
	ss := &StyleSheet{}
	for {
		tok, ok := p.peek()
		if !ok {
			break
		}
		if tok.Type == CSSTokenEOF {
			break
		}
		if tok.Type == CSSTokenCDO || tok.Type == CSSTokenCDC {
			p.consume()
			continue
		}
		if tok.Type == CSSTokenAt {
			rule := p.parseAtRule()
			if rule != nil {
				ss.Rules = append(ss.Rules, rule)
			}
			continue
		}
		rule := p.parseQualifiedRule()
		if rule != nil {
			ss.Rules = append(ss.Rules, rule)
		}
	}
	return ss
}

func (p *cssParser) parseAtRule() *CSSRule {
	tok, _ := p.consume() // the @keyword token
	rule := &CSSRule{Type: tok.Value}

	var prelude strings.Builder
	depth := 0
	for {
		t, ok := p.peek()
		if !ok || t.Type == CSSTokenEOF {
			break
		}
		if t.Type == CSSTokenLBrace {
			depth++
			p.consume()
			if depth == 1 {
				// Parse block.
				rule.AtText = strings.TrimSpace(prelude.String())
				// @font-face, @keyframes, @page, @counter-style contain
				// declarations, not nested rules.
				if isDeclarationAtRule(rule.Type) {
					rule.Declarations = p.parseDeclarationBlock()
				} else {
					rule.ChildRules = p.parseRuleListBlock()
				}
				return rule
			}
			prelude.WriteString("{")
			continue
		}
		if t.Type == CSSTokenSemicolon {
			p.consume()
			rule.AtText = strings.TrimSpace(prelude.String())
			return rule
		}
		tc, _ := p.consume()
		prelude.WriteString(tc.Value)
	}
	rule.AtText = strings.TrimSpace(prelude.String())
	return rule
}

func (p *cssParser) parseQualifiedRule() *CSSRule {
	var selectorBuf strings.Builder
	lastWasMeaningful := false
	for p.pos < len(p.tokens) {
		tok := p.tokens[p.pos]

		if tok.Type == CSSTokenWhitespace {
			// Track that we saw whitespace between meaningful tokens.
			if lastWasMeaningful {
				// Peek ahead: if there's a non-whitespace, non-{ token after this
				// whitespace, emit a space.
				for j := p.pos + 1; j < len(p.tokens); j++ {
					if p.tokens[j].Type == CSSTokenWhitespace {
						continue
					}
					if p.tokens[j].Type == CSSTokenLBrace || p.tokens[j].Type == CSSTokenEOF {
						break
					}
					selectorBuf.WriteString(" ")
					break
				}
			}
			p.pos++
			continue
		}

		if tok.Type == CSSTokenLBrace {
			p.pos++ // consume the {
			break
		}
		if tok.Type == CSSTokenEOF {
			break
		}

		lastWasMeaningful = true
		switch tok.Type {
		case CSSTokenHash:
			selectorBuf.WriteString("#")
			selectorBuf.WriteString(tok.Value)
		default:
			selectorBuf.WriteString(tok.Value)
		}
		p.pos++
	}

	rule := &CSSRule{
		Type:     "style",
		Selector: strings.TrimSpace(selectorBuf.String()),
	}
	rule.Declarations = p.parseDeclarationBlock()
	return rule
}

func (p *cssParser) parseRuleListBlock() []*CSSRule {
	var rules []*CSSRule
	for {
		t, ok := p.peek()
		if !ok || t.Type == CSSTokenEOF || t.Type == CSSTokenRBrace {
			if ok && t.Type == CSSTokenRBrace {
				p.consume()
			}
			break
		}
		if t.Type == CSSTokenAt {
			rule := p.parseAtRule()
			if rule != nil {
				rules = append(rules, rule)
			}
			continue
		}
		rule := p.parseQualifiedRule()
		if rule != nil {
			rules = append(rules, rule)
		}
	}
	return rules
}

func (p *cssParser) parseDeclarationBlock() []*CSSDeclaration {
	var decls []*CSSDeclaration
	for {
		t, ok := p.peek()
		if !ok || t.Type == CSSTokenEOF || t.Type == CSSTokenRBrace {
			if ok && t.Type == CSSTokenRBrace {
				p.consume()
			}
			break
		}
		if t.Type == CSSTokenSemicolon {
			p.consume()
			continue
		}
		decl := p.parseDeclaration()
		if decl != nil {
			decls = append(decls, decl)
		}
	}
	return decls
}

func (p *cssParser) parseDeclaration() *CSSDeclaration {
	propTok, ok := p.consume()
	if !ok || propTok.Type != CSSTokenIdent {
		// Skip until ";" or "}".
		for {
			t, ok2 := p.peek()
			if !ok2 || t.Type == CSSTokenSemicolon || t.Type == CSSTokenRBrace || t.Type == CSSTokenEOF {
				break
			}
			p.consume()
		}
		return nil
	}

	colonTok, ok := p.consume()
	if !ok || colonTok.Type != CSSTokenColon {
		return nil
	}

	var valueBuf strings.Builder
	lastWasFunc := false // true if the previously written token was a function
	for {
		t, ok2 := p.peek()
		if !ok2 || t.Type == CSSTokenSemicolon || t.Type == CSSTokenRBrace || t.Type == CSSTokenEOF {
			break
		}
		tc, _ := p.consume()
		// Tokens that need a space before them when preceded by
		// another value token. Punctuation (delim, comma, function
		// opening) does not need a preceding space.
		needsSpace := tc.Type == CSSTokenIdent ||
			tc.Type == CSSTokenHash ||
			tc.Type == CSSTokenNumber ||
			tc.Type == CSSTokenDimension ||
			tc.Type == CSSTokenPercentage ||
			tc.Type == CSSTokenAt ||
			tc.Type == CSSTokenString ||
			tc.Type == CSSTokenURL
		if valueBuf.Len() > 0 && needsSpace && !lastWasFunc {
			// Do not insert a space after "!" so that "!important" stays joined.
			if v := valueBuf.String(); !strings.HasSuffix(v, "!") {
				valueBuf.WriteByte(' ')
			}
		}
		lastWasFunc = tc.Type == CSSTokenFunction
		// Reconstruct certain token types in the value.
		switch tc.Type {
		case CSSTokenHash:
			valueBuf.WriteString("#" + tc.Value)
		case CSSTokenFunction:
			valueBuf.WriteString(tc.Value + "(")
		case CSSTokenAt:
			valueBuf.WriteString("@" + tc.Value)
		case CSSTokenPercentage:
			valueBuf.WriteString(tc.Value)
		case CSSTokenDimension:
			valueBuf.WriteString(tc.Value + tc.Unit)
		default:
			valueBuf.WriteString(tc.Value)
		}
	}

	value := strings.TrimSpace(valueBuf.String())
	important := false
	if strings.HasSuffix(strings.ToLower(value), "!important") {
		important = true
		value = strings.TrimSpace(value[:len(value)-len("!important")])
		value = strings.TrimSuffix(value, "!")
		value = strings.TrimSpace(value)
	}

	return &CSSDeclaration{
		Property:  strings.ToLower(propTok.Value),
		Value:     value,
		Important: important,
	}
}

// Keyframes stores parsed @keyframes rule data indexed by stop percentage.
type Keyframes struct {
	Name  string
	Stops map[float64][]CSSDeclaration // percentage → properties
}

// ParseKeyframesToStops parses @keyframes CSS text and returns percentage-indexed
// stops suitable for the animation engine. It uses ParseKeyframes from css_enhance.go
// and normalizes "from"/"to" selectors into 0%/100%.
func ParseKeyframesToStops(cssText string) *Keyframes {
	rule := ParseKeyframes(cssText)
	kf := &Keyframes{
		Name:  rule.Name,
		Stops: make(map[float64][]CSSDeclaration, len(rule.Keyframes)),
	}
	for sel, block := range rule.Keyframes {
		var pct float64
		switch strings.ToLower(strings.TrimSpace(sel)) {
		case "from":
			pct = 0
		case "to":
			pct = 100
		default:
			sel = strings.TrimSuffix(strings.TrimSpace(sel), "%")
			pct, _ = strconv.ParseFloat(sel, 64)
		}
		decls := make([]CSSDeclaration, len(block.Declarations))
		copy(decls, block.Declarations)
		kf.Stops[pct] = decls
	}
	return kf
}
