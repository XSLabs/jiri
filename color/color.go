// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package color

import (
	"fmt"
	"os"

	"go.fuchsia.dev/jiri/isatty"
)

type Colorfn func(format string, a ...any) string

const (
	escape = "\033["
	clear  = escape + "0m"
)

type ColorCode int

// Foreground text colors
const (
	BlackFg ColorCode = iota + 30
	RedFg
	GreenFg
	YellowFg
	BlueFg
	MagentaFg
	CyanFg
	WhiteFg
	DefaultFg
)

type Color interface {
	Black(format string, a ...any) string
	Red(format string, a ...any) string
	Green(format string, a ...any) string
	Yellow(format string, a ...any) string
	Blue(format string, a ...any) string
	Magenta(format string, a ...any) string
	Cyan(format string, a ...any) string
	White(format string, a ...any) string
	DefaultColor(format string, a ...any) string
	Enabled() bool
}

type color struct{}

func (color) Black(format string, a ...any) string { return colorString(BlackFg, format, a...) }
func (color) Red(format string, a ...any) string   { return colorString(RedFg, format, a...) }
func (color) Green(format string, a ...any) string { return colorString(GreenFg, format, a...) }
func (color) Yellow(format string, a ...any) string {
	return colorString(YellowFg, format, a...)
}
func (color) Blue(format string, a ...any) string { return colorString(BlueFg, format, a...) }
func (color) Magenta(format string, a ...any) string {
	return colorString(MagentaFg, format, a...)
}
func (color) Cyan(format string, a ...any) string  { return colorString(CyanFg, format, a...) }
func (color) White(format string, a ...any) string { return colorString(WhiteFg, format, a...) }
func (color) DefaultColor(format string, a ...any) string {
	return colorString(DefaultFg, format, a...)
}
func (color) Enabled() bool {
	return true
}

func colorString(c ColorCode, format string, a ...any) string {
	if c == DefaultFg {
		return fmt.Sprintf(format, a...)
	}
	return fmt.Sprintf("%v%vm%v%v", escape, c, fmt.Sprintf(format, a...), clear)
}

type monochrome struct{}

func (monochrome) Black(format string, a ...any) string   { return fmt.Sprintf(format, a...) }
func (monochrome) Red(format string, a ...any) string     { return fmt.Sprintf(format, a...) }
func (monochrome) Green(format string, a ...any) string   { return fmt.Sprintf(format, a...) }
func (monochrome) Yellow(format string, a ...any) string  { return fmt.Sprintf(format, a...) }
func (monochrome) Blue(format string, a ...any) string    { return fmt.Sprintf(format, a...) }
func (monochrome) Magenta(format string, a ...any) string { return fmt.Sprintf(format, a...) }
func (monochrome) Cyan(format string, a ...any) string    { return fmt.Sprintf(format, a...) }
func (monochrome) White(format string, a ...any) string   { return fmt.Sprintf(format, a...) }
func (monochrome) DefaultColor(format string, a ...any) string {
	return fmt.Sprintf(format, a...)
}
func (monochrome) Enabled() bool {
	return false
}

type EnableColor string

const (
	ColorAlways EnableColor = "always"
	ColorNever  EnableColor = "never"
	ColorAuto   EnableColor = "auto"
)

func NewColor(enableColor EnableColor) Color {
	ec := enableColor != ColorNever
	if enableColor != ColorAlways {
		if ec {
			term := os.Getenv("TERM")
			switch term {
			case "dumb", "":
				ec = false
			}
		}
		if ec {
			ec = isatty.IsTerminal()
		}
	}
	if ec {
		return color{}
	} else {
		return monochrome{}
	}
}
