package kitafino

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // kitafino weeks are Berlin weeks, also on hosts without zoneinfo

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

var berlin, _ = time.LoadLocation("Europe/Berlin")

type Week struct {
	Balance float64 `json:"balance_eur"`
	Days    []Day   `json:"days"`
}

type Day struct {
	Date           string `json:"date"`
	Weekday        string `json:"weekday"`
	OrderDeadline  string `json:"order_deadline,omitempty"`
	CancelDeadline string `json:"cancel_deadline,omitempty"`
	Meals          []Meal `json:"meals"`
}

type Meal struct {
	Number     int     `json:"number"`
	Line       string  `json:"line"`
	Name       string  `json:"name"`
	Vegetarian bool    `json:"vegetarian,omitempty"`
	Price      float64 `json:"price_eur"`
	Ordered    bool    `json:"ordered"`
	// Changeable means the deadline has not passed: an ordered meal can be
	// cancelled, any other ordered.
	Changeable bool       `json:"changeable"`
	form       url.Values // the page's own order/cancel form
}

// ParseDate reads YYYY-MM-DD; empty means today.
func ParseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Now().In(berlin), nil
	}
	return time.ParseInLocation(time.DateOnly, s, berlin)
}

// weekTS is the kw_ts kitafino uses in its week links: Monday noon, Berlin.
func weekTS(d time.Time) int64 {
	y, m, dd := d.In(berlin).Date()
	t := time.Date(y, m, dd, 12, 0, 0, 0, berlin)
	return t.AddDate(0, 0, -(int(t.Weekday())+6)%7).Unix()
}

// Week returns the menu of the week containing d.
func (c *Client) Week(ctx context.Context, d time.Time) (Week, error) {
	b, err := c.fetch(ctx, fmt.Sprintf("?action=bestellen&kw_ts=%d", weekTS(d)))
	if err != nil {
		return Week{}, err
	}
	w, err := parseWeek(b)
	if err != nil {
		return Week{}, err
	}
	// kitafino falls back to another week when the requested one has no menu yet.
	if len(w.Days) > 0 {
		first, _ := ParseDate(w.Days[0].Date)
		if weekTS(first) != weekTS(d) {
			return Week{}, fmt.Errorf("no menu for the week of %s yet", d.Format(time.DateOnly))
		}
	}
	return w, nil
}

// Order orders meal number on d, replacing another meal ordered that day.
func (c *Client) Order(ctx context.Context, d time.Time, number int) (Week, error) {
	w, day, err := c.day(ctx, d)
	if err != nil {
		return Week{}, err
	}
	for _, m := range day.Meals {
		if m.Number != number {
			continue
		}
		if m.Ordered {
			return w, nil
		}
		if !m.Changeable {
			return Week{}, fmt.Errorf("%s: order deadline passed (%s)", day.Date, day.OrderDeadline)
		}
		return c.submit(ctx, d, m.form, func(m Meal) bool { return m.Number == number && m.Ordered })
	}
	return Week{}, fmt.Errorf("%s: no meal number %d", day.Date, number)
}

// Cancel cancels the meal ordered on d.
func (c *Client) Cancel(ctx context.Context, d time.Time) (Week, error) {
	w, day, err := c.day(ctx, d)
	if err != nil {
		return Week{}, err
	}
	for _, m := range day.Meals {
		if !m.Ordered {
			continue
		}
		if !m.Changeable {
			return Week{}, fmt.Errorf("%s: cancel deadline passed (%s)", day.Date, day.CancelDeadline)
		}
		return c.submit(ctx, d, m.form, func(m Meal) bool { return !m.Ordered })
	}
	return w, fmt.Errorf("%s: nothing ordered", day.Date)
}

func (c *Client) day(ctx context.Context, d time.Time) (Week, Day, error) {
	w, err := c.Week(ctx, d)
	if err != nil {
		return Week{}, Day{}, err
	}
	date := d.Format(time.DateOnly)
	for _, day := range w.Days {
		if day.Date == date {
			return w, day, nil
		}
	}
	return Week{}, Day{}, fmt.Errorf("%s: no menu that day", date)
}

// submit posts form and checks every meal of d against done on the page
// kitafino redirects to; the POST response is the only confirmation there is.
func (c *Client) submit(ctx context.Context, d time.Time, form url.Values, done func(Meal) bool) (Week, error) {
	b, err := c.post(ctx, "?action=speichere_bestellung", form)
	if err != nil {
		return Week{}, err
	}
	w, err := parseWeek(b)
	if err != nil {
		return Week{}, err
	}
	date := d.Format(time.DateOnly)
	for _, day := range w.Days {
		if day.Date == date && slices.ContainsFunc(day.Meals, done) {
			return w, nil
		}
	}
	return Week{}, fmt.Errorf("%s: kitafino did not confirm the change; check the menu", date)
}

var (
	dateRe     = regexp.MustCompile(`(\d{2})\.(\d{2})\.(\d{4})`)
	deadlineRe = regexp.MustCompile(`(\d{2})\.(\d{2})\.(\d{4}) - (\d{2}:\d{2})`)
)

func parseWeek(b []byte) (Week, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(b))
	if err != nil {
		return Week{}, err
	}
	var w Week
	if w.Balance, err = balance(doc); err != nil {
		return Week{}, err
	}
	var errs []error
	doc.Find(".order_table").Each(func(_ int, t *goquery.Selection) {
		day, err := parseDay(t)
		if err != nil {
			errs = append(errs, err)
			return
		}
		w.Days = append(w.Days, day)
	})
	if len(w.Days) == 0 && len(errs) == 0 {
		return Week{}, errors.New("no days on the order page, kitafino layout changed?")
	}
	return w, errors.Join(errs...)
}

func parseDay(t *goquery.Selection) (Day, error) {
	head := collapse(t.Find(".order_info_wrapper strong").First().Text())
	m := dateRe.FindStringSubmatch(head)
	if m == nil {
		return Day{}, fmt.Errorf("day header %q: no date", head)
	}
	date, err := time.ParseInLocation(time.DateOnly, m[3]+"-"+m[2]+"-"+m[1], berlin)
	if err != nil {
		return Day{}, err
	}
	day := Day{Date: date.Format(time.DateOnly), Weekday: date.Weekday().String()}
	t.Find(".fristen_info").Each(func(_ int, s *goquery.Selection) {
		txt := s.Text()
		d := deadlineRe.FindStringSubmatch(txt)
		if d == nil {
			return
		}
		iso := d[3] + "-" + d[2] + "-" + d[1] + " " + d[4]
		switch {
		case strings.HasPrefix(strings.TrimSpace(txt), "Bestellung"):
			day.OrderDeadline = iso
		case strings.HasPrefix(strings.TrimSpace(txt), "Storno"):
			day.CancelDeadline = iso
		}
	})
	var errs []error
	t.Find(".order_button_wrapper").Each(func(_ int, s *goquery.Selection) {
		meal, err := parseMeal(s)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", day.Date, err))
			return
		}
		day.Meals = append(day.Meals, meal)
	})
	return day, errors.Join(errs...)
}

func parseMeal(s *goquery.Selection) (Meal, error) {
	nr := s.Find(".menu_nr")
	n, err := strconv.Atoi(collapse(nr.Text()))
	if err != nil {
		return Meal{}, fmt.Errorf("menu number: %w", err)
	}
	price, err := euro(s.Find(".preis_button").Text())
	if err != nil {
		return Meal{}, err
	}
	m := Meal{
		Number:     n,
		Line:       collapse(s.Find(".preis_info_zu_men").Text()),
		Name:       ownText(nr.Parent()),
		Vegetarian: s.Find(".vegi").Length() > 0,
		Price:      price,
		Ordered:    s.Find(".order_button_bestellt").Length() > 0,
	}
	// Past the deadline the meal is a plain link instead of a form.
	if f := s.Find("form"); f.Length() > 0 {
		m.Changeable = true
		m.form = url.Values{}
		f.Find("input[type=hidden]").Each(func(_ int, in *goquery.Selection) {
			m.form.Set(in.AttrOr("name", ""), in.AttrOr("value", ""))
		})
	}
	return m, nil
}

func balance(doc *goquery.Document) (float64, error) {
	s := doc.Find(`a[title="Ihr aktuelles Guthaben"]`).First()
	if s.Length() == 0 {
		return 0, errors.New("balance missing, kitafino layout changed?")
	}
	return euro(s.Text())
}

// euro parses German amounts like "€ 1.234,50".
func euro(s string) (float64, error) {
	t := strings.NewReplacer("€", "", ".", "", ",", ".", " ", "", " ", "").Replace(strings.TrimSpace(s))
	t = strings.TrimSuffix(t, "-")
	v, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return 0, fmt.Errorf("amount %q: %w", s, err)
	}
	return v, nil
}

// ownText joins the element's direct text nodes: the dish name sits between
// the menu line and the price elements.
func ownText(s *goquery.Selection) string {
	var parts []string
	for _, n := range s.Nodes {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				parts = append(parts, c.Data)
			}
		}
	}
	return collapse(strings.Join(parts, " "))
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }
