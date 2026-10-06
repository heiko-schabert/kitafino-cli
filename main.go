// Command kitafino-cli reads and orders kitafino meals from the terminal and,
// with the mcp subcommand, serves the same tools over MCP.
package main

import (
	"context"
	"kitafino-cli/kitafino"
	"log/slog"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type none struct{}

type loginStatus struct {
	Status string `json:"status"`
}

type dateArg struct {
	Date string `json:"date,omitempty" jsonschema:"YYYY-MM-DD, default today"`
}

type orderArgs struct {
	Date string `json:"date" jsonschema:"YYYY-MM-DD"`
	Menu int    `json:"menu" jsonschema:"meal number from the menu, e.g. 1"`
}

type cancelArgs struct {
	Date string `json:"date" jsonschema:"YYYY-MM-DD"`
}

// tool hides the SDK's result plumbing.
func tool[In, Out any](s *mcp.Server, name, desc string, fn func(context.Context, In) (Out, error)) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: desc},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
			start := time.Now()
			out, err := fn(ctx, in)
			slog.InfoContext(ctx, "tool call", "tool", name, "duration", time.Since(start).Round(time.Millisecond), "err", err)
			return nil, out, err
		})
}

func newServer(c *kitafino.Client, allowWrite bool) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "kitafino", Version: "v0.1.0"}, nil)
	tool(s, "check_login", "Checks that logging in to kitafino works.",
		func(ctx context.Context, _ none) (loginStatus, error) {
			if err := c.CheckLogin(ctx); err != nil {
				return loginStatus{}, err
			}
			return loginStatus{Status: "ok"}, nil
		})
	tool(s, "get_account", "Child the login belongs to (name, customer ID) and current balance in EUR.",
		func(ctx context.Context, _ none) (kitafino.Account, error) { return c.Account(ctx) })
	tool(s, "get_menu", "Lunch menu of the week containing date: per day the meals (number, menu line, dish, price), which one is ordered, whether ordering/cancelling is still possible, and the deadlines. Also the balance.",
		func(ctx context.Context, in dateArg) (kitafino.Week, error) {
			d, err := kitafino.ParseDate(in.Date)
			if err != nil {
				return kitafino.Week{}, err
			}
			return c.Week(ctx, d)
		})
	if !allowWrite {
		return s
	}
	tool(s, "order_meal", "Orders meal number menu on date, replacing another meal ordered that day. Charges the balance; confirm with the user first.",
		func(ctx context.Context, in orderArgs) (kitafino.Week, error) {
			d, err := kitafino.ParseDate(in.Date)
			if err != nil {
				return kitafino.Week{}, err
			}
			return c.Order(ctx, d, in.Menu)
		})
	tool(s, "cancel_meal", "Cancels the meal ordered on date and refunds it. Confirm with the user first.",
		func(ctx context.Context, in cancelArgs) (kitafino.Week, error) {
			d, err := kitafino.ParseDate(in.Date)
			if err != nil {
				return kitafino.Week{}, err
			}
			return c.Cancel(ctx, d)
		})
	return s
}

func main() {
	cfg, err := kitafino.LoadConfig(os.Getenv, kitafino.DefaultEnvFile())
	ready := func() error { return err }
	os.Exit(run(context.Background(), newServer(kitafino.New(cfg), cfg.AllowWrite), ready, os.Args[1:], os.Stdout, os.Stderr))
}
