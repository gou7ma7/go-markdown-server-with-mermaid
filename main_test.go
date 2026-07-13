package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testServer(t *testing.T, markdown string, securityHeaders bool) *Server {
	t.Helper()

	contentDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(contentDir, "index.md"), []byte(markdown), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contentDir, "style.css"), []byte("body {}"), 0o600); err != nil {
		t.Fatal(err)
	}

	return NewServer(contentDir, "0", securityHeaders)
}

func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder
}

func TestMermaidAssetsAreOnlyLoadedWhenNeeded(t *testing.T) {
	plain := testServer(t, "# Plain\n\n```go\nfmt.Println(\"hello\")\n```\n", true)
	plainPage := get(t, plain.Handler(), "/")
	if plainPage.Code != http.StatusOK {
		t.Fatalf("plain page status = %d", plainPage.Code)
	}
	if strings.Contains(plainPage.Body.String(), "/assets/mermaid") {
		t.Fatal("plain Markdown page unexpectedly loads Mermaid assets")
	}

	withMermaid := testServer(t, "# Diagram\n\n```mermaid\nflowchart LR\n  A --> B\n```\n", true)
	mermaidPage := get(t, withMermaid.Handler(), "/")
	body := mermaidPage.Body.String()
	for _, expected := range []string{
		`<code class="language-mermaid">`,
		mermaidLibraryPath,
		mermaidInitPath,
		mermaidStylePath,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("Mermaid page does not contain %q", expected)
		}
	}
	if strings.Contains(body, "cdn.") {
		t.Fatal("Mermaid page should only load self-hosted assets")
	}
}

func TestEmbeddedMermaidAssets(t *testing.T) {
	server := testServer(t, "# Test\n", true)
	tests := []struct {
		path        string
		contentType string
		contains    string
	}{
		{mermaidLibraryPath, "text/javascript", "mermaid"},
		{mermaidInitPath, "text/javascript", `securityLevel: "strict"`},
		{mermaidStylePath, "text/css", ".mermaid"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			response := get(t, server.Handler(), test.path)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d", response.Code)
			}
			if !strings.HasPrefix(response.Header().Get("Content-Type"), test.contentType) {
				t.Errorf("Content-Type = %q", response.Header().Get("Content-Type"))
			}
			if response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
				t.Errorf("Cache-Control = %q", response.Header().Get("Cache-Control"))
			}
			if !strings.Contains(response.Body.String(), test.contains) {
				t.Errorf("asset does not contain %q", test.contains)
			}
		})
	}

	missing := get(t, server.Handler(), "/assets/not-found.js")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d, want 404", missing.Code)
	}
}

func TestMermaidWorksWithDefaultContentSecurityPolicy(t *testing.T) {
	server := testServer(t, "# Diagram\n\n```mermaid\nflowchart LR\n  A --> B\n```\n", true)
	response := get(t, server.Handler(), "/")
	csp := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") {
		t.Fatalf("CSP does not allow self-hosted scripts: %q", csp)
	}
	if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Fatalf("CSP unnecessarily allows inline scripts: %q", csp)
	}
}
