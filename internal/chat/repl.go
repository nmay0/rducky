package chat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"

	"github.com/chzyer/readline"

	"rducky/internal/config"
	"rducky/internal/llm"
	"rducky/internal/tmux"
)

const (
	reset  = "\x1b[0m"
	dim    = "\x1b[2m"
	bold   = "\x1b[1m"
	cyan   = "\x1b[36m"
	green  = "\x1b[1;32m"
	yellow = "\x1b[33m"
	red    = "\x1b[31m"
)

const systemPromptTemplate = `You are rducky, a quick-question assistant running in a tmux sidebar next to the user's terminal. The user opened you mid-work to get a fast answer, then get back to what they were doing.

Environment:
- OS: %s
- Shell: %s
- Working directory: %s
- Foreground command in the pane: %s

The user's terminal content is provided below (most recent output at the bottom). Use it as context for their questions — errors, output, and prompts on screen are usually what they're asking about.

Guidelines:
- Be concise. Most answers should be a few lines.
- When the answer is a command, lead with the command in a fenced code block, then at most a couple of lines of explanation.
- Don't repeat large chunks of the user's terminal back at them.

<terminal>
%s
</terminal>`

// Run starts the sidebar REPL against the given tmux pane.
func Run(target string, cfg config.Config) error {
	capture, err := tmux.Capture(target, cfg.ContextLines)
	if err != nil {
		return fmt.Errorf("cannot read pane %s: %w", target, err)
	}
	command, cwd := tmux.PaneInfo(target)

	system := fmt.Sprintf(systemPromptTemplate,
		osDescription(), os.Getenv("SHELL"), cwd, command, capture)
	session, err := llm.NewSession(llm.Options{
		Provider:  cfg.Provider,
		Model:     cfg.Model,
		BaseURL:   cfg.BaseURL,
		APIKeyEnv: cfg.APIKeyEnv,
		MaxTokens: cfg.MaxTokens,
		System:    system,
	})
	if err != nil {
		return err
	}
	lastCapture := capture
	contextLines := cfg.ContextLines

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          green + "❯ " + reset,
		InterruptPrompt: "^C",
	})
	if err != nil {
		return err
	}
	defer rl.Close()

	fmt.Println(yellow + "🦆 " + reset + bold + "rducky" + reset +
		dim + "  ·  " + session.Provider + "/" + session.Model + reset)
	fmt.Println(dim + "pane " + target + " · ^D quit · /refresh · /help" + reset)
	fmt.Println()

	for {
		line, err := rl.Readline()
		if errors.Is(err, readline.ErrInterrupt) {
			continue
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		input := strings.TrimSpace(line)
		switch {
		case input == "":
			continue
		case input == "exit" || input == "quit":
			return nil
		case input == "/help":
			fmt.Printf("%sexit, quit, Ctrl+D  close the sidebar\n/refresh            re-read the pane now\nCtrl+C              cancel the current answer%s\n\n", dim, reset)
			continue
		case input == "/refresh":
			lastCapture = "" // force a fresh snapshot into the next question
			fmt.Printf("%spane will be re-read with your next question%s\n\n", dim, reset)
			continue
		}

		// Include a fresh snapshot only when the pane changed since the
		// model last saw it, so follow-ups stay current without bloating
		// the conversation.
		message := input
		if fresh, err := tmux.Capture(target, contextLines); err == nil && fresh != lastCapture {
			message = fmt.Sprintf("The terminal content has updated:\n<terminal>\n%s\n</terminal>\n\n%s", fresh, input)
			lastCapture = fresh
		}

		answer(session, message)
	}
}

// answer streams one reply, letting Ctrl+C cancel it without exiting.
func answer(session *llm.Session, message string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	fmt.Println()
	r := &renderer{out: os.Stdout}
	stopReason, err := session.Ask(ctx, message, r.delta)
	r.flush()
	if err != nil {
		fmt.Printf("\n%s%s%s\n\n", red, llm.Explain(err), reset)
		return
	}
	if stopReason == "refusal" {
		fmt.Printf("\n%sthe model declined to answer this one%s", dim, reset)
	}
	fmt.Println()
	fmt.Println()
}

// renderer prints streamed text line-buffered, giving it a light markdown
// coat: cyan code, bold headings, • bullets, tidy spacing around code blocks.
type renderer struct {
	out       io.Writer
	lineBuf   strings.Builder
	inCode    bool
	prevBlank bool
}

func (r *renderer) delta(text string) {
	for _, ch := range text {
		r.lineBuf.WriteRune(ch)
		if ch == '\n' {
			r.emitLine()
		}
	}
}

func (r *renderer) emitLine() {
	line := strings.TrimRight(r.lineBuf.String(), "\n")
	r.lineBuf.Reset()
	trimmed := strings.TrimSpace(line)

	switch {
	case strings.HasPrefix(trimmed, "```"):
		// Hide the fence markers — the cyan tint signals code — but keep
		// one blank line between the block and surrounding prose.
		r.inCode = !r.inCode
		r.blankGap()
	case r.inCode:
		fmt.Fprintf(r.out, "%s%s%s\n", cyan, line, reset)
		r.prevBlank = false
	case trimmed == "":
		r.blankGap() // collapse runs of blank lines into one
	default:
		fmt.Fprintln(r.out, styleLine(line))
		r.prevBlank = false
	}
}

// blankGap emits a single blank line, swallowing consecutive ones.
func (r *renderer) blankGap() {
	if !r.prevBlank {
		fmt.Fprintln(r.out)
		r.prevBlank = true
	}
}

func (r *renderer) flush() {
	if r.lineBuf.Len() > 0 {
		r.emitLine()
	}
}

// styleLine applies line-level markdown: headings become bold, "-"/"*"
// bullets get a • marker, then inline spans are styled.
func styleLine(line string) string {
	trimmed := strings.TrimSpace(line)

	// Headings (#, ##, ###…): drop the hashes, render the text bold.
	if h := strings.TrimLeft(trimmed, "#"); h != trimmed && strings.HasPrefix(h, " ") {
		return bold + styleInline(strings.TrimSpace(h)) + reset
	}

	// Bullets: swap a leading "- "/"* " for a • while keeping indentation.
	indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
	body := line[len(indent):]
	if strings.HasPrefix(body, "- ") || strings.HasPrefix(body, "* ") {
		return indent + cyan + "•" + reset + " " + styleInline(body[2:])
	}

	return styleInline(line)
}

// styleInline tints `inline code` and bolds **spans** within a line.
func styleInline(s string) string {
	s = wrapPairs(s, "`", cyan)
	s = wrapPairs(s, "**", bold)
	return s
}

// wrapPairs styles text sitting between matched pairs of delim. It only acts
// on an even number of delimiters, so stray markers are left untouched and no
// color ever leaks past the end of the line.
func wrapPairs(s, delim, style string) string {
	if n := strings.Count(s, delim); n < 2 || n%2 != 0 {
		return s
	}
	var b strings.Builder
	open := false
	for {
		i := strings.Index(s, delim)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		if open {
			b.WriteString(reset)
		} else {
			b.WriteString(style)
		}
		open = !open
		s = s[i+len(delim):]
	}
}

func osDescription() string {
	if runtime.GOOS == "darwin" {
		return "macOS"
	}
	if runtime.GOOS == "linux" {
		if data, err := os.ReadFile("/etc/os-release"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if name, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
					return "Linux (" + strings.Trim(name, `"`) + ")"
				}
			}
		}
		return "Linux"
	}
	return runtime.GOOS
}
