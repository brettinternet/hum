# Regenerate from the repository root:
#   task cli:build
#   mise exec ffmpeg@8.1.1 vhs@0.11.0 -- vhs docs/demo.tape
# VHS also requires ttyd and trash on PATH.

Output docs/demo.gif

Require trash

Set Shell "bash"
Set FontSize 24
Set Width 1200
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
Type "clear"
Enter
Show

Type "hum run clock -- sh -c 'i=1; while true; do echo tick-$i; i=$((i + 1)); sleep 1; done'"
Enter
Sleep 4s
Ctrl+C
Sleep 1s

Type "hum status clock"
Enter
Sleep 2s

Type "hum logs clock --tail 3"
Enter
Sleep 2s

Type "hum stop clock"
Enter
Sleep 2s

Type "hum status clock"
Enter
Sleep 3s

Hide
Type "hum down >/dev/null 2>&1; cd /; trash $DEMO_DIR $HUM_RUNTIME_DIR"
Enter
