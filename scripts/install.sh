#!/usr/bin/env bash
# Reverie installer: builds the binary and wires it into Claude Code,
# Claude Desktop, and/or OpenCode on macOS / Linux.
#
# Defaults: Ollama provider with nomic-embed-text (no API keys required).
# Re-run safe -- config merges are idempotent; existing MCP servers are
# preserved. Existing config files are backed up before rewrite.
#
# Usage:
#   ./install.sh                  # build + install + configure detected clients
#   ./install.sh --skip-build     # skip go install (use the binary already on PATH)
#   ./install.sh --skip-ollama    # don't touch Ollama (assume already configured)
#   ./install.sh --skip-preamble  # don't touch CLAUDE.md / AGENTS.md preambles
#   ./install.sh --code-only      # configure Claude Code only
#   ./install.sh --desktop-only   # configure Claude Desktop only
#   ./install.sh --opencode-only  # configure OpenCode only
#   ./install.sh --uninstall      # remove the reverie entry from configured clients
#
# The preamble (scripts/reverie-preamble.md) is injected between
# `<!-- BEGIN reverie-preamble -->` markers into ~/.claude/CLAUDE.md
# (Claude Code) and ~/.config/opencode/AGENTS.md (OpenCode), and is
# printed to stdout at the end for Claude Desktop (paste into
# Settings → Profile → Personal Preferences).

set -uo pipefail

# --- styling ---
if [ -t 1 ]; then
    BOLD=$(tput bold 2>/dev/null || true)
    DIM=$(tput dim 2>/dev/null || true)
    GREEN=$(tput setaf 2 2>/dev/null || true)
    YELLOW=$(tput setaf 3 2>/dev/null || true)
    RED=$(tput setaf 1 2>/dev/null || true)
    RESET=$(tput sgr0 2>/dev/null || true)
else
    BOLD=""; DIM=""; GREEN=""; YELLOW=""; RED=""; RESET=""
fi

ok()    { printf "%s✓%s %s\n" "$GREEN" "$RESET" "$1"; }
info()  { printf "%s•%s %s\n" "$BOLD" "$RESET" "$1"; }
warn()  { printf "%s!%s %s\n" "$YELLOW" "$RESET" "$1" >&2; }
fail()  { printf "%s✗%s %s\n" "$RED"   "$RESET" "$1" >&2; exit 1; }

# --- args ---
DO_BUILD=1
DO_OLLAMA=1
DO_PREAMBLE=1
DO_CODE=1
DO_DESKTOP=1
DO_OPENCODE=1
UNINSTALL=0

for arg in "$@"; do
    case "$arg" in
        --skip-build)    DO_BUILD=0 ;;
        --skip-ollama)   DO_OLLAMA=0 ;;
        --skip-preamble) DO_PREAMBLE=0 ;;
        --code-only)     DO_DESKTOP=0; DO_OPENCODE=0 ;;
        --desktop-only)  DO_CODE=0; DO_OPENCODE=0 ;;
        --opencode-only) DO_CODE=0; DO_DESKTOP=0 ;;
        --uninstall)     UNINSTALL=1; DO_BUILD=0; DO_OLLAMA=0 ;;
        -h|--help)
            sed -n '2,23p' "$0" | sed 's/^# \{0,1\}//'
            exit 0 ;;
        *)
            fail "unknown argument: $arg (see --help)" ;;
    esac
done

# --- paths ---
OS="$(uname -s)"
case "$OS" in
    Darwin)
        DESKTOP_CONFIG="$HOME/Library/Application Support/Claude/claude_desktop_config.json"
        ;;
    Linux)
        DESKTOP_CONFIG="$HOME/.config/Claude/claude_desktop_config.json"
        ;;
    *)
        warn "unsupported OS: $OS — Desktop config path may be wrong"
        DESKTOP_CONFIG="$HOME/.config/Claude/claude_desktop_config.json"
        ;;
esac
CODE_CONFIG="$HOME/.claude/settings.json"
OPENCODE_CONFIG="$HOME/.config/opencode/opencode.json"

# Preamble targets (file-based instructions per client).
# Claude Desktop has no on-disk equivalent -- handled via print-at-end.
CLAUDE_MD="$HOME/.claude/CLAUDE.md"
OPENCODE_AGENTS_MD="$HOME/.config/opencode/AGENTS.md"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
PREAMBLE_FILE="$SCRIPT_DIR/reverie-preamble.md"

PREAMBLE_BEGIN='<!-- BEGIN reverie-preamble (managed by scripts/install.sh) -->'
PREAMBLE_END='<!-- END reverie-preamble -->'

# --- utilities ---
require_cmd() {
    command -v "$1" >/dev/null 2>&1 || fail "$1 not found in PATH${2:+ — $2}"
}

merge_config() {
    # Args: $1 = config path, $2 = binary path, $3 = client label (for messages)
    local cfg="$1"
    local bin="$2"
    local label="$3"

    if [ ! -e "$cfg" ]; then
        info "$label: creating $cfg"
        mkdir -p "$(dirname "$cfg")"
        printf '{\n  "mcpServers": {}\n}\n' > "$cfg"
    else
        local backup="$cfg.bak.$(date +%s)"
        cp "$cfg" "$backup"
        ok "$label: backed up existing config to $backup"
    fi

    local entry
    entry=$(jq -n --arg cmd "$bin" '{type:"stdio", command:$cmd, args:["serve"]}')

    local merged
    merged=$(jq --argjson entry "$entry" '
        .mcpServers = (.mcpServers // {}) |
        .mcpServers.reverie = $entry
    ' "$cfg") || fail "$label: jq merge failed"

    printf '%s\n' "$merged" > "$cfg"
    ok "$label: wired reverie into $cfg"
}

remove_config() {
    local cfg="$1"
    local label="$2"
    if [ ! -e "$cfg" ]; then
        info "$label: no config file at $cfg, nothing to remove"
        return
    fi
    if ! jq -e '.mcpServers.reverie' "$cfg" >/dev/null 2>&1; then
        info "$label: no reverie entry in $cfg"
        return
    fi
    local backup="$cfg.bak.$(date +%s)"
    cp "$cfg" "$backup"
    local stripped
    stripped=$(jq 'del(.mcpServers.reverie)' "$cfg") \
        || fail "$label: jq strip failed"
    printf '%s\n' "$stripped" > "$cfg"
    ok "$label: removed reverie entry (backup at $backup)"
}

add_claude_code_via_cli() {
    # Claude Code reads MCP servers from ~/.claude.json, not from
    # ~/.claude/settings.json. The canonical way to add a user-scope server
    # is `claude mcp add --scope user`, which writes to the correct file
    # and validates the entry.
    local bin="$1"
    local label="Claude Code"

    if ! command -v claude >/dev/null 2>&1; then
        warn "$label: 'claude' CLI not in PATH -- skipping"
        warn "$label: install Claude Code and re-run, or register manually:"
        warn "         claude mcp add --scope user reverie $bin serve"
        return
    fi

    # Clean up any stale entry under ~/.claude/settings.json left by older
    # versions of this installer that wrote MCP config to the wrong file.
    if [ -e "$CODE_CONFIG" ] && jq -e '.mcpServers.reverie' "$CODE_CONFIG" >/dev/null 2>&1; then
        local backup="$CODE_CONFIG.bak.$(date +%s)"
        cp "$CODE_CONFIG" "$backup"
        local stripped
        stripped=$(jq 'del(.mcpServers.reverie) | if (.mcpServers // {}) == {} then del(.mcpServers) else . end' "$CODE_CONFIG") \
            || fail "$label: jq strip of stale settings.json entry failed"
        printf '%s\n' "$stripped" > "$CODE_CONFIG"
        ok "$label: removed stale entry from $CODE_CONFIG (backup at $backup) -- MCP servers belong in ~/.claude.json"
    fi

    if claude mcp get reverie >/dev/null 2>&1; then
        info "$label: reverie already registered, replacing"
        claude mcp remove reverie >/dev/null 2>&1 \
            || warn "$label: 'claude mcp remove' returned non-zero, continuing"
    fi

    if claude mcp add --scope user reverie "$bin" serve >/dev/null 2>&1; then
        ok "$label: wired reverie via 'claude mcp add --scope user'"
    else
        fail "$label: 'claude mcp add' failed -- run it manually to see the error"
    fi
}

remove_claude_code_via_cli() {
    local label="Claude Code"

    # Strip any stale settings.json entry from older installers regardless of
    # whether the CLI is present.
    if [ -e "$CODE_CONFIG" ] && jq -e '.mcpServers.reverie' "$CODE_CONFIG" >/dev/null 2>&1; then
        local backup="$CODE_CONFIG.bak.$(date +%s)"
        cp "$CODE_CONFIG" "$backup"
        local stripped
        stripped=$(jq 'del(.mcpServers.reverie) | if (.mcpServers // {}) == {} then del(.mcpServers) else . end' "$CODE_CONFIG") \
            || fail "$label: jq strip of stale settings.json entry failed"
        printf '%s\n' "$stripped" > "$CODE_CONFIG"
        ok "$label: removed stale entry from $CODE_CONFIG (backup at $backup)"
    fi

    if ! command -v claude >/dev/null 2>&1; then
        info "$label: 'claude' CLI not in PATH, nothing more to remove"
        return
    fi
    if ! claude mcp get reverie >/dev/null 2>&1; then
        info "$label: no reverie entry registered with claude mcp"
        return
    fi
    if claude mcp remove reverie >/dev/null 2>&1; then
        ok "$label: removed reverie entry via 'claude mcp remove'"
    else
        warn "$label: 'claude mcp remove' returned non-zero"
    fi
}

merge_opencode_config() {
    # Args: $1 = config path, $2 = binary path, $3 = client label (for messages)
    # OpenCode uses a different schema -- `.mcp` (not `.mcpServers`) with
    # `command` as an array [executable, ...args] and a `type: "local"` tag.
    local cfg="$1"
    local bin="$2"
    local label="$3"

    if [ ! -e "$cfg" ]; then
        info "$label: creating $cfg"
        mkdir -p "$(dirname "$cfg")"
        printf '{\n  "mcp": {}\n}\n' > "$cfg"
    else
        local backup="$cfg.bak.$(date +%s)"
        cp "$cfg" "$backup"
        ok "$label: backed up existing config to $backup"
    fi

    local entry
    entry=$(jq -n --arg cmd "$bin" '{type:"local", command:[$cmd, "serve"], enabled:true}')

    local merged
    merged=$(jq --argjson entry "$entry" '
        .mcp = (.mcp // {}) |
        .mcp.reverie = $entry
    ' "$cfg") || fail "$label: jq merge failed"

    printf '%s\n' "$merged" > "$cfg"
    ok "$label: wired reverie into $cfg"
}

remove_opencode_config() {
    local cfg="$1"
    local label="$2"
    if [ ! -e "$cfg" ]; then
        info "$label: no config file at $cfg, nothing to remove"
        return
    fi
    if ! jq -e '.mcp.reverie' "$cfg" >/dev/null 2>&1; then
        info "$label: no reverie entry in $cfg"
        return
    fi
    local backup="$cfg.bak.$(date +%s)"
    cp "$cfg" "$backup"
    local stripped
    stripped=$(jq 'del(.mcp.reverie)' "$cfg") \
        || fail "$label: jq strip failed"
    printf '%s\n' "$stripped" > "$cfg"
    ok "$label: removed reverie entry (backup at $backup)"
}

# --- preamble helpers ---
# Inject scripts/reverie-preamble.md into a Markdown instructions file
# (CLAUDE.md for Claude Code, AGENTS.md for OpenCode) wrapped in
# managed BEGIN/END markers. Idempotent: if markers already exist, the
# block is replaced in place; otherwise it's appended. If a Reverie
# section exists without markers, the install is skipped so we don't
# duplicate hand-written content.
merge_preamble() {
    local target="$1"
    local label="$2"

    if [ ! -f "$PREAMBLE_FILE" ]; then
        warn "$label: preamble source not found at $PREAMBLE_FILE -- skipping"
        return
    fi

    if [ ! -e "$target" ]; then
        info "$label: creating $target"
        mkdir -p "$(dirname "$target")"
        : > "$target"
    fi

    # Build the wrapped block (markers + content) into a temp file so we
    # don't have to wrestle with shell-quoting multi-line markdown.
    local block
    block=$(mktemp -t reverie-preamble.XXXXXX) || fail "$label: mktemp failed"
    trap 'rm -f "$block"' RETURN
    {
        printf '%s\n' "$PREAMBLE_BEGIN"
        cat "$PREAMBLE_FILE"
        printf '%s\n' "$PREAMBLE_END"
    } > "$block"

    if grep -qF "$PREAMBLE_BEGIN" "$target"; then
        # Replace existing managed block in place.
        local backup="$target.bak.$(date +%s)"
        cp "$target" "$backup"
        local tmp
        tmp=$(mktemp -t reverie-target.XXXXXX) || fail "$label: mktemp failed"
        awk -v begin="$PREAMBLE_BEGIN" -v end="$PREAMBLE_END" -v blockfile="$block" '
            BEGIN { in_block = 0 }
            index($0, begin) == 1 {
                while ((getline line < blockfile) > 0) print line
                close(blockfile)
                in_block = 1
                next
            }
            index($0, end) == 1 { in_block = 0; next }
            !in_block { print }
        ' "$target" > "$tmp" && mv "$tmp" "$target"
        ok "$label: refreshed preamble in $target (backup at $backup)"
        return
    fi

    if grep -qE '^##[[:space:]]+Memory[[:space:]]*[—-][[:space:]]*Reverie' "$target"; then
        warn "$label: $target already contains an unmanaged '## Memory — Reverie' section"
        warn "$label: skipping preamble install -- wrap that section in $PREAMBLE_BEGIN / $PREAMBLE_END or delete it, then re-run"
        return
    fi

    # Append (with a leading blank line if the file is non-empty and
    # doesn't already end in one).
    local backup="$target.bak.$(date +%s)"
    cp "$target" "$backup"
    if [ -s "$target" ] && [ -n "$(tail -c 1 "$target")" ]; then
        printf '\n' >> "$target"
    fi
    if [ -s "$target" ]; then
        printf '\n' >> "$target"
    fi
    cat "$block" >> "$target"
    ok "$label: appended preamble to $target (backup at $backup)"
}

remove_preamble() {
    local target="$1"
    local label="$2"

    if [ ! -f "$target" ]; then
        info "$label: no $target to clean"
        return
    fi
    if ! grep -qF "$PREAMBLE_BEGIN" "$target"; then
        info "$label: no managed preamble in $target"
        return
    fi

    local backup="$target.bak.$(date +%s)"
    cp "$target" "$backup"
    local tmp
    tmp=$(mktemp -t reverie-target.XXXXXX) || fail "$label: mktemp failed"
    awk -v begin="$PREAMBLE_BEGIN" -v end="$PREAMBLE_END" '
        BEGIN { in_block = 0 }
        index($0, begin) == 1 { in_block = 1; next }
        index($0, end) == 1   { in_block = 0; next }
        !in_block { print }
    ' "$target" > "$tmp" && mv "$tmp" "$target"
    ok "$label: removed managed preamble from $target (backup at $backup)"
}

print_desktop_preamble() {
    if [ ! -f "$PREAMBLE_FILE" ]; then
        return
    fi
    echo
    echo "${BOLD}Claude Desktop preamble${RESET} (no on-disk equivalent of CLAUDE.md):"
    echo "${DIM}Paste the block below into Claude Desktop → Settings → Profile →"
    echo "  'What personal preferences should Claude consider in responses?'${RESET}"
    echo "${DIM}--- begin preamble ---${RESET}"
    cat "$PREAMBLE_FILE"
    echo "${DIM}--- end preamble ---${RESET}"
}

# --- preflight ---
require_cmd jq "install via brew install jq, apt install jq, or equivalent"

# --- uninstall path ---
if [ "$UNINSTALL" -eq 1 ]; then
    info "uninstall mode"
    [ "$DO_CODE"     -eq 1 ] && remove_claude_code_via_cli
    [ "$DO_DESKTOP"  -eq 1 ] && remove_config "$DESKTOP_CONFIG"          "Claude Desktop"
    [ "$DO_OPENCODE" -eq 1 ] && remove_opencode_config "$OPENCODE_CONFIG" "OpenCode"
    if [ "$DO_PREAMBLE" -eq 1 ]; then
        [ "$DO_CODE"     -eq 1 ] && remove_preamble "$CLAUDE_MD"          "Claude Code"
        [ "$DO_OPENCODE" -eq 1 ] && remove_preamble "$OPENCODE_AGENTS_MD" "OpenCode"
        [ "$DO_DESKTOP"  -eq 1 ] && info "Claude Desktop: preamble lives in Settings → Profile (no file) -- remove manually if desired"
    fi
    info "binary at $(command -v reverie 2>/dev/null || echo "<not on PATH>") left in place -- remove manually if desired"
    exit 0
fi

# --- preflight (install path) ---
if [ "$DO_BUILD" -eq 1 ]; then
    require_cmd go "install Go 1.26+ from https://go.dev/dl/"
    GO_VERSION=$(go version | awk '{print $3}' | sed 's/^go//')
    GO_MAJOR=${GO_VERSION%%.*}
    GO_MINOR=$(printf '%s' "$GO_VERSION" | awk -F. '{print $2}')
    if [ "$GO_MAJOR" -lt 1 ] || { [ "$GO_MAJOR" -eq 1 ] && [ "$GO_MINOR" -lt 26 ]; }; then
        fail "Go 1.26+ required, found $GO_VERSION"
    fi
    ok "Go $GO_VERSION"
fi

# --- Ollama check + model pull ---
if [ "$DO_OLLAMA" -eq 1 ]; then
    if curl -fsS --max-time 2 http://localhost:11434/api/tags >/dev/null 2>&1; then
        ok "Ollama running on :11434"
    else
        warn "Ollama not reachable on :11434 — start it (brew services start ollama, or ollama serve in another terminal)"
        warn "continuing — pull will be skipped"
        DO_OLLAMA=0
    fi
fi

if [ "$DO_OLLAMA" -eq 1 ]; then
    if curl -fsS --max-time 2 http://localhost:11434/api/tags 2>/dev/null \
        | jq -e '.models[]?.name | select(startswith("nomic-embed-text"))' >/dev/null 2>&1; then
        ok "nomic-embed-text already pulled"
    else
        info "pulling nomic-embed-text (one-time, ~270MB)"
        if ! ollama pull nomic-embed-text; then
            warn "ollama pull failed — install will continue but recall won't work until you pull manually"
        fi
    fi
fi

# --- build ---
if [ "$DO_BUILD" -eq 1 ]; then
    if [ -f "$REPO_DIR/go.mod" ] && [ -d "$REPO_DIR/cmd/reverie" ]; then
        info "go install from $REPO_DIR"
        ( cd "$REPO_DIR" && go install ./cmd/reverie ) || fail "go install failed"
    else
        info "go install github.com/diffsec/reverie/cmd/reverie@latest"
        go install github.com/diffsec/reverie/cmd/reverie@latest \
            || fail "go install from module path failed — clone the repo and re-run from there, or check network"
    fi
fi

# --- locate binary ---
BIN="$(command -v reverie || true)"
if [ -z "$BIN" ]; then
    GOPATH_BIN="$(go env GOPATH 2>/dev/null)/bin/reverie"
    [ -x "$GOPATH_BIN" ] && BIN="$GOPATH_BIN"
fi
[ -n "$BIN" ] || fail "reverie binary not found after install — check that \$(go env GOPATH)/bin is on PATH"
ok "binary: $BIN"

# --- configure clients ---
if [ "$DO_CODE" -eq 1 ]; then
    add_claude_code_via_cli "$BIN"
fi

if [ "$DO_DESKTOP" -eq 1 ]; then
    if [ -e "$DESKTOP_CONFIG" ] || [ -d "$(dirname "$DESKTOP_CONFIG")" ]; then
        merge_config "$DESKTOP_CONFIG" "$BIN" "Claude Desktop"
    else
        info "Claude Desktop config dir not detected -- skipping (use --desktop-only to force)"
    fi
fi

if [ "$DO_OPENCODE" -eq 1 ]; then
    if [ -e "$OPENCODE_CONFIG" ] || [ -d "$(dirname "$OPENCODE_CONFIG")" ]; then
        merge_opencode_config "$OPENCODE_CONFIG" "$BIN" "OpenCode"
    else
        info "OpenCode config dir not detected at ~/.config/opencode -- skipping (use --opencode-only to force)"
    fi
fi

# --- preamble (CLAUDE.md / AGENTS.md / Desktop print) ---
if [ "$DO_PREAMBLE" -eq 1 ]; then
    [ "$DO_CODE"     -eq 1 ] && merge_preamble "$CLAUDE_MD"          "Claude Code"
    [ "$DO_OPENCODE" -eq 1 ] && merge_preamble "$OPENCODE_AGENTS_MD" "OpenCode"
fi

# --- restart hints ---
echo
ok "install complete"
echo
echo "${BOLD}Next steps:${RESET}"
echo "  • Claude Code: type /exit and reopen, or restart the IDE."
echo "  • Claude Desktop: ${BOLD}fully quit${RESET} (Cmd-Q on macOS) and reopen -- closing the window is not enough."
echo "  • OpenCode: exit (Ctrl-C or :quit) and relaunch."
echo
echo "${DIM}Test: reverie status${RESET}"

if [ "$DO_PREAMBLE" -eq 1 ] && [ "$DO_DESKTOP" -eq 1 ]; then
    print_desktop_preamble
fi
