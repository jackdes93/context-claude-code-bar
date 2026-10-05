package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

type snapshot struct {
	cfg  Config
	accs []Account
	ix   *Index
	now  time.Time
}

func load() *snapshot {
	s := &snapshot{cfg: loadConfig(), now: time.Now()}
	s.accs = loadAccounts(s.cfg)
	s.ix = loadIndex()
	if s.ix.refresh(s.accs, s.now) {
		_ = s.ix.save()
	}
	return s
}

// Tiến trình Claude trong tmux hiện là "claude", "node" hoặc số phiên bản (bản native).
var claudeCmd = regexp.MustCompile(`^(claude|node|\d+\.\d+\.\d+)$`)

// resolveSession tìm transcript cho pane/cwd. Trả "" nếu pane không chạy Claude.
func (s *snapshot) resolveSession(pane, cwd string) (path, state string) {
	if pane != "" {
		out, err := exec.Command("tmux", "display-message", "-p", "-t", pane,
			"#{pane_current_command}\t#{pane_current_path}").Output()
		if err == nil {
			parts := strings.SplitN(strings.TrimSpace(string(out)), "\t", 2)
			if !claudeCmd.MatchString(parts[0]) {
				return "", ""
			}
			if len(parts) == 2 && cwd == "" {
				cwd = parts[1]
			}
		}
		if ps := readPane(pane); ps != nil && ps.Transcript != "" {
			return ps.Transcript, ps.State
		}
	}
	if cwd == "" {
		return "", ""
	}
	return latestForCwd(s.accs, expand(cwd)), ""
}

func (s *snapshot) session(pane, cwd string) *SessionInfo {
	path, state := s.resolveSession(pane, cwd)
	if path == "" {
		return nil
	}
	si := sessionStats(s.ix, s.accs, path, s.cfg)
	if si != nil {
		si.State = state
	}
	return si
}

// ---------- status (tmux) ----------

type painter struct{ tmux bool }

func (p painter) c(color, text string) string {
	if !p.tmux {
		return text
	}
	return "#[fg=" + color + "]" + text + "#[fg=default]"
}

func levelColor(pct float64) string {
	switch {
	case pct >= 80:
		return "colour203"
	case pct >= 50:
		return "colour221"
	default:
		return "colour114"
	}
}

func pie(pct float64) string {
	switch {
	case pct >= 87.5:
		return "●"
	case pct >= 62.5:
		return "◕"
	case pct >= 37.5:
		return "◑"
	case pct >= 12.5:
		return "◔"
	default:
		return "○"
	}
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	pane := fs.String("pane", "", "tmux pane id (#{pane_id})")
	cwd := fs.String("cwd", "", "thư mục làm việc (nếu không có hook)")
	plain := fs.Bool("plain", false, "không dùng mã màu tmux (= --style plain)")
	style := fs.String("style", "tmux", "tmux | powerline | plain")
	fs.Parse(args)
	if *plain {
		*style = "plain"
	}

	s := load()
	p := painter{tmux: *style != "plain"}
	si := s.session(*pane, *cwd)
	stats := computeAccounts(s.ix, s.accs, s.now)
	if *style == "powerline" {
		fmt.Println(renderPowerline(defaultTheme.merged(s.cfg.Theme), si, stats, s.now))
		return nil
	}

	var parts []string
	if si != nil {
		ctx := fmt.Sprintf("%s %.0f%% %s/%s", pie(si.Pct), si.Pct, human(si.Context), human(int64(si.Window)))
		seg := p.c(levelColor(si.Pct), ctx) + " " + p.c("colour250", money(si.Cost.Cost))
		switch si.State {
		case "busy":
			seg += " " + p.c("colour117", "⟳")
		case "wait":
			seg += " " + p.c("colour203", "⚠ chờ duyệt")
		}
		parts = append(parts, seg)
		for _, st := range stats {
			if st.Name == si.Account {
				parts = append(parts, accountSeg(p, st, s.now))
			}
		}
	} else {
		var segs []string
		for _, st := range stats {
			segs = append(segs, accountSeg(p, st, s.now))
		}
		parts = append(parts, strings.Join(segs, " · "))
	}
	fmt.Println(strings.Join(parts, p.c("colour240", " │ ")))
	return nil
}

func accountSeg(p painter, st AccountStats, now time.Time) string {
	name := p.c("colour146", st.Name)
	if st.Block == nil {
		return name + p.c("colour244", " 5h –")
	}
	color := "colour250"
	if st.Block.Pct != nil {
		color = levelColor(*st.Block.Pct)
	}
	return name + " " + p.c(color, limitText(st, now))
}

// ---------- accounts ----------

func cmdAccounts(args []string) error {
	fs := flag.NewFlagSet("accounts", flag.ExitOnError)
	watch := fs.Bool("watch", false, "tự làm mới mỗi 2 giây")
	fs.Parse(args)
	if !*watch {
		printAccounts(load())
		return nil
	}
	for {
		s := load()
		fmt.Print("\033[H\033[2J")
		printAccounts(s)
		fmt.Printf("\n%s · Ctrl-C để thoát\n", s.now.Format("15:04:05"))
		time.Sleep(2 * time.Second)
	}
}

func printAccounts(s *snapshot) {
	stats := computeAccounts(s.ix, s.accs, s.now)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ACCOUNT\tEMAIL\t5H COST\t5H TOKENS\tRESET IN\tTODAY\t7 DAYS")
	for _, st := range stats {
		blkCost, blkTok, reset := "–", "–", "–"
		if b := st.Block; b != nil {
			blkCost, blkTok, reset = money(b.Totals.Cost), human(b.Totals.Tokens), dur(b.End.Sub(s.now))
			if b.Pct != nil {
				blkCost += fmt.Sprintf(" (%.0f%%)", *b.Pct)
			}
		}
		week := money(st.Week.Cost)
		if st.WeekPct != nil {
			week += fmt.Sprintf(" (%.0f%%)", *st.WeekPct)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			st.Name, orDash(st.Email), blkCost, blkTok, reset, money(st.Today.Cost), week)
	}
	w.Flush()

	recent := recentSessions(s, time.Hour)
	if len(recent) == 0 {
		return
	}
	fmt.Println("\nPHIÊN HOẠT ĐỘNG (1h qua)")
	w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ACCOUNT\tPROJECT\tMODEL\tCONTEXT\tCOST\tLAST")
	for _, si := range recent {
		fmt.Fprintf(w, "%s\t%s\t%s\t%.0f%% %s\t%s\t%s trước\n", si.Account, projectName(si.Path),
			strings.TrimPrefix(si.Model, "claude-"), si.Pct, human(si.Context),
			money(si.Cost.Cost), dur(s.now.Sub(si.Updated)))
	}
	w.Flush()
}

func recentSessions(s *snapshot, within time.Duration) []*SessionInfo {
	var out []*SessionInfo
	cut := s.now.Add(-within).UnixNano()
	for p, fsx := range s.ix.Files {
		// chỉ transcript chính: projects/<slug>/<id>.jsonl
		if fsx.MTime < cut || filepath.Base(filepath.Dir(filepath.Dir(p))) != "projects" {
			continue
		}
		if si := sessionStats(s.ix, s.accs, p, s.cfg); si != nil {
			out = append(out, si)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

// projectName rút gọn slug "-Users-me-code-foo" thành "foo" (slug mất dấu '/', nên chỉ là đoán).
func projectName(path string) string {
	parts := strings.Split(filepath.Base(filepath.Dir(path)), "-")
	return strings.Join(parts[max(0, len(parts)-2):], "-")
}

// ---------- json ----------

func cmdJSON(args []string) error {
	fs := flag.NewFlagSet("json", flag.ExitOnError)
	pane := fs.String("pane", "", "tmux pane id")
	cwd := fs.String("cwd", "", "thư mục làm việc (mặc định: thư mục hiện tại)")
	fs.Parse(args)
	if *pane == "" && *cwd == "" {
		*cwd, _ = os.Getwd()
	}
	s := load()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{
		"generated": s.now,
		"session":   s.session(*pane, *cwd),
		"accounts":  computeAccounts(s.ix, s.accs, s.now),
		"recent":    recentSessions(s, time.Hour),
	})
}

// ---------- format ----------

func human(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	default:
		return fmt.Sprint(n)
	}
}

func money(v float64) string {
	if v >= 100 {
		return fmt.Sprintf("$%.0f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

func dur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	h, m := int(d.Hours()), int(d.Minutes())%60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func orDash(s string) string {
	if s == "" {
		return "–"
	}
	return s
}
