#!/usr/bin/env bash
set -euo pipefail

# ctxbar installer — chạy trên máy mới để build + cài đặt ctxbar.
# Usage: ./install.sh [--dry-run]

DRY_RUN=false
[[ "${1:-}" == "--dry-run" ]] && DRY_RUN=true

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
INSTALL_DIR="${HOME}/.local/bin"
NVIM_SRC="${SCRIPT_DIR}/nvim"
NVIM_DST="${HOME}/.config/nvim/lua/plugins/ctxbar.lua"

info()  { printf '\033[38;5;114m✓\033[0m %s\n' "$1"; }
warn()  { printf '\033[38;5;214m⚠\033[0m %s\n' "$1"; }
err()   { printf '\033[38;5;203m✗\033[0m %s\n' "$1" >&2; }

# ---------- 1. Kiểm tra Go ----------
if ! command -v go &>/dev/null; then
  err "Go chưa được cài. Cài Go >= 1.22 rồi chạy lại."
  exit 1
fi
info "Go $(go version | awk '{print $3}')"

# ---------- 2. Build ----------
cd "$SCRIPT_DIR"
info "Building ctxbar..."
go build -o ctxbar .
info "Build thành công"

# ---------- 3. Cài binary ----------
mkdir -p "$INSTALL_DIR"
if $DRY_RUN; then
  warn "[dry-run] sẽ copy ctxbar → ${INSTALL_DIR}/ctxbar"
else
  cp ctxbar "${INSTALL_DIR}/ctxbar"
  chmod +x "${INSTALL_DIR}/ctxbar"
  info "Đã cài ${INSTALL_DIR}/ctxbar"
fi

# Đảm bảo INSTALL_DIR nằm trong PATH
if ! echo "$PATH" | tr ':' '\n' | grep -qx "$INSTALL_DIR"; then
  warn "${INSTALL_DIR} chưa có trong \$PATH"
  echo "  Thêm vào ~/.zshrc (hoặc ~/.bashrc):"
  echo "    export PATH=\"${INSTALL_DIR}:\$PATH\""
fi

# ---------- 4. Setup hooks + statusLine cho Claude Code ----------
if $DRY_RUN; then
  info "[dry-run] ctxbar setup --dry-run"
  ("${INSTALL_DIR}/ctxbar" setup --dry-run 2>/dev/null || "${SCRIPT_DIR}/ctxbar" setup --dry-run) | grep -v '^$' | grep -v 'Thêm vào' | grep -v 'set -g' | grep -v 'bind ' | grep -v 'rồi:' | grep -v '^\s*$' || true
else
  info "Cài hooks vào Claude Code settings..."
  ("${INSTALL_DIR}/ctxbar" setup 2>/dev/null || "${SCRIPT_DIR}/ctxbar" setup) | grep -v '^$' | grep -v 'Thêm vào' | grep -v 'set -g' | grep -v 'bind ' | grep -v 'rồi:' | grep -v '^\s*$' || true
fi

# ---------- 5. Neovim plugin (nếu có nvim) ----------
if command -v nvim &>/dev/null && [[ -d "$NVIM_SRC" ]]; then
  NVIM_PLUGIN_CONTENT="return {
  {
    dir = \"${SCRIPT_DIR}/nvim\",
    name = \"ctxbar\",
    event = \"VeryLazy\",
    opts = { cmd = \"ctxbar\" },
    keys = { { \"<leader>cu\", \"<cmd>ClaudeUsage<cr>\", desc = \"Claude usage\" } },
  },
  {
    \"nvim-lualine/lualine.nvim\",
    optional = true,
    opts = function(_, opts)
      table.insert(opts.sections.lualine_x, 1, require(\"ctxbar\").lualine)
    end,
  },
}"
  if $DRY_RUN; then
    warn "[dry-run] sẽ ghi ${NVIM_DST}"
  else
    mkdir -p "$(dirname "$NVIM_DST")"
    echo "$NVIM_PLUGIN_CONTENT" > "$NVIM_DST"
    info "Neovim plugin → ${NVIM_DST}"
  fi
else
  warn "Bỏ qua Neovim plugin (nvim không có hoặc thư mục nvim/ không tồn tại)"
fi

# ---------- 6. Hướng dẫn tmux ----------
EXE="${INSTALL_DIR}/ctxbar"
cat <<EOF

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Thêm vào ~/.tmux.conf:

  set -g status-interval 2
  set -g status-right-length 120
  set -g status-right '#(${EXE} status --pane #{pane_id}) %H:%M '
  bind C display-popup -E -w 90% -h 60% "${EXE} accounts --watch"

  rồi: tmux source-file ~/.tmux.conf
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

EOF

info "Xong! Chạy 'ctxbar accounts' để kiểm tra."
