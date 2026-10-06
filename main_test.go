package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"kitafino-cli/kitafino"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func fakeKitafino(t *testing.T, fixture string) *kitafino.Client {
	t.Helper()
	page, err := os.ReadFile("kitafino/testdata/" + fixture)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(page) }))
	t.Cleanup(srv.Close)
	oldLogin, oldBase := kitafino.LoginURL, kitafino.BaseURL
	kitafino.LoginURL, kitafino.BaseURL = srv.URL, srv.URL
	t.Cleanup(func() { kitafino.LoginURL, kitafino.BaseURL = oldLogin, oldBase })
	return kitafino.New(kitafino.Config{User: "u", Password: "p"})
}

func runCLI(t *testing.T, c *kitafino.Client, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(context.Background(), newServer(c, false), func() error { return nil }, args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func toolNames(t *testing.T, allowWrite bool) []string {
	t.Helper()
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	if _, err := newServer(kitafino.New(kitafino.Config{}), allowWrite).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
	}
	slices.Sort(names)
	return names
}

func TestWriteToolsGated(t *testing.T) {
	read := []string{"check_login", "get_account", "get_menu"}
	if got := toolNames(t, false); !slices.Equal(got, read) {
		t.Fatalf("read-only: %v", got)
	}
	if got := toolNames(t, true); !slices.Equal(got, []string{"cancel_meal", "check_login", "get_account", "get_menu", "order_meal"}) {
		t.Fatalf("write: %v", got)
	}
}

func TestMenuCommand(t *testing.T) {
	c := fakeKitafino(t, "week_locked.html")
	code, out, errOut := runCLI(t, c, "menu", "--date", "2026-09-30")
	if code != 0 || errOut != "" || !strings.Contains(out, "Thu 2026-10-01  order by 2026-09-30 08:30\n    1  Hot Dog") ||
		!strings.Contains(out, "Mon 2026-09-28  closed") {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s", code, errOut, out)
	}
	code, out, _ = runCLI(t, c, "menu", "--date", "2026-09-30", "--json")
	if code != 0 || !strings.Contains(out, `"name": "Bunte Fussili mit Käsesauce"`) {
		t.Fatalf("--json: exit %d, stdout %s", code, out)
	}
}

func TestMenuOrdered(t *testing.T) {
	_, out, _ := runCLI(t, fakeKitafino(t, "week_ordered.html"), "menu", "--date", "2026-10-12")
	if !strings.Contains(out, "Mon 2026-10-12  cancel by 2026-10-12 08:30\n  ✓ 1  Chili con Carne") || !strings.Contains(out, "Balance 16,80 €") {
		t.Fatalf("stdout:\n%s", out)
	}
}

func TestLogLevel(t *testing.T) {
	c := fakeKitafino(t, "week_locked.html")
	code, _, errOut := runCLI(t, c, "--log-level", "debug", "account")
	if code != 0 || !strings.Contains(errOut, "level=DEBUG") || !strings.Contains(errOut, "action=bestellen") {
		t.Fatalf("debug: exit %d, stderr %q", code, errOut)
	}
	if code, _, _ := runCLI(t, c, "--log-level", "laut", "account"); code != 1 {
		t.Fatalf("invalid level: exit %d", code)
	}
	t.Setenv("KITAFINO_LOG_LEVEL", "info")
	if _, _, errOut := runCLI(t, c, "account"); !strings.Contains(errOut, "tool call") || strings.Contains(errOut, "level=DEBUG") {
		t.Fatalf("env info: stderr %q", errOut)
	}
}
