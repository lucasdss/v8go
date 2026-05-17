package net

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ----------------------- Request / Response types ----------------------------

// RequestMode mirrors the Fetch Standard § 2.2.6 "mode".
type RequestMode string

const (
	// ModeNavigate is used for top-level navigation requests.
	ModeNavigate RequestMode = "navigate"
	// ModeCORS is used for cross-origin requests with CORS checks.
	ModeCORS RequestMode = "cors"
	// ModeNoCORS is used for opaque no-cors requests.
	ModeNoCORS RequestMode = "no-cors"
	// ModeSameOrigin restricts requests to same-origin only.
	ModeSameOrigin RequestMode = "same-origin"
)

// CredentialsMode mirrors the Fetch Standard § 2.2.9 "credentials mode".
type CredentialsMode string

const (
	// CredOmit omits credentials from the request.
	CredOmit CredentialsMode = "omit"
	// CredSameOrigin sends credentials only for same-origin requests.
	CredSameOrigin CredentialsMode = "same-origin"
	// CredInclude always sends credentials.
	CredInclude CredentialsMode = "include"
)

// RequestDestination mirrors the Fetch Standard § 2.2.3 "destination".
type RequestDestination string

const (
	// DestDocument is used for top-level document requests.
	DestDocument RequestDestination = "document"
	// DestScript is used for script resource requests.
	DestScript RequestDestination = "script"
	// DestStyle is used for stylesheet resource requests.
	DestStyle RequestDestination = "style"
	// DestImage is used for image resource requests.
	DestImage RequestDestination = "image"
	// DestFont is used for font resource requests.
	DestFont RequestDestination = "font"
	// DestFetch is used for fetch() API requests.
	DestFetch RequestDestination = "fetch"
	// DestEmpty is the empty destination (default).
	DestEmpty RequestDestination = ""
)

// ----------------------- Resource Priority ---------------------------------

// ResourcePriority defines the priority ordering for resource fetching.
// Lower values indicate higher priority (fetched first).
type ResourcePriority int

const (
	// PriorityCSS represents render-blocking stylesheets.
	PriorityCSS ResourcePriority = 1
	// PriorityJS represents parser-blocking scripts.
	PriorityJS ResourcePriority = 2
	// PriorityFont represents font resources.
	PriorityFont ResourcePriority = 3
	// PriorityImage represents image resources.
	PriorityImage ResourcePriority = 4
	// PriorityOther is the default priority for all other resources.
	PriorityOther ResourcePriority = 5
)

// ----------------------- Request / Response types (continued) -----------------

// Request represents a Fetch request as defined in the Fetch Standard § 2.2.
type Request struct {
	// Method is the HTTP method (GET, POST, …).
	Method string
	// URL is the request URL.
	URL *URL
	// Headers holds the request header map.
	Headers map[string]string
	// Body is the optional request body.
	Body io.Reader
	// Mode specifies the request mode.
	Mode RequestMode
	// Credentials specifies how cookies/auth credentials are sent.
	Credentials CredentialsMode
	// Destination categorises the resource being fetched.
	Destination RequestDestination
	// Referrer holds the referrer URL string or "no-referrer".
	Referrer string
}

// Response represents a Fetch response as defined in the Fetch Standard § 2.3.
type Response struct {
	// Status is the HTTP status code.
	Status int
	// StatusText is the HTTP reason phrase.
	StatusText string
	// Headers holds the response header map.
	Headers map[string]string
	// Body contains the response body.  The caller is responsible for closing it.
	Body io.ReadCloser
	// URL is the final URL after redirects.
	URL *URL
	// MIMEType is the determined effective content type.
	MIMEType MIMEType
	// CORS flags
	CORSExposed bool
}

// OK reports whether the response has a 2xx status code.
func (r *Response) OK() bool { return r.Status >= 200 && r.Status <= 299 }

// ----------------------- CORS helpers ----------------------------------------

// corsSimpleMethods are the methods that do not require a preflight.
var corsSimpleMethods = map[string]bool{
	"GET":  true,
	"HEAD": true,
	"POST": true,
}

// corsSimpleHeaders are headers that may be sent without a preflight.
// See Fetch Standard § 2.2.2 "CORS-safelisted request-header".
var corsSimpleHeaders = map[string]bool{
	"accept":           true,
	"accept-language":  true,
	"content-language": true,
	"content-type":     true,
}

// isCORSSimpleRequest reports whether a request can be sent without a
// preflight according to the Fetch Standard § 4.1.
func isCORSSimpleRequest(req *Request) bool {
	if !corsSimpleMethods[strings.ToUpper(req.Method)] {
		return false
	}
	for h := range req.Headers {
		if !corsSimpleHeaders[strings.ToLower(h)] {
			return false
		}
	}
	return true
}

// FetchClient is the browser's HTTP client that implements the Fetch
// algorithm including CORS handling and redirect following.
type FetchClient struct {
	// HTTPClient is the underlying Go HTTP client.  If nil a default client
	// with a 30-second timeout is used.
	HTTPClient *http.Client
	// MaxRedirects caps the number of automatic redirects (default: 20).
	MaxRedirects int
	// HTTP2Enabled enables HTTP/2 via the Go stdlib's ForceAttemptHTTP2.
	HTTP2Enabled bool
	// Jar is the cookie jar used for storing and sending cookies.
	Jar *CookieJar
	// Cache is the optional response cache for Cache-Control handling.
	Cache *ResponseCache
	// DefaultCacheMaxAge is the default max-age (in seconds) applied when the
	// server does not provide a Cache-Control or Expires header.
	DefaultCacheMaxAge int64
	// MaxRetries is the number of automatic retries for transient network
	// errors (connection refused, reset, timeout). 0 means no retries.
	MaxRetries int
	// RetryBackoff is the base backoff duration between retries.
	// Each attempt doubles the backoff. Default: 500ms.
	RetryBackoff time.Duration

	// RedirectChain holds the sequence of URLs followed during redirects.
	RedirectChain []string
	redirectMu    sync.Mutex
}

// defaultFetchClient is the package-level default FetchClient.
var defaultFetchClient = &FetchClient{}

// sharedClient is the package-level HTTP client with connection pooling.
// All FetchClient instances that do not specify their own HTTPClient share
// this transport, reusing TCP/TLS connections across requests.
var sharedClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   true,
	},
	Timeout: 30 * time.Second,
}

// hostSemaphores limits concurrent fetches per host to avoid overwhelming
// a single origin with too many simultaneous connections.
var (
	hostSemMu      sync.Mutex
	hostSemaphores = make(map[string]chan struct{})
)

// maxConcurrentPerHost is the maximum number of concurrent requests per host.
const maxConcurrentPerHost = 6

// acquireHostSlot acquires a concurrency slot for the given host.
// The returned function must be called to release the slot.
func acquireHostSlot(host string) func() {
	hostSemMu.Lock()
	sem, ok := hostSemaphores[host]
	if !ok {
		sem = make(chan struct{}, maxConcurrentPerHost)
		hostSemaphores[host] = sem
	}
	hostSemMu.Unlock()
	sem <- struct{}{}
	return func() { <-sem }
}

// SetCookieJar sets the shared cookie jar for this FetchClient.
// If jar is nil, calls that follow will lazily create an empty jar
// (preserving the current behaviour of starting with an empty cookie
// store per client).
func (fc *FetchClient) SetCookieJar(jar *CookieJar) {
	fc.Jar = jar
}

// GetCookieJar returns the current cookie jar, which may be nil if none
// has been set and no request has been made yet.
func (fc *FetchClient) GetCookieJar() *CookieJar {
	return fc.Jar
}

// Fetch performs an HTTP fetch following the Fetch Standard § 4.
// It is safe for concurrent use.
func Fetch(ctx context.Context, req *Request) (*Response, error) {
	return defaultFetchClient.Fetch(ctx, req)
}

// Fetch performs an HTTP fetch following the Fetch Standard § 4.
func (fc *FetchClient) Fetch(ctx context.Context, req *Request) (*Response, error) {
	if req == nil {
		return nil, fmt.Errorf("fetch: nil request")
	}
	if req.URL == nil {
		return nil, fmt.Errorf("fetch: nil URL")
	}

	method := strings.ToUpper(req.Method)
	if method == "" {
		method = "GET"
	}

	// Check cache before network fetch (only for GET/HEAD, no-store excluded).
	if fc.Cache != nil && (method == "GET" || method == "HEAD") {
		if cached, ok := fc.Cache.Get(method, req.URL); ok {
			body := io.NopCloser(bytes.NewReader(cached.Body))
			return &Response{
				Status:     cached.Status,
				StatusText: cached.StatusText,
				Headers:    cached.Headers,
				Body:       body,
				URL:        cached.URL,
				MIMEType:   cached.MIMEType,
			}, nil
		}
	}

	// Retry loop for transient errors on idempotent requests.
	maxRetries := fc.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	backoff := fc.RetryBackoff
	if backoff <= 0 {
		backoff = 500 * time.Millisecond
	}
	idempotent := method == "GET" || method == "HEAD" || method == "OPTIONS"

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s, ...
			sleep := backoff * time.Duration(1<<uint(attempt-1))
			if sleep > 15*time.Second {
				sleep = 15 * time.Second
			}
			log.Printf("[fetch] retry %d/%d for %s after %v", attempt, maxRetries, req.URL.Serialize(), sleep)
			select {
			case <-time.After(sleep):
			case <-ctx.Done():
				return nil, fmt.Errorf("fetch: context cancelled during retry backoff: %w", ctx.Err())
			}
		}

		resp, err := fc.doFetch(ctx, req, method)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		// Only retry transient/network errors on idempotent requests.
		if !idempotent {
			break
		}
		if !isRetryable(err) {
			break
		}
	}

	return nil, fmt.Errorf("fetch: %w", lastErr)
}

// isRetryable reports whether an error is likely transient and worth retrying.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// Common transient error patterns.
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "TLS handshake timeout") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "EOF")
}

// doFetch performs a single HTTP fetch attempt (no retry logic).
func (fc *FetchClient) doFetch(ctx context.Context, req *Request, method string) (*Response, error) {

	// Reset redirect chain for this request.
	fc.redirectMu.Lock()
	fc.RedirectChain = fc.RedirectChain[:0]
	fc.redirectMu.Unlock()

	// Acquire per-host concurrency slot to limit simultaneous connections.
	release := acquireHostSlot(req.URL.Host)
	defer release()

	baseClient := fc.httpClient()
	// Shallow-copy the client so we can set a per-request CheckRedirect
	// without mutating the shared client.  The Transport (and thus the
	// connection pool) is shared via the pointer.
	clientCopy := *baseClient
	client := &clientCopy
	rawURL := req.URL.Serialize()

	// Record initial URL in redirect chain.
	fc.appendRedirect(rawURL)

	httpReq, err := http.NewRequestWithContext(ctx, method, rawURL, req.Body)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	// Copy caller-supplied headers.
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// Attach cookies from the cookie jar (lazy-init if nil).
	if fc.Jar == nil {
		fc.Jar = NewCookieJar()
	}
	jarURL, _ := urlParse(rawURL)
	if jarURL != nil {
		for _, c := range fc.Jar.Cookies(jarURL) {
			httpReq.AddCookie(c)
		}
	}

	// For CORS mode requests that are not "simple", send a preflight.
	if req.Mode == ModeCORS && !isCORSSimpleRequest(req) {
		if err := fc.doCORSPreflight(ctx, req, client); err != nil {
			return nil, err
		}
	}

	maxRedirects := fc.MaxRedirects
	if maxRedirects == 0 {
		maxRedirects = 20
	}

	// Disable the stdlib's automatic redirect following so we can apply
	// Fetch Standard redirect policy ourselves.
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if r.URL != nil {
			fc.appendRedirect(r.URL.String())
		}
		if len(via) >= maxRedirects {
			return http.ErrUseLastResponse
		}
		return nil
	}

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	// Build our Response.
	resp := &Response{
		Status:     httpResp.StatusCode,
		StatusText: httpResp.Status,
		Headers:    make(map[string]string),
		Body:       httpResp.Body,
	}

	finalURL, parseErr := ParseURL(httpResp.Request.URL.String())
	if parseErr == nil {
		resp.URL = finalURL
	}

	for key := range httpResp.Header {
		resp.Headers[key] = httpResp.Header.Get(key)
	}

	// Store cookies from Set-Cookie headers.
	if respURL, _ := urlParse(httpResp.Request.URL.String()); respURL != nil {
		cookies := httpResp.Cookies()
		if len(cookies) > 0 {
			fc.Jar.SetCookies(respURL, cookies)
		}
	}

	// Determine MIME type: first from header, then sniff if necessary.
	ct := httpResp.Header.Get("Content-Type")
	resp.MIMEType = DetermineType(ct, nil)

	// CORS: check that the response carries the allow header when in CORS mode.
	if req.Mode == ModeCORS {
		acao := httpResp.Header.Get("Access-Control-Allow-Origin")
		if acao == "*" || acao == req.URL.Origin() {
			resp.CORSExposed = true
		}
	}

	// Cache the response if applicable (GET/HEAD only, status 200-299).
	if fc.Cache != nil && (method == "GET" || method == "HEAD") &&
		resp.Status >= 200 && resp.Status <= 299 {
		maxAge := DetermineCacheMaxAge(resp.Headers, fc.DefaultCacheMaxAge)
		if maxAge > 0 {
			bodyBytes, readErr := io.ReadAll(httpResp.Body)
			if readErr == nil {
				// Replace the consumed body with a fresh reader.
				resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				fc.Cache.Put(method, req.URL, resp, maxAge, bodyBytes)
			}
		}
	}

	return resp, nil
}

// appendRedirect adds a URL to the redirect chain.
func (fc *FetchClient) appendRedirect(urlStr string) {
	fc.redirectMu.Lock()
	fc.RedirectChain = append(fc.RedirectChain, urlStr)
	chainLen := len(fc.RedirectChain)
	fc.redirectMu.Unlock()
	if chainLen > 0 {
		log.Printf("[fetch] redirect #%d -> %s", chainLen-1, urlStr)
	}
}

// urlParse is a small helper to parse a URL string into a *url.URL for
// cookie jar operations.
func urlParse(raw string) (*url.URL, error) {
	return url.Parse(raw)
}

// doCORSPreflight sends an OPTIONS preflight request and validates the
// Access-Control-Allow-* response headers.
// See Fetch Standard § 4.8 "CORS-preflight fetch".
func (fc *FetchClient) doCORSPreflight(ctx context.Context, req *Request, client *http.Client) error {
	rawURL := req.URL.Serialize()
	preflightReq, err := http.NewRequestWithContext(ctx, "OPTIONS", rawURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("cors preflight: %w", err)
	}

	method := strings.ToUpper(req.Method)
	preflightReq.Header.Set("Access-Control-Request-Method", method)

	if len(req.Headers) > 0 {
		headers := make([]string, 0, len(req.Headers))
		for h := range req.Headers {
			headers = append(headers, strings.ToLower(h))
		}
		preflightReq.Header.Set("Access-Control-Request-Headers", strings.Join(headers, ", "))
	}

	if req.URL != nil {
		preflightReq.Header.Set("Origin", req.URL.Origin())
	}

	resp, err := client.Do(preflightReq)
	if err != nil {
		return fmt.Errorf("cors preflight: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			// Best-effort close; the body will be GC'd regardless.
			_ = cerr
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("cors preflight: server responded with %d", resp.StatusCode)
	}

	allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	if allowOrigin != "*" && allowOrigin != req.URL.Origin() {
		return fmt.Errorf("cors preflight: origin not allowed by server")
	}

	allowMethod := resp.Header.Get("Access-Control-Allow-Methods")
	if !strings.Contains(strings.ToUpper(allowMethod), method) &&
		allowMethod != "*" {
		return fmt.Errorf("cors preflight: method %q not allowed", method)
	}

	return nil
}

// httpClient returns the FetchClient's HTTP client.  If the caller supplied a
// custom HTTPClient it is returned; otherwise the package-level sharedClient
// with connection pooling is used.
func (fc *FetchClient) httpClient() *http.Client {
	if fc.HTTPClient != nil {
		return fc.HTTPClient
	}
	return sharedClient
}
