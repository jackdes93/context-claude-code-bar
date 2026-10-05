package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Totals struct {
	Cost   float64 `json:"cost"`
	Tokens int64   `json:"tokens"`
	Msgs   int     `json:"msgs"`
}

func (t *Totals) add(e Entry) {
	t.Cost += e.Cost()
	t.Tokens += e.Tokens()
	t.Msgs++
}

// Block là cửa sổ 5h kiểu Claude: bắt đầu ở giờ tròn của message đầu tiên,
// kéo dài 5h; message sau khi hết hạn mở block mới.
type Block struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Totals Totals    `json:"totals"`
	// Pct: % đã dùng; "server" = rate_limits thật từ Claude Code, "estimate" = so với limit_5h_cost.
	Pct       *float64 `json:"pct,omitempty"`
	PctSource string   `json:"pct_source,omitempty"`
	Reset     int64    `json:"reset_sec"`
}

type AccountStats struct {
	Account
	Block     *Block    `json:"block,omitempty"` // nil = không có block đang chạy
	Today     Totals    `json:"today"`
	Week      Totals    `json:"week"`
	WeekPct   *float64  `json:"week_pct,omitempty"`
	WeekReset int64     `json:"week_reset_sec,omitempty"`
	Daily     []float64 `json:"daily"` // chi phí 7 ngày, cũ -> hôm nay
}

type SessionInfo struct {
	Path    string    `json:"path"`
	ID      string    `json:"id"`
	Account string    `json:"account"`
	Model   string    `json:"model"`
	Context int64     `json:"context"`
	Window  int       `json:"window"`
	Pct     float64   `json:"pct"`
	Cost    Totals    `json:"cost"`
	Project string    `json:"project"`
	Updated time.Time `json:"updated"`
	AgeSec  int64     `json:"age_sec"`
	State   string    `json:"state,omitempty"` // busy | idle | wait (từ hook)
}

// dedupe: Claude Code ghi mỗi content block thành 1 dòng với cùng message id + usage.
func dedupe(entries []Entry) []Entry {
	idx := map[string]int{}
	out := entries[:0:0]
	for _, e := range entries {
		if i, ok := idx[e.ID]; ok {
			if e.Out > out[i].Out {
				out[i] = e
			}
			continue
		}
		idx[e.ID] = len(out)
		out = append(out, e)
	}
	return out
}

func computeAccounts(ix *Index, accs []Account, now time.Time) []AccountStats {
	byAcc := make([][]Entry, len(accs))
	for p, fsx := range ix.Files {
		for i := range accs {
			if strings.HasPrefix(p, accs[i].Dir+string(os.PathSeparator)) {
				byAcc[i] = append(byAcc[i], fsx.Entries...)
				break
			}
		}
	}
	y, m, d := now.Date()
	dayStart := time.Date(y, m, d, 0, 0, 0, 0, now.Location()).Unix()
	weekStart := now.Add(-7 * 24 * time.Hour).Unix()

	out := make([]AccountStats, len(accs))
	for i, a := range accs {
		es := dedupe(byAcc[i])
		sort.Slice(es, func(x, y int) bool { return es[x].T < es[y].T })
		st := AccountStats{Account: a, Daily: make([]float64, 7)}
		for _, e := range es {
			if day := (dayStart - e.T + 86399) / 86400; day >= 0 && day < 7 {
				st.Daily[6-day] += e.Cost()
			}
			if e.T >= weekStart {
				st.Week.add(e)
			}
			if e.T >= dayStart {
				st.Today.add(e)
			}
		}
		st.Block = activeBlock(es, now)
		if st.Block != nil && a.Limit5hCost > 0 {
			st.Block.Pct, st.Block.PctSource = ptr(st.Block.Totals.Cost/a.Limit5hCost*100), "estimate"
		}
		if a.LimitWeekCost > 0 {
			st.WeekPct = ptr(st.Week.Cost / a.LimitWeekCost * 100)
		}
		// Số thật từ server (qua statusLine) luôn thắng ước lượng.
		if l := loadLimits(a.Dir); l != nil {
			if w := l.FiveHour; w.active(now) {
				end := time.Unix(w.ResetsAt, 0)
				if st.Block == nil {
					st.Block = &Block{Start: end.Add(-5 * time.Hour)}
				}
				st.Block.End, st.Block.Pct, st.Block.PctSource = end, ptr(w.Pct), "server"
			}
			if w := l.SevenDay; w.active(now) {
				st.WeekPct, st.WeekReset = ptr(w.Pct), w.ResetsAt-now.Unix()
			}
		}
		if st.Block != nil {
			st.Block.Reset = int64(st.Block.End.Sub(now).Seconds())
		}
		out[i] = st
	}
	return out
}

func activeBlock(sorted []Entry, now time.Time) *Block {
	var cur *Block
	for _, e := range sorted {
		t := time.Unix(e.T, 0)
		if cur == nil || !t.Before(cur.End) {
			start := t.Truncate(time.Hour)
			cur = &Block{Start: start, End: start.Add(5 * time.Hour)}
		}
		cur.Totals.add(e)
	}
	if cur == nil || !now.Before(cur.End) {
		return nil
	}
	return cur
}

// sessionStats tính context + chi phí của một transcript (kèm subagents/ của nó).
func sessionStats(ix *Index, accs []Account, path string, cfg Config) *SessionInfo {
	fsx := ix.Files[path]
	if fsx == nil || fsx.Last == nil {
		return nil
	}
	s := &SessionInfo{
		Path: path,
		ID:   strings.TrimSuffix(filepath.Base(path), ".jsonl"),
	}
	if a := accountFor(accs, path); a != nil {
		s.Account = a.Name
	}
	last := fsx.Last
	s.Model = last.Model
	s.Context = last.ContextTokens()
	s.Window = contextWindow(last.Model, s.Context, cfg.ContextWindow)
	s.Pct = float64(s.Context) / float64(s.Window) * 100
	s.Project = projectName(path)
	s.Updated = time.Unix(0, fsx.MTime)
	s.AgeSec = int64(time.Since(s.Updated).Seconds())

	es := append([]Entry{}, fsx.Entries...)
	subPrefix := strings.TrimSuffix(path, ".jsonl") + string(os.PathSeparator)
	for p, f := range ix.Files {
		if strings.HasPrefix(p, subPrefix) {
			es = append(es, f.Entries...)
		}
	}
	for _, e := range dedupe(es) {
		s.Cost.add(e)
	}
	return s
}

// slug giống cách Claude Code đặt tên thư mục trong projects/.
func slug(dir string) string {
	b := []byte(dir)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			b[i] = '-'
		}
	}
	return string(b)
}

// latestForCwd: transcript mới nhất của thư mục này trên mọi tài khoản.
func latestForCwd(accs []Account, cwd string) string {
	var best string
	var bestT time.Time
	for _, a := range accs {
		matches, _ := filepath.Glob(filepath.Join(a.Dir, "projects", slug(cwd), "*.jsonl"))
		for _, m := range matches {
			if info, err := os.Stat(m); err == nil && info.ModTime().After(bestT) {
				best, bestT = m, info.ModTime()
			}
		}
	}
	return best
}

func ptr[T any](v T) *T { return &v }
