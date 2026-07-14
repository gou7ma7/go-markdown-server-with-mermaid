package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func request(t *testing.T, handler http.Handler, method, path, contentType string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	handler.ServeHTTP(recorder, req)
	return recorder
}

func validDiagnosticReport(runID, role, phase string) diagnosticReport {
	return diagnosticReport{
		SchemaVersion: 1,
		RunID:         runID,
		Role:          role,
		Phase:         phase,
		UserAgent:     "diagnostic-test-agent",
		Environment:   map[string]interface{}{"viewportWidthBucket": 400},
		Capabilities:  map[string]interface{}{"es2020": true},
		Network:       map[string]interface{}{"pingStatus": 200},
		SVG:           map[string]interface{}{"inlineVisible": true},
		Mermaid:       map[string]interface{}{"bundleLoadStatus": "loaded"},
	}
}

func postDiagnosticReport(t *testing.T, server *Server, report diagnosticReport) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	return request(t, server.Handler(), http.MethodPost, "/__diag/reports", "application/json; charset=utf-8", body)
}

func TestDiagnosticRoutesAndHeaders(t *testing.T) {
	server := testServer(t, "# Home\n", false)
	handler := server.Handler()

	redirect := get(t, handler, "/__diag?role=reader_via")
	if redirect.Code != http.StatusTemporaryRedirect {
		t.Fatalf("redirect status = %d", redirect.Code)
	}
	if redirect.Header().Get("Location") != "/__diag/?role=reader_via" {
		t.Fatalf("redirect location = %q", redirect.Header().Get("Location"))
	}

	page := get(t, handler, "/__diag/?role=reader_via")
	if page.Code != http.StatusOK {
		t.Fatalf("diagnostic page status = %d", page.Code)
	}
	for _, expected := range []string{
		"Mermaid 浏览器能力诊断",
		"/__diag/assets/bootstrap-v1.js",
		"/__diag/assets/style-v1.css",
		"language-mermaid",
	} {
		if !strings.Contains(page.Body.String(), expected) {
			t.Errorf("diagnostic page does not contain %q", expected)
		}
	}
	if strings.Contains(page.Body.String(), "<script src=\"/assets/mermaid") {
		t.Fatal("diagnostic HTML should let the ES5 bootstrap load Mermaid dynamically")
	}
	if page.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("diagnostic Cache-Control = %q", page.Header().Get("Cache-Control"))
	}
	if !strings.Contains(page.Header().Get("Permissions-Policy"), "geolocation=()") {
		t.Fatalf("diagnostic Permissions-Policy = %q", page.Header().Get("Permissions-Policy"))
	}
	if !strings.Contains(page.Header().Get("Content-Security-Policy"), "script-src 'self'") {
		t.Fatalf("diagnostic CSP = %q", page.Header().Get("Content-Security-Policy"))
	}

	head := request(t, handler, http.MethodHead, "/__diag/", "", nil)
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("diagnostic HEAD = status %d, body %d bytes", head.Code, head.Body.Len())
	}

	unknown := get(t, handler, "/__diag/not-a-route")
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown diagnostic route status = %d", unknown.Code)
	}
	if strings.Contains(unknown.Body.String(), "<h1>Home") {
		t.Fatal("unknown diagnostic route fell back to Markdown")
	}
}

func TestDiagnosticAssetsAndMetadata(t *testing.T) {
	server := testServer(t, "# Home\n", true)
	handler := server.Handler()

	assets := []struct {
		path        string
		contentType string
		contains    string
	}{
		{"/__diag/assets/bootstrap-v1.js", "text/javascript", "XMLHttpRequest"},
		{"/__diag/assets/style-v1.css", "text/css", ".diagram-output"},
		{"/__diag/assets/probe-es2015-v1.js", "text/javascript", "__diagProbeES2015"},
		{"/__diag/assets/probe-es2017-v1.js", "text/javascript", "__diagProbeES2017"},
		{"/__diag/assets/probe-es2018-v1.js", "text/javascript", "__diagProbeES2018"},
		{"/__diag/assets/probe-es2020-v1.js", "text/javascript", "__diagProbeES2020"},
		{"/__diag/assets/probe-module-v1.js", "text/javascript", "__diagProbeModule"},
	}
	for _, asset := range assets {
		response := get(t, handler, asset.path)
		if response.Code != http.StatusOK {
			t.Errorf("%s status = %d", asset.path, response.Code)
			continue
		}
		if !strings.HasPrefix(response.Header().Get("Content-Type"), asset.contentType) {
			t.Errorf("%s Content-Type = %q", asset.path, response.Header().Get("Content-Type"))
		}
		if response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Errorf("%s Cache-Control = %q", asset.path, response.Header().Get("Cache-Control"))
		}
		if !strings.Contains(response.Body.String(), asset.contains) {
			t.Errorf("%s does not contain %q", asset.path, asset.contains)
		}
	}

	for _, path := range []string{"/__diag/meta.json", "/__diag/ping", "/__diag/reports"} {
		response := get(t, handler, path)
		if response.Code != http.StatusOK {
			t.Errorf("%s status = %d", path, response.Code)
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s Cache-Control = %q", path, response.Header().Get("Cache-Control"))
		}
	}

	meta := get(t, handler, "/__diag/meta.json")
	for _, expected := range []string{"11.16.0", mermaidLibraryPath, "mermaidSHA256"} {
		if !strings.Contains(meta.Body.String(), expected) {
			t.Errorf("metadata does not contain %q", expected)
		}
	}
}

func TestDiagnosticBootstrapUsesES5Syntax(t *testing.T) {
	forbidden := regexp.MustCompile("(?m)\\b(?:await|const|let)\\b|=>|\\?\\.|\\?\\?|`|\\.replaceWith\\(|Array\\.from\\(")
	if match := forbidden.Find(diagnosticBootstrap); match != nil {
		t.Fatalf("diagnostic bootstrap contains modern syntax or API %q", match)
	}
	for _, expected := range []string{"var ", "function ", "XMLHttpRequest", "onreadystatechange"} {
		if !bytes.Contains(diagnosticBootstrap, []byte(expected)) {
			t.Errorf("diagnostic bootstrap does not contain %q", expected)
		}
	}
}

func TestDiagnosticReportLifecycle(t *testing.T) {
	server := testServer(t, "# Home\n", true)
	first := validDiagnosticReport("run-1", "reader_via", "bootstrap")
	response := postDiagnosticReport(t, server, first)
	if response.Code != http.StatusAccepted {
		t.Fatalf("valid report status = %d, body = %s", response.Code, response.Body.String())
	}

	first.Phase = "final"
	first.Mermaid["bundleLoadStatus"] = "error"
	response = postDiagnosticReport(t, server, first)
	if response.Code != http.StatusAccepted {
		t.Fatalf("updated report status = %d", response.Code)
	}

	list := get(t, server.Handler(), "/__diag/reports?role=reader_via")
	var decoded struct {
		Count   int                      `json:"count"`
		Reports []storedDiagnosticReport `json:"reports"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Count != 1 || len(decoded.Reports) != 1 {
		t.Fatalf("report count = %d / %d", decoded.Count, len(decoded.Reports))
	}
	if decoded.Reports[0].Phase != "final" {
		t.Fatalf("stored phase = %q", decoded.Reports[0].Phase)
	}

	malicious := validDiagnosticReport("run-xss", "desktop", "final")
	malicious.UserAgent = "</script><script>alert(1)</script>"
	if response = postDiagnosticReport(t, server, malicious); response.Code != http.StatusAccepted {
		t.Fatalf("escaped report status = %d", response.Code)
	}
	list = get(t, server.Handler(), "/__diag/reports")
	if strings.Contains(list.Body.String(), "</script>") {
		t.Fatal("JSON report response did not HTML-escape untrusted strings")
	}
}

func TestDiagnosticReportValidationAndLimit(t *testing.T) {
	server := testServer(t, "# Home\n", true)
	handler := server.Handler()

	tests := []struct {
		name        string
		contentType string
		body        string
		want        int
	}{
		{"missing content type", "", `{}`, http.StatusUnsupportedMediaType},
		{"wrong content type", "text/plain", `{}`, http.StatusUnsupportedMediaType},
		{"invalid JSON", "application/json", `{`, http.StatusBadRequest},
		{"unknown field", "application/json", `{"schemaVersion":1,"runId":"r","role":"desktop","phase":"final","userAgent":"","surprise":true}`, http.StatusBadRequest},
		{"invalid role", "application/json", `{"schemaVersion":1,"runId":"r","role":"phone","phase":"final","userAgent":""}`, http.StatusBadRequest},
		{"invalid run ID", "application/json", `{"schemaVersion":1,"runId":"bad/id","role":"desktop","phase":"final","userAgent":""}`, http.StatusBadRequest},
		{"two objects", "application/json", `{"schemaVersion":1,"runId":"r","role":"desktop","phase":"final","userAgent":""}{}`, http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := request(t, handler, http.MethodPost, "/__diag/reports", test.contentType, []byte(test.body))
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
		})
	}

	overLimit := bytes.Repeat([]byte(" "), diagnosticMaxBody+1)
	response := request(t, handler, http.MethodPost, "/__diag/reports", "application/json", overLimit)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized report status = %d, want 413", response.Code)
	}
	if len(server.diagnosticReports) != 0 {
		t.Fatalf("invalid requests stored %d reports", len(server.diagnosticReports))
	}
}

func TestDiagnosticReportCapacityAndOrder(t *testing.T) {
	server := testServer(t, "# Home\n", true)
	for i := 0; i < diagnosticMaxItems+1; i++ {
		report := validDiagnosticReport(fmt.Sprintf("run-%02d", i), "desktop", "final")
		response := postDiagnosticReport(t, server, report)
		if response.Code != http.StatusAccepted {
			t.Fatalf("report %d status = %d", i, response.Code)
		}
	}
	if len(server.diagnosticReports) != diagnosticMaxItems {
		t.Fatalf("stored %d reports, want %d", len(server.diagnosticReports), diagnosticMaxItems)
	}
	if server.diagnosticReports[0].RunID != "run-01" {
		t.Fatalf("oldest report = %q", server.diagnosticReports[0].RunID)
	}

	list := get(t, server.Handler(), "/__diag/reports?limit=1")
	if !strings.Contains(list.Body.String(), `"runId":"run-50"`) {
		t.Fatalf("newest report was not returned first: %s", list.Body.String())
	}
}
