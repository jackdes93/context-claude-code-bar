if vim.g.loaded_ctxbar then
  return
end
vim.g.loaded_ctxbar = true

vim.api.nvim_create_user_command("ClaudeUsage", function()
  require("ctxbar").open()
end, { desc = "ctxbar: bảng context/usage Claude Code" })

vim.api.nvim_create_user_command("ClaudeUsageRefresh", function()
  require("ctxbar").refresh()
end, { desc = "ctxbar: làm mới dữ liệu" })
