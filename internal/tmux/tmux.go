package tmux

import (
	"fmt"
	"os/exec"
	"strings"
)

const sidebarOption = "@qq_sidebar"

func run(args ...string) (string, error) {
	out, err := exec.Command("tmux", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// ActivePaneID returns the id (e.g. "%3") of the currently active pane.
func ActivePaneID() (string, error) {
	return run("display-message", "-p", "#{pane_id}")
}

// FindSidebar returns the pane id of an existing qq sidebar in the window
// containing target, or "" if there is none.
func FindSidebar(target string) (string, error) {
	args := []string{"list-panes", "-F", "#{pane_id} #{" + sidebarOption + "}"}
	if target != "" {
		args = append(args, "-t", target)
	}
	out, err := run(args...)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "1" {
			return fields[0], nil
		}
	}
	return "", nil
}

// OpenSidebar splits a new pane next to target running "bin chat --target <target>"
// and marks it so FindSidebar can locate it later.
func OpenSidebar(bin, target, split, size string) error {
	dir := "-h"
	if split == "v" {
		dir = "-v"
	}
	cmd := fmt.Sprintf("'%s' chat --target '%s'", bin, target)
	paneID, err := run("split-window", dir, "-l", size, "-t", target, "-P", "-F", "#{pane_id}", cmd)
	if err != nil {
		return err
	}
	_, err = run("set-option", "-p", "-t", paneID, sidebarOption, "1")
	return err
}

func KillPane(id string) error {
	_, err := run("kill-pane", "-t", id)
	return err
}

// Capture returns the target pane's visible content plus up to scrollback
// lines of history above it, with wrapped lines joined and surrounding blank
// lines trimmed.
func Capture(target string, scrollback int) (string, error) {
	out, err := run("capture-pane", "-p", "-J", "-t", target, "-S", fmt.Sprintf("-%d", scrollback))
	if err != nil {
		return "", err
	}
	return strings.Trim(out, "\n"), nil
}

// PaneInfo returns the foreground command and current working directory of
// the target pane.
func PaneInfo(target string) (command, path string) {
	command, _ = run("display-message", "-p", "-t", target, "#{pane_current_command}")
	path, _ = run("display-message", "-p", "-t", target, "#{pane_current_path}")
	return command, path
}
