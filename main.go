// ctxbar: theo dõi context & usage của Claude Code cho tmux / nvim.
// Mỗi tài khoản là một CLAUDE_CONFIG_DIR riêng (~/.claude, ~/.claude-work, ...).
package main

import (
	"fmt"
	"os"
)

const usageText = `ctxbar - Claude Code context/usage bar

Usage:
  ctxbar status   [--pane %ID] [--cwd DIR] [--style tmux|powerline|plain]
  ctxbar accounts [--watch]                            bảng usage theo tài khoản
  ctxbar json     [--pane %ID] [--cwd DIR]             dữ liệu JSON (cho nvim/script)
  ctxbar statusline                                    lệnh statusLine cho Claude Code (lưu rate_limits)
  ctxbar hook                                          gọi từ hooks của Claude Code (stdin JSON)
  ctxbar setup    [--dir DIR]... [--dry-run]           cài hooks vào settings.json + in snippet tmux
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}
	args := os.Args[2:]
	var err error
	switch os.Args[1] {
	case "status":
		err = cmdStatus(args)
	case "accounts":
		err = cmdAccounts(args)
	case "json":
		err = cmdJSON(args)
	case "statusline":
		err = cmdStatusline()
	case "hook":
		cmdHook()
	case "setup":
		err = cmdSetup(args)
	case "-h", "--help", "help":
		fmt.Print(usageText)
	default:
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ctxbar:", err)
		os.Exit(1)
	}
}
