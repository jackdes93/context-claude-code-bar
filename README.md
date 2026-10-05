# ctxbar

Theo dõi **context** và **usage** của Claude Code ngay trên tmux status bar / nvim,
tách riêng theo từng tài khoản (mỗi tài khoản một `CLAUDE_CONFIG_DIR`).

```
◑ 48% 96k/200k $1.23 ⟳ │ work 5h $4.10 ↻1h20m
```

Không gọi API nào: mọi số liệu đọc từ transcript `<config-dir>/projects/**/*.jsonl`
(trường `message.usage`), đọc tăng dần và cache ở `~/.cache/ctxbar/index.gob`.

## Cài đặt

```bash
go build -o ctxbar . && mv ctxbar ~/.local/bin/   # hoặc thư mục nào có trong PATH

# Mỗi tài khoản một config dir
alias claude-work='CLAUDE_CONFIG_DIR=~/.claude-work claude'
alias claude-me='claude'                          # ~/.claude

ctxbar setup            # thêm hooks vào settings.json của mọi tài khoản dò được (có backup)
```

`setup` in snippet tmux, dán vào `~/.tmux.conf`:

```tmux
set -g status-interval 2
set -g status-right-length 120
set -g status-right '#(ctxbar status --pane #{pane_id}) %H:%M '
bind C display-popup -E -w 90% -h 60% "ctxbar accounts --watch"
```

## Lệnh

| Lệnh | Mô tả |
|---|---|
| `ctxbar status --pane %ID` | một dòng cho tmux: context của phiên trong pane + cửa sổ 5h của tài khoản đó. Pane không chạy Claude → tóm tắt mọi tài khoản |
| `ctxbar status --cwd DIR --plain` | không cần hook: lấy transcript mới nhất của thư mục đó |
| `ctxbar accounts [--watch]` | bảng theo tài khoản (5h, hôm nay, 7 ngày) + các phiên hoạt động trong 1h |
| `ctxbar json [--cwd DIR]` | toàn bộ dữ liệu dạng JSON cho nvim/script |
| `ctxbar hook` | được Claude Code gọi (SessionStart/UserPromptSubmit/Stop/Notification/SessionEnd) để map `$TMUX_PANE` → phiên và trạng thái `⟳` đang chạy / `⚠` chờ duyệt |

## Tài khoản & ngưỡng

Mặc định tự dò `~/.claude`, `~/.claude-*` (có `projects/`) và `$CLAUDE_CONFIG_DIR`;
tên lấy từ `oauthAccount` trong `.claude.json`: `displayName` → phần trước `@` của email →
tên thư mục (trùng tên thì tự lùi xuống mức sau).
Muốn đặt tên/ngưỡng thì tạo `~/.config/ctxbar/config.json`:

```json
{
  "accounts": [
    { "name": "me",   "dir": "~/.claude",      "limit_5h_cost": 15 },
    { "name": "work", "dir": "~/.claude-work", "limit_5h_cost": 60, "limit_week_cost": 600 }
  ],
  "context_window": 0
}
```

### % 5h / 7 ngày

`ctxbar setup` khai báo `statusLine` = `ctxbar statusline` trong settings.json (chỉ khi chưa có).
Claude Code truyền `rate_limits.five_hour` / `seven_day` (**% thật từ server**, chỉ có với gói
Claude.ai) vào lệnh này sau mỗi lượt; ctxbar lưu vào `~/.cache/ctxbar/limits/` theo tài khoản,
tmux/nvim đọc lại: `BINHDP 5h 48% ↻29m · 7d 64%`. Số chỉ cập nhật khi có phiên Claude đang chạy
trên tài khoản đó, và tự bỏ khi qua `resets_at`.

Không có số từ server (dùng API key, hoặc chưa chạy phiên nào) thì hiện `$` tương đương API.
Nếu đặt `limit_5h_cost` / `limit_week_cost` thì hiện `~%` ước lượng theo ngưỡng đó.

## Neovim

Plugin nằm trong `nvim/` (cần nvim ≥ 0.10). Với LazyVim, tạo `~/.config/nvim/lua/plugins/ctxbar.lua`:

```lua
return {
  {
    dir = "~/Documents/RDP/SECRECT_TOOL/context-bar/nvim",
    name = "ctxbar",
    event = "VeryLazy",
    opts = { cmd = "ctxbar" }, -- hoặc đường dẫn tuyệt đối tới binary
    keys = { { "<leader>cu", "<cmd>ClaudeUsage<cr>", desc = "Claude usage" } },
  },
  {
    "nvim-lualine/lualine.nvim",
    opts = function(_, opts)
      table.insert(opts.sections.lualine_x, 1, require("ctxbar").lualine)
    end,
  },
}
```

- **Statusline**: `◔ 13% 130k $2.21 ⟳ │ default 5h $2.30 ↻4h42m`, màu theo mức context
  (`DiagnosticOk/Warn/Error`). Lấy phiên mới nhất của `getcwd()`; phiên không hoạt động quá
  `session_max_age` (30 phút) thì chỉ hiện tài khoản. Không dùng lualine thì gọi `require("ctxbar").status()`.
- **`:ClaudeUsage`** (bật/tắt): cửa sổ nổi có thanh context, chi phí phiên, cửa sổ 5h,
  hôm nay / 7 ngày kèm sparkline theo ngày, và các phiên hoạt động trong 1h. `r` làm mới, `q` đóng;
  cửa sổ tự làm mới mỗi `interval` ms.
- Tuỳ chọn: `cmd`, `interval` (3000), `session_max_age` (1800), `width` (78).

## Ghi chú

- Context = `input + cache_creation + cache_read` của response chính gần nhất; cửa sổ
  lấy theo model (1M cho Opus/Sonnet 4.6+, 200k cho model cũ/Haiku), ép bằng `context_window`.
- Chi phí tính theo bảng giá API (`pricing.go`), gồm cache write 5m/1h, cache read, fast mode;
  với gói Pro/Max đây là "giá trị tương đương API", không phải số tiền bị trừ.
- Block 5h: bắt đầu ở giờ tròn của message đầu tiên, giống cách ccusage tính.
- Chỉ giữ dữ liệu 8 ngày gần nhất trong cache.
