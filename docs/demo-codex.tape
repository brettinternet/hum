# Regenerate from the repository root:
#   task cli:build
#   mise exec ffmpeg@8.1.1 vhs@0.11.0 -- vhs docs/demo-codex.tape
# VHS also requires codex, tmux, ttyd, and trash on PATH.

Output docs/demo-codex.gif

Require codex
Require tmux
Require trash

Set Shell "bash"
Set FontSize 21
Set Width 1400
Set Height 650
Set Padding 24
Set Margin 16
Set MarginFill "#11111b"
Set BorderRadius 10
Set WindowBar "ColorfulRight"
Set Theme "Catppuccin Mocha"
Set TypingSpeed 30ms
Set Framerate 20

Hide
Type "export PATH=$PWD/bin:$PATH; export PS1='hum@demo % '"
Enter
Type "export DEMO_DIR=$(mktemp -d); export HUM_RUNTIME_DIR=$DEMO_DIR/runtime; mkdir -p $HUM_RUNTIME_DIR; cd $DEMO_DIR"
Enter
Type "cat > fail.sh <<'SH'"
Enter
Type "#!/bin/sh"
Enter
Type "echo DATABASE_URL_missing >&2"
Enter
Type "exit 1"
Enter
Type "SH"
Enter
Type "cat > mcp-hum <<'SH'"
Enter
Type "#!/bin/sh"
Enter
Type "exec hum --runtime-dir $PWD/runtime mcp"
Enter
Type "SH"
Enter
Type "cat > ask-codex <<'SH'"
Enter
Type "#!/bin/sh"
Enter
Type "exec codex exec --skip-git-repo-check --ephemeral --ignore-user-config --color never --dangerously-bypass-approvals-and-sandbox -c 'mcp_servers.hum.command=./mcp-hum' -c 'mcp_servers.hum.args=[]' 'Use only the hum MCP tools to read logs for the failed process named broken in the current project. If the logs support it, reply exactly: broken failed because DATABASE_URL was unset.' 2>/dev/null"
Enter
Type "SH"
Enter
Type "chmod +x fail.sh mcp-hum ask-codex"
Enter
Type "tmux -f /dev/null new-session -s hum-codex-demo bash --noprofile --norc"
Enter
Sleep 1s
Type "tmux set-option status off; tmux set-option default-command 'bash --noprofile --norc'; clear"
Enter
Show

Type "hum run broken -d -- ./fail.sh"
Enter
Sleep 3s

Ctrl+B
Type "%"
Sleep 1s
Type "./ask-codex 'Why did broken fail?'"
Enter
Sleep 2s
Hide
Wait+Screen@60s /failed because/
Show
Sleep 4s

Hide
Ctrl+B
Type ":kill-session"
Enter
Sleep 1s
Type "hum remove broken >/dev/null 2>&1; cd /; trash $DEMO_DIR"
Enter
