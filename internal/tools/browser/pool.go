package browser

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

// BrowserInstance represents a browser instance with its page
type BrowserInstance struct {
	Browser playwright.Browser
	Context playwright.BrowserContext
	Page    playwright.Page
	ID      string
}

// Pool manages a pool of browser instances
type Pool struct {
	config    PoolConfig
	instances chan *BrowserInstance
	mu        sync.Mutex
	closed    bool
	pw        *playwright.Playwright
	userAgent int // Counter for user agent rotation
	created   int // Number of live instances
}

// PoolConfig configures the browser pool
type PoolConfig struct {
	MaxInstances   int           // Maximum number of browser instances
	Timeout        time.Duration // Default timeout for operations
	Headless       bool          // Run browsers in headless mode
	ViewportWidth  int           // Viewport width (default: 1920)
	ViewportHeight int           // Viewport height (default: 1080)
}

// NewPool creates a new browser instance pool
func NewPool(config PoolConfig) (*Pool, error) {
	// Set defaults
	if config.MaxInstances == 0 {
		config.MaxInstances = 5
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.ViewportWidth == 0 {
		config.ViewportWidth = 1920
	}
	if config.ViewportHeight == 0 {
		config.ViewportHeight = 1080
	}

	// Install and run Playwright
	err := playwright.Install(&playwright.RunOptions{
		Verbose: false,
	})
	if err != nil {
		return &Pool{
			config:    config,
			instances: make(chan *BrowserInstance, config.MaxInstances),
			closed:    false,
		}, nil // Return pool anyway, will fail on first Acquire
	}

	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to start playwright: %w", err)
	}

	pool := &Pool{
		config:    config,
		instances: make(chan *BrowserInstance, config.MaxInstances),
		closed:    false,
		pw:        pw,
		userAgent: 0,
	}

	return pool, nil
}

// Acquire gets a browser instance from the pool or creates a new one
func (p *Pool) Acquire(ctx context.Context) (*BrowserInstance, error) {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, fmt.Errorf("pool is closed")
		}
		select {
		case instance := <-p.instances:
			p.mu.Unlock()
			return instance, nil
		default:
		}
		if p.created < p.config.MaxInstances {
			p.created++
			p.mu.Unlock()
			instance, err := p.createInstance()
			if err != nil {
				p.mu.Lock()
				p.created--
				p.mu.Unlock()
				return nil, err
			}
			return instance, nil
		}
		p.mu.Unlock()

		select {
		case instance := <-p.instances:
			return instance, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Release returns a browser instance to the pool
func (p *Pool) Release(instance *BrowserInstance) {
	if instance == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		// Pool is closed, close this instance
		instance.cleanup()
		p.created--
		return
	}

	// Try to return to pool, if full then close
	select {
	case p.instances <- instance:
		// Successfully returned to pool
	default:
		// Pool is full, close this instance
		instance.cleanup()
		p.created--
	}
}

// Close closes all browser instances and shuts down the pool
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true

	// Close all instances in the pool
	close(p.instances)
	for instance := range p.instances {
		instance.cleanup()
	}
	p.created = 0

	// Stop Playwright
	if p.pw != nil {
		if err := p.pw.Stop(); err != nil {
			return fmt.Errorf("failed to stop playwright: %w", err)
		}
	}

	return nil
}

// createInstance creates a new browser instance
func (p *Pool) createInstance() (*BrowserInstance, error) {
	if p.pw == nil {
		return nil, fmt.Errorf("playwright not initialized")
	}

	// Launch browser
	browser, err := p.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(p.config.Headless),
		Timeout:  playwright.Float(float64(p.config.Timeout.Milliseconds())),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to launch browser: %w", err)
	}

	// Create browser context with configuration
	userAgent := p.getNextUserAgent()
	contextOptions := playwright.BrowserNewContextOptions{
		UserAgent: playwright.String(userAgent),
		Viewport: &playwright.Size{
			Width:  p.config.ViewportWidth,
			Height: p.config.ViewportHeight,
		},
		AcceptDownloads:   playwright.Bool(true),
		IgnoreHttpsErrors: playwright.Bool(true),
	}

	context, err := browser.NewContext(contextOptions)
	if err != nil {
		browser.Close()
		return nil, fmt.Errorf("failed to create browser context: %w", err)
	}

	// Create new page
	page, err := context.NewPage()
	if err != nil {
		context.Close()
		browser.Close()
		return nil, fmt.Errorf("failed to create page: %w", err)
	}

	// Set default timeout
	page.SetDefaultTimeout(float64(p.config.Timeout.Milliseconds()))

	instance := &BrowserInstance{
		Browser: browser,
		Context: context,
		Page:    page,
		ID:      fmt.Sprintf("browser-%d", time.Now().UnixNano()),
	}

	return instance, nil
}

// getNextUserAgent returns the next user agent in rotation
func (p *Pool) getNextUserAgent() string {
	userAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:123.0) Gecko/20100101 Firefox/123.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2.1 Safari/605.1.15",
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	ua := userAgents[p.userAgent%len(userAgents)]
	p.userAgent++
	return ua
}

// cleanup closes the browser instance
func (instance *BrowserInstance) cleanup() {
	if instance.Page != nil {
		instance.Page.Close()
	}
	if instance.Context != nil {
		instance.Context.Close()
	}
	if instance.Browser != nil {
		instance.Browser.Close()
	}
}

// SetCookie sets a cookie in the browser context
func (instance *BrowserInstance) SetCookie(cookies ...playwright.OptionalCookie) error {
	return instance.Context.AddCookies(cookies)
}

// GetCookies gets all cookies from the browser context
func (instance *BrowserInstance) GetCookies() ([]playwright.Cookie, error) {
	return instance.Context.Cookies()
}

// ClearCookies clears all cookies from the browser context
func (instance *BrowserInstance) ClearCookies() error {
	return instance.Context.ClearCookies()
}

// SetViewport sets the viewport size for the page
func (instance *BrowserInstance) SetViewport(width, height int) error {
	return instance.Page.SetViewportSize(width, height)
}

// GetStats returns pool statistics
func (p *Pool) GetStats() PoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	return PoolStats{
		MaxInstances:       p.config.MaxInstances,
		AvailableInstances: len(p.instances),
		IsClosed:           p.closed,
	}
}

// PoolStats contains pool statistics
type PoolStats struct {
	MaxInstances       int
	AvailableInstances int
	IsClosed           bool
}
