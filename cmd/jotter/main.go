package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"jotter/web"

	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"github.com/starfederation/datastar-go/datastar"
)

const htmlTemplate = `<!doctype html>
<html lang="en">
    <head>
        <meta charset="UTF-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1.0" />
        <title>jotter</title>
        <link rel="icon" type="image/x-icon" href="/static/img/favicon.ico" />
        <link rel="stylesheet" href="/static/css/style.css">
        <script type="module" src="/static/js/datastar.js"></script>
    </head>
    <body data-on-load="@get('/updates')">
        <textarea
            id="jot-field"
            placeholder="Start typing..."
            data-bind-content
            data-on-input__debounce.500ms="@post('/write')"
        >{{.Content}}</textarea>
    </body>
</html>`

// tokenRe validates the format of a token to prevent path traversal.
var tokenRe = regexp.MustCompile(`^[A-Za-z0-9_-]+=*$`)

type Server struct {
	jotDir     string
	host       string
	port       string
	tlsEnabled bool
	certFile   string
	keyFile    string
	watcher    *fsnotify.Watcher
	httpServer *http.Server
	clients    map[string]map[string]chan []byte // token -> sessionId -> channel
	mu         sync.RWMutex
	tmpl       *template.Template
}

// JotAction is used to decode signals from datastar POST requests
type JotAction struct {
	Content string `json:"content"`
}

func NewServer() (*Server, error) {
	jotDir := getEnv("JOT_DIR", "jots")
	host := getEnv("JOT_HOST", "localhost")
	port := getEnv("JOT_PORT", "7086")
	certFile := getEnv("JOT_CERT_FILE", "")
	keyFile := getEnv("JOT_KEY_FILE", "")
	tlsEnabled := certFile != "" && keyFile != ""

	if err := os.MkdirAll(jotDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create jot directory: %w", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	if err := watcher.Add(jotDir); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("failed to watch directory: %w", err)
	}

	tmpl, err := template.New("index").Parse(htmlTemplate)
	if err != nil {
		watcher.Close()
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	return &Server{
		jotDir:     jotDir,
		host:       host,
		port:       port,
		tlsEnabled: tlsEnabled,
		certFile:   certFile,
		keyFile:    keyFile,
		watcher:    watcher,
		clients:    make(map[string]map[string]chan []byte),
		tmpl:       tmpl,
	}, nil
}

func (s *Server) Start() error {
	go s.watchFiles()

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/new", s.handleNew)
	mux.HandleFunc("/write", s.handleWrite)
	mux.HandleFunc("/updates", s.handleUpdates)
	mux.Handle("/static/", web.StaticHandler())

	addr := fmt.Sprintf("%s:%s", s.host, s.port)
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	if s.tlsEnabled {
		log.Printf("Starting TLS server on https://%s", addr)
		return s.httpServer.ListenAndServeTLS(s.certFile, s.keyFile)
	} else {
		log.Printf("Starting server on http://%s", addr)
		return s.httpServer.ListenAndServe()
	}
}

func (s *Server) watchFiles() {
	defer s.watcher.Close()
	for {
		select {
		case event, ok := <-s.watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				s.handleFileChange(event.Name)
			}
		case err, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

func (s *Server) handleFileChange(filename string) {
	base := filepath.Base(filename)
	if !strings.HasPrefix(base, "jot_") || !strings.HasSuffix(base, ".txt") {
		return
	}
	token := strings.TrimPrefix(strings.TrimSuffix(base, ".txt"), "jot_")

	content, err := os.ReadFile(filename)
	if err != nil {
		log.Printf("Error reading file %s: %v", filename, err)
		return
	}

	payload := map[string]any{
		"datastar-patch-signals": map[string]any{
			"content": string(content),
		},
	}
	messageBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling datastar payload: %v", err)
		return
	}

	sseMessage := fmt.Sprintf("data: %s\n\n", messageBytes)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if clientsForToken, ok := s.clients[token]; ok {
		s.broadcastToClients(clientsForToken, []byte(sseMessage))
	}
}

func (s *Server) broadcastToClients(clients map[string]chan []byte, message []byte) {
	for _, ch := range clients {
		select {
		case ch <- message:
		default:
			// Channel is full or closed, skip
		}
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		// Allow clean URLs like /<token> to be shared
		if len(r.URL.Path) > 1 && !strings.Contains(r.URL.Path, ".") {
			token := r.URL.Path[1:]
			if !tokenRe.MatchString(token) {
				http.NotFound(w, r)
				return
			}
			filename := filepath.Join(s.jotDir, fmt.Sprintf("jot_%s.txt", token))
			if _, err := os.Stat(filename); err == nil {
				http.Redirect(w, r, "/?token="+token, http.StatusSeeOther)
				return
			}
		}
		http.NotFound(w, r)
		return
	}

	token, err := s.getOrCreateToken(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	filename := filepath.Join(s.jotDir, fmt.Sprintf("jot_%s.txt", token))

	if _, err := os.Stat(filename); os.IsNotExist(err) {
		defaultContent := s.getDefaultContent(token)
		if err := os.WriteFile(filename, []byte(defaultContent), 0644); err != nil {
			http.Error(w, "Failed to create jot file", http.StatusInternalServerError)
			return
		}
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		http.Error(w, "Failed to read jot file", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	w.Header().Set("Content-Type", "text/html")

	jsonContent, err := json.Marshal(string(content))
	if err != nil {
		http.Error(w, "Failed to marshal content", http.StatusInternalServerError)
		return
	}

	data := struct {
		Content     string
		ContentJSON template.JS
		Token       string
	}{
		Content:     string(content),
		ContentJSON: template.JS(jsonContent),
		Token:       token,
	}

	if err := s.tmpl.Execute(w, data); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token, err := s.getValidToken(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var action JotAction
	if err := datastar.ReadSignals(r, &action); err != nil {
		http.Error(w, "Invalid signals", http.StatusBadRequest)
		return
	}

	filename := filepath.Join(s.jotDir, fmt.Sprintf("jot_%s.txt", token))

	// The file watcher will detect the change and broadcast it.
	if err := os.WriteFile(filename, []byte(action.Content), 0644); err != nil {
		http.Error(w, "Failed to write file", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUpdates(w http.ResponseWriter, r *http.Request) {
	token, err := s.getValidToken(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sessionId := uuid.New().String()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	clientChan := make(chan []byte, 10)

	s.mu.Lock()
	if s.clients[token] == nil {
		s.clients[token] = make(map[string]chan []byte)
	}
	s.clients[token][sessionId] = clientChan
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		if s.clients[token] != nil {
			delete(s.clients[token], sessionId)
			if len(s.clients[token]) == 0 {
				delete(s.clients, token)
			}
		}
		s.mu.Unlock()
		close(clientChan)
	}()

	fmt.Fprintf(w, "event: message\ndata: {\"message\": \"connected\"}\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case message := <-clientChan:
			if _, err := w.Write(message); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		case <-ticker.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}
}

func (s *Server) handleNew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cookie, err := r.Cookie("token")
	if err != nil {
		http.Error(w, "No token cookie found", http.StatusBadRequest)
		return
	}

	filename := filepath.Join(s.jotDir, fmt.Sprintf("jot_%s.txt", cookie.Value))
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		http.Error(w, "Invalid token", http.StatusBadRequest)
		return
	}

	newToken, err := s.generateToken()
	if err != nil {
		http.Error(w, "Failed to generate new token", http.StatusInternalServerError)
		return
	}

	newFilename := filepath.Join(s.jotDir, fmt.Sprintf("jot_%s.txt", newToken))
	defaultContent := s.getDefaultContentWithBackReference(newToken, cookie.Value)
	if err := os.WriteFile(newFilename, []byte(defaultContent), 0644); err != nil {
		http.Error(w, "Failed to create new jot file", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    newToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) getOrCreateToken(r *http.Request) (string, error) {
	if token := r.URL.Query().Get("token"); token != "" {
		if !tokenRe.MatchString(token) {
			return "", fmt.Errorf("invalid token format")
		}
		filename := filepath.Join(s.jotDir, fmt.Sprintf("jot_%s.txt", token))
		if _, err := os.Stat(filename); err == nil {
			return token, nil
		}
		return "", fmt.Errorf("invalid token")
	}

	if cookie, err := r.Cookie("token"); err == nil {
		if tokenRe.MatchString(cookie.Value) {
			filename := filepath.Join(s.jotDir, fmt.Sprintf("jot_%s.txt", cookie.Value))
			if _, err := os.Stat(filename); err == nil {
				return cookie.Value, nil
			}
		}
	}

	files, err := filepath.Glob(filepath.Join(s.jotDir, "jot_*.txt"))
	if err != nil {
		return "", fmt.Errorf("failed to check existing files: %w", err)
	}

	if len(files) > 0 {
		return "", fmt.Errorf("token required")
	}

	return s.generateToken()
}

func (s *Server) getValidToken(r *http.Request) (string, error) {
	token := r.URL.Query().Get("token")
	if token == "" {
		cookie, err := r.Cookie("token")
		if err != nil {
			return "", fmt.Errorf("no token provided")
		}
		token = cookie.Value
	}

	if !tokenRe.MatchString(token) {
		return "", fmt.Errorf("invalid token format: %s", token)
	}

	filename := filepath.Join(s.jotDir, fmt.Sprintf("jot_%s.txt", token))
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return "", fmt.Errorf("invalid token: %s", token)
	} else if err != nil {
		return "", fmt.Errorf("error checking token: %w", err)
	}

	return token, nil
}

func (s *Server) generateToken() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	// Use RawURLEncoding to get a string without padding
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func (s *Server) getDefaultContent(token string) string {
	scheme := "http"
	if s.tlsEnabled {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s:%s", scheme, s.host, s.port)

	return fmt.Sprintf(`Welcome to jotter!

Make sure to save the link below, it's the only way to access this jot:

%s/%s

To create a new jot, visit:

%s/new

*CAUTION*: Creating a new jot in the same browser will switch to the new jot session. Make sure you save the token!

If you want to "log out" of jotter, simply clear your browser's cookies.`,
		baseURL, token, baseURL)
}

func (s *Server) getDefaultContentWithBackReference(newToken, originalToken string) string {
	scheme := "http"
	if s.tlsEnabled {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s:%s", scheme, s.host, s.port)

	return fmt.Sprintf(`Welcome to jotter!

This jot was created from: %s/%s

Make sure to save the link below, it's the only way to access this jot:

%s/%s

To create a new jot, visit:

%s/new

*CAUTION*: Creating a new jot in the same browser will switch to the new jot session. Make sure you save the token!

If you want to "log out" of jotter, simply clear your browser's cookies.`,
		baseURL, originalToken, baseURL, newToken, baseURL)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func main() {
	server, err := NewServer()
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	go func() {
		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}

	log.Println("Server exiting")
}
