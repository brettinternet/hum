# Regenerate from the repository root:
#   task cli:build
#   mise exec ffmpeg@8.1.1 vhs@0.11.0 -- vhs docs/demo-up-down.tape
# VHS also requires ttyd and trash on PATH.

Output docs/demo-up-down.gif

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
Type "export PATH=$PWD/bin:$PATH; export PS1='hum@demo % '"
Enter
Type "export HUM_RUNTIME_DIR=$(mktemp -d); export DEMO_DIR=$(mktemp -d); cd $DEMO_DIR"
Enter
Type "cat > hum.yaml <<'YAML'"
Enter
Type "version: 1"
Enter
Type "processes:"
Enter
Type "  db:"
Enter
Type "    argv: [sh, -c, 'echo database-ready; while :; do sleep 1; done']"
Enter
Type "    ready:"
Enter
Type "      match: database-ready"
Enter
Type "  api:"
Enter
Type "    argv: [sh, -c, 'echo API-listening; while :; do sleep 1; done']"
Enter
Type "    after: [db]"
Enter
Type "    ready:"
Enter
Type "      match: API-listening"
Enter
Type "  worker:"
Enter
Type "    argv: [sh, -c, 'n=$(cat .attempt 2>/dev/null || echo 0); n=$((n+1)); echo $n > .attempt; if [ $n -eq 1 ]; then echo job-failed-retrying; exit 1; fi; echo job-recovered; while :; do sleep 1; done']"
Enter
Type "    after: [api]"
Enter
Type "    restart: on-failure"
Enter
Type "YAML"
Enter
Type "clear"
Enter
Show

Type "hum up --detach"
Enter
Sleep 3s

Type "hum status"
Enter
Sleep 3s

Type "hum logs worker --tail 10"
Enter
Sleep 4s

Type "hum down"
Enter
Sleep 3s

Hide
Type "cd /; trash $DEMO_DIR $HUM_RUNTIME_DIR"
Enter
