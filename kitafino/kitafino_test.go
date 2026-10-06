package kitafino

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseWeek(t *testing.T) {
	w, err := parseWeek(fixture(t, "week_ordered.html"))
	if err != nil {
		t.Fatal(err)
	}
	if w.Balance != 16.8 || len(w.Days) != 5 {
		t.Fatalf("balance %v, %d days", w.Balance, len(w.Days))
	}
	d := w.Days[0]
	if d.Date != "2026-10-12" || d.Weekday != "Monday" || d.OrderDeadline != "2026-10-09 08:30" || d.CancelDeadline != "2026-10-12 08:30" {
		t.Fatalf("day %+v", d)
	}
	m := d.Meals[0]
	if m.Number != 1 || m.Name != "Chili con Carne (Rind) mit Baguette" || m.Line != "Menülinie 1 (Fisch oder Fleisch)" ||
		m.Price != 5.2 || !m.Ordered || !m.Changeable || m.form.Get("do") != "storno" || m.form.Get("sid") != "1585" {
		t.Fatalf("meal %+v form %v", m, m.form)
	}
	if d.Meals[1].Ordered || d.Meals[1].form.Get("former_menu_id") != "1585" {
		t.Fatalf("sibling %+v", d.Meals[1])
	}
}

func TestParseWeekLocked(t *testing.T) {
	w, err := parseWeek(fixture(t, "week_locked.html"))
	if err != nil {
		t.Fatal(err)
	}
	if m := w.Days[0].Meals[0]; m.Changeable || m.form != nil || m.Name != "Hähnchenschlegel mit Bratkartoffeln" {
		t.Fatalf("locked meal %+v", m)
	}
	if m := w.Days[3].Meals[1]; !m.Changeable || !m.Vegetarian || m.Name != "Bunte Fussili mit Käsesauce" {
		t.Fatalf("open meal %+v", m)
	}
}

func TestParseAccount(t *testing.T) {
	a, err := parseAccount(fixture(t, "week_locked.html"))
	if err != nil {
		t.Fatal(err)
	}
	if a != (Account{Name: "Muster, Erika", CustomerID: "12345-678", Balance: 22}) {
		t.Fatalf("%+v", a)
	}
}

func TestWeekTS(t *testing.T) {
	// Values from kitafino's own week links.
	for _, date := range []string{"2026-09-28", "2026-09-30", "2026-10-04"} {
		d, _ := ParseDate(date)
		if got := weekTS(d); got != 1790589600 {
			t.Errorf("%s: %d", date, got)
		}
	}
}

func TestEuro(t *testing.T) {
	for in, want := range map[string]float64{"22,00": 22, "€ 5,20": 5.2, "1.234,50": 1234.5, "-3,20": -3.2, "€ 250,-": 250} {
		if got, err := euro(in); err != nil || got != want {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
}

// fakeKitafino serves page for every GET and reply for the order POST.
type fakeKitafino struct {
	page, reply []byte
	posted      url.Values
}

func newFake(t *testing.T, page, reply []byte) (*fakeKitafino, *Client) {
	f := &fakeKitafino{page: page, reply: reply}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("action") {
		case "do_login":
			w.Write(f.page)
		case "speichere_bestellung":
			r.ParseForm()
			f.posted = r.PostForm
			w.Write(f.reply)
		default:
			w.Write(f.page)
		}
	}))
	t.Cleanup(srv.Close)
	oldLogin, oldBase := LoginURL, BaseURL
	LoginURL, BaseURL = srv.URL, srv.URL
	t.Cleanup(func() { LoginURL, BaseURL = oldLogin, oldBase })
	return f, New(Config{User: "u", Password: "geheimXYZ"})
}

func date(s string) time.Time { d, _ := ParseDate(s); return d }

func TestCancel(t *testing.T) {
	page := fixture(t, "week_ordered.html")
	f, c := newFake(t, page, bytes.ReplaceAll(page, []byte("order_button_bestellt"), []byte("order_button")))
	if _, err := c.Cancel(context.Background(), date("2026-10-12")); err != nil {
		t.Fatal(err)
	}
	if f.posted.Get("do") != "storno" || f.posted.Get("sid") != "1585" || f.posted.Get("transaction_key") != "tk" {
		t.Fatalf("posted %v", f.posted)
	}
}

func TestOrderUnconfirmed(t *testing.T) {
	page := fixture(t, "week_ordered.html")
	f, c := newFake(t, page, page)
	_, err := c.Order(context.Background(), date("2026-10-13"), 2)
	if err == nil || !strings.Contains(err.Error(), "did not confirm") {
		t.Fatalf("err %v", err)
	}
	if f.posted.Get("sid") != "1588" || f.posted.Get("do") != "" {
		t.Fatalf("posted %v", f.posted)
	}
}

func TestOrderDeadline(t *testing.T) {
	f, c := newFake(t, fixture(t, "week_locked.html"), nil)
	_, err := c.Order(context.Background(), date("2026-09-28"), 1)
	if err == nil || !strings.Contains(err.Error(), "deadline passed") || f.posted != nil {
		t.Fatalf("err %v, posted %v", err, f.posted)
	}
}

func TestLoginLogHasNoSecrets(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	_, c := newFake(t, fixture(t, "week_locked.html"), nil)
	if err := c.CheckLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); strings.Contains(out, "geheimXYZ") || !strings.Contains(out, "logged in") {
		t.Fatalf("log:\n%s", out)
	}
}
