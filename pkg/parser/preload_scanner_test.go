package parser

import (
	"sort"
	"testing"

	browserNet "github.com/lucasdss/v8go/pkg/net"
)

func TestPreloadScannerBasicTags(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		wantLen  int
		wantType PreloadResourceType
		wantURL  string
	}{
		{
			name:     "script src",
			html:     `<script src="app.js"></script>`,
			wantLen:  1,
			wantType: PreloadScript,
			wantURL:  "app.js",
		},
		{
			name:     "link stylesheet",
			html:     `<link rel="stylesheet" href="style.css">`,
			wantLen:  1,
			wantType: PreloadStyle,
			wantURL:  "style.css",
		},
		{
			name:     "img src",
			html:     `<img src="photo.jpg" alt="photo">`,
			wantLen:  1,
			wantType: PreloadImage,
			wantURL:  "photo.jpg",
		},
		{
			name:     "iframe src",
			html:     `<iframe src="frame.html"></iframe>`,
			wantLen:  1,
			wantType: PreloadDocument,
			wantURL:  "frame.html",
		},
		{
			name:     "video src",
			html:     `<video src="movie.mp4"></video>`,
			wantLen:  1,
			wantType: PreloadMedia,
			wantURL:  "movie.mp4",
		},
		{
			name:     "audio src",
			html:     `<audio src="song.mp3"></audio>`,
			wantLen:  1,
			wantType: PreloadMedia,
			wantURL:  "song.mp3",
		},
		{
			name:     "source inside video",
			html:     `<video><source src="video.mp4" type="video/mp4"></video>`,
			wantLen:  1,
			wantType: PreloadMedia,
			wantURL:  "video.mp4",
		},
		{
			name:     "link preload font",
			html:     `<link rel="preload" as="font" href="font.woff2" crossorigin>`,
			wantLen:  1,
			wantType: PreloadFont,
			wantURL:  "font.woff2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewPreloadScanner([]byte(tt.html))
			resources := s.Scan()

			if len(resources) != tt.wantLen {
				t.Fatalf("expected %d resources, got %d: %+v", tt.wantLen, len(resources), resources)
			}
			if resources[0].Type != tt.wantType {
				t.Errorf("expected type %v, got %v", tt.wantType, resources[0].Type)
			}
			if resources[0].URL != tt.wantURL {
				t.Errorf("expected URL %q, got %q", tt.wantURL, resources[0].URL)
			}
		})
	}
}

func TestPreloadScannerMultipleResources(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<head>
	<link rel="stylesheet" href="main.css">
	<script src="app.js"></script>
</head>
<body>
	<img src="hero.jpg">
	<img src="logo.png">
	<iframe src="embed.html"></iframe>
	<video>
		<source src="video.mp4">
	</video>
</body>
</html>`

	s := NewPreloadScanner([]byte(html))
	resources := s.Scan()

	if len(resources) < 5 {
		t.Fatalf("expected at least 5 resources, got %d: %+v", len(resources), resources)
	}

	// Check types.
	types := map[PreloadResourceType]int{}
	urls := map[string]bool{}
	for _, r := range resources {
		types[r.Type]++
		urls[r.URL] = true
	}

	if types[PreloadStyle] != 1 {
		t.Errorf("expected 1 style, got %d", types[PreloadStyle])
	}
	if types[PreloadScript] != 1 {
		t.Errorf("expected 1 script, got %d", types[PreloadScript])
	}
	if types[PreloadImage] < 2 {
		t.Errorf("expected at least 2 images, got %d", types[PreloadImage])
	}
	if types[PreloadDocument] < 1 {
		t.Errorf("expected 1 document, got %d", types[PreloadDocument])
	}
	if types[PreloadMedia] < 1 {
		t.Errorf("expected 1 media, got %d", types[PreloadMedia])
	}

	if !urls["main.css"] {
		t.Error("missing main.css")
	}
	if !urls["app.js"] {
		t.Error("missing app.js")
	}
	if !urls["hero.jpg"] {
		t.Error("missing hero.jpg")
	}
}

func TestPreloadScannerNonResourceTags(t *testing.T) {
	html := `<div class="container">
	<p>Hello world</p>
	<a href="page.html">link</a>
	<span>text</span>
	<ul><li>item</li></ul>
</div>`
	s := NewPreloadScanner([]byte(html))
	resources := s.Scan()
	if len(resources) != 0 {
		t.Errorf("expected 0 resources, got %d: %+v", len(resources), resources)
	}
}

func TestPreloadScannerNonResourceLink(t *testing.T) {
	// <link> without rel=stylesheet or rel=preload should NOT be detected.
	html := `<link rel="icon" href="favicon.ico">
<link rel="canonical" href="https://example.com/page">
<link rel="dns-prefetch" href="https://cdn.example.com">`
	s := NewPreloadScanner([]byte(html))
	resources := s.Scan()
	if len(resources) != 0 {
		t.Errorf("expected 0 resources, got %d: %+v", len(resources), resources)
	}
}

func TestPreloadScannerSelfClosing(t *testing.T) {
	html := `<img src="photo.jpg" />
<br />
<hr />
<link rel="stylesheet" href="style.css" />`
	s := NewPreloadScanner([]byte(html))
	resources := s.Scan()
	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d: %+v", len(resources), resources)
	}
	if resources[0].Type != PreloadImage || resources[0].URL != "photo.jpg" {
		t.Errorf("first resource wrong: %+v", resources[0])
	}
	if resources[1].Type != PreloadStyle || resources[1].URL != "style.css" {
		t.Errorf("second resource wrong: %+v", resources[1])
	}
}

func TestPreloadScannerScriptBodySkipping(t *testing.T) {
	// The scanner should skip the body of <script> tags to avoid
	// false-positive tag matches inside JS strings.
	html := `<script>var x = "<img src='fake.jpg'>";</script>
<img src="real.jpg">`
	s := NewPreloadScanner([]byte(html))
	resources := s.Scan()
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource (only real.jpg), got %d: %+v", len(resources), resources)
	}
	if resources[0].URL != "real.jpg" {
		t.Errorf("expected real.jpg, got %q", resources[0].URL)
	}
}

func TestPreloadScannerStyleBodySkipping(t *testing.T) {
	// <style> body should not be scanned for resources.
	html := `<style>.bg { background: url("bg.jpg"); }</style>
<img src="real.jpg">`
	s := NewPreloadScanner([]byte(html))
	resources := s.Scan()
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d: %+v", len(resources), resources)
	}
	if resources[0].URL != "real.jpg" {
		t.Errorf("expected real.jpg, got %q", resources[0].URL)
	}
}

func TestPreloadScannerURLResolution(t *testing.T) {
	html := `<img src="images/photo.jpg">
<script src="/js/app.js"></script>
<link rel="stylesheet" href="//cdn.example.com/style.css">`
	s := NewPreloadScanner([]byte(html))
	s.SetBaseURL("https://www.example.com/page/index.html")
	resources := s.Scan()

	expected := map[string]string{
		"images/photo.jpg":          "https://www.example.com/page/images/photo.jpg",
		"/js/app.js":                "https://www.example.com/js/app.js",
		"//cdn.example.com/style.css": "https://cdn.example.com/style.css",
	}

	for _, r := range resources {
		want, ok := expected[r.URL]
		if !ok {
			t.Errorf("unexpected resource URL: %q", r.URL)
			continue
		}
		if r.ResolvedURL != want {
			t.Errorf("for %q: expected resolved %q, got %q", r.URL, want, r.ResolvedURL)
		}
	}
}

func TestPreloadScannerSrcset(t *testing.T) {
	html := `<img src="photo.jpg" srcset="photo-1x.jpg 1x, photo-2x.jpg 2x, photo-800w.jpg 800w">`
	s := NewPreloadScanner([]byte(html))
	resources := s.Scan()

	if len(resources) != 4 {
		t.Fatalf("expected 4 resources (1 src + 3 srcset), got %d: %+v", len(resources), resources)
	}

	urls := make([]string, len(resources))
	for i, r := range resources {
		urls[i] = r.URL
	}
	sort.Strings(urls)

	expected := []string{"photo-1x.jpg", "photo-2x.jpg", "photo-800w.jpg", "photo.jpg"}
	sort.Strings(expected)

	for i := range expected {
		if urls[i] != expected[i] {
			t.Errorf("URL[%d]: expected %q, got %q", i, expected[i], urls[i])
		}
	}
}

func TestPreloadScannerAttributes(t *testing.T) {
	html := `<script src="app.js" crossorigin="anonymous" integrity="sha384-abc"></script>
<link rel="stylesheet" href="style.css" media="screen and (min-width: 600px)">`
	s := NewPreloadScanner([]byte(html))
	resources := s.Scan()

	if len(resources) < 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}

	for _, r := range resources {
		switch r.URL {
		case "app.js":
			if r.CrossOrigin != "anonymous" {
				t.Errorf("expected crossorigin=anonymous, got %q", r.CrossOrigin)
			}
			if r.Integrity != "sha384-abc" {
				t.Errorf("expected integrity=sha384-abc, got %q", r.Integrity)
			}
		case "style.css":
			if r.Media != "screen and (min-width: 600px)" {
				t.Errorf("expected media attr, got %q", r.Media)
			}
		}
	}
}

func TestPreloadScannerEmptyInput(t *testing.T) {
	s := NewPreloadScanner([]byte(""))
	resources := s.Scan()
	if len(resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(resources))
	}
}

func TestPreloadScannerCommentSkipping(t *testing.T) {
	html := `<!-- <img src="commented.jpg"> -->
<img src="real.jpg">`
	s := NewPreloadScanner([]byte(html))
	resources := s.Scan()
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d: %+v", len(resources), resources)
	}
	if resources[0].URL != "real.jpg" {
		t.Errorf("expected real.jpg, got %q", resources[0].URL)
	}
}

func TestPreloadScannerUpperCaseTags(t *testing.T) {
	html := `<SCRIPT SRC="app.js"></SCRIPT>
<LINK REL="stylesheet" HREF="style.css">
<IMG SRC="photo.jpg">`
	s := NewPreloadScanner([]byte(html))
	resources := s.Scan()

	if len(resources) != 3 {
		t.Fatalf("expected 3 resources, got %d: %+v", len(resources), resources)
	}
}

func TestPreloadScannerSingleQuotes(t *testing.T) {
	html := `<img src='photo.jpg' alt='photo'>
<script src='app.js'></script>`
	s := NewPreloadScanner([]byte(html))
	resources := s.Scan()

	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d: %+v", len(resources), resources)
	}
	if resources[0].URL != "photo.jpg" {
		t.Errorf("expected photo.jpg, got %q", resources[0].URL)
	}
	if resources[1].URL != "app.js" {
		t.Errorf("expected app.js, got %q", resources[1].URL)
	}
}

func TestPreloadScannerUnquotedAttributes(t *testing.T) {
	html := `<img src=photo.jpg alt=photo>
<script src=app.js></script>`
	s := NewPreloadScanner([]byte(html))
	resources := s.Scan()

	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d: %+v", len(resources), resources)
	}
}

func TestPreloadScannerResolveMethod(t *testing.T) {
	res := &PreloadResource{
		URL:  "images/test.png",
		Type: PreloadImage,
	}
	res.Resolve("https://example.com/page/")
	if res.ResolvedURL != "https://example.com/page/images/test.png" {
		t.Errorf("expected resolved URL, got %q", res.ResolvedURL)
	}
	if !res.IsResolved() {
		t.Error("expected IsResolved to be true")
	}
}

func TestPreloadResourceTypeString(t *testing.T) {
	tests := []struct {
		typ  PreloadResourceType
		want string
	}{
		{PreloadScript, "script"},
		{PreloadStyle, "style"},
		{PreloadImage, "image"},
		{PreloadMedia, "media"},
		{PreloadDocument, "document"},
		{PreloadFont, "font"},
		{PreloadResourceType(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.typ.String(); got != tt.want {
			t.Errorf("PreloadResourceType(%d).String() = %q, want %q", tt.typ, got, tt.want)
		}
	}
}

func TestPreloadScannerIntegration(t *testing.T) {
	// Simulate the browser integration pattern: scan + resolve + dispatch.
	html := `<!DOCTYPE html>
<html>
<head>
	<meta charset="utf-8">
	<title>Test Page</title>
	<link rel="stylesheet" href="/css/main.css">
	<link rel="stylesheet" href="theme.css">
	<link rel="preload" as="font" href="/fonts/body.woff2" crossorigin>
	<script src="/js/lib.js"></script>
	<script src="app.js"></script>
</head>
<body>
	<header>
		<img src="/images/logo.png" srcset="/images/logo@2x.png 2x">
	</header>
	<main>
		<img src="hero.jpg">
		<iframe src="/embed/widget.html"></iframe>
	</main>
	<footer>
		<script src="tracker.js"></script>
	</footer>
</body>
</html>`

	baseURL := "https://www.example.com/page/index.html"
	s := NewPreloadScanner([]byte(html))
	s.SetBaseURL(baseURL)
	resources := s.Scan()

	// Verify we found resources.
	if len(resources) < 8 {
		t.Errorf("expected at least 8 resources, got %d", len(resources))
	}

	// Verify all resolved URLs are absolute.
	for _, r := range resources {
		if !r.IsResolved() {
			t.Errorf("resource %q was not resolved", r.URL)
		}
		if !stringsContainProtocol(r.ResolvedURL) {
			t.Errorf("resolved URL %q is not absolute", r.ResolvedURL)
		}
	}

	// Verify specific resource discovery.
	found := map[string]bool{}
	for _, r := range resources {
		found[r.ResolvedURL] = true
	}

	expected := []string{
		"https://www.example.com/css/main.css",
		"https://www.example.com/page/theme.css",
		"https://www.example.com/fonts/body.woff2",
		"https://www.example.com/js/lib.js",
		"https://www.example.com/page/app.js",
		"https://www.example.com/images/logo.png",
		"https://www.example.com/images/logo@2x.png",
		"https://www.example.com/page/hero.jpg",
		"https://www.example.com/embed/widget.html",
		"https://www.example.com/page/tracker.js",
	}

	for _, exp := range expected {
		if !found[exp] {
			t.Errorf("expected resource not found: %q", exp)
		}
	}
}

func stringsContainProtocol(s string) bool {
	return stringsContains(s, "://")
}

// Small helper to avoid importing strings again (already imported at package level).
func stringsContains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestPreloadScannerRequestDestination(t *testing.T) {
	tests := []struct {
		r    PreloadResource
		want browserNet.RequestDestination
	}{
		{PreloadResource{Type: PreloadScript}, browserNet.DestScript},
		{PreloadResource{Type: PreloadStyle}, browserNet.DestStyle},
		{PreloadResource{Type: PreloadImage}, browserNet.DestImage},
		{PreloadResource{Type: PreloadFont}, browserNet.DestFont},
		{PreloadResource{Type: PreloadMedia}, browserNet.DestEmpty},
		{PreloadResource{Type: PreloadDocument}, browserNet.DestEmpty},
	}

	for _, tt := range tests {
		if got := tt.r.requestDestination(); got != tt.want {
			t.Errorf("resource type %v: expected dest %v, got %v", tt.r.Type, tt.want, got)
		}
	}
}

func TestPreloadScannerParseSrcsetURLs(t *testing.T) {
	tests := []struct {
		srcset   string
		expected []string
	}{
		{"image-1x.jpg 1x", []string{"image-1x.jpg"}},
		{"image-1x.jpg 1x, image-2x.jpg 2x", []string{"image-1x.jpg", "image-2x.jpg"}},
		{"image-400w.jpg 400w, image-800w.jpg 800w", []string{"image-400w.jpg", "image-800w.jpg"}},
		{"image.jpg", []string{"image.jpg"}},
		{"", nil},
	}

	for _, tt := range tests {
		got := parseSrcsetURLs(tt.srcset)
		if len(got) != len(tt.expected) {
			t.Errorf("srcset %q: expected %d URLs, got %d: %v", tt.srcset, len(tt.expected), len(got), got)
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("srcset %q, URL[%d]: expected %q, got %q", tt.srcset, i, tt.expected[i], got[i])
			}
		}
	}
}

func TestPreloadScannerHTML5Document(t *testing.T) {
	// Test with a real-world HTML5 document.
	html := `<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Real World Page</title>
	<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Roboto">
	<link rel="stylesheet" href="/assets/main-cb3f9a2.css">
	<link rel="preload" href="/assets/fonts/inter-var.woff2" as="font" type="font/woff2" crossorigin>
	<script src="https://cdn.jsdelivr.net/npm/react@18/umd/react.production.min.js" crossorigin="anonymous"></script>
	<script src="/assets/vendor-7a2b8c1.js" defer></script>
</head>
<body>
	<div id="root">
		<img src="/images/sprite.svg" width="0" height="0" alt="">
		<img src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg'%3E%3C/svg%3E" alt="">
		<picture>
			<source srcset="/images/hero-800w.webp 800w, /images/hero-1200w.webp 1200w" type="image/webp">
			<img src="/images/hero.jpg" alt="Hero" loading="lazy">
		</picture>
	</div>
	<script src="/assets/main-a1b2c3d.js"></script>
</body>
</html>`

	baseURL := "https://www.example.com/"
	s := NewPreloadScanner([]byte(html))
	s.SetBaseURL(baseURL)
	resources := s.Scan()

	// We should discover: 3 scripts, 2 stylesheets, 1 font, images + sources
	// (data: URIs are still discovered as URLs but won't be network-fetched - that's for the caller to handle)

	if len(resources) < 6 {
		t.Errorf("expected at least 6 resources, got %d", len(resources))
	}

	// Count by type.
	counts := map[PreloadResourceType]int{}
	for _, r := range resources {
		counts[r.Type]++
	}

	if counts[PreloadScript] < 3 {
		t.Errorf("expected at least 3 scripts, got %d", counts[PreloadScript])
	}
	if counts[PreloadStyle] < 2 {
		t.Errorf("expected at least 2 stylesheets, got %d", counts[PreloadStyle])
	}
	if counts[PreloadFont] < 1 {
		t.Errorf("expected at least 1 font, got %d", counts[PreloadFont])
	}
	if counts[PreloadImage] < 2 {
		t.Errorf("expected at least 2 images (hero.jpg + sprite.svg), got %d", counts[PreloadImage])
	}
}
