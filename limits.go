package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Limits là rate_limits thật từ server mà Claude Code đưa cho lệnh statusLine
// (chỉ có với tài khoản Claude.ai subscription). Lưu theo từng config dir.
type Limits struct {
	FiveHour *LimitWindow `json:"five_hour,omitempty"`
	SevenDay *LimitWindow `json:"seven_day,omitempty"`
	TS       int64        `json:"ts"` // lúc ghi
}

type LimitWindow struct {
	Pct      float64 `json:"used_percentage"`
	ResetsAt int64   `json:"resets_at"`
}

// active: còn hiệu lực (chưa tới giờ reset).
func (w *LimitWindow) active(now time.Time) bool { return w != nil && w.ResetsAt > now.Unix() }

func limitsFile(accDir string) string {
	return filepath.Join(cacheDir(), "limits", slug(accDir)+".json")
}

func loadLimits(accDir string) *Limits {
	b, err := os.ReadFile(limitsFile(accDir))
	if err != nil {
		return nil
	}
	var l Limits
	if json.Unmarshal(b, &l) != nil {
		return nil
	}
	return &l
}

func saveLimits(accDir string, l Limits) {
	p := limitsFile(accDir)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	out, _ := json.Marshal(l)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, out, 0o644) == nil {
		_ = os.Rename(tmp, p)
	}
}

// cmdStatusline là lệnh statusLine của Claude Code: ghi rate_limits lại cho tmux/nvim
// rồi in một dòng status (ANSI) cho chính Claude Code.
func cmdStatusline() error {
	b, _ := io.ReadAll(io.LimitReader(os.Stdin, 4<<20))
	var in struct {
		Transcript string  `json:"transcript_path"`
		RateLimits *Limits `json:"rate_limits"`
	}
	_ = json.Unmarshal(b, &in)

	s := load()
	transcript := expand(in.Transcript)
	acc := accountFor(s.accs, transcript)
	if acc == nil {
		if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
			for i := range s.accs {
				if s.accs[i].Dir == expand(d) {
					acc = &s.accs[i]
				}
			}
		}
	}
	if acc != nil && in.RateLimits != nil {
		l := *in.RateLimits
		l.TS = s.now.Unix()
		saveLimits(acc.Dir, l)
	}

	var parts []string
	si := sessionStats(s.ix, s.accs, transcript, s.cfg)
	if si != nil {
		model := ansi("1", "◆") + " " + modelShort(si.Model) + " (" + human(int64(si.Window)) + ")"
		switch si.State {
		case "busy":
			model += " " + ansi("38;5;117", "⟳")
		case "wait":
			model += " " + ansi("38;5;203", "⚠")
		}
		parts = append(parts, model)
		parts = append(parts, progressBar(si.Pct, 10)+"  "+ansi(levelANSI(si.Pct), fmt.Sprintf("%.0f%%", si.Pct))+" of "+human(int64(si.Window)))
	}
	if acc != nil {
		for _, st := range computeAccounts(s.ix, s.accs, s.now) {
			if st.Name == acc.Name {
				if b := st.Block; b != nil {
					pct5h := 0.0
					if b.Pct != nil {
						pct5h = *b.Pct
					}
					parts = append(parts, ansi("38;5;250", "5hr")+" ("+dur(b.End.Sub(s.now))+")")
					parts = append(parts, progressBar(pct5h, 10)+"  "+ansi(levelANSI(pct5h), fmt.Sprintf("%.0f%%", pct5h)))
				}
				if st.WeekPct != nil {
					resetStr := "–"
					if st.WeekReset > 0 {
						resetStr = durDays(time.Duration(st.WeekReset) * time.Second)
					}
					parts = append(parts, ansi("38;5;250", "wk")+" ("+resetStr+")")
					parts = append(parts, progressBar(*st.WeekPct, 10)+"  "+ansi(levelANSI(*st.WeekPct), fmt.Sprintf("%.0f%%", *st.WeekPct)))
				}
			}
		}
	}
	if si != nil {
		parts = append(parts, ansi("38;5;250", money(si.Cost.Cost)))
	}
	fmt.Println(strings.Join(parts, "  "))
	return nil
}

func ansi(code, s string) string { return "\033[" + code + "m" + s + "\033[0m" }

func levelANSI(pct float64) string {
	switch {
	case pct >= 80:
		return "38;5;203"
	case pct >= 50:
		return "38;5;221"
	default:
		return "38;5;114"
	}
}

func modelShort(model string) string {
	m := strings.TrimPrefix(model, "claude-")
	parts := strings.Split(m, "-")
	if len(parts) < 3 {
		if len(parts) > 0 {
			return strings.ToUpper(parts[0][:1]) + parts[0][1:]
		}
		return m
	}
	family := strings.ToUpper(parts[0][:1]) + parts[0][1:]
	return family + " " + parts[1] + "." + parts[2]
}

func progressBar(pct float64, width int) string {
	filled := int(pct/100*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	var b strings.Builder
	for i := 0; i < width; i++ {
		pos := float64(i) / float64(width) * 100
		if i < filled {
			color := "38;5;76"
			if pos >= 80 {
				color = "38;5;196"
			} else if pos >= 50 {
				color = "38;5;214"
			}
			fmt.Fprintf(&b, "\033[%sm█\033[0m", color)
		} else {
			b.WriteString("\033[38;5;238m░\033[0m")
		}
	}
	return b.String()
}

func durDays(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	if days > 0 {
		return fmt.Sprintf("%dd%dh", days, hours)
	}
	return dur(d)
}

// limitText: "5h 23% ↻2h10m · 7d 41%" (server), hoặc "5h $3.75 ↻.." khi không có %.
func limitText(st AccountStats, now time.Time) string {
	var out string
	if b := st.Block; b != nil {
		val := money(b.Totals.Cost)
		if b.Pct != nil {
			val = fmt.Sprintf("%.0f%%", *b.Pct)
			if b.PctSource == "estimate" {
				val = "~" + val
			}
		}
		out = fmt.Sprintf("5h %s ↻%s", val, dur(b.End.Sub(now)))
	} else {
		out = "5h –"
	}
	if st.WeekPct != nil {
		out += fmt.Sprintf(" · 7d %.0f%%", *st.WeekPct)
	}
	return out
}
