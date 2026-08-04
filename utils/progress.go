package utils

import (
	"fmt"
	"strings"
	"sync"
)

// Bar is a minimal ASCII progress bar. Replaces cheggaaa/pb/v3, which pulled
// in go-runewidth/uniseg unicode-width tables that dominated binary size.
const barWidth = 30

type Bar struct {
	mu      sync.Mutex
	total   int
	current int
	prefix  string
	suffix  string
}

func NewBar(count int, prefix, suffix string) *Bar {
	if count <= 0 {
		count = 1
	}
	return &Bar{total: count, prefix: prefix, suffix: suffix}
}

func (b *Bar) Grow(n int, status string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current += n
	if b.current > b.total {
		b.current = b.total
	}
	filled := b.current * barWidth / b.total
	bar := strings.Repeat("=", filled) + strings.Repeat("-", barWidth-filled)
	fmt.Printf("\r%s[%s] %d/%d %s%s   ", b.prefix, bar, b.current, b.total, status, b.suffix)
}

func (b *Bar) Done() {
	fmt.Println()
}
