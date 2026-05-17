// lexer.go — JavaScript tokenizer.
//
// A hand-written lexer that processes []byte source into a token stream.
// Supports ES5+ tokens: keywords, identifiers, numbers (decimal, hex, octal, binary),
// strings (single, double, template), operators (including compound like ===, =>),
// comments (line and block), and regular expression literals.
package js

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TokenKind classifies each token the lexer produces.
type TokenKind int

const (
	TokEOF TokenKind = iota

	// Literals
	TokNumber
	TokBigInt
	TokString
	TokTrue
	TokFalse
	TokNull
	TokUndefined

	// Identifiers and keywords
	TokIdentifier

	// Punctuation
	TokDot
	TokComma
	TokSemicolon
	TokQuestion
	TokQuestionDot // ?.
	TokColon
	TokLParen
	TokRParen
	TokLBrace
	TokRBrace
	TokLBracket
	TokRBracket
	TokArrow // =>

	// Operators
	TokPlus
	TokMinus
	TokStar
	TokStarStar // **
	TokSlash
	TokPercent
	TokPlusPlus
	TokMinusMinus

	// Comparison
	TokEqEq     // ==
	TokNotEq    // !=
	TokEqEqEq   // ===
	TokNotEqEq  // !==
	TokLt       // <
	TokGt       // >
	TokLtEq     // <=
	TokGtEq     // >=

	// Assignment
	TokEq          // =
	TokPlusEq      // +=
	TokMinusEq     // -=
	TokStarEq      // *=
	TokSlashEq     // /=
	TokPercentEq   // %=
	TokAndAndEq    // &&=
	TokPipePipeEq  // ||=
	TokQuestionQuestionEq // ??=

	// Logical
	TokAndAnd  // &&
	TokPipePipe // ||
	TokBang     // !

	// Nullish coalescing
	TokQuestionQuestion // ??

	// Bitwise
	TokAnd   // &
	TokPipe  // |
	TokCaret // ^
	TokTilde // ~
	TokLtLt  // <<
	TokGtGt  // >>
	TokGtGtGt // >>>

	// Keywords
	TokVar
	TokLet
	TokConst
	TokIf
	TokElse
	TokFor
	TokWhile
	TokDo
	TokReturn
	TokFunction
	TokNew
	TokThis
	TokTypeof
	TokInstanceof
	TokIn
	TokOf
	TokDelete
	TokVoid
	TokThrow
	TokTry
	TokCatch
	TokFinally
	TokSwitch
	TokCase
	TokDefault
	TokBreak
	TokContinue
	TokClass
	TokExtends
	TokSuper
	TokGet
	TokSet
	TokYield
	TokAsync
	TokAwait
	TokImport
	TokExport
	TokFrom
	TokAs
	TokWith

	// Template literal tokens
	TokTemplateStart   // backtick that opens a template literal
	TokTemplateExprEnd // } that closes a ${expr} inside a template literal
	TokTemplateEnd     // backtick that closes a template literal

	TokDotDotDot // ... (spread operator)
	TokRegExp    // /pattern/flags
)

var tokenKindNames = map[TokenKind]string{
	TokEOF:       "EOF",
	TokNumber:    "Number",
	TokBigInt:    "BigInt",
	TokString:    "String",
	TokTrue:      "true",
	TokFalse:     "false",
	TokNull:      "null",
	TokUndefined: "undefined",
	TokIdentifier: "Identifier",
	TokDot:       ".",
	TokComma:     ",",
	TokSemicolon: ";",
	TokQuestion:    "?",
	TokQuestionDot: "?.",
	TokColon:       ":",
	TokLParen:    "(",
	TokRParen:    ")",
	TokLBrace:    "{",
	TokRBrace:    "}",
	TokLBracket:  "[",
	TokRBracket:  "]",
	TokArrow:     "=>",
	TokDotDotDot: "...",
	TokRegExp:    "RegExp",
	TokPlus:      "+",
	TokMinus:     "-",
	TokStar:      "*",
	TokStarStar:  "**",
	TokSlash:     "/",
	TokPercent:   "%",
	TokPlusPlus:  "++",
	TokMinusMinus: "--",
	TokEqEq:      "==",
	TokNotEq:     "!=",
	TokEqEqEq:    "===",
	TokNotEqEq:   "!==",
	TokLt:        "<",
	TokGt:        ">",
	TokLtEq:      "<=",
	TokGtEq:      ">=",
	TokEq:        "=",
	TokPlusEq:    "+=",
	TokMinusEq:   "-=",
	TokStarEq:    "*=",
	TokSlashEq:   "/=",
	TokPercentEq: "%=",
	TokAndAndEq:  "&&=",
	TokPipePipeEq: "||=",
	TokQuestionQuestionEq: "??=",
	TokAndAnd:    "&&",
	TokPipePipe:  "||",
	TokQuestionQuestion: "??",
	TokBang:      "!",
	TokAnd:       "&",
	TokPipe:      "|",
	TokCaret:     "^",
	TokTilde:     "~",
	TokLtLt:      "<<",
	TokGtGt:      ">>",
	TokGtGtGt:    ">>>",
	TokVar:       "var",
	TokLet:       "let",
	TokConst:     "const",
	TokIf:        "if",
	TokElse:      "else",
	TokFor:       "for",
	TokWhile:     "while",
	TokDo:        "do",
	TokReturn:    "return",
	TokFunction:  "function",
	TokNew:       "new",
	TokThis:      "this",
	TokTypeof:    "typeof",
	TokInstanceof: "instanceof",
	TokIn:        "in",
	TokOf:        "of",
	TokDelete:    "delete",
	TokVoid:      "void",
	TokThrow:     "throw",
	TokTry:       "try",
	TokCatch:     "catch",
	TokFinally:   "finally",
	TokSwitch:    "switch",
	TokCase:      "case",
	TokDefault:   "default",
	TokBreak:     "break",
	TokContinue:  "continue",
	TokClass:     "class",
	TokExtends:   "extends",
	TokSuper:     "super",
	TokGet:       "get",
	TokSet:       "set",
	TokYield:     "yield",
	TokAsync:     "async",
	TokAwait:     "await",
	TokImport:    "import",
	TokExport:    "export",
	TokFrom:      "from",
	TokAs:        "as",
	TokWith:      "with",
}

func (k TokenKind) String() string {
	if name, ok := tokenKindNames[k]; ok {
		return name
	}
	return fmt.Sprintf("TokenKind(%d)", k)
}

// Token represents a single lexical token.
type Token struct {
	Kind          TokenKind
	Value         string // Raw text for identifiers/strings, or number string.
	NumVal        float64
	Line          int
	Column        int
	StartPos      int
	EndPos        int
	IsLegacyOctal bool // true for legacy octal literals like 010 (forbidden in strict mode)
}

// Lexer tokenizes JavaScript source code.
type Lexer struct {
	src         []byte
	pos         int
	line        int
	column      int
	tokens      []Token
	quasiMode   bool // true while reading template quasi text (after backtick, not inside ${})
	braceDepth  int  // tracks {} nesting depth inside template expressions
	pendingTemplateEnd bool // true when we need to emit TokTemplateEnd next
	expectRegExp       bool // true when / should start a RegExp literal (after operators/punctuation)
	lastTokenKind      TokenKind // kind of the last non-comment token emitted
}

// NewLexer creates a new Lexer for the given source.
func NewLexer(src string) *Lexer {
	return &Lexer{
		src:    []byte(src),
		pos:    0,
		line:   1,
		column: 1,
	}
}

// Tokenize runs the full tokenization and returns all tokens.
func (l *Lexer) Tokenize() []Token {
	l.tokens = make([]Token, 0, len(l.src)/4)
	l.expectRegExp = true // start of program: / can start a regexp
	l.lastTokenKind = TokEOF
	for {
		tok := l.nextToken()
		l.updateRegExpContext(tok.Kind)
		l.tokens = append(l.tokens, tok)
		if tok.Kind == TokEOF {
			break
		}
	}
	return l.tokens
}

// updateRegExpContext tracks whether the next / should be interpreted as a RegExp literal.
// After tokens that can end an expression (identifier, number, string, ), ], ++, --,
// true, false, null, undefined, this, bigint), the next / is a division operator.
// After everything else (operators, (, [, {, etc.), the next / starts a RegExp.
func (l *Lexer) updateRegExpContext(kind TokenKind) {
	switch kind {
	// Tokens that CAN end a primary expression: next / is DIVISION
	case TokIdentifier, TokNumber, TokBigInt, TokString, TokRParen, TokRBracket,
		TokTrue, TokFalse, TokNull, TokUndefined,
		TokPlusPlus, TokMinusMinus,
		TokThis, TokRBrace, TokTemplateEnd:
		l.expectRegExp = false
	// Tokens that appear at the START of an expression: next / is REGEXP
	default:
		l.expectRegExp = true
	}
	l.lastTokenKind = kind
}

func (l *Lexer) nextToken() Token {
	// Do NOT skip whitespace when in quasi mode — template quasis preserve all whitespace.
	if !l.quasiMode {
		l.skipWhitespaceAndComments()
	}

	// If we just exited template mode, emit TokTemplateEnd.
	if l.pendingTemplateEnd {
		l.pendingTemplateEnd = false
		return Token{Kind: TokTemplateEnd, Value: "`", Line: l.line, Column: l.column, StartPos: l.pos, EndPos: l.pos}
	}

	// If in template quasi-reading mode, read the next quasi text segment.
	if l.quasiMode {
		return l.readTemplateQuasi()
	}

	if l.pos >= len(l.src) {
		return Token{Kind: TokEOF, Line: l.line, Column: l.column, StartPos: l.pos, EndPos: l.pos}
	}
	startPos := l.pos
	startLine := l.line
	startCol := l.column

	ch := l.src[l.pos]

	// String literals (single and double quote only; backtick handled below).
	if ch == '"' || ch == '\'' {
		tok := l.readString(ch)
		tok.Line = startLine
		tok.Column = startCol
		tok.StartPos = startPos
		tok.EndPos = l.pos
		return tok
	}

	// Template literal: backtick enters quasi-reading mode.
	if ch == '`' {
		startTok := Token{Kind: TokTemplateStart, Value: "`", Line: startLine, Column: startCol, StartPos: startPos, EndPos: startPos + 1}
		l.advance() // skip backtick
		// If the template is empty (`\`\``), emit TemplatesStart then a quasi with empty string.
		// Otherwise read the first quasi.
		l.quasiMode = true
		l.braceDepth = 0
		// We can't return two tokens. So we push the quasi-reading state and return TokTemplateStart.
		// The next call to nextToken() will read the first quasi because quasiMode is true.
		// But we need a token to return NOW. We'll store the quasi result for next time.
		// Actually, simpler: just return TokTemplateStart now, and let quasiMode handle the rest.
		return startTok
	}

	// Numbers.
	if ch >= '0' && ch <= '9' || (ch == '.' && l.pos+1 < len(l.src) && l.src[l.pos+1] >= '0' && l.src[l.pos+1] <= '9') {
		tok := l.readNumber()
		tok.Line = startLine
		tok.Column = startCol
		tok.StartPos = startPos
		tok.EndPos = l.pos
		return tok
	}

	// Identifiers and keywords.
	if isIdentStart(ch) {
		ident := l.readIdentifier()
		kind := lookupKeyword(ident)
		if kind != TokIdentifier {
			return Token{Kind: kind, Value: ident, Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		return Token{Kind: TokIdentifier, Value: ident, Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	}

	// Multi-character operators and single-character tokens.
	switch ch {
	case '.':
		// Check for spread operator: ...
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '.' && l.pos+2 < len(l.src) && l.src[l.pos+2] == '.' {
			l.advance()
			l.advance()
			l.advance()
			return Token{Kind: TokDotDotDot, Value: "...", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		l.advance()
		return Token{Kind: TokDot, Value: ".", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case ',':
		l.advance()
		return Token{Kind: TokComma, Value: ",", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case ';':
		l.advance()
		return Token{Kind: TokSemicolon, Value: ";", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case ':':
		l.advance()
		return Token{Kind: TokColon, Value: ":", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case '(':
		l.advance()
		return Token{Kind: TokLParen, Value: "(", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case ')':
		l.advance()
		return Token{Kind: TokRParen, Value: ")", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case '{':
		l.advance()
		if l.braceDepth > 0 {
			l.braceDepth++
		}
		return Token{Kind: TokLBrace, Value: "{", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case '}':
		l.advance()
		if l.braceDepth > 0 {
			l.braceDepth--
			if l.braceDepth == 0 {
				// This } closes the ${expr} — switch back to quasi mode.
				l.quasiMode = true
				return Token{Kind: TokTemplateExprEnd, Value: "}", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
			}
		}
		return Token{Kind: TokRBrace, Value: "}", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case '[':
		l.advance()
		return Token{Kind: TokLBracket, Value: "[", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case ']':
		l.advance()
		return Token{Kind: TokRBracket, Value: "]", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case '?':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '?' {
			l.advance()
			l.advance()
			if l.pos < len(l.src) && l.src[l.pos] == '=' {
				l.advance()
				return Token{Kind: TokQuestionQuestionEq, Value: "??=", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
			}
			return Token{Kind: TokQuestionQuestion, Value: "??", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		// Optional chaining: ?.
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '.' {
			l.advance()
			l.advance()
			return Token{Kind: TokQuestionDot, Value: "?.", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		l.advance()
		return Token{Kind: TokQuestion, Value: "?", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case '~':
		l.advance()
		return Token{Kind: TokTilde, Value: "~", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case '^':
		l.advance()
		return Token{Kind: TokCaret, Value: "^", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case '+':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '+' {
			l.advance()
			l.advance()
			return Token{Kind: TokPlusPlus, Value: "++", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '=' {
			l.advance()
			l.advance()
			return Token{Kind: TokPlusEq, Value: "+=", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		l.advance()
		return Token{Kind: TokPlus, Value: "+", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case '-':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '-' {
			l.advance()
			l.advance()
			return Token{Kind: TokMinusMinus, Value: "--", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '=' {
			l.advance()
			l.advance()
			return Token{Kind: TokMinusEq, Value: "-=", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		l.advance()
		return Token{Kind: TokMinus, Value: "-", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case '*':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '*' {
			l.advance()
			l.advance()
			return Token{Kind: TokStarStar, Value: "**", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '=' {
			l.advance()
			l.advance()
			return Token{Kind: TokStarEq, Value: "*=", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		l.advance()
		return Token{Kind: TokStar, Value: "*", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case '%':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '=' {
			l.advance()
			l.advance()
			return Token{Kind: TokPercentEq, Value: "%=", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		l.advance()
		return Token{Kind: TokPercent, Value: "%", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case '/':
		// Division vs. RegExp literal: check context.
		if l.expectRegExp {
			tok := l.scanRegExp()
			tok.Line = startLine
			tok.Column = startCol
			tok.StartPos = startPos
			tok.EndPos = l.pos
			return tok
		}
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '=' {
			l.advance()
			l.advance()
			return Token{Kind: TokSlashEq, Value: "/=", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		l.advance()
		return Token{Kind: TokSlash, Value: "/", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case '!':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '=' {
			l.advance()
			if l.pos+1 < len(l.src) && l.src[l.pos+1] == '=' {
				l.advance()
				l.advance()
				return Token{Kind: TokNotEqEq, Value: "!==", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
			}
			l.advance()
			return Token{Kind: TokNotEq, Value: "!=", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		l.advance()
		return Token{Kind: TokBang, Value: "!", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case '=':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '=' {
			l.advance()
			if l.pos+1 < len(l.src) && l.src[l.pos+1] == '=' {
				l.advance()
				l.advance()
				return Token{Kind: TokEqEqEq, Value: "===", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
			}
			l.advance()
			return Token{Kind: TokEqEq, Value: "==", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '>' {
			l.advance()
			l.advance()
			return Token{Kind: TokArrow, Value: "=>", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		l.advance()
		return Token{Kind: TokEq, Value: "=", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case '<':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '=' {
			l.advance()
			l.advance()
			return Token{Kind: TokLtEq, Value: "<=", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '<' {
			l.advance()
			l.advance()
			return Token{Kind: TokLtLt, Value: "<<", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		l.advance()
		return Token{Kind: TokLt, Value: "<", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case '>':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '=' {
			l.advance()
			l.advance()
			return Token{Kind: TokGtEq, Value: ">=", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '>' {
			l.advance()
			if l.pos+1 < len(l.src) && l.src[l.pos+1] == '>' {
				l.advance()
				l.advance()
				return Token{Kind: TokGtGtGt, Value: ">>>", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
			}
			l.advance()
			return Token{Kind: TokGtGt, Value: ">>", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		l.advance()
		return Token{Kind: TokGt, Value: ">", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case '&':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '&' {
			l.advance()
			l.advance()
			if l.pos < len(l.src) && l.src[l.pos] == '=' {
				l.advance()
				return Token{Kind: TokAndAndEq, Value: "&&=", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
			}
			return Token{Kind: TokAndAnd, Value: "&&", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		l.advance()
		return Token{Kind: TokAnd, Value: "&", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	case '|':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '|' {
			l.advance()
			l.advance()
			if l.pos < len(l.src) && l.src[l.pos] == '=' {
				l.advance()
				return Token{Kind: TokPipePipeEq, Value: "||=", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
			}
			return Token{Kind: TokPipePipe, Value: "||", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		l.advance()
		return Token{Kind: TokPipe, Value: "|", Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
	}

	// Unknown character.
	l.advance()
	return Token{Kind: TokEOF, Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
}

func (l *Lexer) advance() {
	if l.pos >= len(l.src) {
		return
	}
	if l.src[l.pos] == '\n' {
		l.line++
		l.column = 1
	} else {
		l.column++
	}
	l.pos++
}

func (l *Lexer) peek() byte {
	if l.pos < len(l.src) {
		return l.src[l.pos]
	}
	return 0
}

func (l *Lexer) peekN(n int) byte {
	if l.pos+n < len(l.src) {
		return l.src[l.pos+n]
	}
	return 0
}

func (l *Lexer) skipWhitespaceAndComments() {
	for l.pos < len(l.src) {
		ch := l.src[l.pos]
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			l.advance()
			continue
		}
		// Line comment.
		if ch == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/' {
			l.advance()
			l.advance()
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.advance()
			}
			continue
		}
		// Block comment.
		if ch == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '*' {
			l.advance()
			l.advance()
			for l.pos < len(l.src) {
				if l.src[l.pos] == '*' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/' {
					l.advance()
					l.advance()
					break
				}
				l.advance()
			}
			continue
		}
		break
	}
}

func (l *Lexer) readString(quote byte) Token {
	var buf strings.Builder
	startPos := l.pos
	l.advance()
	for l.pos < len(l.src) {
		ch := l.src[l.pos]
		if ch == quote {
			l.advance()
			return Token{Kind: TokString, Value: buf.String(), StartPos: startPos, EndPos: l.pos}
		}
		if ch == '\\' && l.pos+1 < len(l.src) {
			l.advance()
			esc := l.src[l.pos]
			switch esc {
			case 'n': buf.WriteByte('\n')
			case 't': buf.WriteByte('\t')
			case 'r': buf.WriteByte('\r')
			case '\\': buf.WriteByte('\\')
			case '"': buf.WriteByte('"')
			case '\'': buf.WriteByte('\'')
			case '0': buf.WriteByte(0)
			default:
				buf.WriteByte('\\')
				buf.WriteByte(esc)
			}
			l.advance()
			continue
		}
		if ch == '\n' {
			l.advance()
			buf.WriteByte(ch)
			continue
		}
		buf.WriteByte(ch)
		l.advance()
	}
	return Token{Kind: TokString, Value: buf.String(), StartPos: startPos, EndPos: l.pos}
}

// readTemplateQuasi reads a quasi-text segment from a template literal.
// It accumulates characters until ${, backtick, or EOF.
// On ${, switches to expression mode (braceDepth=1). On backtick, exits quasi mode
// and emits TokTemplateEnd if at depth 0.
func (l *Lexer) readTemplateQuasi() Token {
	startPos := l.pos
	startLine := l.line
	startCol := l.column
	var buf strings.Builder
	for l.pos < len(l.src) {
		ch := l.src[l.pos]
		// Closing backtick — end of template literal.
		if ch == '`' {
			l.advance()
			l.quasiMode = false
			l.pendingTemplateEnd = true
			return Token{Kind: TokString, Value: buf.String(), Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		// Start of expression: ${
		if ch == '$' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '{' {
			l.advance() // skip $
			l.advance() // skip {
			l.quasiMode = false
			l.braceDepth = 1
			return Token{Kind: TokString, Value: buf.String(), Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
		}
		// Escape sequences in template quasis.
		if ch == '\\' && l.pos+1 < len(l.src) {
			l.advance()
			esc := l.src[l.pos]
			switch esc {
			case 'n': buf.WriteByte('\n')
			case 't': buf.WriteByte('\t')
			case 'r': buf.WriteByte('\r')
			case '\\': buf.WriteByte('\\')
			case '`': buf.WriteByte('`')
			case '$': buf.WriteByte('$')
			default: buf.WriteByte('\\'); buf.WriteByte(esc)
			}
			l.advance()
			continue
		}
		// Multi-line: preserve newlines in template quasis.
		buf.WriteByte(ch)
		l.advance()
	}
	// EOF while in template — treat as end.
	l.quasiMode = false
	return Token{Kind: TokString, Value: buf.String(), Line: startLine, Column: startCol, StartPos: startPos, EndPos: l.pos}
}

func (l *Lexer) readNumber() Token {
	startPos := l.pos
	var numStr strings.Builder

	// Hex: 0x / 0X
	if l.peek() == '0' && l.pos+1 < len(l.src) && (l.src[l.pos+1] == 'x' || l.src[l.pos+1] == 'X') {
		l.advance()
		l.advance()
		numStr.WriteString("0x")
		for l.pos < len(l.src) {
			ch := l.src[l.pos]
			if isHexDigit(ch) {
				numStr.WriteByte(ch)
				l.advance()
			} else if ch == '_' && l.pos+1 < len(l.src) && isHexDigit(l.src[l.pos+1]) && numStr.Len() > 2 {
				l.advance() // skip underscore
			} else {
				break
			}
		}
		n, _ := strconv.ParseInt(numStr.String(), 0, 64)
		return Token{Kind: TokNumber, NumVal: float64(n), Value: numStr.String(), StartPos: startPos, EndPos: l.pos}
	}

	// Octal: 0o / 0O
	if l.peek() == '0' && l.pos+1 < len(l.src) && (l.src[l.pos+1] == 'o' || l.src[l.pos+1] == 'O') {
		l.advance()
		l.advance()
		numStr.WriteString("0o")
		for l.pos < len(l.src) {
			ch := l.src[l.pos]
			if ch >= '0' && ch <= '7' {
				numStr.WriteByte(ch)
				l.advance()
			} else if ch == '_' && l.pos+1 < len(l.src) && l.src[l.pos+1] >= '0' && l.src[l.pos+1] <= '7' && numStr.Len() > 2 {
				l.advance() // skip underscore
			} else {
				break
			}
		}
		n, _ := strconv.ParseInt(numStr.String(), 0, 64)
		return Token{Kind: TokNumber, NumVal: float64(n), Value: numStr.String(), StartPos: startPos, EndPos: l.pos}
	}

	// Binary: 0b / 0B
	if l.peek() == '0' && l.pos+1 < len(l.src) && (l.src[l.pos+1] == 'b' || l.src[l.pos+1] == 'B') {
		l.advance()
		l.advance()
		numStr.WriteString("0b")
		for l.pos < len(l.src) {
			ch := l.src[l.pos]
			if ch == '0' || ch == '1' {
				numStr.WriteByte(ch)
				l.advance()
			} else if ch == '_' && l.pos+1 < len(l.src) && (l.src[l.pos+1] == '0' || l.src[l.pos+1] == '1') && numStr.Len() > 2 {
				l.advance() // skip underscore
			} else {
				break
			}
		}
		n, _ := strconv.ParseInt(numStr.String(), 0, 64)
		return Token{Kind: TokNumber, NumVal: float64(n), Value: numStr.String(), StartPos: startPos, EndPos: l.pos}
	}

	// Legacy octal: starts with '0' followed by at least one octal digit (0-7).
	// In strict mode, these are SyntaxErrors. We flag them so the parser can reject.
	if l.peek() == '0' && l.pos+1 < len(l.src) && (l.src[l.pos+1] >= '0' && l.src[l.pos+1] <= '9' || l.src[l.pos+1] == '_') {
		// Check if the digits following '0' are all octal (0-7).
		// If any digit is 8 or 9, it's decimal (e.g., 08 → 8, not legacy octal).
		// Skip '_' separators during the scan.
		allOctal := true
		for i := l.pos + 1; i < len(l.src); i++ {
			ch := l.src[i]
			if ch >= '0' && ch <= '9' {
				if ch == '8' || ch == '9' {
					allOctal = false
					break
				}
			} else if ch == '_' {
				continue // skip separators in legacy octal scan
			} else {
				break
			}
		}
		if allOctal {
			// Consume leading '0' and following octal digits.
			l.advance() // consume '0'
			for l.pos < len(l.src) {
				ch := l.src[l.pos]
				if ch >= '0' && ch <= '7' {
					numStr.WriteByte(ch)
					l.advance()
				} else if ch == '_' && l.pos+1 < len(l.src) && l.src[l.pos+1] >= '0' && l.src[l.pos+1] <= '7' {
					l.advance() // skip separator
				} else {
					break
				}
			}
			// Parse as octal integer.
			n, _ := strconv.ParseInt("0o"+numStr.String(), 0, 64)
			return Token{Kind: TokNumber, NumVal: float64(n), Value: numStr.String(), StartPos: startPos, EndPos: l.pos, IsLegacyOctal: true}
		}
	}

	// Decimal integer or float.
	for l.pos < len(l.src) {
		ch := l.src[l.pos]
		if ch >= '0' && ch <= '9' {
			numStr.WriteByte(ch)
			l.advance()
		} else if ch == '_' && l.pos+1 < len(l.src) && l.src[l.pos+1] >= '0' && l.src[l.pos+1] <= '9' {
			l.advance() // skip underscore
		} else {
			break
		}
	}

	// BigInt literal: decimal integer followed by 'n' (no dot, no exponent).
	if l.pos < len(l.src) && l.src[l.pos] == 'n' {
		l.advance()
		return Token{Kind: TokBigInt, Value: numStr.String(), StartPos: startPos, EndPos: l.pos}
	}

	// Fractional part.
	if l.pos < len(l.src) && l.src[l.pos] == '.' && l.pos+1 < len(l.src) && l.src[l.pos+1] >= '0' && l.src[l.pos+1] <= '9' {
		numStr.WriteByte('.')
		l.advance()
		for l.pos < len(l.src) {
			ch := l.src[l.pos]
			if ch >= '0' && ch <= '9' {
				numStr.WriteByte(ch)
				l.advance()
			} else if ch == '_' && l.pos+1 < len(l.src) && l.src[l.pos+1] >= '0' && l.src[l.pos+1] <= '9' {
				l.advance() // skip underscore
			} else {
				break
			}
		}
	}
	// Exponent.
	if l.pos < len(l.src) && (l.src[l.pos] == 'e' || l.src[l.pos] == 'E') {
		numStr.WriteByte(l.src[l.pos])
		l.advance()
		if l.pos < len(l.src) && (l.src[l.pos] == '+' || l.src[l.pos] == '-') {
			numStr.WriteByte(l.src[l.pos])
			l.advance()
		}
		for l.pos < len(l.src) {
			ch := l.src[l.pos]
			if ch >= '0' && ch <= '9' {
				numStr.WriteByte(ch)
				l.advance()
			} else if ch == '_' && l.pos+1 < len(l.src) && l.src[l.pos+1] >= '0' && l.src[l.pos+1] <= '9' {
				l.advance() // skip underscore
			} else {
				break
			}
		}
	}

	n, err := strconv.ParseFloat(numStr.String(), 64)
	if err != nil {
		n = 0
	}
	return Token{Kind: TokNumber, NumVal: n, Value: numStr.String(), StartPos: startPos, EndPos: l.pos}
}

func (l *Lexer) readIdentifier() string {
	start := l.pos
	for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
		l.advance()
	}
	return string(l.src[start:l.pos])
}

func isIdentStart(ch byte) bool {
	if ch == '_' || ch == '$' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' {
		return true
	}
	r, _ := utf8.DecodeRune([]byte{ch})
	return unicode.IsLetter(r)
}

func isIdentPart(ch byte) bool {
	if isIdentStart(ch) {
		return true
	}
	return ch >= '0' && ch <= '9'
}

func isHexDigit(ch byte) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

// isRegExpAfterWhitespace checks whether a / after whitespace/comments should be a RegExp.
// It uses lastTokenKind: if the previous token was something that can end an expression,
// then / is division; otherwise it's a RegExp.
func (l *Lexer) isRegExpAfterWhitespace() bool {
	return l.expectRegExp
}

// skipLineComment skips a // comment (caller already consumed the //).
func (l *Lexer) skipLineComment() {
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		l.advance()
	}
}

// skipBlockComment skips a /* comment (caller already consumed the /*).
func (l *Lexer) skipBlockComment() {
	for l.pos < len(l.src) {
		if l.src[l.pos] == '*' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/' {
			l.advance()
			l.advance()
			return
		}
		l.advance()
	}
}

// scanRegExp scans a regular expression literal: /pattern/flags.
// Expects current character to be '/'. Reads until an unescaped '/' and then optional flags.
func (l *Lexer) scanRegExp() Token {
	l.advance() // skip opening /
	var pattern strings.Builder
	inCharClass := false
	for l.pos < len(l.src) {
		ch := l.src[l.pos]
		if ch == '\n' || ch == '\r' {
			// Unterminated regexp literal.
			l.advance()
			return Token{Kind: TokRegExp, Value: pattern.String()}
		}
		if ch == '\\' && l.pos+1 < len(l.src) {
			pattern.WriteByte(ch)
			l.advance()
			pattern.WriteByte(l.src[l.pos])
			l.advance()
			continue
		}
		if ch == '[' && !inCharClass {
			inCharClass = true
			pattern.WriteByte(ch)
			l.advance()
			continue
		}
		if ch == ']' && inCharClass {
			inCharClass = false
			pattern.WriteByte(ch)
			l.advance()
			continue
		}
		if ch == '/' && !inCharClass {
			l.advance() // skip closing /
			flags := l.readFlags()
			return Token{Kind: TokRegExp, Value: "/" + pattern.String() + "/" + flags}
		}
		pattern.WriteByte(ch)
		l.advance()
	}
	return Token{Kind: TokRegExp, Value: "/" + pattern.String() + "/"}
}

// readFlags reads the flag characters after a regexp closing /.
func (l *Lexer) readFlags() string {
	var flags strings.Builder
	for l.pos < len(l.src) {
		ch := l.src[l.pos]
		if isIdentStart(ch) {
			flags.WriteByte(ch)
			l.advance()
		} else {
			break
		}
	}
	return flags.String()
}

func lookupKeyword(s string) TokenKind {
	switch s {
	case "true":
		return TokTrue
	case "false":
		return TokFalse
	case "null":
		return TokNull
	case "undefined":
		return TokUndefined
	case "var":
		return TokVar
	case "let":
		return TokLet
	case "const":
		return TokConst
	case "if":
		return TokIf
	case "else":
		return TokElse
	case "for":
		return TokFor
	case "while":
		return TokWhile
	case "do":
		return TokDo
	case "return":
		return TokReturn
	case "function":
		return TokFunction
	case "new":
		return TokNew
	case "this":
		return TokThis
	case "typeof":
		return TokTypeof
	case "instanceof":
		return TokInstanceof
	case "in":
		return TokIn
	case "of":
		return TokOf
	case "delete":
		return TokDelete
	case "void":
		return TokVoid
	case "throw":
		return TokThrow
	case "try":
		return TokTry
	case "catch":
		return TokCatch
	case "finally":
		return TokFinally
	case "switch":
		return TokSwitch
	case "case":
		return TokCase
	case "default":
		return TokDefault
	case "break":
		return TokBreak
	case "continue":
		return TokContinue
	case "class":
		return TokClass
	case "extends":
		return TokExtends
	case "super":
		return TokSuper
	case "get":
		return TokGet
	case "set":
		return TokSet
	case "yield":
		return TokYield
	case "async":
		return TokAsync
	case "await":
		return TokAwait
	case "import":
		return TokImport
	case "export":
		return TokExport
	case "from":
		return TokFrom
	case "as":
		return TokAs
	case "with":
		return TokWith
	default:
		return TokIdentifier
	}
}
