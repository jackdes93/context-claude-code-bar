package main

import (
	"fmt"
	"strings"
	"time"
)

// Theme cho --style powerline. Mặc định khớp theme Solarized powerline của tmux;
// ghi đè trong config.json: "theme": {"bg": "#1e1e2e", ...}.
type Theme struct {
	Bg        string `json:"bg"`         // nền status bar (bên trái khối đầu tiên)
	Fg        string `json:"fg"`         // chữ trên khối context
	Ok        string `json:"ok"`         // nền khối context < 50%
	Warn      string `json:"warn"`       // 50–80%
	Crit      string `json:"crit"`       // >= 80% / chờ duyệt
	Account   string `json:"account"`    // nền khối tài khoản (khối cuối cùng)
	AccountFg string `json:"account_fg"` // chữ khối tài khoản
	Sep       string `json:"sep"`        // separator đầy
	SepThin   string `json:"sep_thin"`   // separator mảnh giữa 2 khối cùng màu
}

var defaultTheme = Theme{
	Bg: "#002b36", Fg: "#002b36",
	Ok: "#859900", Warn: "#b58900", Crit: "#dc322f",
	Account: "#586e75", AccountFg: "#eee8d5",
	Sep: "", SepThin: "",
}

func (t Theme) merged(o Theme) Theme {
	pick := func(a, b string) string {
		if b != "" {
			return b
		}
		return a
	}
	return Theme{
		Bg: pick(t.Bg, o.Bg), Fg: pick(t.Fg, o.Fg),
		Ok: pick(t.Ok, o.Ok), Warn: pick(t.Warn, o.Warn), Crit: pick(t.Crit, o.Crit),
		Account: pick(t.Account, o.Account), AccountFg: pick(t.AccountFg, o.AccountFg),
		Sep: pick(t.Sep, o.Sep), SepThin: pick(t.SepThin, o.SepThin),
	}
}

func (t Theme) level(pct float64) string {
	switch {
	case pct >= 80:
		return t.Crit
	case pct >= 50:
		return t.Warn
	default:
		return t.Ok
	}
}

type plSeg struct{ bg, fg, text string }

// renderPowerline vẽ các khối hướng phải->trái (kiểu status-right). Khối cuối luôn có
// nền theme.Account để phần status-right phía sau nối tiếp được.
func renderPowerline(t Theme, si *SessionInfo, stats []AccountStats, now time.Time) string {
	var segs []plSeg
	if si != nil {
		text := fmt.Sprintf("%s %.0f%% %s · %s", pie(si.Pct), si.Pct, human(si.Context), money(si.Cost.Cost))
		if si.State == "busy" {
			text += " ⟳"
		}
		segs = append(segs, plSeg{t.level(si.Pct), t.Fg, text})
		if si.State == "wait" {
			segs = append(segs, plSeg{t.Crit, "#eee8d5", "⚠ chờ duyệt"})
		}
		for _, st := range stats {
			if st.Name == si.Account {
				segs = append(segs, plSeg{t.Account, t.AccountFg, accountText(st, now)})
			}
		}
	} else {
		for _, st := range stats {
			segs = append(segs, plSeg{t.Account, t.AccountFg, accountText(st, now)})
		}
	}
	if len(segs) == 0 || segs[len(segs)-1].bg != t.Account {
		segs = append(segs, plSeg{t.Account, t.AccountFg, "claude"})
	}

	var b strings.Builder
	prev := t.Bg
	for _, s := range segs {
		if s.bg == prev {
			fmt.Fprintf(&b, "#[fg=%s,bg=%s,nobold]%s", s.fg, s.bg, t.SepThin)
		} else {
			fmt.Fprintf(&b, "#[fg=%s,bg=%s,nobold,nounderscore,noitalics]%s", s.bg, prev, t.Sep)
		}
		fmt.Fprintf(&b, "#[fg=%s,bg=%s,bold] %s ", s.fg, s.bg, s.text)
		prev = s.bg
	}
	b.WriteString("#[nobold]")
	return b.String()
}

func accountText(st AccountStats, now time.Time) string {
	return st.Name + " " + limitText(st, now)
}
