package net

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// CookieJar implements http.CookieJar and stores cookies keyed by domain.
// It respects Domain, Path, Secure, HttpOnly, SameSite, MaxAge, and Expires
// attributes according to RFC 6265.
type CookieJar struct {
	mu      sync.RWMutex
	cookies map[string][]*http.Cookie // key = domain
}

// NewCookieJar returns an empty CookieJar.
func NewCookieJar() *CookieJar {
	return &CookieJar{
		cookies: make(map[string][]*http.Cookie),
	}
}

// SetCookies implements http.CookieJar.SetCookies. Cookies are stored keyed by
// the effective domain (derived from the cookie's Domain attribute or the URL
// host). Expired cookies and cookies that fail domain/path matching are discarded.
func (j *CookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if u == nil {
		return
	}

	host := canonicalHost(u.Host)

	j.mu.Lock()
	defer j.mu.Unlock()

	now := time.Now()

	for _, c := range cookies {
		if c == nil {
			continue
		}

		// Determine the effective domain for storage.
		domain := strings.TrimPrefix(strings.ToLower(c.Domain), ".")
		if domain == "" {
			domain = host
		}

		// Domain matching: the effective domain must be a suffix of the request host.
		if !strings.HasSuffix(host, domain) {
			// Also accept when host == domain (exact match)
			if host != domain && !strings.HasSuffix("."+host, "."+domain) {
				continue
			}
		}

		// Handle expiry via MaxAge.
		if c.MaxAge > 0 {
			c.Expires = now.Add(time.Duration(c.MaxAge) * time.Second)
		} else if c.MaxAge < 0 {
			// Delete the cookie.
			j.deleteCookie(domain, c)
			continue
		}

		// Handle expiry via Expires.
		if !c.Expires.IsZero() && c.Expires.Before(now) {
			j.deleteCookie(domain, c)
			continue
		}

		// Normalise path.
		if c.Path == "" {
			c.Path = "/"
		}

		// Store the cookie.
		list := j.cookies[domain]
		replaced := false
		for i, existing := range list {
			if existing.Name == c.Name && existing.Path == c.Path {
				list[i] = c
				replaced = true
				break
			}
		}
		if !replaced {
			j.cookies[domain] = append(list, c)
		}
	}
}

// deleteCookie removes a cookie by name from the jar for the given domain.
func (j *CookieJar) deleteCookie(domain string, c *http.Cookie) {
	list := j.cookies[domain]
	for i := 0; i < len(list); i++ {
		if list[i].Name == c.Name {
			list = append(list[:i], list[i+1:]...)
			i--
		}
	}
	if len(list) == 0 {
		delete(j.cookies, domain)
	} else {
		j.cookies[domain] = list
	}
}

// Cookies implements http.CookieJar.Cookies. It returns cookies that match the
// given URL based on domain, path, Secure flag, and expiry.
func (j *CookieJar) Cookies(u *url.URL) []*http.Cookie {
	if u == nil {
		return nil
	}

	host := canonicalHost(u.Host)
	isSecure := strings.EqualFold(u.Scheme, "https")

	j.mu.RLock()
	defer j.mu.RUnlock()

	now := time.Now()
	var result []*http.Cookie

	for domain, cookies := range j.cookies {
		// Domain must be a suffix of the request host.
		if !strings.HasSuffix(host, domain) && host != domain {
			continue
		}

		for _, c := range cookies {
			// Expiry check.
			if !c.Expires.IsZero() && c.Expires.Before(now) {
				continue
			}

			// MaxAge <= 0 means expired.
			if c.MaxAge < 0 {
				continue
			}

			// Secure flag: only send over HTTPS.
			if c.Secure && !isSecure {
				continue
			}

			// Path matching: the cookie's path must be a prefix of the URL path.
			reqPath := u.Path
			if reqPath == "" {
				reqPath = "/"
			}
			if !pathMatch(c.Path, reqPath) {
				continue
			}

			// HttpOnly cookies are included in the jar but only sent by the
			// browser's HTTP layer — we include them here so they participate
			// in HTTP fetches (non-HTTP APIs should filter them, but our Fetch
			// client always sends them over HTTP).
			// SameSite enforcement is done at request time.

			result = append(result, c)
		}
	}

	return result
}

// pathMatch returns true if cookiePath is a prefix of requestPath according
// to RFC 6265 § 5.1.4.
func pathMatch(cookiePath, requestPath string) bool {
	if cookiePath == "" || cookiePath == "/" {
		return true
	}
	if !strings.HasPrefix(requestPath, cookiePath) {
		return false
	}
	// The cookie path must be a full path component prefix: either the
	// request path equals the cookie path, or the next character after the
	// prefix is a '/'.
	return len(requestPath) == len(cookiePath) ||
		requestPath[len(cookiePath)] == '/'
}

// canonicalHost returns the hostname in lower-case, stripping any port.
func canonicalHost(hostPort string) string {
	h := strings.ToLower(hostPort)
	if i := strings.LastIndex(h, "]"); i != -1 {
		// IPv6
		if strings.HasPrefix(h, "[") {
			return h[:i+1]
		}
	}
	if host, _, err := netSplitHostPort(h); err == nil {
		return host
	}
	return h
}

// netSplitHostPort splits host:port, returning host and port.
func netSplitHostPort(hostPort string) (host, port string, err error) {
	// Simple split respecting IPv6 brackets.
	if len(hostPort) == 0 {
		return "", "", &parseError{}
	}
	if hostPort[0] == '[' {
		end := strings.LastIndex(hostPort, "]")
		if end < 0 {
			return "", "", &parseError{}
		}
		host = hostPort[1:end]
		rest := hostPort[end+1:]
		if len(rest) > 0 && rest[0] == ':' {
			port = rest[1:]
		}
		return
	}
	colon := strings.LastIndex(hostPort, ":")
	if colon < 0 {
		return hostPort, "", nil
	}
	return hostPort[:colon], hostPort[colon+1:], nil
}

type parseError struct{}

func (e *parseError) Error() string { return "parse error" }

// cookieEntry is a JSON-serialisable representation of an http.Cookie.
type cookieEntry struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Path     string `json:"path,omitempty"`
	Domain   string `json:"domain,omitempty"`
	Expires  int64  `json:"expires,omitempty"`
	MaxAge   int    `json:"max_age"`
	Secure   bool   `json:"secure"`
	HttpOnly bool   `json:"http_only"`
	SameSite int    `json:"same_site"`
}

// fromCookie converts an http.Cookie to a cookieEntry.
func fromCookie(c *http.Cookie) cookieEntry {
	e := cookieEntry{
		Name:     c.Name,
		Value:    c.Value,
		Path:     c.Path,
		Domain:   c.Domain,
		MaxAge:   c.MaxAge,
		Secure:   c.Secure,
		HttpOnly: c.HttpOnly,
		SameSite: int(c.SameSite),
	}
	if !c.Expires.IsZero() {
		e.Expires = c.Expires.Unix()
	}
	return e
}

// toCookie converts a cookieEntry back to an http.Cookie.
func (e cookieEntry) toCookie() *http.Cookie {
	return &http.Cookie{
		Name:     e.Name,
		Value:    e.Value,
		Path:     e.Path,
		Domain:   e.Domain,
		MaxAge:   e.MaxAge,
		Secure:   e.Secure,
		HttpOnly: e.HttpOnly,
		SameSite: http.SameSite(e.SameSite),
	}
}

// cookieFile is the top-level structure serialised to disk.
type cookieFile struct {
	Cookies map[string][]cookieEntry `json:"cookies"`
}

// Save serialises the cookie jar to a file in JSON format.
func (j *CookieJar) Save(path string) error {
	j.mu.RLock()
	defer j.mu.RUnlock()

	cf := cookieFile{Cookies: make(map[string][]cookieEntry, len(j.cookies))}
	for domain, cookies := range j.cookies {
		entries := make([]cookieEntry, 0, len(cookies))
		for _, c := range cookies {
			entries = append(entries, fromCookie(c))
		}
		cf.Cookies[domain] = entries
	}

	data, err := json.Marshal(cf)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Load deserialises the cookie jar from a JSON file previously written by Save.
func (j *CookieJar) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var cf cookieFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return err
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	// Clear existing cookies and rebuild.
	j.cookies = make(map[string][]*http.Cookie, len(cf.Cookies))
	for domain, entries := range cf.Cookies {
		cookies := make([]*http.Cookie, 0, len(entries))
		for _, e := range entries {
			cookies = append(cookies, e.toCookie())
		}
		j.cookies[domain] = cookies
	}
	return nil
}

// Clone returns a deep copy of the cookie jar. It is safe for concurrent use.
func (j *CookieJar) Clone() *CookieJar {
	j.mu.RLock()
	defer j.mu.RUnlock()

	clone := NewCookieJar()
	for domain, cookies := range j.cookies {
		cc := make([]*http.Cookie, len(cookies))
		for i, c := range cookies {
			cp := *c
			cc[i] = &cp
		}
		clone.cookies[domain] = cc
	}
	return clone
}
