-- ctxbar.nvim: hiển thị context/usage Claude Code từ binary `ctxbar json`.
local M = {}

M.config = {
  cmd = "ctxbar", -- đường dẫn binary
  interval = 3000, -- ms giữa 2 lần làm mới
  session_max_age = 1800, -- giây; phiên cũ hơn thì statusline chỉ hiện tài khoản
  width = 78,
}

M.data = nil
local busy, timer, warned = false, nil, false
local ns = vim.api.nvim_create_namespace("ctxbar")

-- ---------- data ----------

function M.refresh(cb)
  if busy then
    return
  end
  if vim.fn.executable(M.config.cmd) == 0 then
    if not warned then
      warned = true
      vim.notify("ctxbar: không tìm thấy binary '" .. M.config.cmd .. "'", vim.log.levels.WARN)
    end
    return
  end
  busy = true
  vim.system({ M.config.cmd, "json", "--cwd", vim.fn.getcwd() }, { text = true }, function(r)
    vim.schedule(function()
      busy = false
      if r.code ~= 0 then
        return
      end
      local ok, d = pcall(vim.json.decode, r.stdout, { luanil = { object = true, array = true } })
      if not ok then
        return
      end
      M.data = d
      vim.cmd.redrawstatus()
      if cb then
        cb(d)
      end
    end)
  end)
end

-- ---------- format ----------

local function human(n)
  n = n or 0
  if n >= 1e6 then
    return string.format("%.1fM", n / 1e6)
  elseif n >= 1e3 then
    return string.format("%.0fk", n / 1e3)
  end
  return tostring(n)
end

local function money(v)
  v = v or 0
  return v >= 100 and string.format("$%.0f", v) or string.format("$%.2f", v)
end

local function dur(sec)
  sec = math.max(0, sec or 0)
  local h, m = math.floor(sec / 3600), math.floor(sec % 3600 / 60)
  return h > 0 and string.format("%dh%02dm", h, m) or string.format("%dm", m)
end

local function level(pct)
  pct = pct or 0
  return pct >= 80 and "DiagnosticError" or pct >= 50 and "DiagnosticWarn" or "DiagnosticOk"
end

local function pie(pct)
  local icons = { "○", "◔", "◑", "◕", "●" }
  return icons[math.min(5, math.floor((pct or 0) / 25 + 0.5) + 1)]
end

local function bar(pct, width)
  local fill = math.floor(math.min(100, pct or 0) / 100 * width + 0.5)
  return string.rep("█", fill), string.rep("░", width - fill)
end

local function spark(vals)
  local chars = { "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█" }
  local mx = 0
  for _, v in ipairs(vals or {}) do
    mx = math.max(mx, v)
  end
  local out = {}
  for _, v in ipairs(vals or {}) do
    out[#out + 1] = (v <= 0 or mx == 0) and "·" or chars[math.max(1, math.ceil(v / mx * 8))]
  end
  return table.concat(out)
end

local function hl_hex(name)
  local h = vim.api.nvim_get_hl(0, { name = name, link = false })
  return h.fg and string.format("#%06x", h.fg) or nil
end

-- ---------- statusline ----------

local function live_session()
  local s = M.data and M.data.session
  if s and (s.age_sec or 0) <= M.config.session_max_age then
    return s
  end
end

local function account(name)
  for _, a in ipairs(M.data and M.data.accounts or {}) do
    if a.name == name then
      return a
    end
  end
end

local function account_seg(a)
  if not a.block then
    return a.name .. " 5h –"
  end
  local v = a.block.pct and string.format("%s%.0f%%", a.block.pct_source == "estimate" and "~" or "", a.block.pct)
    or money(a.block.totals.cost)
  local seg = string.format("%s 5h %s ↻%s", a.name, v, dur(a.block.reset_sec))
  if a.week_pct then
    seg = seg .. string.format(" · 7d %.0f%%", a.week_pct)
  end
  return seg
end

--- Chuỗi cho statusline, "" nếu chưa có dữ liệu.
function M.status()
  if not M.data then
    return ""
  end
  local s = live_session()
  if s then
    local seg = string.format("%s %.0f%% %s %s", pie(s.pct), s.pct, human(s.context), money(s.cost.cost))
    if s.state == "busy" then
      seg = seg .. " ⟳"
    elseif s.state == "wait" then
      seg = seg .. " ⚠"
    end
    local a = account(s.account)
    return a and (seg .. " │ " .. account_seg(a)) or seg
  end
  local segs = {}
  for _, a in ipairs(M.data.accounts or {}) do
    segs[#segs + 1] = account_seg(a)
  end
  return table.concat(segs, " · ")
end

--- Màu theo mức context (hoặc % 5h nếu không có phiên).
function M.color()
  local s = live_session()
  local pct = s and s.pct
  if not pct then
    local a = M.data and M.data.accounts and M.data.accounts[1]
    pct = a and a.block and a.block.pct or 0
  end
  return { fg = hl_hex(level(pct)) }
end

--- Component dùng thẳng trong lualine: `lualine_x = { require("ctxbar").lualine }`.
M.lualine = {
  function()
    return M.status()
  end,
  cond = function()
    return M.status() ~= ""
  end,
  color = function()
    return M.color()
  end,
}

-- ---------- floating window ----------

local view = { buf = nil, win = nil, timer = nil }

-- Builder: mỗi dòng là danh sách {text, hl?}; ghi lại vị trí byte để đặt extmark.
local function builder()
  local b = { lines = {}, marks = {} }
  function b:add(parts)
    local text, col = {}, 0
    for _, p in ipairs(parts or {}) do
      local t = p[1] or ""
      if p[2] then
        self.marks[#self.marks + 1] = { #self.lines, col, col + #t, p[2] }
      end
      text[#text + 1] = t
      col = col + #t
    end
    self.lines[#self.lines + 1] = table.concat(text)
  end
  return b
end

local function render(d)
  local b = builder()
  b:add({ { " Claude Code usage", "Title" }, { "   " .. os.date("%H:%M:%S"), "Comment" } })
  b:add({})

  local s = d.session
  if s then
    b:add({ { " Phiên hiện tại ", "Title" }, { string.format(" %s · %s · %s", s.account, s.project, (s.model or ""):gsub("^claude%-", "")), "Comment" } })
    local fill, empty = bar(s.pct, 30)
    b:add({
      { "   Context  " },
      { fill, level(s.pct) },
      { empty, "NonText" },
      { string.format("  %.0f%%  %s / %s", s.pct, human(s.context), human(s.window)) },
    })
    local state = ({ busy = "  ⟳ đang chạy", wait = "  ⚠ chờ duyệt" })[s.state or ""] or ""
    b:add({
      { "   Chi phí  " },
      { money(s.cost.cost), "Number" },
      { string.format("  · %d lượt · cập nhật %s trước", s.cost.msgs, dur(s.age_sec)), "Comment" },
      { state, s.state == "wait" and "DiagnosticError" or "DiagnosticInfo" },
    })
  else
    b:add({ { "   (không có phiên Claude nào cho " .. vim.fn.fnamemodify(vim.fn.getcwd(), ":~") .. ")", "Comment" } })
  end
  b:add({})

  b:add({ { " Tài khoản", "Title" } })
  for _, a in ipairs(d.accounts or {}) do
    b:add({ { "   " .. a.name, "Identifier" }, { "  " .. (a.email or ""), "Comment" } })
    if a.block then
      local pct = a.block.pct
      local fill, empty = bar(pct or 0, 20)
      local val = money(a.block.totals.cost) .. (pct and string.format(" (%.0f%%)", pct) or "")
      local src = a.block.pct_source == "server" and "  (server)" or a.block.pct_source == "estimate" and "  (ước lượng)" or ""
      b:add({
        { "     5h      " },
        { pct and fill or "", level(pct) },
        { pct and empty or "", "NonText" },
        { (pct and "  " or "") .. val, "Number" },
        { string.format("  reset sau %s%s", dur(a.block.reset_sec), src), "Comment" },
      })
    else
      b:add({ { "     5h      –  chưa có block đang chạy", "Comment" } })
    end
    local week = money(a.week.cost) .. (a.week_pct and string.format(" (%.0f%%)", a.week_pct) or "")
    b:add({
      { "     Hôm nay " },
      { money(a.today.cost), "Number" },
      { "   7 ngày " },
      { week, "Number" },
      { "   " },
      { spark(a.daily), "Special" },
    })
  end

  if d.recent and #d.recent > 0 then
    b:add({})
    b:add({ { " Phiên hoạt động (1h)", "Title" } })
    for _, r in ipairs(d.recent) do
      b:add({
        { string.format("   %-10s %-22s %-10s ", r.account, r.project:sub(1, 22), (r.model or ""):gsub("^claude%-", "")) },
        { string.format("%3.0f%%", r.pct), level(r.pct) },
        { string.format(" %6s  %8s  %s", human(r.context), money(r.cost.cost), dur(r.age_sec)), "Comment" },
      })
    end
  end
  b:add({})
  b:add({ { " r", "Special" }, { " làm mới  ", "Comment" }, { "q", "Special" }, { " đóng", "Comment" } })
  return b
end

local function draw(d)
  if not (view.buf and vim.api.nvim_buf_is_valid(view.buf)) then
    return
  end
  local b = render(d)
  vim.bo[view.buf].modifiable = true
  vim.api.nvim_buf_set_lines(view.buf, 0, -1, false, b.lines)
  vim.bo[view.buf].modifiable = false
  vim.api.nvim_buf_clear_namespace(view.buf, ns, 0, -1)
  for _, m in ipairs(b.marks) do
    vim.api.nvim_buf_set_extmark(view.buf, ns, m[1], m[2], { end_col = m[3], hl_group = m[4] })
  end
  if view.win and vim.api.nvim_win_is_valid(view.win) then
    local h = math.min(#b.lines, vim.o.lines - 4)
    vim.api.nvim_win_set_config(view.win, {
      relative = "editor",
      width = M.config.width,
      height = h,
      row = math.floor((vim.o.lines - h) / 2) - 1,
      col = math.floor((vim.o.columns - M.config.width) / 2),
    })
  end
end

function M.close()
  if view.timer then
    view.timer:stop()
    view.timer:close()
    view.timer = nil
  end
  if view.win and vim.api.nvim_win_is_valid(view.win) then
    vim.api.nvim_win_close(view.win, true)
  end
  view.win, view.buf = nil, nil
end

function M.open()
  if view.win and vim.api.nvim_win_is_valid(view.win) then
    return M.close()
  end
  view.buf = vim.api.nvim_create_buf(false, true)
  vim.bo[view.buf].bufhidden = "wipe"
  vim.bo[view.buf].filetype = "ctxbar"
  view.win = vim.api.nvim_open_win(view.buf, true, {
    relative = "editor",
    width = M.config.width,
    height = 3,
    row = 2,
    col = math.floor((vim.o.columns - M.config.width) / 2),
    style = "minimal",
    border = "rounded",
    title = " ctxbar ",
    title_pos = "center",
  })
  vim.wo[view.win].cursorline = false
  vim.api.nvim_buf_set_lines(view.buf, 0, -1, false, { " đang tải..." })

  local o = { buffer = view.buf, nowait = true, silent = true }
  vim.keymap.set("n", "q", M.close, o)
  vim.keymap.set("n", "<Esc>", M.close, o)
  vim.keymap.set("n", "r", function()
    M.refresh(draw)
  end, o)
  vim.api.nvim_create_autocmd("BufWipeout", { buffer = view.buf, once = true, callback = M.close })

  if M.data then
    draw(M.data)
  end
  M.refresh(draw)
  view.timer = vim.uv.new_timer()
  view.timer:start(M.config.interval, M.config.interval, vim.schedule_wrap(function()
    M.refresh(draw)
  end))
end

-- ---------- setup ----------

function M.setup(opts)
  M.config = vim.tbl_deep_extend("force", M.config, opts or {})
  if timer then
    timer:stop()
    timer:close()
  end
  timer = vim.uv.new_timer()
  timer:start(0, M.config.interval, vim.schedule_wrap(function()
    M.refresh()
  end))
  vim.api.nvim_create_autocmd({ "DirChanged", "FocusGained" }, {
    group = vim.api.nvim_create_augroup("ctxbar", { clear = true }),
    callback = function()
      M.refresh()
    end,
  })
end

return M
