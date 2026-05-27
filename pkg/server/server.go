package server

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Server is a development HTTP server with WebSocket live reload.
type Server struct {
	publicDir  string
	contentDir string
	port       int
	onRebuild  func() error

	mu      sync.Mutex
	clients map[chan string]struct{}
}

// New creates a new development server.
func New(publicDir, contentDir string, port int, onRebuild func() error) *Server {
	return &Server{
		publicDir:  publicDir,
		contentDir: contentDir,
		port:       port,
		onRebuild:  onRebuild,
		clients:    make(map[chan string]struct{}),
	}
}

// Start starts the server and file watcher. Blocks until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// WebSocket upgrade handler for live reload
	mux.HandleFunc("/__tago_ws", s.handleWS)

	// Serve static files from publicDir
	mux.Handle("/", http.FileServer(http.Dir(s.publicDir)))

	addr := fmt.Sprintf(":%d", s.port)
	srv := &http.Server{Addr: addr, Handler: mux}

	// Start file watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer watcher.Close()

	if err := watcher.Add(s.contentDir); err != nil {
		log.Printf("watcher: %v", err)
	}

	go s.watchLoop(ctx, watcher)

	// Start HTTP server
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	fmt.Printf("tago: serving at http://localhost%s\n", addr)

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx) //nolint
	}()

	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) watchLoop(ctx context.Context, watcher *fsnotify.Watcher) {
	var debounce *time.Timer
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			if debounce != nil {
				debounce.Stop()
			}
			eventName := event.Name
			debounce = time.AfterFunc(100*time.Millisecond, func() {
				log.Printf("tago: rebuilding (changed: %s)", eventName)
				if err := s.onRebuild(); err != nil {
					log.Printf("tago: rebuild error: %v", err)
				} else {
					s.broadcast("reload")
				}
			})
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("watcher error: %v", err)
		}
	}
}

func (s *Server) broadcast(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Upgrade") != "websocket" {
		s.handleSSE(w, r)
		return
	}

	key := r.Header.Get("Sec-Websocket-Key")
	if key == "" {
		http.Error(w, "missing Sec-Websocket-Key", http.StatusBadRequest)
		return
	}

	accept := wsAccept(key)

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	conn, buf, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()

	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	buf.WriteString(response)
	buf.Flush()

	ch := make(chan string, 1)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
	}()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			frame := wsTextFrame([]byte(msg))
			if _, err := conn.Write(frame); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan string, 1)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
	}()

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-time.After(30 * time.Second):
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func wsAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func wsTextFrame(payload []byte) []byte {
	n := len(payload)
	var frame []byte
	frame = append(frame, 0x81) // FIN + text opcode
	if n < 126 {
		frame = append(frame, byte(n))
	} else if n < 65536 {
		frame = append(frame, 126, byte(n>>8), byte(n))
	} else {
		frame = append(frame, 127,
			0, 0, 0, 0,
			byte(n>>24), byte(n>>16), byte(n>>8), byte(n),
		)
	}
	frame = append(frame, payload...)
	return frame
}
