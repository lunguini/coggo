package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// prompter wraps a Scanner and a writer for interactive prompts.
type prompter struct {
	in  *bufio.Scanner
	out io.Writer
}

func newPrompter(in io.Reader, out io.Writer) *prompter {
	s := bufio.NewScanner(in)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &prompter{in: s, out: out}
}

// ask prints the prompt and returns the trimmed response (or fallback on
// empty input).
func (p *prompter) ask(prompt, fallback string) string {
	if fallback != "" {
		fmt.Fprintf(p.out, "%s [%s]: ", prompt, fallback)
	} else {
		fmt.Fprintf(p.out, "%s: ", prompt)
	}
	if !p.in.Scan() {
		return fallback
	}
	v := strings.TrimSpace(p.in.Text())
	if v == "" {
		return fallback
	}
	return v
}

// askYN prompts a yes/no question. defYes selects "Y/n" vs "y/N".
func (p *prompter) askYN(prompt string, defYes bool) bool {
	suffix := "[y/N]"
	if defYes {
		suffix = "[Y/n]"
	}
	fmt.Fprintf(p.out, "%s %s: ", prompt, suffix)
	if !p.in.Scan() {
		return defYes
	}
	v := strings.ToLower(strings.TrimSpace(p.in.Text()))
	if v == "" {
		return defYes
	}
	return v == "y" || v == "yes"
}
