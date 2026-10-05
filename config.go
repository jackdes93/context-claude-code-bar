package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// AccountCfg là một mục trong ~/.config/ctxbar/config.json.
type AccountCfg struct {
	Name          string  `json:"name"`
	Dir           string  `json:"dir"`
	Limit5hCost   float64 `json:"limit_5h_cost"`   // ngưỡng ước lượng (USD) cho cửa sổ 5h, 0 = không hiện %
	LimitWeekCost float64 `json:"limit_week_cost"` // ngưỡng ước lượng (USD) cho 7 ngày
}

type Config struct {
	Accounts      []AccountCfg `json:"accounts"`
	ContextWindow int          `json:"context_window"` // ép cửa sổ context, 0 = theo model
	Theme         Theme        `json:"theme"`          // màu cho --style powerline
}

type Account struct {
	Name          string  `json:"name"`
	Dir           string  `json:"dir"`
	Email         string  `json:"email,omitempty"`
	DisplayName   string  `json:"display_name,omitempty"`
	Org           string  `json:"org,omitempty"`
	Tier          string  `json:"tier,omitempty"`
	Limit5hCost   float64 `json:"limit_5h_cost,omitempty"`
	LimitWeekCost float64 `json:"limit_week_cost,omitempty"`
}

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

func expand(p string) string {
	if p == "~" {
		return home()
	}
	if strings.HasPrefix(p, "~/") {
		p = filepath.Join(home(), p[2:])
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.Clean(p)
}

func cacheDir() string {
	if x := os.Getenv("XDG_CACHE_HOME"); x != "" {
		return filepath.Join(x, "ctxbar")
	}
	return filepath.Join(home(), ".cache", "ctxbar")
}

func loadConfig() Config {
	var c Config
	p := filepath.Join(home(), ".config", "ctxbar", "config.json")
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

// loadAccounts trả về danh sách tài khoản: từ config nếu có, nếu không thì tự dò
// ~/.claude, ~/.claude-* (có thư mục projects/) và $CLAUDE_CONFIG_DIR.
func loadAccounts(cfg Config) []Account {
	var cfgs []AccountCfg
	if len(cfg.Accounts) > 0 {
		cfgs = cfg.Accounts
	} else {
		seen := map[string]bool{}
		add := func(dir string) {
			dir = expand(dir)
			if seen[dir] {
				return
			}
			if st, err := os.Stat(filepath.Join(dir, "projects")); err != nil || !st.IsDir() {
				return
			}
			seen[dir] = true
			cfgs = append(cfgs, AccountCfg{Dir: dir})
		}
		add("~/.claude")
		matches, _ := filepath.Glob(filepath.Join(home(), ".claude-*"))
		sort.Strings(matches)
		for _, m := range matches {
			add(m)
		}
		if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
			add(d)
		}
	}

	accs := make([]Account, 0, len(cfgs))
	for _, c := range cfgs {
		a := Account{Name: c.Name, Dir: expand(c.Dir), Limit5hCost: c.Limit5hCost, LimitWeekCost: c.LimitWeekCost}
		readOAuth(&a)
		accs = append(accs, a)
	}
	nameAccounts(accs)
	return accs
}

// nameAccounts đặt tên cho tài khoản chưa có name trong config:
// displayName -> phần trước @ của email -> tên thư mục; trùng thì lùi xuống mức sau.
func nameAccounts(accs []Account) {
	local := func(a Account) string {
		if i := strings.IndexByte(a.Email, '@'); i > 0 {
			return a.Email[:i]
		}
		return ""
	}
	levels := []func(Account) string{
		func(a Account) string { return a.DisplayName },
		local,
		func(a Account) string { return defaultName(a.Dir) },
	}
	for _, f := range levels {
		count := map[string]int{}
		for _, a := range accs {
			if a.Name != "" {
				count[a.Name] += 2 // tên đã cố định: không ai được dùng lại
			} else if n := f(a); n != "" {
				count[n]++
			}
		}
		for i := range accs {
			if n := f(accs[i]); accs[i].Name == "" && n != "" && count[n] == 1 {
				accs[i].Name = n
			}
		}
	}
	for i := range accs {
		if accs[i].Name == "" {
			accs[i].Name = defaultName(accs[i].Dir)
		}
	}
}

func defaultName(dir string) string {
	base := filepath.Base(dir)
	if base == ".claude" {
		return "default"
	}
	return strings.TrimPrefix(strings.TrimPrefix(base, ".claude-"), ".")
}

// readOAuth đọc oauthAccount từ <dir>/.claude.json (hoặc ~/.claude.json cho ~/.claude).
func readOAuth(a *Account) {
	cands := []string{filepath.Join(a.Dir, ".claude.json")}
	if a.Dir == expand("~/.claude") {
		cands = append(cands, filepath.Join(home(), ".claude.json"))
	}
	for _, p := range cands {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var v struct {
			OAuth struct {
				Email string `json:"emailAddress"`
				Name  string `json:"displayName"`
				Org   string `json:"organizationName"`
				Tier  string `json:"userRateLimitTier"`
			} `json:"oauthAccount"`
		}
		if json.Unmarshal(b, &v) == nil && v.OAuth.Email != "" {
			a.Email, a.Org, a.Tier, a.DisplayName = v.OAuth.Email, v.OAuth.Org, v.OAuth.Tier, v.OAuth.Name
			return
		}
	}
}

// accountFor tìm tài khoản chứa đường dẫn transcript.
func accountFor(accs []Account, path string) *Account {
	for i := range accs {
		if strings.HasPrefix(path, accs[i].Dir+string(os.PathSeparator)) {
			return &accs[i]
		}
	}
	return nil
}
