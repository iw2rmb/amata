# claude-wrapper.sh

Reliable non-interactive `claude -p` wrapper with auth/state checks and timeout.

## Problem

Direct `claude -p` invocation intermittently:
- Reports missing prompt input
- Hangs without output

## Setup

```bash
chmod +x ~/\@iw2rmb/auto/claude-wrapper.sh
ln -sf ~/\@iw2rmb/auto/claude-wrapper.sh ~/bin/claude-wrapper
```

## Usage

```bash
# Prompt as arguments
claude-wrapper "What is 2+2?"

# Prompt from stdin
echo "Summarize this" | claude-wrapper

# Custom timeout (seconds)
CLAUDE_TIMEOUT=300 claude-wrapper "Long task..."
```

## What it does

1. **PATH check** — exits if `claude` binary not found
2. **Auth check** — runs `claude auth status`, exits early if not logged in
3. **Prompt source** — accepts args or piped stdin, rejects empty input
4. **Timeout** — wraps call with `timeout` (default 120s, override via `CLAUDE_TIMEOUT`)
5. **Stderr suppressed** — hides progress/spinner noise that confuses piped workflows

## Troubleshooting

If hangs persist despite the wrapper, force no-TTY on stdin:

```bash
echo "" | timeout 120 claude -p "prompt"
```

This prevents claude from waiting on interactive input.
