package net_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	browserNet "github.com/lucasdss/v8go/pkg/net"
)

// ----------------------------- URL parsing tests ----------------------------

func TestParseURL_ValidHTTPS(t *testing.T) {
	u, err := browserNet.ParseURL("https://example.com/path?q=1#frag")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.Scheme != "https" {
		t.Errorf("Scheme: got %q, want %q", u.Scheme, "https")
	}
	if u.Host != "example.com" {
		t.Errorf("Host: got %q, want %q", u.Host, "example.com")
	}
	if len(u.Path) == 0 || u.Path[0] != "path" {
		t.Errorf("Path[0]: got %v, want %q", u.Path, "path")
	}
	if u.Query != "q=1" {
		t.Errorf("Query: got %q, want %q", u.Query, "q=1")
	}
	if u.Fragment != "frag" {
		t.Errorf("Fragment: got %q, want %q", u.Fragment, "frag")
	}
}

func TestParseURL_DefaultPortOmitted(t *testing.T) {
	u, err := browserNet.ParseURL("https://example.com:443/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.Port != "" {
		t.Errorf("Port should be empty for default HTTPS port, got %q", u.Port)
	}
}

func TestParseURL_NonDefaultPortKept(t *testing.T) {
	u, err := browserNet.ParseURL("https://example.com:8443/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.Port != "8443" {
		t.Errorf("Port: got %q, want %q", u.Port, "8443")
	}
}

func TestParseURL_HTTPDefaultPortOmitted(t *testing.T) {
	u, err := browserNet.ParseURL("http://example.com:80/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.Port != "" {
		t.Errorf("Port should be empty for default HTTP port, got %q", u.Port)
	}
}

func TestParseURL_MissingScheme(t *testing.T) {
	_, err := browserNet.ParseURL("example.com/path")
	if err == nil {
		t.Error("expected error for URL without scheme")
	}
}

func TestParseURL_Empty(t *testing.T) {
	_, err := browserNet.ParseURL("")
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

func TestParseURL_Whitespace(t *testing.T) {
	_, err := browserNet.ParseURL("   ")
	if err == nil {
		t.Error("expected error for whitespace URL")
	}
}

func TestParseURL_DataSchemeOpaque(t *testing.T) {
	u, err := browserNet.ParseURL("data:text/html,<h1>Hello</h1>")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.Scheme != "data" {
		t.Errorf("Scheme: got %q, want %q", u.Scheme, "data")
	}
	if u.Origin() != "null" {
		t.Errorf("Origin: got %q, want %q", u.Origin(), "null")
	}
}

func TestURL_Origin_HTTP(t *testing.T) {
	u, _ := browserNet.ParseURL("http://example.com/")
	if u.Origin() != "http://example.com" {
		t.Errorf("Origin: got %q, want %q", u.Origin(), "http://example.com")
	}
}

func TestURL_Origin_HTTPS_WithPort(t *testing.T) {
	u, _ := browserNet.ParseURL("https://example.com:8443/path")
	if u.Origin() != "https://example.com:8443" {
		t.Errorf("Origin: got %q, want %q", u.Origin(), "https://example.com:8443")
	}
}

func TestURL_IsSpecial(t *testing.T) {
	cases := []struct {
		url     string
		special bool
	}{
		{"https://example.com", true},
		{"http://example.com", true},
		{"ftp://example.com", true},
		{"data:text/html,foo", false},
		{"blob:https://example.com/uuid", false},
	}
	for _, tc := range cases {
		u, err := browserNet.ParseURL(tc.url)
		if err != nil {
			t.Logf("skipping %s (parse error: %v)", tc.url, err)
			continue
		}
		if u.IsSpecial() != tc.special {
			t.Errorf("IsSpecial(%q) = %v, want %v", tc.url, u.IsSpecial(), tc.special)
		}
	}
}

func TestURL_Serialize_RoundTrip(t *testing.T) {
	inputs := []string{
		"https://example.com/",
		"https://user:pass@example.com:8080/path?q=1#frag",
		"http://example.com/a/b/c",
	}
	for _, raw := range inputs {
		u, err := browserNet.ParseURL(raw)
		if err != nil {
			t.Errorf("ParseURL(%q): %v", raw, err)
			continue
		}
		serialized := u.Serialize()
		u2, err := browserNet.ParseURL(serialized)
		if err != nil {
			t.Errorf("re-parse of serialized URL %q: %v", serialized, err)
			continue
		}
		if u2.Scheme != u.Scheme || u2.Host != u.Host {
			t.Errorf("round-trip mismatch for %q: got %q", raw, serialized)
		}
	}
}

func TestURL_ResolveReference(t *testing.T) {
	base, _ := browserNet.ParseURL("https://example.com/a/b/")
	ref, _ := browserNet.ParseURL("https://example.com/a/c")
	resolved, err := base.ResolveReference(ref)
	if err != nil {
		t.Fatalf("ResolveReference: %v", err)
	}
	if resolved.Host != "example.com" {
		t.Errorf("resolved host: got %q, want %q", resolved.Host, "example.com")
	}
}

func TestURL_ResolveReference_NilRef(t *testing.T) {
	base, _ := browserNet.ParseURL("https://example.com/")
	_, err := base.ResolveReference(nil)
	if err == nil {
		t.Error("expected error for nil reference URL")
	}
}

func TestURL_ResolveReference_RelativePaths(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		ref      string
		expected string
	}{
		{"absolute path", "https://example.com/a/b/", "/css/style.css", "https://example.com/css/style.css"},
		{"relative path", "https://example.com/a/b/", "style.css", "https://example.com/a/b/style.css"},
		{"parent path", "https://example.com/a/b/", "../images/logo.png", "https://example.com/a/images/logo.png"},
		{"root path", "https://example.com/a/b/c", "/", "https://example.com/"},
		{"absolute URL", "https://example.com/", "https://cdn.example.com/app.js", "https://cdn.example.com/app.js"},
		{"scheme-relative", "https://example.com/", "//cdn.example.com/font.woff", "https://cdn.example.com/font.woff"},
		{"dot slash", "https://example.com/a/b/", "./page.html", "https://example.com/a/b/page.html"},
		{"complex relative", "https://example.com/a/b/c/d", "../../../x/y", "https://example.com/x/y"},
		{"two levels up", "https://example.com/a/b/c/", "../../x/y", "https://example.com/a/x/y"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, err := browserNet.ParseURL(tt.base)
			if err != nil {
				t.Fatalf("ParseURL(base): %v", err)
			}
			ref, err := browserNet.ParseRefURL(tt.ref)
			if err != nil {
				t.Fatalf("ParseURL(ref): %v", err)
			}
			resolved, err := base.ResolveReference(ref)
			if err != nil {
				t.Fatalf("ResolveReference: %v", err)
			}
			got := resolved.Serialize()
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestFetch_ReturnsStreamingBodyBeforeResponseCompletes(t *testing.T) {
	firstChunkWritten := make(chan struct{})
	allowFinish := make(chan struct{})
	var finishOnce sync.Once

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("first-"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(firstChunkWritten)

		select {
		case <-allowFinish:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte("second"))
	}))
	defer server.Close()
	defer finishOnce.Do(func() { close(allowFinish) })

	parsedURL, err := browserNet.ParseURL(server.URL)
	if err != nil {
		t.Fatalf("ParseURL: %v", err)
	}

	respCh := make(chan *browserNet.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := browserNet.Fetch(context.Background(), &browserNet.Request{
			Method: "GET",
			URL:    parsedURL,
			Mode:   browserNet.ModeNavigate,
		})
		if err != nil {
			errCh <- err
			return
		}
		respCh <- resp
	}()

	select {
	case <-firstChunkWritten:
	case <-time.After(time.Second):
		t.Fatal("server did not produce the first response chunk")
	}

	var resp *browserNet.Response
	select {
	case err := <-errCh:
		t.Fatalf("Fetch: %v", err)
	case resp = <-respCh:
	case <-time.After(time.Second):
		t.Fatal("Fetch blocked until the full response body completed; expected streaming Body")
	}
	defer func() { _ = resp.Body.Close() }()

	buf := make([]byte, len("first-"))
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		t.Fatalf("ReadFull first chunk: %v", err)
	}
	if string(buf) != "first-" {
		t.Fatalf("first chunk = %q, want %q", string(buf), "first-")
	}

	finishOnce.Do(func() { close(allowFinish) })
	rest, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll rest: %v", err)
	}
	if string(rest) != "second" {
		t.Fatalf("remaining body = %q, want %q", string(rest), "second")
	}
}

// ----------------------------- MIME tests ------------------------------------

func TestParseMIMEType_Basic(t *testing.T) {
	mt, err := browserNet.ParseMIMEType("text/html; charset=utf-8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mt.Type != "text" || mt.Subtype != "html" {
		t.Errorf("type/subtype: got %q/%q, want text/html", mt.Type, mt.Subtype)
	}
	if mt.Params["charset"] != "utf-8" {
		t.Errorf("charset param: got %q, want %q", mt.Params["charset"], "utf-8")
	}
}

func TestParseMIMEType_MissingSubtype(t *testing.T) {
	_, err := browserNet.ParseMIMEType("text")
	if err == nil {
		t.Error("expected error for missing subtype")
	}
}

func TestParseMIMEType_CaseInsensitive(t *testing.T) {
	mt, err := browserNet.ParseMIMEType("TEXT/HTML")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mt.Type != "text" || mt.Subtype != "html" {
		t.Errorf("got %q/%q, want text/html", mt.Type, mt.Subtype)
	}
}

func TestMIMEType_IsHTML(t *testing.T) {
	mt, _ := browserNet.ParseMIMEType("text/html")
	if !mt.IsHTML() {
		t.Error("text/html should be IsHTML")
	}
}

func TestMIMEType_IsJavaScript(t *testing.T) {
	cases := []string{"text/javascript", "application/javascript", "application/x-javascript"}
	for _, ct := range cases {
		mt, _ := browserNet.ParseMIMEType(ct)
		if !mt.IsJavaScript() {
			t.Errorf("%q should be IsJavaScript", ct)
		}
	}
}

func TestMIMEType_IsCSS(t *testing.T) {
	mt, _ := browserNet.ParseMIMEType("text/css")
	if !mt.IsCSS() {
		t.Error("text/css should be IsCSS")
	}
}

func TestSniffMIMEType_HTML(t *testing.T) {
	cases := []string{"<!DOCTYPE html>", "<html>", "<head>", "<div>"}
	for _, html := range cases {
		got := browserNet.SniffMIMEType([]byte(html))
		if got != "text/html" {
			t.Errorf("SniffMIMEType(%q) = %q, want text/html", html, got)
		}
	}
}

func TestSniffMIMEType_PNG(t *testing.T) {
	pngHeader := []byte("\x89PNG\r\n\x1a\n")
	got := browserNet.SniffMIMEType(pngHeader)
	if got != "image/png" {
		t.Errorf("SniffMIMEType(png header) = %q, want image/png", got)
	}
}

func TestSniffMIMEType_JPEG(t *testing.T) {
	jpegHeader := []byte{0xff, 0xd8, 0xff, 0xe0}
	got := browserNet.SniffMIMEType(jpegHeader)
	if got != "image/jpeg" {
		t.Errorf("SniffMIMEType(jpeg header) = %q, want image/jpeg", got)
	}
}

func TestSniffMIMEType_PDF(t *testing.T) {
	got := browserNet.SniffMIMEType([]byte("%PDF-1.5"))
	if got != "application/pdf" {
		t.Errorf("SniffMIMEType(pdf header) = %q, want application/pdf", got)
	}
}

func TestSniffMIMEType_XML(t *testing.T) {
	got := browserNet.SniffMIMEType([]byte("<?xml version"))
	if got != "text/xml" {
		t.Errorf("SniffMIMEType(xml header) = %q, want text/xml", got)
	}
}

func TestSniffMIMEType_PlainText(t *testing.T) {
	got := browserNet.SniffMIMEType([]byte("Hello, World!\n"))
	if got != "text/plain" {
		t.Errorf("SniffMIMEType(plain text) = %q, want text/plain", got)
	}
}

func TestSniffMIMEType_Binary(t *testing.T) {
	got := browserNet.SniffMIMEType([]byte{0x00, 0x01, 0x02, 0x03})
	if got != "application/octet-stream" {
		t.Errorf("SniffMIMEType(binary) = %q, want application/octet-stream", got)
	}
}

func TestDetermineType_PreferHeader(t *testing.T) {
	mt := browserNet.DetermineType("text/html; charset=utf-8", []byte("<html>"))
	if mt.Type != "text" || mt.Subtype != "html" {
		t.Errorf("DetermineType with header: got %q/%q", mt.Type, mt.Subtype)
	}
}

func TestDetermineType_SniffWhenEmpty(t *testing.T) {
	mt := browserNet.DetermineType("", []byte("<html>"))
	if mt.Type != "text" || mt.Subtype != "html" {
		t.Errorf("DetermineType with empty header and html body: got %q/%q", mt.Type, mt.Subtype)
	}
}

func TestDetermineType_SniffWhenOctetStream(t *testing.T) {
	mt := browserNet.DetermineType("application/octet-stream", []byte("<!DOCTYPE html>"))
	if mt.Type != "text" || mt.Subtype != "html" {
		t.Errorf("DetermineType with octet-stream header and html body: got %q/%q", mt.Type, mt.Subtype)
	}
}

// ---------------------------- Cookie Jar tests --------------------------------

func TestCookieJar_SetAndGetCookies(t *testing.T) {
	jar := browserNet.NewCookieJar()
	u, _ := url.Parse("https://example.com/")

	jar.SetCookies(u, []*http.Cookie{
		{Name: "session", Value: "abc123", Path: "/"},
		{Name: "theme", Value: "dark", Path: "/"},
	})

	cookies := jar.Cookies(u)
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(cookies))
	}

	names := map[string]string{}
	for _, c := range cookies {
		names[c.Name] = c.Value
	}
	if names["session"] != "abc123" {
		t.Errorf("session cookie: got %q, want %q", names["session"], "abc123")
	}
	if names["theme"] != "dark" {
		t.Errorf("theme cookie: got %q, want %q", names["theme"], "dark")
	}
}

func TestCookieJar_DomainScope(t *testing.T) {
	jar := browserNet.NewCookieJar()
	u, _ := url.Parse("https://example.com/")

	jar.SetCookies(u, []*http.Cookie{
		{Name: "a", Value: "1", Domain: "example.com", Path: "/"},
		{Name: "b", Value: "2", Domain: "other.com", Path: "/"},
	})

	cookies := jar.Cookies(u)
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie matching domain, got %d", len(cookies))
	}
	if cookies[0].Name != "a" {
		t.Errorf("got cookie %q, want %q", cookies[0].Name, "a")
	}
}

func TestCookieJar_SubdomainMatch(t *testing.T) {
	jar := browserNet.NewCookieJar()

	// Cookie set for .example.com should match sub.example.com.
	jar.SetCookies(mustParseURL("https://example.com/"), []*http.Cookie{
		{Name: "shared", Value: "1", Domain: "example.com", Path: "/"},
	})

	sub := mustParseURL("https://sub.example.com/")
	cookies := jar.Cookies(sub)
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie for subdomain, got %d", len(cookies))
	}
	if cookies[0].Name != "shared" {
		t.Errorf("got %q, want %q", cookies[0].Name, "shared")
	}
}

func TestCookieJar_SecureCookieNotSentOverHTTP(t *testing.T) {
	jar := browserNet.NewCookieJar()

	jar.SetCookies(mustParseURL("https://example.com/"), []*http.Cookie{
		{Name: "secure", Value: "1", Secure: true, Path: "/"},
	})

	httpURL := mustParseURL("http://example.com/")
	cookies := jar.Cookies(httpURL)
	if len(cookies) != 0 {
		t.Errorf("expected secure cookie NOT sent over HTTP, got %d cookies", len(cookies))
	}
}

func TestCookieJar_SecureCookieSentOverHTTPS(t *testing.T) {
	jar := browserNet.NewCookieJar()

	jar.SetCookies(mustParseURL("https://example.com/"), []*http.Cookie{
		{Name: "secure", Value: "1", Secure: true, Path: "/"},
	})

	httpsURL := mustParseURL("https://example.com/")
	cookies := jar.Cookies(httpsURL)
	if len(cookies) != 1 {
		t.Errorf("expected secure cookie sent over HTTPS, got %d cookies", len(cookies))
	}
}

func TestCookieJar_ExpiredCookieRemoved(t *testing.T) {
	jar := browserNet.NewCookieJar()
	u := mustParseURL("https://example.com/")

	jar.SetCookies(u, []*http.Cookie{
		{Name: "expired", Value: "1", Path: "/", Expires: time.Now().Add(-1 * time.Hour)},
	})

	cookies := jar.Cookies(u)
	if len(cookies) != 0 {
		t.Errorf("expected expired cookie to be removed, got %d cookies", len(cookies))
	}
}

func TestCookieJar_MaxAgeExpired(t *testing.T) {
	jar := browserNet.NewCookieJar()
	u := mustParseURL("https://example.com/")

	// MaxAge < 0 should delete the cookie.
	jar.SetCookies(u, []*http.Cookie{
		{Name: "temp", Value: "1", Path: "/", MaxAge: -1},
	})

	cookies := jar.Cookies(u)
	if len(cookies) != 0 {
		t.Errorf("expected MaxAge=-1 cookie to be deleted, got %d cookies", len(cookies))
	}
}

func TestCookieJar_PathMatch(t *testing.T) {
	jar := browserNet.NewCookieJar()

	jar.SetCookies(mustParseURL("https://example.com/"), []*http.Cookie{
		{Name: "root", Value: "1", Path: "/"},
		{Name: "sub", Value: "2", Path: "/sub"},
	})

	// Request to / should only get root cookie.
	cookies := jar.Cookies(mustParseURL("https://example.com/"))
	if len(cookies) != 1 || cookies[0].Name != "root" {
		t.Errorf("expected only root cookie for /, got %d cookies", len(cookies))
	}

	// Request to /sub/page should get both.
	cookies = jar.Cookies(mustParseURL("https://example.com/sub/page"))
	if len(cookies) != 2 {
		t.Errorf("expected 2 cookies for /sub/page, got %d", len(cookies))
	}

	// Request to /other should only get root cookie.
	cookies = jar.Cookies(mustParseURL("https://example.com/other"))
	if len(cookies) != 1 || cookies[0].Name != "root" {
		t.Errorf("expected only root cookie for /other, got %d cookies", len(cookies))
	}
}

func TestCookieJar_CookieReplacement(t *testing.T) {
	jar := browserNet.NewCookieJar()
	u := mustParseURL("https://example.com/")

	jar.SetCookies(u, []*http.Cookie{
		{Name: "x", Value: "1", Path: "/"},
	})
	jar.SetCookies(u, []*http.Cookie{
		{Name: "x", Value: "2", Path: "/"},
	})

	cookies := jar.Cookies(u)
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie after replacement, got %d", len(cookies))
	}
	if cookies[0].Value != "2" {
		t.Errorf("expected value '2', got %q", cookies[0].Value)
	}
}

func TestCookieJar_Concurrent(t *testing.T) {
	jar := browserNet.NewCookieJar()
	u := mustParseURL("https://example.com/")

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			jar.SetCookies(u, []*http.Cookie{
				{Name: "c", Value: "v", Path: "/"},
			})
			_ = jar.Cookies(u)
		}(i)
	}
	wg.Wait()

	cookies := jar.Cookies(u)
	if len(cookies) == 0 {
		t.Error("expected at least 1 cookie after concurrent access")
	}
}

// ---------------------------- Cache-Control tests ----------------------------

func TestParseCacheControl_MaxAge(t *testing.T) {
	dirs := browserNet.ParseCacheControl("max-age=3600, public")
	if len(dirs) != 2 {
		t.Fatalf("expected 2 directives, got %d", len(dirs))
	}
	if dirs[0].Name != "max-age" || dirs[0].Value != "3600" {
		t.Errorf("first directive: %s=%s", dirs[0].Name, dirs[0].Value)
	}
	if dirs[1].Name != "public" {
		t.Errorf("second directive: %s", dirs[1].Name)
	}
}

func TestParseCacheControl_NoCache(t *testing.T) {
	dirs := browserNet.ParseCacheControl("no-cache, no-store")
	if len(dirs) != 2 {
		t.Fatalf("expected 2 directives, got %d", len(dirs))
	}
	if dirs[0].Name != "no-cache" {
		t.Errorf("got %q, want no-cache", dirs[0].Name)
	}
	if dirs[1].Name != "no-store" {
		t.Errorf("got %q, want no-store", dirs[1].Name)
	}
}

func TestCachePolicy_NoStore(t *testing.T) {
	dirs := browserNet.ParseCacheControl("no-store")
	noStore, _, _, _ := browserNet.CachePolicy(dirs)
	if !noStore {
		t.Error("expected noStore=true")
	}
}

func TestCachePolicy_MaxAge(t *testing.T) {
	dirs := browserNet.ParseCacheControl("max-age=1800")
	_, _, maxAge, _ := browserNet.CachePolicy(dirs)
	if maxAge != 1800 {
		t.Errorf("maxAge: got %d, want 1800", maxAge)
	}
}

func TestDetermineCacheMaxAge_FromCacheControl(t *testing.T) {
	headers := map[string]string{"Cache-Control": "max-age=600"}
	got := browserNet.DetermineCacheMaxAge(headers, 0)
	if got != 600 {
		t.Errorf("got %d, want 600", got)
	}
}

func TestDetermineCacheMaxAge_NoStoreZero(t *testing.T) {
	headers := map[string]string{"Cache-Control": "no-store, max-age=600"}
	got := browserNet.DetermineCacheMaxAge(headers, 0)
	if got != 0 {
		t.Errorf("got %d, want 0 (no-store)", got)
	}
}

func TestDetermineCacheMaxAge_FromExpires(t *testing.T) {
	future := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC1123)
	headers := map[string]string{"Expires": future}
	got := browserNet.DetermineCacheMaxAge(headers, 0)
	if got <= 0 {
		t.Errorf("expected positive max-age from Expires header, got %d", got)
	}
	if got > 3605 {
		t.Errorf("max-age too large: got %d", got)
	}
}

func TestDetermineCacheMaxAge_FallbackDefault(t *testing.T) {
	headers := map[string]string{}
	got := browserNet.DetermineCacheMaxAge(headers, 300)
	if got != 300 {
		t.Errorf("got %d, want 300 (default)", got)
	}
}

func TestResponseCache_HitAndMiss(t *testing.T) {
	cache := browserNet.NewResponseCache(100, 1<<20)

	u, _ := browserNet.ParseURL("https://example.com/page")
	// Pre-populate with a Put.
	resp := &browserNet.Response{
		Status:     200,
		StatusText: "200 OK",
		Headers: map[string]string{
			"Cache-Control": "max-age=3600",
		},
		MIMEType: browserNet.MIMEType{Type: "text", Subtype: "html"},
		URL:      u,
	}
	cache.Put("GET", u, resp, 3600, []byte("hello"))

	// Cache hit.
	cached, ok := cache.Get("GET", u)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if string(cached.Body) != "hello" {
		t.Errorf("body: got %q, want %q", string(cached.Body), "hello")
	}
	if cached.Status != 200 {
		t.Errorf("status: got %d, want 200", cached.Status)
	}

	// Different URL should miss.
	u2, _ := browserNet.ParseURL("https://example.com/other")
	_, ok = cache.Get("GET", u2)
	if ok {
		t.Error("expected cache miss for different URL")
	}

	// Different method should miss.
	_, ok = cache.Get("HEAD", u)
	if ok {
		t.Error("expected cache miss for different method")
	}
}

func TestResponseCache_ExpiredEntryRemoved(t *testing.T) {
	cache := browserNet.NewResponseCache(100, 1<<20)

	u, _ := browserNet.ParseURL("https://example.com/page")
	resp := &browserNet.Response{
		Status:     200,
		StatusText: "200 OK",
		Headers:    map[string]string{},
		MIMEType:   browserNet.MIMEType{Type: "text", Subtype: "html"},
		URL:        u,
	}
	// max-age of 0 seconds — should expire immediately.
	cache.Put("GET", u, resp, 0, []byte("stale"))

	// Entry should never be stored with max-age 0.
	_, ok := cache.Get("GET", u)
	if ok {
		t.Error("expected miss for max-age=0 entry")
	}
}

func TestResponseCache_SizeAndStats(t *testing.T) {
	cache := browserNet.NewResponseCache(100, 1<<20)
	u, _ := browserNet.ParseURL("https://example.com/page")
	resp := &browserNet.Response{
		Status:     200,
		StatusText: "200 OK",
		Headers:    map[string]string{},
		MIMEType:   browserNet.MIMEType{Type: "text", Subtype: "html"},
		URL:        u,
	}
	cache.Put("GET", u, resp, 3600, []byte("hello"))

	if cache.Size() != 1 {
		t.Errorf("size: got %d, want 1", cache.Size())
	}

	hits, misses := cache.Stats()
	// We haven't called Get yet, so hits=0, misses=0 initially from Put.
	if hits != 0 || misses != 0 {
		t.Errorf("initial stats: hits=%d misses=%d, want 0/0", hits, misses)
	}

	cache.Get("GET", u) // hit
	cache.Get("GET", u) // hit
	hits, misses = cache.Stats()
	if hits != 2 {
		t.Errorf("hits: got %d, want 2", hits)
	}
	if misses != 0 {
		t.Errorf("misses: got %d, want 0", misses)
	}
}

func TestResponseCache_Clear(t *testing.T) {
	cache := browserNet.NewResponseCache(100, 1<<20)
	u, _ := browserNet.ParseURL("https://example.com/page")
	resp := &browserNet.Response{
		Status:     200,
		StatusText: "200 OK",
		Headers:    map[string]string{},
		MIMEType:   browserNet.MIMEType{Type: "text", Subtype: "html"},
		URL:        u,
	}
	cache.Put("GET", u, resp, 3600, []byte("hello"))
	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("size after clear: got %d, want 0", cache.Size())
	}
	_, ok := cache.Get("GET", u)
	if ok {
		t.Error("expected miss after clear")
	}
}

func TestResponseCache_Concurrent(t *testing.T) {
	cache := browserNet.NewResponseCache(100, 1<<20)
	u, _ := browserNet.ParseURL("https://example.com/page")
	resp := &browserNet.Response{
		Status:     200,
		StatusText: "200 OK",
		Headers:    map[string]string{},
		MIMEType:   browserNet.MIMEType{Type: "text", Subtype: "html"},
		URL:        u,
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.Put("GET", u, resp, 3600, []byte("hello"))
			cache.Get("GET", u)
		}()
	}
	wg.Wait()
}

func TestResponseCache_ETagStored(t *testing.T) {
	cache := browserNet.NewResponseCache(100, 1<<20)
	u, _ := browserNet.ParseURL("https://example.com/page")
	resp := &browserNet.Response{
		Status:     200,
		StatusText: "200 OK",
		Headers: map[string]string{
			"Etag": `"abc123"`,
		},
		MIMEType: browserNet.MIMEType{Type: "text", Subtype: "html"},
		URL:      u,
	}
	cache.Put("GET", u, resp, 3600, []byte("cached"))

	cached, ok := cache.Get("GET", u)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if cached.ETag != `"abc123"` {
		t.Errorf("ETag: got %q, want %q", cached.ETag, `"abc123"`)
	}
}

// ---------------------------- HTTP/2 tests ------------------------------------

func TestFetchClient_HTTP2EnabledDefault(t *testing.T) {
	fc := &browserNet.FetchClient{}
	if fc.HTTP2Enabled {
		t.Error("HTTP2Enabled should default to false")
	}
}

func TestFetchClient_HTTP2Enabled(t *testing.T) {
	fc := &browserNet.FetchClient{HTTP2Enabled: true}
	if !fc.HTTP2Enabled {
		t.Error("HTTP2Enabled should be true")
	}
}

func TestFetchClient_HTTP2TransportConfigured(t *testing.T) {
	// Create a FetchClient with HTTP2 enabled and verify it works with a real server.
	fc := &browserNet.FetchClient{
		HTTP2Enabled: true,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	u, err := browserNet.ParseURL(server.URL)
	if err != nil {
		t.Fatalf("ParseURL: %v", err)
	}

	ctx := context.Background()
	resp, err := fc.Fetch(ctx, &browserNet.Request{
		Method: "GET",
		URL:    u,
		Mode:   browserNet.ModeNavigate,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("body: got %q, want %q", string(body), "ok")
	}
}

// -------------------------- Redirect chain tests ------------------------------

func TestFetchClient_RedirectChainNoRedirect(t *testing.T) {
	fc := &browserNet.FetchClient{MaxRedirects: 5}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("done"))
	}))
	defer server.Close()

	u, _ := browserNet.ParseURL(server.URL)
	ctx := context.Background()
	resp, err := fc.Fetch(ctx, &browserNet.Request{
		Method: "GET",
		URL:    u,
		Mode:   browserNet.ModeNavigate,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer resp.Body.Close()

	if len(fc.RedirectChain) == 0 {
		t.Error("RedirectChain should contain at least the initial URL")
	}
	// Normalise both URLs before comparing to account for serialisation differences.
	expected, _ := browserNet.ParseURL(server.URL)
	got, err := browserNet.ParseURL(fc.RedirectChain[0])
	if err != nil {
		t.Fatalf("parse redirect chain URL: %v", err)
	}
	if got.Serialize() != expected.Serialize() {
		t.Errorf("first redirect chain entry: got %q, want %q", got.Serialize(), expected.Serialize())
	}
}

func TestFetchClient_RedirectChainWithRedirects(t *testing.T) {
	fc := &browserNet.FetchClient{MaxRedirects: 5}

	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)
		} else if r.URL.Path == "/final" {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("final"))
		}
	}))
	defer redirectServer.Close()

	u, _ := browserNet.ParseURL(redirectServer.URL + "/start")
	ctx := context.Background()
	resp, err := fc.Fetch(ctx, &browserNet.Request{
		Method: "GET",
		URL:    u,
		Mode:   browserNet.ModeNavigate,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "final" {
		t.Errorf("body: got %q, want %q", string(body), "final")
	}
	if len(fc.RedirectChain) < 2 {
		t.Errorf("RedirectChain should have at least 2 entries, got %d: %v", len(fc.RedirectChain), fc.RedirectChain)
	}
}

// --------------------------- Cookie jar integration ----------------------------

func TestFetchClient_SendsCookiesFromJar(t *testing.T) {
	jar := browserNet.NewCookieJar()
	fc := &browserNet.FetchClient{Jar: jar}

	var receivedCookies []*http.Cookie
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCookies = r.Cookies()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Pre-seed the jar.
	serverURL, _ := url.Parse(server.URL)
	jar.SetCookies(serverURL, []*http.Cookie{
		{Name: "token", Value: "xyz", Path: "/"},
	})

	u, _ := browserNet.ParseURL(server.URL)
	resp, err := fc.Fetch(context.Background(), &browserNet.Request{
		Method: "GET",
		URL:    u,
		Mode:   browserNet.ModeNavigate,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer resp.Body.Close()

	if len(receivedCookies) != 1 {
		t.Fatalf("expected 1 cookie received by server, got %d", len(receivedCookies))
	}
	if receivedCookies[0].Name != "token" || receivedCookies[0].Value != "xyz" {
		t.Errorf("cookie: %s=%s, want token=xyz", receivedCookies[0].Name, receivedCookies[0].Value)
	}
}

func TestFetchClient_StoresSetCookieInJar(t *testing.T) {
	jar := browserNet.NewCookieJar()
	fc := &browserNet.FetchClient{Jar: jar}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "sss", Path: "/"})
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	u, _ := browserNet.ParseURL(server.URL)
	resp, err := fc.Fetch(context.Background(), &browserNet.Request{
		Method: "GET",
		URL:    u,
		Mode:   browserNet.ModeNavigate,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer resp.Body.Close()

	serverURL, _ := url.Parse(server.URL)
	cookies := jar.Cookies(serverURL)
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie in jar, got %d", len(cookies))
	}
	if cookies[0].Name != "session" || cookies[0].Value != "sss" {
		t.Errorf("cookie: %s=%s, want session=sss", cookies[0].Name, cookies[0].Value)
	}
}

// ---------------------------- Cache integration -------------------------------

func TestFetchClient_CacheIntegration(t *testing.T) {
	cache := browserNet.NewResponseCache(100, 1<<20)
	fc := &browserNet.FetchClient{
		Cache:             cache,
		DefaultCacheMaxAge: 300,
	}

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("cached"))
	}))
	defer server.Close()

	u, _ := browserNet.ParseURL(server.URL)

	// First fetch should hit the server.
	resp, err := fc.Fetch(context.Background(), &browserNet.Request{
		Method: "GET",
		URL:    u,
		Mode:   browserNet.ModeNavigate,
	})
	if err != nil {
		t.Fatalf("Fetch 1: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "cached" {
		t.Errorf("body: got %q, want %q", string(body), "cached")
	}
	if callCount != 1 {
		t.Errorf("server call count: got %d, want 1", callCount)
	}

	// Second fetch should hit the cache.
	resp2, err := fc.Fetch(context.Background(), &browserNet.Request{
		Method: "GET",
		URL:    u,
		Mode:   browserNet.ModeNavigate,
	})
	if err != nil {
		t.Fatalf("Fetch 2: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if string(body2) != "cached" {
		t.Errorf("body: got %q, want %q", string(body2), "cached")
	}
	if callCount != 1 {
		t.Errorf("server call count: got %d, want 1 (should be cached)", callCount)
	}
}

func TestFetchClient_CacheWithNoStore(t *testing.T) {
	cache := browserNet.NewResponseCache(100, 1<<20)
	fc := &browserNet.FetchClient{
		Cache:             cache,
		DefaultCacheMaxAge: 300,
	}

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("no-cache"))
	}))
	defer server.Close()

	u, _ := browserNet.ParseURL(server.URL)

	// First fetch.
	resp, _ := fc.Fetch(context.Background(), &browserNet.Request{
		Method: "GET", URL: u, Mode: browserNet.ModeNavigate,
	})
	resp.Body.Close()

	// Second fetch should also hit server (no-store).
	resp2, _ := fc.Fetch(context.Background(), &browserNet.Request{
		Method: "GET", URL: u, Mode: browserNet.ModeNavigate,
	})
	resp2.Body.Close()

	if callCount != 2 {
		t.Errorf("server call count: got %d, want 2 (no-store should bypass cache)", callCount)
	}
}

// ------------------------------ Helpers ---------------------------------------

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}
