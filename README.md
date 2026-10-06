# kitafino-cli

Command line for [kitafino](https://www.kitafino.de), the lunch ordering service used by many German schools and daycares: weekly menu, orders, deadlines and balance, as text or, with `--json`, as JSON. `kitafino-cli mcp` exposes the same commands as an MCP server for AI assistants.

kitafino-cli has no public API; this scrapes the parent web app (`user.kitafino.de`). Dish names stay German.

## Install

```bash
go install .
nix profile install .   # or: nix build, nix run . -- menu
```

`devenv shell` (or direnv) provides Go and goimports. After changing Go dependencies, run `nix run .#vendor-hash` to update `go.mod.sri`, the flake's vendor hash.

## Configuration

Environment variables take precedence over `~/.config/kitafino/env`.

| Key | Required | Meaning |
|---|---|---|
| `KITAFINO_USERNAME` | yes | login name |
| `KITAFINO_PASSWORD` | yes | password |
| `KITAFINO_ALLOW_WRITE` | no | `1` enables `order-meal` and `cancel-meal` |
| `KITAFINO_LOG_LEVEL` | no | `debug`, `info`, `warn` (default) or `error`; same as `--log-level` |

kitafino-cli has one login per child. Logs go to stderr, so stdout stays clean text, JSON or MCP protocol. `debug` logs every request with URL, status, size and duration; `info` adds logins, tool calls and submitted orders. Passwords and cookies are never logged.

## Usage

```bash
kitafino-cli                                   # command overview
kitafino-cli account                           # name, customer ID, balance
kitafino-cli menu                              # this week
kitafino-cli menu --date 2026-10-12 --json | jq '.days[] | {date, meals: [.meals[] | {number, name, ordered}]}'
KITAFINO_ALLOW_WRITE=1 kitafino-cli order-meal --date 2026-10-12 --menu 2
KITAFINO_ALLOW_WRITE=1 kitafino-cli cancel-meal --date 2026-10-12
kitafino-cli completion fish | source
```

Output is text by default; `--json` prints the full result, the same structure MCP clients get. In the menu, `✓` marks the ordered meal and each day shows `closed`, `order by …` or `cancel by …`.

`menu` shows the week containing `--date` (default today). kitafino publishes a few weeks ahead; later weeks fail with "no menu for the week of … yet". Per meal, `changeable` says whether the deadline still allows ordering or cancelling; `order_deadline` and `cancel_deadline` give the times.

`order-meal` replaces a meal already ordered that day and is a no-op if that meal is already ordered. Both write commands re-read the page kitafino returns and fail unless it shows the change. Errors go to stderr with exit code 1.

## MCP

```bash
claude mcp add -s user kitafino -- ~/go/bin/kitafino-cli mcp
```

MCP tool names: `check_login`, `get_account`, `get_menu`; with write access also `order_meal`, `cancel_meal`.

### Remote (HTTP)

```bash
kitafino-cli mcp --http 100.64.0.1:8080
```

Streamable HTTP without authentication of its own: bind to a private interface only (e.g. the Tailscale IP), never publicly. Client: `claude mcp add -s user -t http kitafino http://<host>:8080/`.

### NixOS

```nix
inputs.kitafino.url = "github:heiko-schabert/kitafino-cli";
# in a module, with inputs.kitafino.nixosModules.default imported:
services.kitafino-cli = {
  enable = true;
  listen = "100.64.0.1:8080";
  environmentFile = "/run/secrets/kitafino"; # KITAFINO_USERNAME=…, KITAFINO_PASSWORD=…
  allowWrite = true; # order/cancel tools
};
```
