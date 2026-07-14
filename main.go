package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

const (
	mermaidLibraryPath           = "/assets/mermaid-11.16.0.min.js"
	mermaidLegacyLibraryPath     = "/assets/mermaid-10.9.6.min.js"
	mermaidModernSyntaxProbePath = "/assets/mermaid-modern-syntax-probe-v1.js"
	mermaidLoaderPath            = "/assets/mermaid-loader-v1.js"
	mermaidInitPath              = "/assets/mermaid-init-v2.js"
	mermaidStylePath             = "/assets/mermaid-v1.css"
	diagnosticVersion            = "1"
	diagnosticMaxBody            = 64 << 10
	diagnosticMaxItems           = 50
)

//go:embed web/mermaid-11.16.0.min.js
var mermaidLibrary []byte

//go:embed web/mermaid-10.9.6.min.js
var mermaidLegacyLibrary []byte

//go:embed web/mermaid-modern-syntax-probe.js
var mermaidModernSyntaxProbe []byte

//go:embed web/mermaid-loader.js
var mermaidLoader []byte

//go:embed web/mermaid-init.js
var mermaidInit []byte

//go:embed web/mermaid.css
var mermaidStyle []byte

//go:embed web/diagnostic.html
var diagnosticHTML []byte

//go:embed web/diagnostic.css
var diagnosticStyle []byte

//go:embed web/diagnostic-bootstrap.js
var diagnosticBootstrap []byte

//go:embed web/diagnostic-probe-es2015.js
var diagnosticProbeES2015 []byte

//go:embed web/diagnostic-probe-es2017.js
var diagnosticProbeES2017 []byte

//go:embed web/diagnostic-probe-es2018.js
var diagnosticProbeES2018 []byte

//go:embed web/diagnostic-probe-es2020.js
var diagnosticProbeES2020 []byte

//go:embed web/diagnostic-probe-module.js
var diagnosticProbeModule []byte

var diagnosticRunIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

type diagnosticError struct {
	Kind     string `json:"kind"`
	Message  string `json:"message"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Resource string `json:"resource,omitempty"`
}

type diagnosticReport struct {
	SchemaVersion int                    `json:"schemaVersion"`
	RunID         string                 `json:"runId"`
	Role          string                 `json:"role"`
	Phase         string                 `json:"phase"`
	ClientTime    string                 `json:"clientTime,omitempty"`
	UserAgent     string                 `json:"userAgent"`
	Platform      string                 `json:"platform,omitempty"`
	Vendor        string                 `json:"vendor,omitempty"`
	Environment   map[string]interface{} `json:"environment,omitempty"`
	Capabilities  map[string]interface{} `json:"capabilities,omitempty"`
	Network       map[string]interface{} `json:"network,omitempty"`
	SVG           map[string]interface{} `json:"svg,omitempty"`
	Mermaid       map[string]interface{} `json:"mermaid,omitempty"`
	Errors        []diagnosticError      `json:"errors,omitempty"`
}

type storedDiagnosticReport struct {
	diagnosticReport
	ReceivedAt time.Time `json:"receivedAt"`
}

type Server struct {
	contentDir            string
	port                  string
	enableSecurityHeaders bool
	diagnosticMu          sync.Mutex
	diagnosticReports     []storedDiagnosticReport
}

func NewServer(contentDir, port string, enableSecurityHeaders bool) *Server {
	return &Server{
		contentDir:            contentDir,
		port:                  port,
		enableSecurityHeaders: enableSecurityHeaders,
	}
}

func (s *Server) Start() error {
	fmt.Printf("Starting server on port %s, serving content from %s\n", s.port, s.contentDir)
	return http.ListenAndServe(":"+s.port, s.Handler())
}

// Handler returns the complete HTTP handler used by the server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/__diag", s.diagnosticHeadersMiddleware(s.handleDiagnosticRedirect))
	mux.HandleFunc("/__diag/meta.json", s.diagnosticHeadersMiddleware(s.handleDiagnosticMeta))
	mux.HandleFunc("/__diag/ping", s.diagnosticHeadersMiddleware(s.handleDiagnosticPing))
	mux.HandleFunc("/__diag/reports", s.diagnosticHeadersMiddleware(s.handleDiagnosticReports))
	mux.HandleFunc("/__diag/assets/bootstrap-v1.js", s.diagnosticHeadersMiddleware(s.embeddedAssetHandler("text/javascript; charset=utf-8", diagnosticBootstrap)))
	mux.HandleFunc("/__diag/assets/style-v1.css", s.diagnosticHeadersMiddleware(s.embeddedAssetHandler("text/css; charset=utf-8", diagnosticStyle)))
	mux.HandleFunc("/__diag/assets/probe-es2015-v1.js", s.diagnosticHeadersMiddleware(s.embeddedAssetHandler("text/javascript; charset=utf-8", diagnosticProbeES2015)))
	mux.HandleFunc("/__diag/assets/probe-es2017-v1.js", s.diagnosticHeadersMiddleware(s.embeddedAssetHandler("text/javascript; charset=utf-8", diagnosticProbeES2017)))
	mux.HandleFunc("/__diag/assets/probe-es2018-v1.js", s.diagnosticHeadersMiddleware(s.embeddedAssetHandler("text/javascript; charset=utf-8", diagnosticProbeES2018)))
	mux.HandleFunc("/__diag/assets/probe-es2020-v1.js", s.diagnosticHeadersMiddleware(s.embeddedAssetHandler("text/javascript; charset=utf-8", diagnosticProbeES2020)))
	mux.HandleFunc("/__diag/assets/probe-module-v1.js", s.diagnosticHeadersMiddleware(s.embeddedAssetHandler("text/javascript; charset=utf-8", diagnosticProbeModule)))
	mux.HandleFunc("/__diag/", s.diagnosticHeadersMiddleware(s.handleDiagnosticPage))
	mux.HandleFunc(mermaidLibraryPath, s.securityHeadersMiddleware(s.embeddedAssetHandler("text/javascript; charset=utf-8", mermaidLibrary)))
	mux.HandleFunc(mermaidLegacyLibraryPath, s.securityHeadersMiddleware(s.embeddedAssetHandler("text/javascript; charset=utf-8", mermaidLegacyLibrary)))
	mux.HandleFunc(mermaidModernSyntaxProbePath, s.securityHeadersMiddleware(s.embeddedAssetHandler("text/javascript; charset=utf-8", mermaidModernSyntaxProbe)))
	mux.HandleFunc(mermaidLoaderPath, s.securityHeadersMiddleware(s.embeddedAssetHandler("text/javascript; charset=utf-8", mermaidLoader)))
	mux.HandleFunc(mermaidInitPath, s.securityHeadersMiddleware(s.embeddedAssetHandler("text/javascript; charset=utf-8", mermaidInit)))
	mux.HandleFunc(mermaidStylePath, s.securityHeadersMiddleware(s.embeddedAssetHandler("text/css; charset=utf-8", mermaidStyle)))
	mux.HandleFunc("/assets/", s.securityHeadersMiddleware(http.NotFound))
	mux.HandleFunc("/", s.securityHeadersMiddleware(s.handleMarkdown))
	return mux
}

func (s *Server) handleDiagnosticRedirect(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/__diag" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	target := "/__diag/"
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
}

func (s *Server) handleDiagnosticPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/__diag/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		role := normalizeDiagnosticRole(r.URL.Query().Get("role"))
		log.Printf("DIAG_VISIT role=%s user_agent=%q", role, truncateString(r.UserAgent(), 2048))
	}
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(diagnosticHTML)
}

func (s *Server) handleDiagnosticMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sum := sha256.Sum256(mermaidLibrary)
	if r.Method == http.MethodHead {
		writeDiagnosticJSONHead(w, http.StatusOK)
		return
	}
	writeDiagnosticJSON(w, http.StatusOK, map[string]interface{}{
		"diagnosticVersion":     diagnosticVersion,
		"mermaidVersion":        "11.16.0",
		"mermaidPath":           mermaidLibraryPath,
		"mermaidBytes":          len(mermaidLibrary),
		"mermaidSHA256":         fmt.Sprintf("%x", sum),
		"legacyMermaidVersion":  "10.9.6",
		"legacyMermaidPath":     mermaidLegacyLibraryPath,
		"legacyMermaidBytes":    len(mermaidLegacyLibrary),
		"modernSyntaxProbePath": mermaidModernSyntaxProbePath,
		"loaderPath":            mermaidLoaderPath,
		"initPath":              mermaidInitPath,
		"securityHeaders":       s.enableSecurityHeaders,
	})
}

func (s *Server) handleDiagnosticPing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Method == http.MethodHead {
		writeDiagnosticJSONHead(w, http.StatusOK)
		return
	}
	writeDiagnosticJSON(w, http.StatusOK, map[string]interface{}{
		"ok":                true,
		"diagnosticVersion": diagnosticVersion,
		"serverTime":        time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (s *Server) handleDiagnosticReports(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		s.handleDiagnosticReportList(w, r)
	case http.MethodPost:
		s.handleDiagnosticReportPost(w, r)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDiagnosticReportList(w http.ResponseWriter, r *http.Request) {
	roleFilter := r.URL.Query().Get("role")
	if roleFilter != "" && normalizeDiagnosticRole(roleFilter) != roleFilter {
		http.Error(w, "Invalid role", http.StatusBadRequest)
		return
	}
	limit := diagnosticMaxItems
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > diagnosticMaxItems {
			http.Error(w, "Invalid limit", http.StatusBadRequest)
			return
		}
		limit = parsed
	}

	s.diagnosticMu.Lock()
	reports := make([]storedDiagnosticReport, 0, len(s.diagnosticReports))
	for i := len(s.diagnosticReports) - 1; i >= 0 && len(reports) < limit; i-- {
		report := s.diagnosticReports[i]
		if roleFilter == "" || report.Role == roleFilter {
			reports = append(reports, report)
		}
	}
	s.diagnosticMu.Unlock()

	if r.Method == http.MethodHead {
		writeDiagnosticJSONHead(w, http.StatusOK)
		return
	}
	writeDiagnosticJSON(w, http.StatusOK, map[string]interface{}{
		"diagnosticVersion": diagnosticVersion,
		"count":             len(reports),
		"reports":           reports,
	})
}

func (s *Server) handleDiagnosticReportPost(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, diagnosticMaxBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var report diagnosticReport
	if err := decoder.Decode(&report); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			http.Error(w, "Report is too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Invalid diagnostic report", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "Invalid diagnostic report", http.StatusBadRequest)
		return
	}
	if err := validateDiagnosticReport(&report); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	stored := storedDiagnosticReport{
		diagnosticReport: report,
		ReceivedAt:       time.Now().UTC(),
	}
	s.storeDiagnosticReport(stored)
	encoded, _ := json.Marshal(stored)
	log.Printf("DIAG_REPORT %s", encoded)
	writeDiagnosticJSON(w, http.StatusAccepted, map[string]interface{}{
		"stored":     true,
		"runId":      stored.RunID,
		"receivedAt": stored.ReceivedAt,
	})
}

func (s *Server) storeDiagnosticReport(report storedDiagnosticReport) {
	s.diagnosticMu.Lock()
	defer s.diagnosticMu.Unlock()
	for i := range s.diagnosticReports {
		if s.diagnosticReports[i].RunID == report.RunID {
			copy(s.diagnosticReports[i:], s.diagnosticReports[i+1:])
			s.diagnosticReports[len(s.diagnosticReports)-1] = report
			return
		}
	}
	if len(s.diagnosticReports) == diagnosticMaxItems {
		copy(s.diagnosticReports, s.diagnosticReports[1:])
		s.diagnosticReports[len(s.diagnosticReports)-1] = report
		return
	}
	s.diagnosticReports = append(s.diagnosticReports, report)
}

func validateDiagnosticReport(report *diagnosticReport) error {
	if report.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schemaVersion")
	}
	if !diagnosticRunIDPattern.MatchString(report.RunID) {
		return fmt.Errorf("invalid runId")
	}
	if normalizeDiagnosticRole(report.Role) != report.Role {
		return fmt.Errorf("invalid role")
	}
	if !diagnosticRunIDPattern.MatchString(report.Phase) {
		return fmt.Errorf("invalid phase")
	}
	if len(report.UserAgent) > 2048 || len(report.Platform) > 256 || len(report.Vendor) > 256 || len(report.ClientTime) > 64 {
		return fmt.Errorf("diagnostic string is too long")
	}
	for _, values := range []map[string]interface{}{report.Environment, report.Capabilities, report.Network, report.SVG, report.Mermaid} {
		if len(values) > 100 {
			return fmt.Errorf("too many diagnostic fields")
		}
	}
	if len(report.Errors) > 20 {
		return fmt.Errorf("too many errors")
	}
	for _, diagnosticError := range report.Errors {
		if len(diagnosticError.Kind) > 64 || len(diagnosticError.Message) > 1000 || len(diagnosticError.File) > 256 || len(diagnosticError.Resource) > 256 {
			return fmt.Errorf("diagnostic error is too long")
		}
	}
	return nil
}

func normalizeDiagnosticRole(role string) string {
	switch role {
	case "reader_builtin", "reader_via", "phone_via", "desktop":
		return role
	default:
		return "unknown"
	}
}

func truncateString(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func writeDiagnosticJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeDiagnosticJSONHead(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
}

func (s *Server) embeddedAssetHandler(contentType string, content []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeContent(w, r, r.URL.Path, time.Time{}, bytes.NewReader(content))
	}
}

// securityHeadersMiddleware adds security headers to all responses if enabled
func (s *Server) securityHeadersMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only add security headers if enabled
		if s.enableSecurityHeaders {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")

			// Content Security Policy - allowing iframe embedding as requested
			// Note: Omitting X-Frame-Options since user wants iframe support
			csp := "default-src 'self'; " +
				"style-src 'self' 'unsafe-inline'; " +
				"script-src 'self'; " +
				"img-src 'self' data: https:; " +
				"font-src 'self'; " +
				"connect-src 'self'; " +
				"frame-ancestors *; " + // Allow iframe embedding
				"base-uri 'self'"
			w.Header().Set("Content-Security-Policy", csp)
		}

		// Call the next handler
		next(w, r)
	}
}

// diagnosticHeadersMiddleware keeps the probe self-contained and explicitly
// denies device sensors that are irrelevant to Mermaid rendering.
func (s *Server) diagnosticHeadersMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return s.securityHeadersMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "geolocation=(), camera=(), microphone=(), accelerometer=(), gyroscope=(), magnetometer=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		next(w, r)
	})
}

func (s *Server) handleMarkdown(w http.ResponseWriter, r *http.Request) {
	// Clean the URL path
	urlPath := strings.TrimPrefix(r.URL.Path, "/")
	if urlPath == "" {
		urlPath = "index.md"
	}

	// Security: Validate and sanitize the path to prevent directory traversal
	if err := s.validatePath(urlPath); err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Handle CSS file requests
	if urlPath == "style.css" {
		cssPath := filepath.Join(s.contentDir, "style.css")
		// Security: Ensure the resolved path is still within content directory
		if !s.isPathSafe(cssPath) {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}
		if _, err := os.Stat(cssPath); err == nil {
			w.Header().Set("Content-Type", "text/css")
			http.ServeFile(w, r, cssPath)
			return
		}
		http.NotFound(w, r)
		return
	}

	// Add .md extension if not present and not a directory
	if !strings.HasSuffix(urlPath, ".md") && !strings.HasSuffix(urlPath, "/") {
		urlPath += ".md"
	}

	filePath := filepath.Join(s.contentDir, urlPath)

	// Security: Ensure the resolved path is still within content directory
	if !s.isPathSafe(filePath) {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// Try with index.md if it's a directory
		if strings.HasSuffix(urlPath, "/") {
			indexPath := filepath.Join(s.contentDir, urlPath, "index.md")
			if !s.isPathSafe(indexPath) {
				http.Error(w, "Invalid path", http.StatusBadRequest)
				return
			}
			filePath = indexPath
		} else {
			// If the requested file doesn't exist, try to serve index.md instead
			indexPath := filepath.Join(s.contentDir, "index.md")
			if !s.isPathSafe(indexPath) {
				http.Error(w, "Invalid path", http.StatusBadRequest)
				return
			}
			if _, indexErr := os.Stat(indexPath); indexErr == nil {
				filePath = indexPath
			} else {
				http.NotFound(w, r)
				return
			}
		}
	}

	// Read markdown file
	content, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("Error reading Markdown file %q: %v", filePath, err)
		http.Error(w, "Error reading file", http.StatusInternalServerError)
		return
	}

	// Convert markdown to HTML
	htmlContent := s.markdownToHTML(content)
	hasMermaid := strings.Contains(htmlContent, `class="language-mermaid"`)

	// Render with template
	tmpl := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
    <link rel="stylesheet" href="/style.css">
    {{if .HasMermaid}}<link rel="stylesheet" href="` + mermaidStylePath + `">
    <script defer src="` + mermaidModernSyntaxProbePath + `"></script>
    <script defer src="` + mermaidLoaderPath + `"></script>
    <script defer src="` + mermaidInitPath + `"></script>{{end}}
</head>
<body>
    <div class="container">
        <nav>
            <a href="/">Home</a>
        </nav>
        <main>
            {{.Content}}
        </main>
    </div>
</body>
</html>`

	t, err := template.New("page").Parse(tmpl)
	if err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	data := struct {
		Title      string
		Content    template.HTML
		HasMermaid bool
	}{
		Title:      s.extractTitle(string(content)),
		Content:    template.HTML(htmlContent),
		HasMermaid: hasMermaid,
	}

	w.Header().Set("Content-Type", "text/html")
	if err := t.Execute(w, data); err != nil {
		http.Error(w, "Template execution error", http.StatusInternalServerError)
		return
	}
}

func (s *Server) markdownToHTML(md []byte) string {
	// Create markdown parser with extensions
	extensions := parser.CommonExtensions | parser.AutoHeadingIDs
	p := parser.NewWithExtensions(extensions)

	// Create HTML renderer with options
	htmlFlags := html.CommonFlags | html.HrefTargetBlank
	opts := html.RendererOptions{Flags: htmlFlags}
	renderer := html.NewRenderer(opts)

	// Parse and render
	doc := p.Parse(md)
	return string(markdown.Render(doc, renderer))
}

func (s *Server) extractTitle(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}
	return "Markdown Server"
}

func (s *Server) ensureSampleContent() error {
	// Create sample index.md file if content directory is empty
	if err := s.ensureIndexFile(); err != nil {
		return err
	}

	// Create style.css file if it doesn't exist
	if err := s.ensureStyleFile(); err != nil {
		return err
	}

	return nil
}

func (s *Server) ensureIndexFile() error {
	// Check if content directory is empty
	isEmpty, err := s.isContentDirEmpty()
	if err != nil {
		return err
	}

	if isEmpty {
		// Create sample index.md file
		indexPath := filepath.Join(s.contentDir, "index.md")
		sampleContent := `# Welcome to the Markdown Server

This is a sample markdown file that demonstrates the functionality of our Go-based markdown server.

## Features

- **Markdown to HTML conversion**: All ` + "`" + `.md` + "`" + ` files are automatically converted to HTML
- **Mermaid diagrams**: Fenced ` + "`" + `mermaid` + "`" + ` blocks render as SVG in the browser
- **Clean URLs**: Access files with or without the ` + "`" + `.md` + "`" + ` extension
- **Template rendering**: Content is wrapped in a clean HTML template
- **CSS styling**: Styles are served from the content directory
- **Auto-generated content**: This sample file was created automatically!

## Getting Started

1. Place your markdown files in the ` + "`" + `content/` + "`" + ` directory
2. Start the server
3. Navigate to ` + "`" + `http://localhost:8080` + "`" + ` to view your content

## Sample Content

Here's some sample markdown content:

### Code Example

` + "```" + `go
func main() {
    fmt.Println("Hello, Markdown Server!")
}
` + "```" + `

### Lists

- Item 1
- Item 2
- Item 3

### Mermaid Diagram

` + "```" + `mermaid
flowchart LR
    Markdown --> HTML
    HTML --> SVG
` + "```" + `

### Links

Visit [GitHub](https://github.com) for more projects.

---

*This server automatically converts this markdown to HTML!*
`

		if err := os.WriteFile(indexPath, []byte(sampleContent), 0644); err != nil {
			return fmt.Errorf("failed to create sample index.md: %w", err)
		}

		fmt.Printf("Created sample index.md file at %s\n", indexPath)
	}

	return nil
}

func (s *Server) ensureStyleFile() error {
	cssPath := filepath.Join(s.contentDir, "style.css")
	if _, err := os.Stat(cssPath); os.IsNotExist(err) {
		cssContent := `/* Reset and base styles */
* {
    margin: 0;
    padding: 0;
    box-sizing: border-box;
}

/* CSS Custom Properties for light and dark themes */
:root {
    --bg-color: #f8f9fa;
    --container-bg: white;
    --text-color: #333;
    --heading-color: #2c3e50;
    --heading-secondary: #34495e;
    --nav-bg: #2c3e50;
    --nav-text: white;
    --nav-accent: #3498db;
    --link-color: #3498db;
    --link-hover: #2980b9;
    --code-bg: #f4f4f4;
    --border-color: #ddd;
    --table-bg: #f8f9fa;
    --blockquote-bg: #f8f9fa;
    --hr-color: #ecf0f1;
    --shadow: rgba(0, 0, 0, 0.1);
}

/* Dark mode variables - automatically applied when user prefers dark mode */
@media (prefers-color-scheme: dark) {
    :root {
        --bg-color: #1a1a1a;
        --container-bg: #2d2d2d;
        --text-color: #e0e0e0;
        --heading-color: #ffffff;
        --heading-secondary: #b0b0b0;
        --nav-bg: #1f1f1f;
        --nav-text: #ffffff;
        --nav-accent: #4fc3f7;
        --link-color: #4fc3f7;
        --link-hover: #81d4fa;
        --code-bg: #3a3a3a;
        --border-color: #555;
        --table-bg: #3a3a3a;
        --blockquote-bg: #3a3a3a;
        --hr-color: #555;
        --shadow: rgba(0, 0, 0, 0.3);
    }
}

body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
    line-height: 1.6;
    color: var(--text-color);
    background-color: var(--bg-color);
    transition: background-color 0.3s ease, color 0.3s ease;
}

.container {
    max-width: 800px;
    margin: 0 auto;
    background-color: var(--container-bg);
    min-height: 100vh;
    box-shadow: 0 0 20px var(--shadow);
    transition: background-color 0.3s ease, box-shadow 0.3s ease;
}

/* Navigation */
nav {
    background-color: var(--nav-bg);
    padding: 1rem 2rem;
    border-bottom: 3px solid var(--nav-accent);
    transition: background-color 0.3s ease, border-color 0.3s ease;
}

nav a {
    color: var(--nav-text);
    text-decoration: none;
    font-weight: 500;
    font-size: 1.1rem;
    transition: color 0.3s ease;
}

nav a:hover {
    color: var(--nav-accent);
}

/* Main content */
main {
    padding: 2rem;
}

/* Typography */
h1, h2, h3, h4, h5, h6 {
    margin-bottom: 1rem;
    color: var(--heading-color);
    line-height: 1.2;
    transition: color 0.3s ease;
}

h1 {
    font-size: 2.5rem;
    border-bottom: 3px solid var(--nav-accent);
    padding-bottom: 0.5rem;
    margin-bottom: 1.5rem;
    transition: border-color 0.3s ease;
}

h2 {
    font-size: 2rem;
    margin-top: 2rem;
    color: var(--heading-secondary);
}

h3 {
    font-size: 1.5rem;
    margin-top: 1.5rem;
    color: var(--heading-secondary);
}

p {
    margin-bottom: 1rem;
    text-align: justify;
}

/* Links */
a {
    color: var(--link-color);
    text-decoration: none;
    transition: color 0.3s ease;
}

a:hover {
    text-decoration: underline;
    color: var(--link-hover);
}

/* Lists */
ul, ol {
    margin-bottom: 1rem;
    padding-left: 2rem;
}

li {
    margin-bottom: 0.5rem;
}

/* Code blocks */
pre {
    background-color: var(--code-bg);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    padding: 1rem;
    margin-bottom: 1rem;
    overflow-x: auto;
    font-family: 'Monaco', 'Courier New', monospace;
    font-size: 0.9rem;
    transition: background-color 0.3s ease, border-color 0.3s ease;
}

code {
    background-color: var(--code-bg);
    padding: 0.2rem 0.4rem;
    border-radius: 3px;
    font-family: 'Monaco', 'Courier New', monospace;
    font-size: 0.9rem;
    transition: background-color 0.3s ease;
}

pre code {
    background-color: transparent;
    padding: 0;
}

/* Blockquotes */
blockquote {
    border-left: 4px solid var(--nav-accent);
    margin: 1rem 0;
    padding: 0.5rem 1rem;
    background-color: var(--blockquote-bg);
    font-style: italic;
    transition: background-color 0.3s ease, border-color 0.3s ease;
}

/* Horizontal rules */
hr {
    border: none;
    border-top: 2px solid var(--hr-color);
    margin: 2rem 0;
    transition: border-color 0.3s ease;
}

/* Tables */
table {
    width: 100%;
    border-collapse: collapse;
    margin-bottom: 1rem;
}

th, td {
    border: 1px solid var(--border-color);
    padding: 0.75rem;
    text-align: left;
    transition: border-color 0.3s ease;
}

th {
    background-color: var(--table-bg);
    font-weight: 600;
    transition: background-color 0.3s ease;
}

tr:nth-child(even) {
    background-color: var(--table-bg);
    transition: background-color 0.3s ease;
}

/* Strong and emphasis */
strong {
    font-weight: 600;
    color: var(--heading-color);
    transition: color 0.3s ease;
}

em {
    font-style: italic;
    color: var(--heading-secondary);
    transition: color 0.3s ease;
}

/* Responsive design */
@media (max-width: 768px) {
    .container {
        margin: 0;
        box-shadow: none;
    }
    
    nav {
        padding: 1rem;
    }
    
    main {
        padding: 1rem;
    }
    
    h1 {
        font-size: 2rem;
    }
    
    h2 {
        font-size: 1.5rem;
    }
    
    pre {
        font-size: 0.8rem;
    }
}`

		if err := os.WriteFile(cssPath, []byte(cssContent), 0644); err != nil {
			return fmt.Errorf("failed to create sample style.css: %w", err)
		}

		fmt.Printf("Created sample style.css file at %s\n", cssPath)
	}

	return nil
}

func (s *Server) isContentDirEmpty() (bool, error) {
	entries, err := os.ReadDir(s.contentDir)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}

	// Check if there are any .md files
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			return false, nil
		}
	}

	return true, nil
}

// validatePath checks for obvious path traversal attempts
func (s *Server) validatePath(path string) error {
	// Check for path traversal patterns
	if strings.Contains(path, "..") ||
		strings.Contains(path, "//") ||
		strings.HasPrefix(path, "/") ||
		strings.Contains(path, "\\") {
		return fmt.Errorf("invalid path: contains dangerous characters")
	}

	// Only allow alphanumeric, dash, underscore, dot, and slash
	for _, char := range path {
		if !((char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_' || char == '.' || char == '/') {
			return fmt.Errorf("invalid path: contains invalid characters")
		}
	}

	return nil
}

// isPathSafe ensures the resolved path is within the content directory
func (s *Server) isPathSafe(requestedPath string) bool {
	// Get absolute paths
	contentAbs, err := filepath.Abs(s.contentDir)
	if err != nil {
		return false
	}

	requestedAbs, err := filepath.Abs(requestedPath)
	if err != nil {
		return false
	}

	// Check if the requested path is within the content directory
	rel, err := filepath.Rel(contentAbs, requestedAbs)
	if err != nil {
		return false
	}

	// If the relative path starts with "..", it's outside the content directory
	return !strings.HasPrefix(rel, "..")
}

func main() {
	contentDir := os.Getenv("CONTENT_DIR")
	if contentDir == "" {
		contentDir = "./content"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Check if security headers should be enabled (default: enabled)
	enableSecurityHeaders := true
	if securityHeadersEnv := os.Getenv("HTTP_SECURITY_HEADERS"); securityHeadersEnv == "disable" {
		enableSecurityHeaders = false
		fmt.Println("HTTP security headers disabled")
	}

	// Create content directory if it doesn't exist
	if err := os.MkdirAll(contentDir, 0755); err != nil {
		log.Fatal("Failed to create content directory:", err)
	}

	server := NewServer(contentDir, port, enableSecurityHeaders)

	// Ensure sample content exists if directory is empty
	if err := server.ensureSampleContent(); err != nil {
		log.Printf("Warning: Failed to create sample content: %v", err)
	}

	log.Fatal(server.Start())
}
