package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"jotter/web"

	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
)

const htmlTemplate = `<!doctype html>
<html lang="en">
    <head>
        <meta charset="UTF-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1.0" />
        <title>jotter</title>
        <link rel="icon" type="image/x-icon" href="/static/img/favicon.ico" />
        <link rel="stylesheet" href="/static/css/style.css">
    </head>
    <body>
        <div
        	contenteditable="plaintext-only"
            id="jot-field"
            placeholder="Start typing..."
            oninput="handleInput()"
        >{{.Content}}</div>
        <script src="/static/js/script.js"></script>
    </body>
</html>`

type Server struct {
	jotDir     string
	host       string
	port       string
	tlsEnabled bool
	certFile   string
	keyFile    string
	watcher    *fsnotify.Watcher
	clients    map[string]map[string]chan []byte // token -> sessionId -> channel
	lastWriter map[string]string                 // token -> sessionId
	mu         sync.RWMutex
	tmpl       *template.Template
}

type UpdateMessage struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	Writer  string `json:"writer"`
}

type WriteRequest struct {
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
		return nil, fmt.Errorf("failed to watch directory: %w", err)
	}

	tmpl, err := template.New("index").Parse(htmlTemplate)
	if err != nil {
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
		lastWriter: make(map[string]string),
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

	if s.tlsEnabled {
		log.Printf("Starting TLS server on https://%s", addr)
		return http.ListenAndServeTLS(addr, s.certFile, s.keyFile, mux)
	} else {
		log.Printf("Starting server on http://%s", addr)
		return http.ListenAndServe(addr, mux)
	}
}

func (s *Server) watchFiles() {
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

	s.mu.RLock()
	clients := s.clients[token]
	lastWriter := s.lastWriter[token]
	s.mu.RUnlock()

	if clients == nil {
		return
	}

	message := UpdateMessage{
		Type:    "content_update",
		Content: string(content),
		Writer:  lastWriter,
	}

	s.broadcastToClients(clients, message, lastWriter)
}

func (s *Server) broadcastToClients(clients map[string]chan []byte, message UpdateMessage, excludeSession string) {
	messageBytes := s.formatSSEMessage(message)

	for sessionId, ch := range clients {
		if sessionId != excludeSession {
			select {
			case ch <- messageBytes:
			default:
				// Channel is full or closed, skip
			}
		}
	}
}

func (s *Server) formatSSEMessage(message UpdateMessage) []byte {
	data := fmt.Sprintf(`{"type":"%s","content":%q,"writer":"%s"}`,
		message.Type, message.Content, message.Writer)
	return fmt.Appendf(nil, "data: %s\n\n", data)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
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
	data := struct {
		Content string
	}{
		Content: string(content),
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

	token, err := s.getTokenFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sessionId := r.Header.Get("X-Session-Id")
	if sessionId == "" {
		sessionId = uuid.New().String()
	}

	var req WriteRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	filename := filepath.Join(s.jotDir, fmt.Sprintf("jot_%s.txt", token))

	s.mu.Lock()
	s.lastWriter[token] = sessionId
	s.mu.Unlock()

	if err := os.WriteFile(filename, []byte(req.Content), 0644); err != nil {
		http.Error(w, "Failed to write file", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUpdates(w http.ResponseWriter, r *http.Request) {
	token, err := s.getTokenFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sessionId := r.Header.Get("X-Session-Id")
	if sessionId == "" {
		sessionId = uuid.New().String()
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create channel for this client
	clientChan := make(chan []byte, 10)

	s.mu.Lock()
	if s.clients[token] == nil {
		s.clients[token] = make(map[string]chan []byte)
	}
	s.clients[token][sessionId] = clientChan
	s.mu.Unlock()

	// Clean up on disconnect
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

	// Send initial connection message
	initialMsg := UpdateMessage{
		Type:    "connected",
		Content: "",
		Writer:  "",
	}
	initialBytes := s.formatSSEMessage(initialMsg)
	if _, err := w.Write(initialBytes); err != nil {
		return
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Send keep-alive and handle client messages
	ticker := time.NewTicker(5 * time.Second)
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
			// Send keep-alive data message
			keepAlive := UpdateMessage{
				Type:    "heartbeat",
				Content: "",
				Writer:  "",
			}
			messageBytes := s.formatSSEMessage(keepAlive)
			if _, err := w.Write(messageBytes); err != nil {
				return
			}
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
	// Check URL parameter first
	if token := r.URL.Query().Get("token"); token != "" {
		filename := filepath.Join(s.jotDir, fmt.Sprintf("jot_%s.txt", token))
		if _, err := os.Stat(filename); err == nil {
			return token, nil
		}
		return "", fmt.Errorf("invalid token")
	}

	// Check cookie
	if cookie, err := r.Cookie("token"); err == nil {
		filename := filepath.Join(s.jotDir, fmt.Sprintf("jot_%s.txt", cookie.Value))
		if _, err := os.Stat(filename); err == nil {
			return cookie.Value, nil
		}
	}

	// Check if any jot files exist
	files, err := filepath.Glob(filepath.Join(s.jotDir, "jot_*.txt"))
	if err != nil {
		return "", fmt.Errorf("failed to check existing files")
	}

	if len(files) > 0 {
		return "", fmt.Errorf("token required")
	}

	// No files exist, create new token
	return s.generateToken()
}

func (s *Server) getTokenFromRequest(r *http.Request) (string, error) {
	if cookie, err := r.Cookie("token"); err == nil {
		return cookie.Value, nil
	}
	return "", fmt.Errorf("no token found")
}

func (s *Server) generateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate token")
	}
	return base64.URLEncoding.EncodeToString(bytes)[:32], nil
}

func (s *Server) getDefaultContent(token string) string {
	scheme := "http"
	if s.tlsEnabled {
		scheme = "https"
	}

	return fmt.Sprintf(`Welcome to jotter!

Make sure to save the link below, it's the only way to access this jot (go to it once and it will show up in your browser's history):

%s://%s:%s/?token=%s

To create a new jot, visit:

%s://%s:%s/new

*CAUTION*: Creating a new jot in the same browser will switch to the new jot session. Make sure you save the token!

If you want to "log out" of jotter, simply clear your browser's cookies.`,
		scheme, s.host, s.port, token, scheme, s.host, s.port)
}

func (s *Server) getDefaultContentWithBackReference(newToken, originalToken string) string {
	scheme := "http"
	if s.tlsEnabled {
		scheme = "https"
	}

	return fmt.Sprintf(`Welcome to jotter!

This jot was created from: %s://%s:%s/?token=%s

Make sure to save the link below, it's the only way to access this website:

%s://%s:%s/?token=%s

To create a new jot, visit:

%s://%s:%s/new

*CAUTION*: Creating a new jot in the same browser will switch to the new jot session. Make sure you save the token!

If you want to "log out" of jotter, simply clear your browser's cookies.`,
		scheme, s.host, s.port, originalToken, scheme, s.host, s.port, newToken, scheme, s.host, s.port)
}

func decodeJSON(r io.Reader, v any) error {
	// Simple JSON decoder for WriteRequest
	body, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	// Basic JSON parsing for our simple struct
	str := string(body)
	if !strings.Contains(str, `"content"`) {
		return fmt.Errorf("missing content field")
	}

	// Extract content value (simple approach for this specific case)
	start := strings.Index(str, `"content":"`) + 11
	if start < 11 {
		return fmt.Errorf("invalid JSON format")
	}

	end := strings.LastIndex(str, `"`)
	if end <= start {
		return fmt.Errorf("invalid JSON format")
	}

	content := str[start:end]
	// Unescape basic JSON escapes
	content = strings.ReplaceAll(content, `\"`, `"`)
	content = strings.ReplaceAll(content, `\\`, `\`)
	content = strings.ReplaceAll(content, `\n`, "\n")
	content = strings.ReplaceAll(content, `\r`, "\r")
	content = strings.ReplaceAll(content, `\t`, "\t")

	if req, ok := v.(*WriteRequest); ok {
		req.Content = content
		return nil
	}

	return fmt.Errorf("unsupported type")
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

	if err := server.Start(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
