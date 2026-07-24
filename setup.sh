#!/usr/bin/env bash
# rducky setup — build, install on PATH, configure provider API keys, and wire
# up the tmux keybinding. Safe to re-run; skips anything already done.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="$HOME/.local/bin"
BIN_PATH="$INSTALL_DIR/rducky"

info() { printf '\033[36m==>\033[0m %s\n' "$1"; }
warn() { printf '\033[33m!!\033[0m %s\n' "$1"; }

# --- pick the shell rc file to persist PATH / API key into -----------------
case "$(basename "${SHELL:-}")" in
  zsh)  RC_FILE="$HOME/.zshrc" ;;
  bash) RC_FILE="$HOME/.bashrc" ;;
  *)    RC_FILE="$HOME/.zshrc" ;;
esac
touch "$RC_FILE"

append_once() {
  # append_once <marker> <block> — appends <block> to RC_FILE unless <marker>
  # is already present in it.
  if ! grep -qF "$1" "$RC_FILE" 2>/dev/null; then
    printf '\n%s\n' "$2" >> "$RC_FILE"
  fi
}

# --- 1. prerequisites --------------------------------------------------
if ! command -v go >/dev/null 2>&1; then
  warn "go is not installed. On macOS: brew install go. On Arch: pacman -S go."
  exit 1
fi
if ! command -v tmux >/dev/null 2>&1; then
  warn "tmux is not installed. On macOS: brew install tmux. On Arch: pacman -S tmux."
  exit 1
fi

# --- 2. build ------------------------------------------------------------
info "Building rducky..."
(cd "$SCRIPT_DIR" && go build -o rducky .)

# --- 3. install onto PATH -------------------------------------------------
mkdir -p "$INSTALL_DIR"
cp "$SCRIPT_DIR/rducky" "$BIN_PATH"
chmod +x "$BIN_PATH"
info "Installed to $BIN_PATH"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    if grep -qF "# rducky — PATH" "$RC_FILE" 2>/dev/null; then
      warn "$INSTALL_DIR is in $RC_FILE already but not in this shell — open a new shell to pick it up"
    else
      append_once "# rducky — PATH" \
        "# rducky — PATH (added by setup.sh)
export PATH=\"$INSTALL_DIR:\$PATH\""
      warn "Added $INSTALL_DIR to PATH in $RC_FILE (open a new shell to pick it up)"
    fi
    export PATH="$INSTALL_DIR:$PATH"
    ;;
esac

# --- 4. API keys -----------------------------------------------------------
# Keyed providers (mirrors the registry in internal/llm/llm.go; ollama runs
# locally and needs no key).
PROVIDERS="anthropic openai gemini xai groq cerebras mistral deepseek openrouter"

key_env_for() {
  case "$1" in
    anthropic)  echo "ANTHROPIC_API_KEY" ;;
    openai)     echo "OPENAI_API_KEY" ;;
    gemini)     echo "GEMINI_API_KEY" ;;
    xai)        echo "XAI_API_KEY" ;;
    groq)       echo "GROQ_API_KEY" ;;
    cerebras)   echo "CEREBRAS_API_KEY" ;;
    mistral)    echo "MISTRAL_API_KEY" ;;
    deepseek)   echo "DEEPSEEK_API_KEY" ;;
    openrouter) echo "OPENROUTER_API_KEY" ;;
    *)          echo "" ;;
  esac
}

resolve_alias() {
  case "$1" in
    claude) echo "anthropic" ;;
    google) echo "gemini" ;;
    grok)   echo "xai" ;;
    *)      echo "$1" ;;
  esac
}

key_configured() {
  # configured = exported in this shell, or persisted in RC_FILE (matches
  # both the per-provider marker blocks and the pre-multi-provider
  # "# rducky — API key" block, which also wrote an export line).
  [ -n "${!1:-}" ] && return 0
  grep -qE "export $1=" "$RC_FILE" 2>/dev/null
}

configure_key() {
  local name="$1" env_var key
  env_var="$(key_env_for "$name")"
  if key_configured "$env_var"; then
    info "$env_var is already configured — skipping. (Edit $RC_FILE to change it.)"
    return 0
  fi
  printf 'Enter your %s API key (%s): ' "$name" "$env_var"
  read -rs key
  printf '\n'
  if [ -z "$key" ]; then
    warn "No key entered — skipping $name. Set $env_var yourself later, or re-run this script."
    return 0
  fi
  append_once "# rducky — $env_var" \
    "# rducky — $env_var (added by setup.sh)
export $env_var=\"$key\""
  info "Saved $env_var to $RC_FILE"
  export "$env_var=$key"
  # Running tmux servers were started before the rc file change, so they
  # won't inherit it until restarted — push it into the live server too.
  if [ -n "${TMUX:-}" ]; then
    tmux set-environment -g "$env_var" "$key"
    info "Pushed $env_var into your running tmux server."
  fi
}

echo
info "rducky supports several providers; set up a key for each one you use:"
for p in $PROVIDERS; do
  if key_configured "$(key_env_for "$p")"; then
    printf '    %-11s %s (configured)\n' "$p" "$(key_env_for "$p")"
  else
    printf '    %-11s %s\n' "$p" "$(key_env_for "$p")"
  fi
done
printf '    %-11s %s\n' "ollama" "no key needed (local)"

DEFAULT_CHOICE=""
key_configured ANTHROPIC_API_KEY || DEFAULT_CHOICE="anthropic"
FIRST_CONFIGURED=""
while :; do
  if [ -n "$DEFAULT_CHOICE" ]; then
    printf 'Set up a key for which provider? [%s] (name, or "none" to skip): ' "$DEFAULT_CHOICE"
  else
    printf 'Set up a key for another provider? (name, or Enter to finish): '
  fi
  read -r CHOICE
  CHOICE="${CHOICE:-$DEFAULT_CHOICE}"
  case "$CHOICE" in
    ""|none|done) break ;;
  esac
  CHOICE="$(resolve_alias "$(echo "$CHOICE" | tr 'A-Z' 'a-z')")"
  if [ -z "$(key_env_for "$CHOICE")" ]; then
    warn "Unknown provider '$CHOICE' — valid: $PROVIDERS"
    continue
  fi
  configure_key "$CHOICE"
  if [ -z "$FIRST_CONFIGURED" ] && key_configured "$(key_env_for "$CHOICE")"; then
    FIRST_CONFIGURED="$CHOICE"
  fi
  DEFAULT_CHOICE=""
done

# rducky defaults to anthropic; if the user set up some other provider and
# has no Anthropic key, point the config at what they actually configured.
if [ -n "$FIRST_CONFIGURED" ] && [ "$FIRST_CONFIGURED" != "anthropic" ] \
   && ! key_configured ANTHROPIC_API_KEY; then
  CONFIG_FILE="${XDG_CONFIG_HOME:-$HOME/.config}/rducky/config.yaml"
  if [ -f "$CONFIG_FILE" ] && grep -qE '^[[:space:]]*provider:' "$CONFIG_FILE"; then
    :
  else
    printf 'rducky defaults to anthropic — make %s the default provider in %s? [Y/n] ' \
      "$FIRST_CONFIGURED" "$CONFIG_FILE"
    read -r YN
    case "$YN" in
      [Nn]*) ;;
      *)
        mkdir -p "$(dirname "$CONFIG_FILE")"
        printf 'provider: %s\n' "$FIRST_CONFIGURED" >> "$CONFIG_FILE"
        info "Set 'provider: $FIRST_CONFIGURED' in $CONFIG_FILE"
        ;;
    esac
  fi
fi

# --- 5. tmux keybinding ------------------------------------------------
info "Wiring up the tmux keybinding..."
"$BIN_PATH" install --write

if [ -n "${TMUX:-}" ]; then
  tmux source-file "$HOME/.tmux.conf"
  info "Reloaded tmux config."
  echo
  info "All set — press prefix + a to open rducky."
else
  echo
  info "All set. Open a new terminal (or 'source $RC_FILE'), start tmux, and press prefix + a."
fi
