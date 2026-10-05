package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// PaneState được hook ghi vào ~/.cache/ctxbar/panes/<pane>.json.
type PaneState struct {
	SessionID  string `json:"session_id"`
	Transcript string `json:"transcript_path"`
	Cwd        string `json:"cwd"`
	Event      string `json:"event"`
	State      string `json:"state"`
	TS         int64  `json:"ts"`
}

func paneFile(pane string) string {
	return filepath.Join(cacheDir(), "panes", strings.TrimPrefix(pane, "%")+".json")
}

func readPane(pane string) *PaneState {
	b, err := os.ReadFile(paneFile(pane))
	if err != nil {
		return nil
	}
	var ps PaneState
	if json.Unmarshal(b, &ps) != nil {
		return nil
	}
	return &ps
}

var eventState = map[string]string{
	"SessionStart":     "idle",
	"UserPromptSubmit": "busy",
	"Stop":             "idle",
	"Notification":     "wait",
}

// cmdHook không bao giờ được làm hỏng phiên Claude: mọi lỗi đều bị nuốt, exit 0.
func cmdHook() {
	pane := os.Getenv("TMUX_PANE")
	b, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if pane == "" {
		return
	}
	var in struct {
		SessionID  string `json:"session_id"`
		Transcript string `json:"transcript_path"`
		Cwd        string `json:"cwd"`
		Event      string `json:"hook_event_name"`
	}
	if json.Unmarshal(b, &in) != nil {
		return
	}
	pf := paneFile(pane)
	if in.Event == "SessionEnd" {
		os.Remove(pf)
	} else {
		ps := PaneState{in.SessionID, expand(in.Transcript), in.Cwd, in.Event, eventState[in.Event], time.Now().Unix()}
		if old := readPane(pane); ps.Transcript == "" && old != nil {
			ps.Transcript = old.Transcript
		}
		out, _ := json.Marshal(ps)
		_ = os.MkdirAll(filepath.Dir(pf), 0o755)
		tmp := pf + ".tmp"
		if os.WriteFile(tmp, out, 0o644) == nil {
			_ = os.Rename(tmp, pf)
		}
	}
	// Cập nhật status bar ngay thay vì chờ status-interval.
	_ = exec.Command("tmux", "refresh-client", "-S").Start()
}

var hookEvents = []string{"SessionStart", "UserPromptSubmit", "Stop", "Notification", "SessionEnd"}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// cmdSetup thêm hook `ctxbar hook` vào settings.json của từng config dir (giữ nguyên hook cũ).
func cmdSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	var dirs multiFlag
	fs.Var(&dirs, "dir", "config dir (lặp lại được); mặc định: mọi tài khoản dò được")
	dry := fs.Bool("dry-run", false, "chỉ in, không ghi")
	fs.Parse(args)

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	cmd := exe + " hook"

	if len(dirs) == 0 {
		for _, a := range loadAccounts(loadConfig()) {
			dirs = append(dirs, a.Dir)
		}
	}
	for _, d := range dirs {
		d = expand(d)
		p := filepath.Join(d, "settings.json")
		added, err := installHooks(p, cmd, *dry)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		verb := "đã cập nhật"
		if *dry {
			verb = "sẽ cập nhật"
		}
		if added == 0 {
			fmt.Printf("✓ %s: hooks đã có sẵn\n", p)
		} else {
			fmt.Printf("✓ %s: %s %d mục hooks/statusLine (backup: settings.json.ctxbar.bak)\n", p, verb, added)
		}
	}
	fmt.Printf(`
Thêm vào ~/.tmux.conf:

  set -g status-interval 2
  set -g status-right-length 120
  set -g status-right '#(%[1]s status --pane #{pane_id}) %%H:%%M '
  bind C display-popup -E -w 90%% -h 60%% "%[1]s accounts --watch"

rồi: tmux source-file ~/.tmux.conf
`, exe)
	return nil
}

func installHooks(path, cmd string, dry bool) (int, error) {
	raw, err := os.ReadFile(path)
	settings := map[string]any{}
	if err == nil {
		if err := json.Unmarshal(raw, &settings); err != nil {
			return 0, fmt.Errorf("settings.json không hợp lệ: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	added := 0
	for _, ev := range hookEvents {
		groups, _ := hooks[ev].([]any)
		if hasCtxbar(groups) {
			continue
		}
		groups = append(groups, map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": cmd, "timeout": 5}},
		})
		hooks[ev] = groups
		added++
	}
	// statusLine nhận rate_limits thật (% 5h / 7 ngày); không đè statusLine có sẵn.
	if _, ok := settings["statusLine"]; !ok {
		settings["statusLine"] = map[string]any{
			"type": "command", "command": strings.TrimSuffix(cmd, " hook") + " statusline", "padding": 0,
		}
		added++
	}
	if added == 0 || dry {
		return added, nil
	}
	settings["hooks"] = hooks
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return 0, err
	}
	if raw != nil {
		if err := os.WriteFile(path+".ctxbar.bak", raw, 0o644); err != nil {
			return 0, err
		}
	}
	return added, os.WriteFile(path, append(out, '\n'), 0o644)
}

func hasCtxbar(groups []any) bool {
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		hs, _ := gm["hooks"].([]any)
		for _, h := range hs {
			hm, _ := h.(map[string]any)
			if c, _ := hm["command"].(string); strings.Contains(c, "ctxbar") && strings.HasSuffix(c, " hook") {
				return true
			}
		}
	}
	return false
}
