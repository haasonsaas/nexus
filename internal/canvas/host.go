package canvas

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Host serves a canvas directory with optional live reload.
type Host struct {
	root   string
	logger *slog.Logger

	mu       sync.RWMutex
	clients  map[chan struct{}]struct{}
	watcher  *fsnotify.Watcher
	watching bool
}

// NewHost creates a canvas host for the given root directory.
func NewHost(root string, logger *slog.Logger) (*Host, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("canvas root is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Host{
		root:    root,
		logger:  logger.With("component", "canvas"),
		clients: make(map[chan struct{}]struct{}),
	}, nil
}

// Start begins watching the canvas directory for changes.
func (h *Host) Start(ctx context.Context) error {
	if h == nil {
		return nil
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := h.watchRecursive(watcher, h.root); err != nil {
		_ = watcher.Close()
		return err
	}
	h.mu.Lock()
	h.watcher = watcher
	h.watching = true
	h.mu.Unlock()

	go h.watchLoop(ctx, watcher)
	return nil
}

// Close stops watching.
func (h *Host) Close() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.watcher != nil {
		err := h.watcher.Close()
		h.watcher = nil
		h.watching = false
		return err
	}
	return nil
}

// Handler serves static canvas files with live-reload script injection.
func (h *Host) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Clean(r.URL.Path)
		if strings.HasPrefix(path, "..") {
			http.NotFound(w, r)
			return
		}
		fullPath := filepath.Join(h.root, path)
		info, err := os.Stat(fullPath)
		if err == nil && info.IsDir() {
			fullPath = filepath.Join(fullPath, "index.html")
		}
		if strings.HasSuffix(fullPath, ".html") {
			h.serveHTML(w, r, fullPath)
			return
		}
		http.ServeFile(w, r, fullPath)
	})
}

// LiveReloadHandler streams reload events to the browser.
func (h *Host) LiveReloadHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		ch := make(chan struct{}, 1)
		h.addClient(ch)
		defer h.removeClient(ch)

		_, _ = fmt.Fprintf(w, "event: hello\ndata: %d\n\n", time.Now().Unix())
		flusher.Flush()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ch:
				_, _ = fmt.Fprintf(w, "event: reload\ndata: %d\n\n", time.Now().Unix())
				flusher.Flush()
			}
		}
	})
}

// LiveReloadScriptHandler serves the client-side live reload script.
func (h *Host) LiveReloadScriptHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		if _, err := io.WriteString(w, liveReloadScript); err != nil {
			h.logger.Warn("failed to write live reload script", "error", err)
		}
	})
}

func (h *Host) serveHTML(w http.ResponseWriter, r *http.Request, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	html := injectLiveReload(string(data))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := io.WriteString(w, html); err != nil {
		h.logger.Warn("failed to write canvas html", "error", err)
	}
}

func (h *Host) addClient(ch chan struct{}) {
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
}

func (h *Host) removeClient(ch chan struct{}) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

func (h *Host) broadcastReload() {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- struct{}{}:
		default:
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
		return watcher.Add(path)
	})
}

func injectLiveReload(html string) string {
	snippet := `<script src="/canvas/live.js"></script>`
	if strings.Contains(html, "</body>") {
		return strings.Replace(html, "</body>", snippet+"</body>", 1)
	}
	return html + snippet
}

const liveReloadScript = `
(() => {
  const source = new EventSource('/canvas/live');
  source.addEventListener('reload', () => {
    window.location.reload();
  });
})();
`
