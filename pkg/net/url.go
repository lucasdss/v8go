// Package net implements networking primitives for the browser, including URL
// parsing (aligned with the WHATWG URL Standard), MIME sniffing, and HTTP/HTTPS
// resource fetching with CORS support.
//
// References:
//   - WHATWG URL Standard: https://url.spec.whatwg.org/
//   - WHATWG Fetch Standard: https://fetch.spec.whatwg.org/
package net

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Scheme constants for supported URL schemes.
const (
	SchemeHTTP       = "http"
	SchemeHTTPS      = "https"
	SchemeData       = "data"
	SchemeBlob       = "blob"
	SchemeAbout      = "about"
	SchemeJavaScript = "javascript"
)

// specialSchemes maps special schemes to their default ports as defined in the
// WHATWG URL Standard § 4.1 "URL writing".
var specialSchemes = map[string]string{
	SchemeHTTP:  "80",
	SchemeHTTPS: "443",
	"ftp":       "21",
	"ws":        "80",
	"wss":       "443",
}

// ParseError represents a URL parse failure.
type ParseError struct {
	Input   string
	Message string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("url parse error for %q: %s", e.Input, e.Message)
}

// URL represents a parsed URL aligned with the WHATWG URL record model.
// See https://url.spec.whatwg.org/#concept-url
type URL struct {
	// Scheme is the URL's scheme (e.g. "https"). Always lower-cased.
	Scheme string
	// Username is the URL's username component (percent-decoded on access).
	Username string
	// Password is the URL's password component (percent-decoded on access).
	Password string
	// Host is the host string without port (domain or IP).
	Host string
	// Port is the explicit port string (empty if the port equals the scheme
	// default or if no port was specified).
	Port string
	// Path is the list of path segments (decoded).
	Path []string
	// Query is the raw query string (without the leading "?").
	Query string
	// Fragment is the fragment identifier (without the leading "#").
	Fragment string

	// raw stores the original input for round-trip serialization.
	raw string
}

// ParseRefURL parses a reference URL (e.g., "/css/style.css", "../page")
// that may be relative. Absolute URLs are parsed normally.
func ParseRefURL(rawURL string) (*URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, &ParseError{Input: rawURL, Message: "empty input"}
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse ref url: %w", err)
	}

	result := &URL{
		Scheme:   strings.ToLower(u.Scheme),
		Query:    u.RawQuery,
		Fragment: u.Fragment,
		raw:      rawURL,
	}

	if u.User != nil {
		result.Username = u.User.Username()
		result.Password, _ = u.User.Password()
	}

	result.Host = u.Hostname()
	port := u.Port()

	if port != "" && port != specialSchemes[result.Scheme] {
		result.Port = port
	}

	rawPath := u.Path
	if rawPath != "" {
		segments := strings.Split(rawPath, "/")
		if len(segments) > 0 && segments[0] == "" {
			segments = segments[1:]
		}
		result.Path = segments
	}

	return result, nil
}

// ParseURL works like ParseRefURL but requires an absolute URL (has scheme).
func ParseURL(rawURL string) (*URL, error) {
	result, err := ParseRefURL(rawURL)
	if err != nil {
		return nil, err
	}
	if result.Scheme == "" {
		return nil, &ParseError{Input: rawURL, Message: "missing scheme"}
	}
	return result, nil
}

// IsSpecial reports whether u has a special scheme as defined in the WHATWG
// URL Standard § 4.1.
func (u *URL) IsSpecial() bool {
	_, ok := specialSchemes[u.Scheme]
	return ok
}

// Origin returns the serialized origin of u.  For opaque origins (data:, blob:
// without a stored origin, etc.) the string "null" is returned.
//
// See https://url.spec.whatwg.org/#concept-url-origin
func (u *URL) Origin() string {
	switch u.Scheme {
	case SchemeHTTP, SchemeHTTPS, "ftp", "ws", "wss":
		host := u.Host
		if u.Port != "" {
			host = host + ":" + u.Port
		}
		return fmt.Sprintf("%s://%s", u.Scheme, host)
	default:
		return "null"
	}
}

// Serialize returns the WHATWG URL serialization of u.
// See https://url.spec.whatwg.org/#concept-url-serializer
func (u *URL) Serialize() string {
	var b strings.Builder
	b.WriteString(u.Scheme)
	b.WriteString(":")

	if u.Host != "" {
		b.WriteString("//")
		if u.Username != "" {
			b.WriteString(u.Username)
			if u.Password != "" {
				b.WriteString(":")
				b.WriteString(u.Password)
			}
			b.WriteString("@")
		}
		b.WriteString(u.Host)
		if u.Port != "" {
			b.WriteString(":")
			b.WriteString(u.Port)
		}
	}

	if len(u.Path) > 0 {
		b.WriteString("/")
		b.WriteString(strings.Join(u.Path, "/"))
	} else if u.Host != "" {
		b.WriteString("/")
	}

	if u.Query != "" {
		b.WriteString("?")
		b.WriteString(u.Query)
	}
	if u.Fragment != "" {
		b.WriteString("#")
		b.WriteString(u.Fragment)
	}
	return b.String()
}

// String implements the fmt.Stringer interface and returns the serialized URL.
func (u *URL) String() string { return u.Serialize() }

// ResolveReference resolves a reference URL against u as the base, following
// the algorithm in WHATWG URL Standard § 5.2.
func (u *URL) ResolveReference(ref *URL) (*URL, error) {
	if ref == nil {
		return nil, errors.New("nil reference URL")
	}
	// Serialize the base URL and parse with Go's stdlib.
	base, err := url.Parse(u.Serialize())
	if err != nil {
		return nil, fmt.Errorf("parse base url: %w", err)
	}
	// For reference URLs, use Go's parser directly on the original raw URL.
	// This correctly handles relative paths like "/css/style.css".
	refParsed, err := url.Parse(ref.raw)
	if err != nil {
		return nil, fmt.Errorf("parse ref url: %w", err)
	}
	resolved := base.ResolveReference(refParsed)
	return ParseURL(resolved.String())
}
