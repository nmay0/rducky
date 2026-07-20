package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"qq/internal/chat"
	"qq/internal/config"
	"qq/internal/tmux"
)

const usage = `qq — hotkey AI sidebar for your terminal (tmux)

Usage:
  qq [toggle] [-t pane]   open the sidebar next to the pane (or close it if open)
  qq chat --target pane   run the chat REPL (what the sidebar pane runs)
  qq install [--write]    show the tmux.conf keybinding (--write appends it)

Config: ~/.config/qq/config.yaml (model, max_tokens, context_lines, split, size)
Auth:   ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, or an ` + "`ant auth login`" + ` profile
`

func main() {
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}

	switch cmd {
	case "", "toggle":
		if cmd == "" && os.Getenv("TMUX") == "" {
			fmt.Print(usage)
			return
		}
		if err := cmdToggle(args); err != nil {
			fatal(err)
		}
	case "chat":
		if err := cmdChat(args); err != nil {
			// The sidebar pane dies the instant we exit, so hold it open
			// long enough for the error to be read.
			fmt.Fprintf(os.Stderr, "\x1b[31mqq: %v\x1b[0m\n", err)
			fmt.Fprint(os.Stderr, "press Enter to close ")
			bufio.NewReader(os.Stdin).ReadString('\n')
			os.Exit(1)
		}
	case "install":
		if err := cmdInstall(args); err != nil {
			fatal(err)
		}
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "qq: unknown command %q\n\n%s", cmd, usage)
		os.Exit(1)
	}
}

func cmdToggle(args []string) error {
	fs := flag.NewFlagSet("toggle", flag.ExitOnError)
	target := fs.String("t", "", "pane to read (defaults to the active pane)")
	fs.Parse(args)

	cfg := config.Load()

	pane := *target
	if pane == "" {
		var err error
		if pane, err = tmux.ActivePaneID(); err != nil {
			return err
		}
	}

	if sidebar, err := tmux.FindSidebar(pane); err != nil {
		return err
	} else if sidebar != "" {
		return tmux.KillPane(sidebar)
	}

	bin, err := os.Executable()
	if err != nil {
		return err
	}
	return tmux.OpenSidebar(bin, pane, cfg.Split, cfg.Size)
}

func cmdChat(args []string) error {
	fs := flag.NewFlagSet("chat", flag.ExitOnError)
	target := fs.String("target", "", "tmux pane id to read for context")
	fs.Parse(args)

	if os.Getenv("TMUX") == "" {
		return fmt.Errorf("chat must run inside tmux (use `qq toggle`)")
	}
	pane := *target
	if pane == "" {
		var err error
		if pane, err = tmux.ActivePaneID(); err != nil {
			return err
		}
	}

	cfg := config.Load()
	return chat.Run(pane, cfg.Model, cfg.MaxTokens, cfg.ContextLines)
}

func cmdInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	write := fs.Bool("write", false, "append the keybinding to ~/.tmux.conf")
	fs.Parse(args)

	bin, err := os.Executable()
	if err != nil {
		return err
	}
	snippet := fmt.Sprintf(`
# qq — AI sidebar (prefix + a). Added by 'qq install'.
bind-key a run-shell "%s toggle -t '#{pane_id}'"
`, bin)

	if !*write {
		fmt.Println("Add this to ~/.tmux.conf, then run: tmux source-file ~/.tmux.conf")
		fmt.Println(snippet)
		fmt.Println("Or run `qq install --write` to append it for you.")
		fmt.Println(`Prefer no prefix key? Use: bind-key -n M-a run-shell "..." (Alt+a)`)
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	confPath := filepath.Join(home, ".tmux.conf")
	existing, _ := os.ReadFile(confPath)
	if strings.Contains(string(existing), "qq — AI sidebar") {
		fmt.Println("qq keybinding already present in", confPath)
		return nil
	}
	f, err := os.OpenFile(confPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(snippet); err != nil {
		return err
	}
	fmt.Println("Added keybinding to", confPath)
	fmt.Println("Reload with: tmux source-file ~/.tmux.conf — then press prefix + a")
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "qq:", err)
	os.Exit(1)
}
