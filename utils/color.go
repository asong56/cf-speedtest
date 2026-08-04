package utils

import (
	"fmt"
	"os"
)

// ansiColor replaces the previous fatih/color dependency with plain ANSI
// codes, dropping the go-colorable/go-isatty transitive deps entirely.
type ansiColor struct{ code string }

var colorEnabled = shouldColor()

func shouldColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func (c ansiColor) wrap(s string) string {
	if !colorEnabled {
		return s
	}
	return "\x1b[" + c.code + "m" + s + "\x1b[0m"
}

func (c ansiColor) Printf(format string, a ...interface{}) {
	fmt.Print(c.wrap(fmt.Sprintf(format, a...)))
}

func (c ansiColor) Println(a ...interface{}) {
	fmt.Println(c.wrap(fmt.Sprint(a...)))
}

var (
	Red     = ansiColor{"31"}
	Green   = ansiColor{"32"}
	Yellow  = ansiColor{"33"}
	Blue    = ansiColor{"34;1"}
	Magenta = ansiColor{"35"}
	Cyan    = ansiColor{"96;1"}
	White   = ansiColor{"37"}
)
