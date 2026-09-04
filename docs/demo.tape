# Regenerate from the repository root:
#   task cli:build
#   mise exec ffmpeg@8.1.1 vhs@0.11.0 -- vhs docs/demo.tape
# VHS also requires tmux, ttyd, and trash on PATH.

Output docs/demo.gif

Require tmux
Require trash

Set Shell "bash"
Set FontSize 22
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
Type "export PATH=$PWD/bin:$PATH"
Enter
Type "export HUM_RUNTIME_DIR=$(mktemp -d); export DEMO_DIR=$(mktemp -d); cd $DEMO_DIR"
Enter
Type "echo '#!/bin/sh' > clock.sh"
Enter
Type "echo 'while :; do date +%T; sleep 1; done' >> clock.sh; chmod +x clock.sh"
Enter
Type "tmux -f /dev/null new-session -s hum-demo"
Enter
Sleep 1s
Type "clear"
Enter
Show

Type "hum run clock -- ./clock.sh"
Enter
Sleep 4s

Ctrl+B
Type "%"
Sleep 1s
Type "hum logs clock --follow"
Enter
Sleep 7s

Hide
Ctrl+B
Type ":kill-session"
Enter
Sleep 1s
Type "hum down >/dev/null 2>&1; cd /; trash $DEMO_DIR $HUM_RUNTIME_DIR"
Enter
