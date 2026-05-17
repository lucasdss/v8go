package net

import (
	"bytes"
	"strings"
)

// MIMEType represents a parsed MIME type with type and subtype.
type MIMEType struct {
	Type    string
	Subtype string
	Params  map[string]string
}

// String returns the serialized MIME type.
func (m MIMEType) String() string {
	var b strings.Builder
	b.WriteString(m.Type)
	b.WriteString("/")
	b.WriteString(m.Subtype)
	for k, v := range m.Params {
		b.WriteString("; ")
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(v)
	}
	return b.String()
}

// IsHTML reports whether the MIME type represents an HTML document.
func (m MIMEType) IsHTML() bool {
	return m.Type == "text" && m.Subtype == "html"
}

// IsJavaScript reports whether the MIME type represents a JavaScript resource.
func (m MIMEType) IsJavaScript() bool {
	switch m.Type + "/" + m.Subtype {
	case "text/javascript", "application/javascript",
		"application/x-javascript", "text/ecmascript":
		return true
	}
	return false
}

// IsCSS reports whether the MIME type represents a CSS stylesheet.
func (m MIMEType) IsCSS() bool {
	return m.Type == "text" && m.Subtype == "css"
}

// ParseMIMEType parses a raw Content-Type header value and returns a MIMEType.
// It is case-insensitive per RFC 7231.
func ParseMIMEType(raw string) (MIMEType, error) {
	parts := strings.SplitN(raw, ";", 2)
	typeParts := strings.SplitN(strings.TrimSpace(parts[0]), "/", 2)
	if len(typeParts) != 2 {
		return MIMEType{}, &ParseError{Input: raw, Message: "missing subtype"}
	}

	mt := MIMEType{
		Type:    strings.ToLower(strings.TrimSpace(typeParts[0])),
		Subtype: strings.ToLower(strings.TrimSpace(typeParts[1])),
		Params:  make(map[string]string),
	}

	if len(parts) > 1 {
		for _, param := range strings.Split(parts[1], ";") {
			param = strings.TrimSpace(param)
			kv := strings.SplitN(param, "=", 2)
			if len(kv) == 2 {
				key := strings.ToLower(strings.TrimSpace(kv[0]))
				val := strings.TrimSpace(kv[1])
				val = strings.Trim(val, `"`)
				mt.Params[key] = val
			}
		}
	}
	return mt, nil
}

// sniffSignature pairs a byte pattern (and optional mask) with the MIME type
// it signals, following MIME Sniffing § 5.
type sniffSignature struct {
	pattern []byte
	mime    string
}

// bytePatternSignatures contains the byte-pattern–based MIME sniff table
// from the MIME Sniffing specification § 5.
var bytePatternSignatures = []sniffSignature{
	// HTML
	{pattern: []byte("<!DOCTYPE html"), mime: "text/html"},
	{pattern: []byte("<html"), mime: "text/html"},
	{pattern: []byte("<head"), mime: "text/html"},
	{pattern: []byte("<script"), mime: "text/html"},
	{pattern: []byte("<iframe"), mime: "text/html"},
	{pattern: []byte("<h1"), mime: "text/html"},
	{pattern: []byte("<div"), mime: "text/html"},
	{pattern: []byte("<font"), mime: "text/html"},
	{pattern: []byte("<table"), mime: "text/html"},
	{pattern: []byte("<a "), mime: "text/html"},
	{pattern: []byte("<style"), mime: "text/html"},
	{pattern: []byte("<title"), mime: "text/html"},
	{pattern: []byte("<b"), mime: "text/html"},
	{pattern: []byte("<body"), mime: "text/html"},
	{pattern: []byte("<br"), mime: "text/html"},
	{pattern: []byte("<p"), mime: "text/html"},
	{pattern: []byte("<!--"), mime: "text/html"},
	// XML
	{pattern: []byte("<?xml"), mime: "text/xml"},
	// PDF
	{pattern: []byte("%PDF-"), mime: "application/pdf"},
	// PostScript
	{pattern: []byte("%!PS-Adobe-"), mime: "application/postscript"},
	// PNG
	{pattern: []byte("\x89PNG\r\n\x1a\n"), mime: "image/png"},
	// JPEG
	{pattern: []byte("\xff\xd8\xff"), mime: "image/jpeg"},
	// GIF87a / GIF89a
	{pattern: []byte("GIF87a"), mime: "image/gif"},
	{pattern: []byte("GIF89a"), mime: "image/gif"},
	// BMP
	{pattern: []byte("BM"), mime: "image/bmp"},
	// WebP
	{pattern: []byte("RIFF"), mime: "image/webp"}, // needs further validation
	// ZIP (used by Office Open XML, JAR, etc.)
	{pattern: []byte("PK\x03\x04"), mime: "application/zip"},
	// gzip
	{pattern: []byte("\x1f\x8b"), mime: "application/x-gzip"},
}

// SniffMIMEType determines the MIME type from the first up to 512 bytes of
// content, applying the algorithm described in MIME Sniffing § 5.
// If no signature matches, "application/octet-stream" is returned.
func SniffMIMEType(data []byte) string {
	// Limit to the sniff buffer defined in the spec.
	if len(data) > 512 {
		data = data[:512]
	}

	// Skip leading whitespace and BOM bytes before matching text patterns.
	trimmed := bytes.TrimLeft(data, " \t\n\r\f")

	for _, sig := range bytePatternSignatures {
		if bytes.HasPrefix(bytes.ToLower(trimmed), bytes.ToLower(sig.pattern)) {
			return sig.mime
		}
	}

	// Binary vs. text heuristic: if the buffer contains no bytes in the
	// "binary" range then treat it as text/plain.
	for _, b := range data {
		if b <= 0x08 || b == 0x0B || (b >= 0x0E && b <= 0x1A) ||
			(b >= 0x1C && b <= 0x1F) {
			return "application/octet-stream"
		}
	}
	return "text/plain"
}

// DetermineType resolves the effective MIME type for a resource following
// MIME Sniffing § 7 "Sniffing a MIME type".  It prefers the supplied
// Content-Type header value but may override it based on byte sniffing when
// the header value is considered "unsafe" (empty or "application/octet-stream").
func DetermineType(contentType string, body []byte) MIMEType {
	if contentType == "" || strings.EqualFold(contentType, "application/octet-stream") {
		sniffed := SniffMIMEType(body)
		mt, err := ParseMIMEType(sniffed)
		if err != nil {
			return MIMEType{Type: "application", Subtype: "octet-stream"}
		}
		return mt
	}
	mt, err := ParseMIMEType(contentType)
	if err != nil {
		return MIMEType{Type: "application", Subtype: "octet-stream"}
	}
	return mt
}
