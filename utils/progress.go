package utils

import (
	"fmt"
	"os"
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

// Grow advances the bar. It writes to stderr, not stdout: a progress bar is
// information about the run, not the run's data output, so it must not land
// in a pipeline (e.g. `tool ... | grep ...`) and must stay silent under -q.
func (b *Bar) Grow(n int, status string) {
	if Quiet {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current += n
	if b.current > b.total {
		b.current = b.total
	}
	filled := b.current * barWidth / b.total
	bar := strings.Repeat("=", filled) + strings.Repeat("-", barWidth-filled)
	fmt.Fprintf(os.Stderr, "\r%s[%s] %d/%d %s%s   ", b.prefix, bar, b.current, b.total, status, b.suffix)
}

func (b *Bar) Done() {
	if Quiet {
		return
	}
	fmt.Fprintln(os.Stderr)
}
