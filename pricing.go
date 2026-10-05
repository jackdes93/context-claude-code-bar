package main

import "strings"

// Giá USD / 1M token (bảng Anthropic, cập nhật 2026-09).
// Cache write 5m = 1.25x input, 1h = 2x input; Read là giá cache read.
type price struct {
	In, Out, Read float64
	Window        int
}

// Thứ tự quan trọng: prefix cụ thể hơn đứng trước.
var priceTable = []struct {
	prefix string
	p      price
}{
	{"claude-fable-5-1", price{10, 50, 0.25, 1_000_000}},
	{"claude-mythos-5-1", price{10, 50, 0.25, 1_000_000}},
	{"claude-fable-5", price{10, 50, 1.0, 1_000_000}},
	{"claude-mythos", price{10, 50, 1.0, 1_000_000}},
	{"claude-opus-5-5", price{4, 20, 0.20, 1_000_000}},
	{"claude-opus-5", price{5, 25, 0.5, 1_000_000}},
	{"claude-opus-4-8", price{5, 25, 0.5, 1_000_000}},
	{"claude-opus-4-7", price{5, 25, 0.5, 1_000_000}},
	{"claude-opus-4-6", price{5, 25, 0.5, 1_000_000}},
	{"claude-opus-4-5", price{5, 25, 0.5, 200_000}},
	{"claude-opus-4", price{15, 75, 1.5, 200_000}},
	{"claude-sonnet-5", price{2, 10, 0.20, 1_000_000}},
	{"claude-sonnet-4-6", price{3, 15, 0.3, 1_000_000}},
	{"claude-sonnet-4", price{3, 15, 0.3, 200_000}},
	{"claude-3-7-sonnet", price{3, 15, 0.3, 200_000}},
	{"claude-haiku-4-5", price{1, 5, 0.1, 200_000}},
	{"claude-3-5-haiku", price{0.8, 4, 0.08, 200_000}},
}

var fallbackPrice = price{3, 15, 0.3, 200_000}

func priceFor(model string) price {
	for _, e := range priceTable {
		if strings.HasPrefix(model, e.prefix) {
			return e.p
		}
	}
	return fallbackPrice
}

func (e Entry) Cost() float64 {
	p := priceFor(e.Model)
	c := (float64(e.In)*p.In + float64(e.CW5)*p.In*1.25 + float64(e.CW1)*p.In*2 +
		float64(e.CR)*p.Read + float64(e.Out)*p.Out) / 1e6
	if e.Fast {
		c *= 2
	}
	return c
}

func (e Entry) Tokens() int64 { return e.In + e.CW5 + e.CW1 + e.CR + e.Out }

// ContextTokens = toàn bộ input của lượt gần nhất (đúng bằng độ lớn context đã gửi).
func (e Entry) ContextTokens() int64 { return e.In + e.CW5 + e.CW1 + e.CR }

func contextWindow(model string, used int64, override int) int {
	if override > 0 {
		return override
	}
	w := priceFor(model).Window
	if strings.Contains(model, "[1m]") || used > int64(w) {
		w = 1_000_000
	}
	return w
}
