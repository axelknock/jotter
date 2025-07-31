package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestServer_HandleIndex(t *testing.T) {
	// Create temporary directory for testing
	tempDir := t.TempDir()

	server := &Server{
		jotDir:     tempDir,
		host:       "localhost",
		port:       "8000",
		clients:    make(map[string]map[string]chan []byte),
		lastWriter: make(map[string]string),
	}

	// Parse template
	tmpl, err := template.New("index").Parse(htmlTemplate)
	if err != nil {
		t.Fatalf("Failed to parse template: %v", err)
	}
	server.tmpl = tmpl

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	server.handleIndex(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Check that a token cookie was set
	cookies := resp.Cookies()
	found := false
	for _, cookie := range cookies {
		if cookie.Name == "token" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected token cookie to be set")
	}
}

func TestServer_HandleWrite(t *testing.T) {
	tempDir := t.TempDir()

	server := &Server{
		jotDir:     tempDir,
		host:       "localhost",
		port:       "8000",
		clients:    make(map[string]map[string]chan []byte),
		lastWriter: make(map[string]string),
	}

	// Create a test token and file
	token := "test-token"
	filename := filepath.Join(tempDir, fmt.Sprintf("jot_%s.txt", token))
	err := os.WriteFile(filename, []byte("initial content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	writeReq := WriteRequest{Content: "updated content"}
	body, _ := json.Marshal(writeReq)

	req := httptest.NewRequest("POST", "/write", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "token", Value: token})
	w := httptest.NewRecorder()

	server.handleWrite(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", resp.StatusCode)
	}

	// Verify file was updated
	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(content) != "updated content" {
		t.Errorf("Expected 'updated content', got '%s'", string(content))
	}
}

func TestTokenGeneration(t *testing.T) {
	server := &Server{}

	token1, err := server.generateToken()
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	token2, err := server.generateToken()
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	if token1 == token2 {
		t.Error("Generated tokens should be unique")
	}

	if len(token1) != 32 {
		t.Errorf("Expected token length 32, got %d", len(token1))
	}
}

func TestDefaultContent(t *testing.T) {
	server := &Server{
		host: "localhost",
		port: "8000",
	}

	token := "test-token"
	content := server.getDefaultContent(token)

	if !strings.Contains(content, token) {
		t.Error("Default content should contain the token")
	}

	if !strings.Contains(content, "Welcome to Jotter") {
		t.Error("Default content should contain welcome message")
	}
}

func BenchmarkConcurrentWrites(b *testing.B) {
	tempDir := b.TempDir()

	server := &Server{
		jotDir:     tempDir,
		host:       "localhost",
		port:       "8000",
		clients:    make(map[string]map[string]chan []byte),
		lastWriter: make(map[string]string),
	}

	// Create test file
	token := "benchmark-token"
	filename := filepath.Join(tempDir, fmt.Sprintf("jot_%s.txt", token))
	err := os.WriteFile(filename, []byte("initial"), 0644)
	if err != nil {
		b.Fatalf("Failed to create test file: %v", err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			writeReq := WriteRequest{Content: fmt.Sprintf("content-%d", i)}
			body, _ := json.Marshal(writeReq)

			req := httptest.NewRequest("POST", "/write", bytes.NewReader(body))
			req.AddCookie(&http.Cookie{Name: "token", Value: token})
			w := httptest.NewRecorder()

			server.handleWrite(w, req)
			i++
		}
	})
}

func BenchmarkSSEConnections(b *testing.B) {
	tempDir := b.TempDir()

	server := &Server{
		jotDir:     tempDir,
		host:       "localhost",
		port:       "8000",
		clients:    make(map[string]map[string]chan []byte),
		lastWriter: make(map[string]string),
	}

	// Create test file
	token := "sse-benchmark-token"
	filename := filepath.Join(tempDir, fmt.Sprintf("jot_%s.txt", token))
	err := os.WriteFile(filename, []byte("initial"), 0644)
	if err != nil {
		b.Fatalf("Failed to create test file: %v", err)
	}

	b.ResetTimer()

	var wg sync.WaitGroup
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			req := httptest.NewRequest("GET", "/updates", nil)
			req.AddCookie(&http.Cookie{Name: "token", Value: token})
			req.Header.Set("X-Session-Id", fmt.Sprintf("session-%d", id))

			w := httptest.NewRecorder()

			// Simulate brief connection
			go func() {
				time.Sleep(10 * time.Millisecond)
				// Connection would be closed by client disconnect in real scenario
			}()

			server.handleUpdates(w, req)
		}(i)
	}

	wg.Wait()
}

func BenchmarkFileOperations(b *testing.B) {
	tempDir := b.TempDir()
	filename := filepath.Join(tempDir, "benchmark.txt")

	b.Run("WriteFile", func(b *testing.B) {
		content := []byte("benchmark content for write operations")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			err := os.WriteFile(filename, content, 0644)
			if err != nil {
				b.Fatalf("WriteFile failed: %v", err)
			}
		}
	})

	b.Run("ReadFile", func(b *testing.B) {
		// Create file first
		err := os.WriteFile(filename, []byte("benchmark content"), 0644)
		if err != nil {
			b.Fatalf("Failed to create test file: %v", err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := os.ReadFile(filename)
			if err != nil {
				b.Fatalf("ReadFile failed: %v", err)
			}
		}
	})
}

// Stress test for memory leaks and resource cleanup
func TestConnectionCleanup(t *testing.T) {
	tempDir := t.TempDir()

	server := &Server{
		jotDir:     tempDir,
		host:       "localhost",
		port:       "7086",
		clients:    make(map[string]map[string]chan []byte),
		lastWriter: make(map[string]string),
	}

	token := "cleanup-test-token"
	filename := filepath.Join(tempDir, fmt.Sprintf("jot_%s.txt", token))
	err := os.WriteFile(filename, []byte("test"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Simulate multiple connections and disconnections
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/updates", nil)
		req.AddCookie(&http.Cookie{Name: "token", Value: token})
		req.Header.Set("X-Session-Id", fmt.Sprintf("session-%d", i))

		w := httptest.NewRecorder()

		// Start connection in goroutine
		go func() {
			server.handleUpdates(w, req)
		}()

		// Brief wait then "disconnect"
		time.Sleep(1 * time.Millisecond)
	}

	// Wait longer for cleanup to allow timeouts to trigger
	time.Sleep(100 * time.Millisecond)

	// Check that connections were cleaned up
	server.mu.RLock()
	clientCount := len(server.clients[token])
	server.mu.RUnlock()

	if clientCount > 10 { // Allow some connections to still be active
		t.Errorf("Expected most connections to be cleaned up, but found %d active", clientCount)
	}
}
