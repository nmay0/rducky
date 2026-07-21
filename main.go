package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"rducky/internal/chat"
	"rducky/internal/config"
	"rducky/internal/llm"
	"rducky/internal/tmux"
)

const usage = `rducky — hotkey AI sidebar for your terminal (tmux)

Usage:
  rducky [toggle] [-t pane]   open the sidebar next to the pane (or close it if open)
  rducky chat --target pane   run the chat REPL (what the sidebar pane runs)
  rducky install [--write]    show the tmux.conf keybinding (--write appends it)
  rducky providers            list supported AI providers and their env keys

Config: ~/.config/rducky/config.yaml
        (provider, model, base_url, api_key_env, max_tokens, context_lines, split, size)
Auth:   export the provider's key env var (see ` + "`rducky providers`" + `); ollama needs none
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
			fmt.Fprintf(os.Stderr, "\x1b[31mrducky: %v\x1b[0m\n", err)
			fmt.Fprint(os.Stderr, "press Enter to close ")
			bufio.NewReader(os.Stdin).ReadString('\n')
			os.Exit(1)
		}
	case "install":
		if err := cmdInstall(args); err != nil {
			fatal(err)
		}
	case "providers":
		cmdProviders()
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "rducky: unknown command %q\n\n%s", cmd, usage)
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
		return fmt.Errorf("chat must run inside tmux (use `rducky toggle`)")
	}
	pane := *target
	if pane == "" {
		var err error
		if pane, err = tmux.ActivePaneID(); err != nil {
			return err
		}
	}

	return chat.Run(pane, config.Load())
}

func cmdProviders() {
	fmt.Printf("%-12s %-20s %-26s %s\n", "PROVIDER", "AUTH ENV VAR", "DEFAULT MODEL", "ENDPOINT")
	for _, p := range llm.Table() {
		keyEnv := p.KeyEnv
		if keyEnv == "" {
			keyEnv = "(none)"
		}
		endpoint := p.BaseURL
		if endpoint == "" {
			endpoint = "(official SDK)"
		}
		fmt.Printf("%-12s %-20s %-26s %s\n", p.Name, keyEnv, p.DefaultModel, endpoint)
	}
	fmt.Printf("%-12s %-20s %-26s %s\n", "custom", "(set api_key_env)", "(set model)", "(set base_url) — any OpenAI-compatible API")
	fmt.Print(`
Select in ~/.config/rducky/config.yaml, e.g.:
  provider: openai
  model: gpt-5.1        # optional — omit for the provider default
`)
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
# rducky — AI sidebar (prefix + a). Added by 'rducky install'.
bind-key a run-shell "%s toggle -t '#{pane_id}'"
`, bin)

	if !*write {
		fmt.Println("Add this to ~/.tmux.conf, then run: tmux source-file ~/.tmux.conf")
		fmt.Println(snippet)
		fmt.Println("Or run `rducky install --write` to append it for you.")
		fmt.Println(`Prefer no prefix key? Use: bind-key -n M-a run-shell "..." (Alt+a)`)
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	confPath := filepath.Join(home, ".tmux.conf")
	existing, _ := os.ReadFile(confPath)
	if strings.Contains(string(existing), "rducky — AI sidebar") {
		fmt.Println("rducky keybinding already present in", confPath)
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
	fmt.Fprintln(os.Stderr, "rducky:", err)
	os.Exit(1)
}
