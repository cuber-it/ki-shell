// Copyright 2026 cuber IT service. Assisted by Claude Code (Anthropic).
// Licensed under Apache 2.0.
package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

//go:embed web/static
var webFS embed.FS

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type WebConfig struct {
	Addr     string
	Token    string
	TLS      bool
	CertFile string
	KeyFile  string
}

var webServer *http.Server
var webToken string

// startWebBackground starts the web server as a goroutine within the running shell.
func startWebBackground(addr, token string, insecure bool) {
	if webServer != nil {
		fmt.Fprintln(os.Stderr, "[web] already running")
		return
	}
	if addr == "" {
		addr = ":12080"
	}
	webToken = token

	cfg := WebConfig{Addr: addr, Token: token, TLS: !insecure}
	go func() {
		err := runWebServer(cfg)
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "[web] error: %s\n", err)
		}
		webServer = nil
	}()

	proto := "https"
	if insecure {
		proto = "http"
	}
	hostname, _ := os.Hostname()
	if token == "" {
		fmt.Fprintf(os.Stderr, "[web] %s://%s%s (no auth!)\n", proto, hostname, addr)
	} else {
		fmt.Fprintf(os.Stderr, "[web] %s://%s%s (token: %s)\n", proto, hostname, addr, token)
	}
}

func stopWebBackground() {
	if webServer == nil {
		fmt.Fprintln(os.Stderr, "[web] not running")
		return
	}
	webServer.Shutdown(context.Background())
	webServer = nil
	fmt.Fprintln(os.Stderr, "[web] stopped")
}

func runWebServer(cfg WebConfig) error {
	mux := http.NewServeMux()

	// Static files
	staticFS, _ := fs.Sub(webFS, "web/static")
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	// WebSocket terminal
	mux.HandleFunc("/ws/terminal", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, cfg.Token) {
			http.Error(w, "unauthorized", 401)
			return
		}
		handleTerminalWS(w, r)
	})

	// REST API
	mux.HandleFunc("/api/status", authWrap(cfg.Token, handleAPIStatus))
	mux.HandleFunc("/api/ki", authWrap(cfg.Token, handleAPIKI))
	mux.HandleFunc("/api/exec", authWrap(cfg.Token, handleAPIExec))
	mux.HandleFunc("/api/history", authWrap(cfg.Token, handleAPIHistory))
	mux.HandleFunc("/api/costs", authWrap(cfg.Token, handleAPICosts))
	mux.HandleFunc("/api/memory", authWrap(cfg.Token, handleAPIMemory))

	addr := cfg.Addr
	if addr == "" {
		addr = ":12080"
	}

	srv := &http.Server{Addr: addr, Handler: mux}
	webServer = srv

	if cfg.TLS {
		certFile, keyFile := cfg.CertFile, cfg.KeyFile
		if certFile == "" {
			certFile, keyFile = ensureSelfSignedCert()
		}
		return srv.ListenAndServeTLS(certFile, keyFile)
	}
	return srv.ListenAndServe()
}

func checkAuth(r *http.Request, token string) bool {
	if token == "" {
		return true // no auth required
	}
	auth := r.Header.Get("Authorization")
	if strings.TrimPrefix(auth, "Bearer ") == token {
		return true
	}
	if r.URL.Query().Get("token") == token {
		return true
	}
	return false
}

func authWrap(token string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, token) {
			if audit != nil {
				audit.Log("WEB", "unauthorized: "+r.URL.Path, "denied", r.RemoteAddr)
			}
			http.Error(w, "unauthorized", 401)
			return
		}
		if audit != nil {
			audit.Log("WEB", r.Method+" "+r.URL.Path, "allowed", r.RemoteAddr)
		}
		handler(w, r)
	}
}

// handleTerminalWS creates a PTY running kish and bridges it to a WebSocket.
func handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Start kish in a PTY
	sessionID := generateToken()[:8]
	kishPath, _ := os.Executable()
	cmd := exec.Command(kishPath)
	cmd.Env = append(os.Environ(), "KISH_WEB_SESSION="+sessionID, "KISH_WEB_CLIENT="+r.RemoteAddr)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("failed to start shell: "+err.Error()))
		return
	}
	defer ptmx.Close()

	if audit != nil {
		audit.Log("WEB", fmt.Sprintf("terminal session %s started", sessionID), "auto", r.RemoteAddr)
	}

	var once sync.Once
	done := make(chan struct{})

	// PTY → WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				once.Do(func() { close(done) })
				return
			}
			conn.WriteMessage(websocket.TextMessage, buf[:n])
		}
	}()

	// WebSocket → PTY
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				once.Do(func() { close(done) })
				return
			}
			// Check for resize message
			var resize struct {
				Type string `json:"type"`
				Cols int    `json:"cols"`
				Rows int    `json:"rows"`
			}
			if json.Unmarshal(msg, &resize) == nil && resize.Type == "resize" {
				pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(resize.Cols), Rows: uint16(resize.Rows)})
				continue
			}
			ptmx.Write(msg)
		}
	}()

	<-done
	cmd.Process.Kill()
	cmd.Wait()

	if audit != nil {
		audit.Log("WEB", fmt.Sprintf("terminal session %s ended", sessionID), "auto", r.RemoteAddr)
	}
}

// REST API handlers

func handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	ensureKIEngine()
	costToday := 0.0
	if pe, ok := kiEngine.(*ProviderEngine); ok {
		if stats := pe.TodayStats(); stats != nil {
			costToday = stats.Cost
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"engine":     kiEngine.Name(),
		"available":  kiEngine.Available(),
		"memories":   len(kiMemory.AllFacts()),
		"cost_today": fmt.Sprintf("%.4f", costToday),
	})
}

func handleAPIKI(w http.ResponseWriter, r *http.Request) {
	ensureKIEngine()
	var req struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Query == "" {
		json.NewEncoder(w).Encode(map[string]string{"error": "missing query"})
		return
	}

	filteredCtx := kiPermissions.FilterContext(shellContext.Collect())
	var output strings.Builder
	_, err := kiEngine.Query(context.Background(), req.Query, filteredCtx, &output)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"response": output.String()})
}

func handleAPIExec(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cmd string `json:"cmd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Cmd == "" {
		json.NewEncoder(w).Encode(map[string]string{"error": "missing cmd"})
		return
	}

	// Permission check
	allowed, _, reason := kiPermissions.CheckCommand(req.Cmd)
	if !allowed {
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "blocked: " + reason, "exit_code": -1})
		return
	}

	stdout, stderr, exitCode := ExecuteAction(context.Background(), req.Cmd, 30e9)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"stdout":    stdout,
		"stderr":    stderr,
		"exit_code": exitCode,
	})
}

func handleAPIHistory(w http.ResponseWriter, r *http.Request) {
	entries := kishHistory.Last(50)
	var result []map[string]interface{}
	for _, e := range entries {
		result = append(result, map[string]interface{}{
			"num": e.Num, "time": e.Time.Format("2006-01-02 15:04:05"),
			"command": e.Command, "tty": e.TTY,
		})
	}
	json.NewEncoder(w).Encode(result)
}

func handleAPICosts(w http.ResponseWriter, r *http.Request) {
	ensureKIEngine()
	if pe, ok := kiEngine.(*ProviderEngine); ok {
		today := pe.TodayStats()
		reqs, tokIn, tokOut, cost := pe.TotalStats()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"today":         today,
			"total_requests": reqs, "total_input_tokens": tokIn,
			"total_output_tokens": tokOut, "total_cost": cost,
		})
	} else {
		json.NewEncoder(w).Encode(map[string]string{"error": "no cost tracking"})
	}
}

func handleAPIMemory(w http.ResponseWriter, r *http.Request) {
	facts := kiMemory.AllFacts()
	var result []map[string]string
	for _, f := range facts {
		result = append(result, map[string]string{
			"key": f.Key, "value": f.Value, "category": f.Category,
		})
	}
	json.NewEncoder(w).Encode(result)
}

func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func ensureSelfSignedCert() (string, string) {
	certFile := os.Getenv("HOME") + "/.kish/cert.pem"
	keyFile := os.Getenv("HOME") + "/.kish/key.pem"
	if fileExists(certFile) && fileExists(keyFile) {
		return certFile, keyFile
	}
	// Generate self-signed cert
	cert, key, err := generateSelfSignedCert()
	if err != nil {
		fmt.Fprintf(os.Stderr, "kish web: TLS cert generation failed: %s\n", err)
		return "", ""
	}
	os.WriteFile(certFile, cert, 0600)
	os.WriteFile(keyFile, key, 0600)
	fmt.Fprintf(os.Stderr, "kish web: self-signed cert created\n")
	return certFile, keyFile
}

func generateSelfSignedCert() ([]byte, []byte, error) {
	// Use openssl for simplicity
	cmd := exec.Command("openssl", "req", "-x509", "-newkey", "rsa:2048", "-keyout", "/dev/stdout",
		"-out", "/dev/stdout", "-days", "365", "-nodes", "-subj", "/CN=kish")
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, err
	}
	// Split PEM blocks
	parts := strings.SplitN(string(out), "-----END PRIVATE KEY-----", 2)
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("unexpected openssl output")
	}
	key := []byte(parts[0] + "-----END PRIVATE KEY-----\n")
	cert := []byte(strings.TrimSpace(parts[1]) + "\n")
	return cert, key, nil
}

// Ensure TLS config ignores self-signed cert warnings
var _ = tls.Config{InsecureSkipVerify: true}

// Ensure io import
var _ = io.EOF
