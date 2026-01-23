package canvas

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"

	"github.com/haasonsaas/nexus/internal/config"
)

const (
	defaultIndexHTML = "<!doctype html><html><head><meta charset=\"utf-8\" /><title>Nexus Canvas</title></head><body><h1>Nexus Canvas</h1><p>Drop files into this folder to render them here.</p></body></html>"
)

// Host serves a canvas directory on a dedicated HTTP server with optional live reload.
type Host struct {
	host         string
	port         int
	root         string
	rootReal     string
	namespace    string
	a2uiRoot     string
	liveReload   bool
	injectClient bool
	autoIndex    bool

	logger *slog.Logger

	server   *http.Server
	listener net.Listener

	mu          sync.RWMutex
	clients     map[*websocket.Conn]struct{}
	watcher     *fsnotify.Watcher
	watchCancel context.CancelFunc
	upgrader    websocket.Upgrader
}

// NewHost creates a canvas host for the given configuration.
func NewHost(cfg config.CanvasHostConfig, logger *slog.Logger) (*Host, error) {
	if strings.TrimSpace(cfg.Root) == "" {
		return nil, fmt.Errorf("canvas root is required")
	}
	if cfg.Port <= 0 {
		return nil, fmt.Errorf("canvas port must be set")
	}
	if logger == nil {
		logger = slog.Default()
	}
	namespace := normalizeNamespace(cfg.Namespace)
	liveReload := cfg.LiveReload != nil && *cfg.LiveReload
	injectClient := cfg.InjectClient != nil && *cfg.InjectClient
	autoIndex := cfg.AutoIndex != nil && *cfg.AutoIndex
	return &Host{
		host:         cfg.Host,
		port:         cfg.Port,
		root:         cfg.Root,
		namespace:    namespace,
		a2uiRoot:     strings.TrimSpace(cfg.A2UIRoot),
		liveReload:   liveReload,
		injectClient: injectClient,
		autoIndex:    autoIndex,
		logger:       logger.With("component", "canvas"),
		clients:      make(map[*websocket.Conn]struct{}),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  8192,
			WriteBufferSize: 8192,
			CheckOrigin: func(*http.Request) bool {
				return true
			},
		},
	}, nil
}

// Start begins serving the canvas host and optional live reload watcher.
func (h *Host) Start(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if h.server != nil {
		return nil
	}
	if err := h.ensureRoot(); err != nil {
		return err
	}
	rootReal, err := filepath.EvalSymlinks(h.root)
	if err != nil {
		return fmt.Errorf("resolve canvas root: %w", err)
	}
	h.rootReal = rootReal
	if h.autoIndex {
		h.ensureIndex(h.root)
	}

	mux := http.NewServeMux()

	canvasPrefix := h.canvasPrefix()
	mux.Handle(canvasPrefix+"/", http.StripPrefix(canvasPrefix+"/", h.canvasHandler()))
	mux.HandleFunc(canvasPrefix, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, canvasPrefix+"/", http.StatusFound)
	})

	if h.liveReload {
		mux.Handle(h.liveReloadScriptPath(), h.liveReloadScriptHandler())
		mux.Handle(h.liveReloadWSPath(), h.liveReloadWSHandler())
	}

	if h.a2uiRoot != "" {
		if info, err := os.Stat(h.a2uiRoot); err == nil && info.IsDir() {
			a2uiPrefix := h.a2uiPrefix()
			mux.Handle(a2uiPrefix+"/", http.StripPrefix(a2uiPrefix+"/", http.FileServer(http.Dir(h.a2uiRoot))))
			mux.HandleFunc(a2uiPrefix, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, a2uiPrefix+"/", http.StatusFound)
			})
		} else if err != nil && !os.IsNotExist(err) {
			h.logger.Warn("canvas a2ui root unavailable", "path", h.a2uiRoot, "error", err)
		}
	}

	addr := net.JoinHostPort(h.host, strconv.Itoa(h.port))
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("canvas listen: %w", err)
	}
	var watcher *fsnotify.Watcher
	if h.liveReload {
		watcher, err = fsnotify.NewWatcher()
		if err != nil {
			if closeErr := listener.Close(); closeErr != nil {
				h.logger.Warn("failed to close canvas listener", "error", closeErr)
			}
			return err
		}
		if err := h.watchRecursive(watcher, h.root); err != nil {
			if closeErr := watcher.Close(); closeErr != nil {
				h.logger.Warn("failed to close canvas watcher", "error", closeErr)
			}
			if closeErr := listener.Close(); closeErr != nil {
				h.logger.Warn("failed to close canvas listener", "error", closeErr)
			}
			return err
		}
	}

	h.server = server
	h.listener = listener

	if watcher != nil {
		watchCtx := ctx
		if watchCtx == nil {
			watchCtx = context.Background()
		}
		watchCtx, cancel := context.WithCancel(watchCtx)
		h.watchCancel = cancel
		h.watcher = watcher
		go h.watchLoop(watchCtx, watcher)
	}

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			h.logger.Error("canvas server error", "error", err)
		}
	}()

	h.logger.Info("starting canvas host", "addr", addr, "root", h.root, "namespace", h.namespace)
	return nil
}

// Close shuts down the canvas host and watcher.
func (h *Host) Close() error {
	if h == nil {
		return nil
	}
	if h.watchCancel != nil {
		h.watchCancel()
		h.watchCancel = nil
	}
	if h.watcher != nil {
		if err := h.watcher.Close(); err != nil {
			h.logger.Warn("failed to close canvas watcher", "error", err)
		}
		h.watcher = nil
	}
	if h.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.server.Shutdown(ctx); err != nil {
			h.logger.Warn("canvas server shutdown error", "error", err)
		}
		h.server = nil
		h.listener = nil
	}
	h.closeClients()
	return nil
}

// CanvasURL returns the absolute URL for the canvas root.
// requestHost should be the host name from the incoming client request (without port).
func (h *Host) CanvasURL(requestHost string) string {
	if h == nil {
		return ""
	}
	host := strings.TrimSpace(h.host)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = strings.TrimSpace(requestHost)
	}
	if host == "" {
		host = "localhost"
	}
	hostPort := net.JoinHostPort(host, strconv.Itoa(h.port))
	return fmt.Sprintf("http://%s%s/", hostPort, h.canvasPrefix())
}

func (h *Host) canvasHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte("Method Not Allowed")) //nolint:errcheck
			return
		}
		clean := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		if strings.HasPrefix(clean, "/..") {
			http.NotFound(w, r)
			return
		}
		fullPath, err := h.resolveFilePath(clean)
		if err != nil {
			if clean == "/" || strings.HasSuffix(clean, "/") {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("<!doctype html><meta charset=\"utf-8\" /><title>Nexus Canvas</title><pre>Missing file. Create index.html</pre>")) //nolint:errcheck
				return
			}
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if strings.HasSuffix(strings.ToLower(fullPath), ".html") {
			h.serveHTML(w, r, fullPath)
			return
		}
		http.ServeFile(w, r, fullPath)
	})
}

func (h *Host) liveReloadWSHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := h.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		h.addClient(conn)
		defer h.removeClient(conn)

		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
}

func (h *Host) liveReloadScriptHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		script := h.liveReloadScript()
		if _, err := io.WriteString(w, script); err != nil {
			h.logger.Warn("failed to write live reload script", "error", err)
		}
	})
}

func (h *Host) serveHTML(w http.ResponseWriter, r *http.Request, fullPath string) {
	data, err := os.ReadFile(fullPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	html := string(data)
	if h.injectClient && h.liveReload {
		html = h.injectLiveReload(html)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := io.WriteString(w, html); err != nil {
		h.logger.Warn("failed to write canvas html", "error", err)
	}
}

func (h *Host) injectLiveReload(html string) string {
	snippet := fmt.Sprintf("<script src=\"%s\"></script>", h.liveReloadScriptPath())
	if strings.Contains(html, snippet) || strings.Contains(html, h.liveReloadScriptPath()) {
		return html
	}
	if strings.Contains(html, "</body>") {
		return strings.Replace(html, "</body>", snippet+"</body>", 1)
	}
	if strings.Contains(html, "</head>") {
		return strings.Replace(html, "</head>", snippet+"</head>", 1)
	}
	return html + snippet
}

func (h *Host) liveReloadScript() string {
	return fmt.Sprintf(`(() => {
  const wsPath = %q;
  const scheme = window.location.protocol === "https:" ? "wss" : "ws";
  const wsUrl = scheme + "://" + window.location.host + wsPath;
  let socket = null;

  const connect = () => {
    socket = new WebSocket(wsUrl);
    socket.addEventListener("message", (event) => {
      if (event.data === "reload") {
        window.location.reload();
      }
    });
    socket.addEventListener("close", () => {
      setTimeout(connect, 1000);
    });
  };

  connect();
})();
`, h.liveReloadWSPath())
}

func (h *Host) addClient(conn *websocket.Conn) {
	h.mu.Lock()
	h.clients[conn] = struct{}{}
	h.mu.Unlock()
}

func (h *Host) removeClient(conn *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, conn)
	h.mu.Unlock()
	_ = conn.Close() //nolint:errcheck
}

func (h *Host) closeClients() {
	h.mu.Lock()
	clients := make([]*websocket.Conn, 0, len(h.clients))
	for conn := range h.clients {
		clients = append(clients, conn)
	}
	h.clients = make(map[*websocket.Conn]struct{})
	h.mu.Unlock()
	for _, conn := range clients {
		_ = conn.Close() //nolint:errcheck
	}
}

func (h *Host) broadcastReload() {
	h.mu.RLock()
	clients := make([]*websocket.Conn, 0, len(h.clients))
	for conn := range h.clients {
		clients = append(clients, conn)
	}
	h.mu.RUnlock()

	for _, conn := range clients {
		_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
		if err := conn.WriteMessage(websocket.TextMessage, []byte("reload")); err != nil {
			h.removeClient(conn)
		}
	}
}

func (h *Host) watchLoop(ctx context.Context, watcher *fsnotify.Watcher) {
	if watcher == nil {
		return
	}
	var mu sync.Mutex
	var timer *time.Timer
	debounce := 200 * time.Millisecond

	schedule := func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(debounce, func() {
			h.broadcastReload()
		})
	}

	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-watcher.Events:
			if !ok {
				return
			}
			if evt.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) != 0 {
				if shouldIgnorePath(evt.Name) {
					continue
				}
				if evt.Op&fsnotify.Create != 0 {
					info, err := os.Stat(evt.Name)
					if err == nil && info.IsDir() {
						if err := h.watchRecursive(watcher, evt.Name); err != nil {
							h.logger.Warn("failed to watch new directory", "path", evt.Name, "error", err)
						}
					}
				}
				schedule()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			h.logger.Warn("canvas watch error", "error", err)
		}
	}
}

func (h *Host) watchRecursive(watcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && shouldIgnorePath(path) {
			return filepath.SkipDir
		}
		return watcher.Add(path)
	})
}

func (h *Host) ensureRoot() error {
	info, err := os.Stat(h.root)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(h.root, 0o755)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("canvas root is not a directory: %s", h.root)
	}
	return nil
}

func (h *Host) ensureIndex(dir string) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	indexPath := filepath.Join(dir, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.logger.Warn("failed to create canvas directory", "path", dir, "error", err)
		return
	}
	if err := os.WriteFile(indexPath, []byte(defaultIndexHTML), 0o644); err != nil {
		h.logger.Warn("failed to write canvas index", "path", indexPath, "error", err)
	}
}

func (h *Host) canvasPrefix() string {
	return h.namespacedPath("canvas")
}

func (h *Host) a2uiPrefix() string {
	return h.namespacedPath("a2ui")
}

func (h *Host) liveReloadWSPath() string {
	return h.namespacedPath("ws")
}

func (h *Host) liveReloadScriptPath() string {
	return h.namespacedPath("live.js")
}

func (h *Host) namespacedPath(suffix string) string {
	suffix = strings.TrimPrefix(suffix, "/")
	if h.namespace == "/" {
		return "/" + suffix
	}
	return h.namespace + "/" + suffix
}

func normalizeNamespace(namespace string) string {
	clean := strings.TrimSpace(namespace)
	if clean == "" {
		clean = "/__nexus__"
	}
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	clean = strings.TrimRight(clean, "/")
	if clean == "" {
		clean = "/"
	}
	return clean
}

func (h *Host) resolveFilePath(urlPath string) (string, error) {
	rootReal := strings.TrimSpace(h.rootReal)
	if rootReal == "" {
		rootReal = h.root
		if resolved, err := filepath.EvalSymlinks(h.root); err == nil {
			rootReal = resolved
		}
	}

	normalized := path.Clean("/" + strings.TrimPrefix(urlPath, "/"))
	if strings.HasPrefix(normalized, "/..") {
		return "", os.ErrNotExist
	}
	rel := strings.TrimPrefix(normalized, "/")
	candidate := filepath.Join(h.root, filepath.FromSlash(rel))

	info, err := os.Stat(candidate)
	if err == nil && info.IsDir() {
		if h.autoIndex {
			h.ensureIndex(candidate)
		}
		candidate = filepath.Join(candidate, "index.html")
	}

	lstat, err := os.Lstat(candidate)
	if err != nil {
		return "", err
	}
	if lstat.Mode()&os.ModeSymlink != 0 {
		return "", os.ErrNotExist
	}
	realPath, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}

	rootReal = filepath.Clean(rootReal)
	realPath = filepath.Clean(realPath)
	rootPrefix := rootReal
	if !strings.HasSuffix(rootPrefix, string(os.PathSeparator)) {
		rootPrefix += string(os.PathSeparator)
	}
	if realPath != rootReal && !strings.HasPrefix(realPath, rootPrefix) {
		return "", os.ErrNotExist
	}
	return realPath, nil
}

func shouldIgnorePath(p string) bool {
	if p == "" {
		return false
	}
	parts := strings.FieldsFunc(p, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			continue
		}
		if strings.HasPrefix(part, ".") {
			return true
		}
		if part == "node_modules" {
			return true
		}
	}
	return false
}
