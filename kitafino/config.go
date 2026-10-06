package kitafino

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Config holds kitafino credentials.
type Config struct {
	User, Password string
	AllowWrite     bool
}

func DefaultEnvFile() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "kitafino", "env")
}

// LoadConfig prefers env vars over the file so an MCP client config can override it.
func LoadConfig(getenv func(string) string, envFile string) (Config, error) {
	file := map[string]string{}
	b, err := os.ReadFile(envFile)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Config{}, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			file[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	get := func(k string) string {
		if v := getenv(k); v != "" {
			return v
		}
		return file[k]
	}
	c := Config{
		User:       get("KITAFINO_USERNAME"),
		Password:   get("KITAFINO_PASSWORD"),
		AllowWrite: get("KITAFINO_ALLOW_WRITE") == "1",
	}
	if c.User == "" || c.Password == "" {
		return Config{}, fmt.Errorf("missing config: KITAFINO_USERNAME, KITAFINO_PASSWORD (environment or %s)", envFile)
	}
	return c, nil
}
