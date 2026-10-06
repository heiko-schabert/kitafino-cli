package kitafino

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ErrLogin means kitafino rejected the credentials.
var ErrLogin = errors.New("login failed, check credentials")

// Login lives on auth.kitafino.de, the app on user.kitafino.de; the session
// cookie is set for .kitafino.de. Vars so tests can point them at httptest.
var (
	LoginURL = "https://auth.kitafino.de/sys_k2/index.php"
	BaseURL  = "https://user.kitafino.de/sys_k2/index.php"
)

const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// Client is one kitafino session; the PHP session is shared state, so
// requests are serialized.
type Client struct {
	cfg      Config
	http     *http.Client
	mu       sync.Mutex
	loggedIn bool
}

func New(cfg Config) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{cfg: cfg, http: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
}

func (c *Client) CheckLogin(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.login(ctx)
}

func (c *Client) login(ctx context.Context) error {
	c.loggedIn = false
	c.http.Jar, _ = cookiejar.New(nil)
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.WriteField("benutzername", c.cfg.User)
	w.WriteField("passwort", c.cfg.Password)
	if err := w.Close(); err != nil {
		return err
	}
	body, err := c.do(ctx, http.MethodPost, LoginURL+"?action=do_login", &buf, w.FormDataContentType())
	if err != nil {
		return err
	}
	if isLoginPage(body) {
		return ErrLogin
	}
	c.loggedIn = true
	slog.InfoContext(ctx, "logged in")
	return nil
}

// isLoginPage detects the login form; kitafino answers expired sessions
// with it instead of an HTTP error.
func isLoginPage(body []byte) bool {
	return bytes.Contains(body, []byte(`name="passwort"`))
}

func (c *Client) do(ctx context.Context, method, url string, body io.Reader, contentType string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "de-DE,de;q=0.9")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		slog.DebugContext(ctx, "request failed", "method", method, "url", url, "err", err)
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	slog.DebugContext(ctx, "request", "method", method, "url", url, "status", resp.StatusCode,
		"final", resp.Request.URL.String(), "bytes", len(b), "duration", time.Since(start).Round(time.Millisecond))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s %s: %s", method, url, resp.Status)
	}
	return b, err
}

// fetch GETs query with a valid session, re-logging in once if it expired.
func (c *Client) fetch(ctx context.Context, query string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loggedIn {
		if err := c.login(ctx); err != nil {
			return nil, err
		}
	}
	b, err := c.do(ctx, http.MethodGet, BaseURL+query, nil, "")
	if err != nil || !isLoginPage(b) {
		return b, err
	}
	slog.WarnContext(ctx, "session expired, logging in again", "query", query)
	if err := c.login(ctx); err != nil {
		return nil, err
	}
	b, err = c.do(ctx, http.MethodGet, BaseURL+query, nil, "")
	if err == nil && isLoginPage(b) {
		return nil, fmt.Errorf("%s: session invalid after re-login", query)
	}
	return b, err
}

// post submits a form scraped from a page. Its transaction_key belongs to the
// session, so an expired session cannot be retried with the same form.
func (c *Client) post(ctx context.Context, query string, form url.Values) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	slog.InfoContext(ctx, "submitting form", "query", query, "sid", form.Get("sid"), "do", form.Get("do"))
	b, err := c.do(ctx, http.MethodPost, BaseURL+query, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
	if err != nil {
		return nil, fmt.Errorf("%w; status unknown, check the menu before retrying", err)
	}
	if isLoginPage(b) {
		c.loggedIn = false
		return nil, errors.New("session expired, nothing changed; try again")
	}
	return b, nil
}
