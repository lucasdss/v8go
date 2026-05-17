// Package parser — PreloadScanner implements Chromium-style subresource
// discovery that runs during HTML tokenization.  It scans the input for
// resource-defining tags (<link>, <script>, <img>, <video>, <audio>,
// <source>, <iframe>) and extracts URLs before the main parser builds
// the DOM tree, so that network fetches can start early.
//
// Reference: third_party/blink/renderer/core/html/parser/html_preload_scanner.h

package parser

import (
	"strings"

	browserNet "github.com/lucasdss/v8go/pkg/net"
)

// PreloadResourceType categorises the kind of subresource that was discovered.
type PreloadResourceType int

const (
	// PreloadScript is a <script src> external JavaScript resource.
	PreloadScript PreloadResourceType = iota
	// PreloadStyle is a <link rel="stylesheet" href> CSS resource.
	PreloadStyle
	// PreloadImage is an <img src> or <img srcset> image resource.
	PreloadImage
	// PreloadMedia is a <video><source> or <audio><source> media resource.
	PreloadMedia
	// PreloadDocument is an <iframe src> nested document.
	PreloadDocument
	// PreloadFont is a <link rel="preload" as="font"> font resource.
	PreloadFont
)

// String returns a human-readable name for the resource type.
func (t PreloadResourceType) String() string {
	switch t {
	case PreloadScript:
		return "script"
	case PreloadStyle:
		return "style"
	case PreloadImage:
		return "image"
	case PreloadMedia:
		return "media"
	case PreloadDocument:
		return "document"
	case PreloadFont:
		return "font"
	default:
		return "unknown"
	}
}

// PreloadResource describes a single subresource discovered by the scanner.
type PreloadResource struct {
	// URL is the raw (unresolved) URL string from the attribute.
	URL string
	// ResolvedURL is the URL resolved against the document base URL.
	// It is empty until Resolve() is called.
	ResolvedURL string
	// Type categorises the resource.
	Type PreloadResourceType
	// CrossOrigin is the value of the crossorigin attribute, if any.
	CrossOrigin string
	// Integrity is the value of the integrity attribute (SRI), if any.
	Integrity string
	// Media is the value of the media attribute on <link>, for media-conditional CSS.
	Media string
	// As is the value of the as attribute on <link rel="preload">.
	As string
}

// Resolve resolves the raw URL against the given base and stores the result.
func (r *PreloadResource) Resolve(base string) {
	r.ResolvedURL = resolveRelative(base, r.URL)
}

// IsResolved reports whether the URL has been resolved.
func (r *PreloadResource) IsResolved() bool {
	return r.ResolvedURL != ""
}

// requestDestination maps resource type to Fetch Standard destination.
func (r *PreloadResource) requestDestination() browserNet.RequestDestination {
	switch r.Type {
	case PreloadScript:
		return browserNet.DestScript
	case PreloadStyle:
		return browserNet.DestStyle
	case PreloadImage:
		return browserNet.DestImage
	case PreloadFont:
		return browserNet.DestFont
	default:
		return browserNet.DestEmpty
	}
}

// PreloadScanner is a lightweight, pull-based scanner that discovers
// subresource URLs in HTML without building a DOM tree.  It runs a
// minimal state machine that only looks for start-tag openings and
// attribute parsing — it skips text content, comments, and raw text
// bodies.
//
// It is safe for concurrent use.
type PreloadScanner struct {
	input  []rune
	pos    int
	baseURL string

	// tags that we care about
	targetTags map[string]PreloadResourceType
}

// NewPreloadScanner creates a scanner for the given HTML input.
func NewPreloadScanner(html []byte) *PreloadScanner {
	return &PreloadScanner{
		input: []rune(string(html)),
		targetTags: map[string]PreloadResourceType{
			"script": PreloadScript,
			"link":   PreloadStyle,    // will be refined by rel check
			"img":    PreloadImage,
			"video":  PreloadMedia,
			"audio":  PreloadMedia,
			"source": PreloadMedia,
			"iframe": PreloadDocument,
		},
	}
}

// SetBaseURL sets the document base URL for resolving relative URLs.
func (s *PreloadScanner) SetBaseURL(base string) {
	s.baseURL = base
}

// Scan runs the scanner and returns all discovered resources.
// Callers should call Resolve() on each resource after scanning.
func (s *PreloadScanner) Scan() []PreloadResource {
	var resources []PreloadResource

	for s.pos < len(s.input) {
		// Find the next '<'.
		lt := s.findNext('<')
		if lt < 0 {
			break
		}
		s.pos = lt + 1

		if s.pos >= len(s.input) {
			break
		}

		// Check for '/' (end tag), '!' (comment/doctype), or start tag.
		ch := s.input[s.pos]

		if ch == '/' {
			// End tag — skip past '>'.
			s.skipPast('>')
			continue
		}
		if ch == '!' {
			// Comment or DOCTYPE — skip.
			s.skipPast('>')
			continue
		}
		if ch == '?' {
			// Processing instruction or XML decl — skip.
			s.skipPast('>')
			continue
		}

		// Must be a start tag — read the tag name.
		// readTagName returns: tagName, selfClosing, tagWasClosed ('>' already consumed).
		tagName, selfClosing, tagClosed := s.readTagName()

		if tagName == "" {
			// Could not parse tag name — skip to next '>'.
			s.skipPast('>')
			continue
		}

		// For tags that are not targets, skip efficiently.
		resType, isTarget := s.targetTags[tagName]

		if !isTarget {
			// Not a resource tag.
			if tagName == "script" || tagName == "style" || tagName == "textarea" || tagName == "title" {
				s.skipRawText(tagName)
				continue
			}
			// Skip to end of this tag (if not already closed).
			if !tagClosed {
				s.skipPast('>')
			}
			continue
		}

		// It's a target tag — parse its attributes.
		// If tag is already closed (bare tag like <img>), skip parsing.
		var attrs map[string]string
		if tagClosed || selfClosing {
			attrs = make(map[string]string)
		} else {
			attrs = s.parseAttributes()
		}

		// For <link>, we need to check the rel attribute to determine the type.
		if tagName == "link" {
			rel := strings.ToLower(attrs["rel"])
			switch {
			case rel == "stylesheet":
				resType = PreloadStyle
			case rel == "preload":
				as := strings.ToLower(attrs["as"])
				switch as {
				case "font":
					resType = PreloadFont
				case "image":
					resType = PreloadImage
				case "script":
					resType = PreloadScript
				case "style":
					resType = PreloadStyle
				}
			default:
				// Not a resource link (e.g., rel="icon", rel="canonical") — skip.
				continue
			}
		}

		// Extract the URL from the appropriate attribute.
		url := s.extractURL(tagName, attrs)
		if url == "" {
			// No URL found. For tags with raw text bodies (inline script/style),
			// skip the body to avoid false positives.
			if tagName == "script" || tagName == "style" {
				s.skipRawText(tagName)
			}
			continue
		}

		res := PreloadResource{
			URL:         url,
			Type:        resType,
			CrossOrigin: attrs["crossorigin"],
			Integrity:   attrs["integrity"],
			Media:       attrs["media"],
			As:          attrs["as"],
		}

		if s.baseURL != "" {
			res.Resolve(s.baseURL)
		}

		resources = append(resources, res)

		// Handle srcset for <img> tags.
		if tagName == "img" {
			if srcset := attrs["srcset"]; srcset != "" {
				urls := parseSrcsetURLs(srcset)
				for _, u := range urls {
					sr := PreloadResource{
						URL:         u,
						Type:        PreloadImage,
						CrossOrigin: attrs["crossorigin"],
					}
					if s.baseURL != "" {
						sr.Resolve(s.baseURL)
					}
					resources = append(resources, sr)
				}
			}
		}
	}

	return resources
}

// findNext returns the index of the next occurrence of ch starting at pos,
// or -1 if not found.
func (s *PreloadScanner) findNext(ch rune) int {
	for i := s.pos; i < len(s.input); i++ {
		if s.input[i] == ch {
			return i
		}
	}
	return -1
}

// skipPast advances pos past the next occurrence of ch.
func (s *PreloadScanner) skipPast(ch rune) {
	for s.pos < len(s.input) {
		if s.input[s.pos] == ch {
			s.pos++
			return
		}
		s.pos++
	}
}

// readTagName reads a start tag name starting at pos and returns the
// lowercased name, whether it's self-closing, and whether '>' was already
// consumed.  pos should be at the first character of the name.
func (s *PreloadScanner) readTagName() (string, bool, bool) {
	var buf strings.Builder
	selfClosing := false
	tagClosed := false

	for s.pos < len(s.input) {
		ch := s.input[s.pos]

		if ch == '>' {
			s.pos++
			tagClosed = true
			return strings.ToLower(buf.String()), selfClosing, tagClosed
		}
		if ch == '/' {
			// Check if next is '>' for self-closing.
			if s.pos+1 < len(s.input) && s.input[s.pos+1] == '>' {
				selfClosing = true
				tagClosed = true
				s.pos += 2
				return strings.ToLower(buf.String()), selfClosing, tagClosed
			}
			// Otherwise it might be part of the tag name... unlikely but handle.
			s.pos++
			continue
		}
		if isWhitespace(ch) || ch == '<' || ch == '=' {
			// Tag name ended before '>' — attributes follow.
			if isWhitespace(ch) {
				s.pos++
			}
			return strings.ToLower(buf.String()), selfClosing, tagClosed
		}

		buf.WriteRune(ch)
		s.pos++
	}

	return strings.ToLower(buf.String()), selfClosing, tagClosed
}

// parseAttributes parses tag attributes starting at pos (after tag name).
// It stops when it encounters '>' or '/>' or '<' (next tag boundary).
func (s *PreloadScanner) parseAttributes() map[string]string {
	attrs := make(map[string]string)

	for s.pos < len(s.input) {
		ch := s.input[s.pos]

		if ch == '>' {
			s.pos++
			return attrs
		}
		if ch == '/' {
			if s.pos+1 < len(s.input) && s.input[s.pos+1] == '>' {
				s.pos += 2
				return attrs
			}
		}
		if ch == '<' {
			// Next tag starts — current tag probably malformed but bail out.
			return attrs
		}

		// Skip whitespace.
		if isWhitespace(ch) {
			s.pos++
			continue
		}

		// Read attribute name.
		attrName := s.readAttrName()
		if attrName == "" {
			// Couldn't parse — try to recover.
			s.skipPast('>')
			return attrs
		}

		// Check for '='.
		s.skipWhitespace()
		if s.pos < len(s.input) && s.input[s.pos] == '=' {
			s.pos++ // consume '='
			s.skipWhitespace()
			attrValue := s.readAttrValue()
			attrs[attrName] = attrValue
		} else {
			// Boolean attribute.
			attrs[attrName] = ""
		}
	}

	return attrs
}

// readAttrName reads an attribute name and returns it lowercased.
func (s *PreloadScanner) readAttrName() string {
	var buf strings.Builder

	for s.pos < len(s.input) {
		ch := s.input[s.pos]

		if ch == '>' || ch == '/' || ch == '=' || isWhitespace(ch) || ch == '<' {
			return strings.ToLower(buf.String())
		}

		buf.WriteRune(ch)
		s.pos++
	}

	return strings.ToLower(buf.String())
}

// readAttrValue reads a quoted or unquoted attribute value.
func (s *PreloadScanner) readAttrValue() string {
	if s.pos >= len(s.input) {
		return ""
	}

	ch := s.input[s.pos]
	var delimiter rune

	if ch == '"' || ch == '\'' {
		delimiter = ch
		s.pos++ // consume opening quote

		var buf strings.Builder
		for s.pos < len(s.input) {
			ch2 := s.input[s.pos]
			if ch2 == delimiter {
				s.pos++ // consume closing quote
				return strings.TrimSpace(buf.String())
			}
			buf.WriteRune(ch2)
			s.pos++
		}
		return strings.TrimSpace(buf.String())
	}

	// Unquoted value.
	var buf strings.Builder
	for s.pos < len(s.input) {
		ch2 := s.input[s.pos]
		if ch2 == '>' || isWhitespace(ch2) {
			return strings.TrimSpace(buf.String())
		}
		buf.WriteRune(ch2)
		s.pos++
	}
	return strings.TrimSpace(buf.String())
}

// skipWhitespace advances pos past any whitespace characters.
func (s *PreloadScanner) skipWhitespace() {
	for s.pos < len(s.input) && isWhitespace(s.input[s.pos]) {
		s.pos++
	}
}

// skipRawText skips content until the matching closing tag is found.
// This handles <script>, <style>, <textarea>, <title> body skipping.
func (s *PreloadScanner) skipRawText(tagName string) {
	closeTag := "</" + tagName
	for {
		idx := s.findNext('<')
		if idx < 0 {
			s.pos = len(s.input)
			return
		}
		s.pos = idx

		// Check if this is the closing tag.
		if s.pos+len(closeTag) <= len(s.input) {
			candidate := strings.ToLower(string(s.input[s.pos : s.pos+len(closeTag)]))
			if candidate == closeTag {
				s.pos += len(closeTag)
				// Skip to '>'
				s.skipPast('>')
				return
			}
		}
		s.pos++ // skip past '<' and continue scanning
	}
}

// extractURL extracts the resource URL from the appropriate attribute based on tag name.
func (s *PreloadScanner) extractURL(tagName string, attrs map[string]string) string {
	switch tagName {
	case "script":
		return attrs["src"]
	case "link":
		return attrs["href"]
	case "img":
		return attrs["src"]
	case "iframe":
		return attrs["src"]
	case "video", "audio":
		// First check for src on the element itself.
		if src := attrs["src"]; src != "" {
			return src
		}
		return ""
	case "source":
		return attrs["src"]
	}
	return ""
}

// parseSrcsetURLs extracts image URLs from a srcset attribute value.
// srcset format: "url1 1x, url2 2x, url3 100w"
func parseSrcsetURLs(srcset string) []string {
	var urls []string
	for _, candidate := range splitComma(srcset) {
		// Each candidate is "URL [descriptor]" — strip the descriptor.
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		// Split on whitespace, take the first token.
		parts := strings.Fields(candidate)
		if len(parts) > 0 && parts[0] != "" {
			urls = append(urls, parts[0])
		}
	}
	return urls
}

// splitComma splits a string on commas, respecting the same rules as the
// HTML spec for srcset parsing.
func splitComma(s string) []string {
	return strings.Split(s, ",")
}

// resolveRelative resolves a relative URL against a base absolute URL.
// It mirrors the logic in browser.go:resolveRelativeURL.
func resolveRelative(base, rel string) string {
	if rel == "" {
		return base
	}
	// Already absolute.
	if strings.Contains(rel, "://") {
		return rel
	}
	// Protocol-relative.
	if strings.HasPrefix(rel, "//") {
		u, err := browserNet.ParseURL(base)
		if err != nil || u == nil {
			return rel
		}
		return u.Scheme + ":" + rel
	}
	// Absolute path.
	if strings.HasPrefix(rel, "/") {
		u, err := browserNet.ParseURL(base)
		if err != nil || u == nil {
			return rel
		}
		return u.Scheme + "://" + u.Host + rel
	}
	// Relative path — resolve against base directory.
	lastSlash := strings.LastIndex(base, "/")
	if lastSlash > 8 { // past "https://"
		return base[:lastSlash+1] + rel
	}
	return base + "/" + rel
}

// (isWhitespace is declared in html.go)
