package net

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CacheDirective holds a single parsed value from a Cache-Control header.
type CacheDirective struct {
	Name  string
	Value string // empty for valueless directives like "no-cache"
}

// CachedResponse holds a cached HTTP response with its metadata.
type CachedResponse struct {
	Status     int
	StatusText string
	Headers    map[string]string
	Body       []byte
	URL        *URL
	MIMEType   MIMEType
	// StoredAt is when this entry was cached.
	StoredAt time.Time
	// MaxAge is the max-age directive value in seconds (0 means not set).
	MaxAge int64
	// ETag for conditional revalidation.
	ETag string
	// LastModified for conditional revalidation.
	LastModified string
}

// IsFresh reports whether the cached response is still fresh based on max-age.
func (cr *CachedResponse) IsFresh() bool {
	if cr.MaxAge <= 0 {
		return false
	}
	return time.Since(cr.StoredAt) < time.Duration(cr.MaxAge)*time.Second
}

// ResponseCache is an in-memory, size-limited response cache keyed by URL+method.
// It is safe for concurrent use.
type ResponseCache struct {
	mu       sync.RWMutex
	entries  map[string]*CachedResponse // key = method + " " + url
	maxSize  int                        // maximum number of entries
	maxBytes int64                      // approximate maximum total body bytes
	totalBytes int64                    // current total body bytes

	// stats (informational only, not needed for correctness)
	hits   int64
	misses int64
}

// NewResponseCache creates a ResponseCache with the given maximum number of
// entries and approximate maximum total body bytes. A value of 0 for maxSize
// or maxBytes means no limit.
func NewResponseCache(maxSize int, maxBytes int64) *ResponseCache {
	return &ResponseCache{
		entries:  make(map[string]*CachedResponse),
		maxSize:  maxSize,
		maxBytes: maxBytes,
	}
}

// cacheKey builds a storage key from method and URL.
func cacheKey(method string, u *URL) string {
	return strings.ToUpper(method) + " " + u.Serialize()
}

// Get looks up a cached response for the given method and URL. It returns the
// cached response and true if a fresh cache entry exists.
func (rc *ResponseCache) Get(method string, u *URL) (*CachedResponse, bool) {
	key := cacheKey(method, u)

	rc.mu.RLock()
	entry, ok := rc.entries[key]
	rc.mu.RUnlock()

	if !ok {
		rc.mu.Lock()
		rc.misses++
		rc.mu.Unlock()
		return nil, false
	}

	if !entry.IsFresh() {
		rc.mu.Lock()
		delete(rc.entries, key)
		rc.totalBytes -= int64(len(entry.Body))
		rc.mu.Unlock()
		return nil, false
	}

	rc.mu.Lock()
	rc.hits++
	rc.mu.Unlock()
	return entry, true
}

// Put stores a response in the cache. The response is stored with the given
// max-age value. If maxAge <= 0, the response is not cached (controlled by
// no-cache/no-store directives).
func (rc *ResponseCache) Put(method string, u *URL, resp *Response, maxAge int64, body []byte) {
	if maxAge <= 0 || resp == nil {
		return
	}

	key := cacheKey(method, u)

	rc.mu.Lock()
	defer rc.mu.Unlock()

	// Evict if we're at capacity.
	if rc.maxSize > 0 && len(rc.entries) >= rc.maxSize {
		rc.evictOne()
	}
	if rc.maxBytes > 0 && rc.totalBytes+int64(len(body)) > rc.maxBytes {
		// Evict until we have room.
		for rc.maxBytes > 0 && rc.totalBytes+int64(len(body)) > rc.maxBytes && len(rc.entries) > 0 {
			rc.evictOne()
		}
	}

	headers := make(map[string]string, len(resp.Headers))
	for k, v := range resp.Headers {
		headers[k] = v
	}

	entry := &CachedResponse{
		Status:       resp.Status,
		StatusText:   resp.StatusText,
		Headers:      headers,
		Body:         body,
		URL:          resp.URL,
		MIMEType:     resp.MIMEType,
		StoredAt:     time.Now(),
		MaxAge:       maxAge,
		ETag:         resp.Headers["Etag"],
		LastModified: resp.Headers["Last-Modified"],
	}

	// Remove old entry to update totalBytes correctly.
	if old, ok := rc.entries[key]; ok {
		rc.totalBytes -= int64(len(old.Body))
	}

	rc.entries[key] = entry
	rc.totalBytes += int64(len(body))
}

// evictOne removes one entry from the cache (the first one found). Must be
// called with mu held.
func (rc *ResponseCache) evictOne() {
	for k, v := range rc.entries {
		rc.totalBytes -= int64(len(v.Body))
		delete(rc.entries, k)
		return
	}
}

// Stats returns cache hit and miss counts.
func (rc *ResponseCache) Stats() (hits, misses int64) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.hits, rc.misses
}

// Size returns the current number of entries in the cache.
func (rc *ResponseCache) Size() int {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return len(rc.entries)
}

// Clear removes all entries from the cache.
func (rc *ResponseCache) Clear() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.entries = make(map[string]*CachedResponse)
	rc.totalBytes = 0
}

// ParseCacheControl parses a Cache-Control header value and returns a list of
// directives. It handles quoted string values (e.g., max-age="3600").
func ParseCacheControl(header string) []CacheDirective {
	var dirs []CacheDirective
	for _, part := range splitCommas(header) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		d := CacheDirective{}
		if idx := strings.Index(part, "="); idx >= 0 {
			d.Name = strings.TrimSpace(strings.ToLower(part[:idx]))
			d.Value = strings.Trim(strings.TrimSpace(part[idx+1:]), "\"")
		} else {
			d.Name = strings.TrimSpace(strings.ToLower(part))
		}
		dirs = append(dirs, d)
	}
	return dirs
}

// splitCommas splits a string by commas, respecting quoted sections.
func splitCommas(s string) []string {
	var result []string
	var current strings.Builder
	inQuotes := false
	for _, ch := range s {
		switch ch {
		case '"':
			inQuotes = !inQuotes
			current.WriteRune(ch)
		case ',':
			if inQuotes {
				current.WriteRune(ch)
			} else {
				result = append(result, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		result = append(result, current.String())
	}
	return result
}

// CachePolicy summarizes the caching policy from a set of Cache-Control
// directives. It returns:
//   - noStore: true if the response must not be stored
//   - noCache: true if the response must be revalidated before use
//   - maxAge: the max-age value in seconds (0 if not present)
//   - isPublic: true if the response can be cached by shared caches
func CachePolicy(directives []CacheDirective) (noStore, noCache bool, maxAge int64, isPublic bool) {
	for _, d := range directives {
		switch d.Name {
		case "no-store":
			noStore = true
		case "no-cache":
			noCache = true
		case "max-age":
			if v, err := strconv.ParseInt(d.Value, 10, 64); err == nil {
				maxAge = v
			}
		case "public":
			isPublic = true
		case "private":
			isPublic = false
		}
	}
	return
}

// DetermineCacheMaxAge returns the effective max-age for a response, or 0 if
// the response should not be cached. It checks Cache-Control first, falling
// back to Expires, and uses a default if neither is present.
func DetermineCacheMaxAge(respHeaders map[string]string, defaultMaxAge int64) int64 {
	cacheControl := respHeaders["Cache-Control"]
	if cacheControl == "" {
		cacheControl = respHeaders["Cache-Control"] // try case-insensitive via headers map
	}

	if cacheControl != "" {
		dirs := ParseCacheControl(cacheControl)
		noStore, _, maxAge, _ := CachePolicy(dirs)
		if noStore {
			return 0
		}
		if maxAge > 0 {
			return maxAge
		}
	}

	// Fall back to Expires header.
	if expires := respHeaders["Expires"]; expires != "" {
		if t, err := time.Parse(time.RFC1123, expires); err == nil {
			if remaining := time.Until(t); remaining > 0 {
				return int64(remaining.Seconds())
			}
			return 0
		}
	}

	// Fall back to default.
	return defaultMaxAge
}

// StaleError indicates a cached entry exists but is stale.
type StaleError struct {
	Entry *CachedResponse
}

func (e *StaleError) Error() string {
	return fmt.Sprintf("cached response for %s is stale", e.Entry.URL)
}
