package console

import (
	"fmt"
	"github.com/VirtualRoyalty/go-mutesting/internal/models"
	"log"
	"strings"

	"github.com/fatih/color"
)

// for colouring
const (
	PASS    = "PASS"
	FAIL    = "FAIL"
	SKIP    = "SKIP"
	UNKNOWN = "UNKNOWN"
)

var (
	length    = 150
	frameLine = strings.Repeat("-", length)
)

// PrintPass prints in green
func PrintPass(out string) {
	pass := color.New(color.FgHiWhite, color.BgGreen).SprintfFunc()
	out = strings.Replace(out, PASS, pass(PASS), 1)
	fmt.Print(out)
	color.Blue(frameLine)
}

// PrintFail prints in red
func PrintFail(out string) {
	fail := color.New(color.FgHiWhite, color.BgRed).SprintfFunc()
	out = strings.Replace(out, FAIL, fail(FAIL), 1)
	fmt.Print(out)
	color.Blue(frameLine)
}

// PrintSkip prints in yellow
func PrintSkip(out string) {
	skip := color.New(color.FgHiWhite, color.BgYellow).SprintfFunc()
	out = strings.Replace(out, SKIP, skip(SKIP), 1)
	fmt.Print(out)
	color.Blue(frameLine)
}

// PrintUnknown prints in magenta
func PrintUnknown(out string) {
	unknown := color.New(color.FgHiWhite, color.BgMagenta).SprintfFunc()
	out = strings.Replace(out, UNKNOWN, unknown(UNKNOWN), 1)
	fmt.Print(out)
	color.Blue(frameLine)
}

// PrintDiff prints colorful diff
func PrintDiff(diff []byte) {
	green := color.New(color.FgHiWhite).Add(color.BgGreen)
	red := color.New(color.FgHiWhite).Add(color.BgRed)

	lines := string(diff)
	for _, line := range strings.Split(lines, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"):
			_, err := green.Println(line)
			if err != nil {
				log.Printf("Error printing output: %s", err)
			}
		case strings.HasPrefix(line, "---"):
			_, err := red.Println(line)
			if err != nil {
				log.Printf("Error printing output: %s", err)
			}
		case strings.HasPrefix(line, "+"):
			_, err := green.Println(line)
			if err != nil {
				log.Printf("Error printing output: %s", err)
			}
		case strings.HasPrefix(line, "-"):
			_, err := red.Println(line)
			if err != nil {
				log.Printf("Error printing output: %s", err)
			}
		default:
			fmt.Println(line)
		}
	}
}

// Debug prints formatted debug messages when debug mode is enabled in options.
func Debug(opts *models.Options, format string, args ...interface{}) {
	if opts.General.Debug {
		fmt.Printf(format+"\n", args...)
	}
}

// Verbose prints formatted messages when either verbose or debug mode is enabled.
func Verbose(opts *models.Options, format string, args ...interface{}) {
	if opts.General.Verbose || opts.General.Debug {
		fmt.Printf(format+"\n", args...)
	}
}
