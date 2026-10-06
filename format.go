package main

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"kitafino-cli/kitafino"
)

// formatters render a tool's structured result for humans; tools without one
// print JSON. MCP clients always get JSON.
var formatters = map[string]func(io.Writer, json.RawMessage) error{
	"check_login": textOf(func(w io.Writer, s loginStatus) { fmt.Fprintln(w, "login", s.Status) }),
	"get_account": textOf(func(w io.Writer, a kitafino.Account) {
		fmt.Fprintf(w, "%s (%s)\nBalance %s\n", a.Name, a.CustomerID, eur(a.Balance))
	}),
	"get_menu":    textOf(writeWeek),
	"order_meal":  textOf(writeWeek),
	"cancel_meal": textOf(writeWeek),
}

func format(w io.Writer, v any, f func(io.Writer, json.RawMessage) error) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return f(w, b)
}

// textOf decodes the tool result back into its Go type.
func textOf[T any](f func(io.Writer, T)) func(io.Writer, json.RawMessage) error {
	return func(w io.Writer, b json.RawMessage) error {
		var v T
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		f(w, v)
		return nil
	}
}

func writeWeek(w io.Writer, week kitafino.Week) {
	fmt.Fprintf(w, "Balance %s\n", eur(week.Balance))
	name := func(m kitafino.Meal) string {
		if m.Vegetarian && !strings.Contains(strings.ToLower(m.Name), "vegetarisch") {
			return m.Name + " (vegetarisch)"
		}
		return m.Name
	}
	// One width for the whole week so prices line up across days; fmt pads by runes.
	width := 0
	for _, d := range week.Days {
		for _, m := range d.Meals {
			width = max(width, utf8.RuneCountInString(name(m)))
		}
	}
	for _, d := range week.Days {
		fmt.Fprintf(w, "\n%s %s  %s\n", d.Weekday[:3], d.Date, dayStatus(d))
		for _, m := range d.Meals {
			mark := " "
			if m.Ordered {
				mark = "✓"
			}
			fmt.Fprintf(w, "  %s %d  %-*s  %s\n", mark, m.Number, width, name(m), eur(m.Price))
		}
	}
}

func dayStatus(d kitafino.Day) string {
	if !slices.ContainsFunc(d.Meals, func(m kitafino.Meal) bool { return m.Changeable }) {
		return "closed"
	}
	if slices.ContainsFunc(d.Meals, func(m kitafino.Meal) bool { return m.Ordered }) {
		return "cancel by " + d.CancelDeadline
	}
	return "order by " + d.OrderDeadline
}

func eur(v float64) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', 2, 64), ".", ",", 1) + " €"
}
