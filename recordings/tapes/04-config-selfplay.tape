# VHS Tape: Deep Dive into Self-Play Config
# Duration: ~20 seconds
# Output: 04-config-selfplay.gif

Output "/recordings/gifs/04-config-selfplay.gif"

Set FontFamily "JetBrainsMono Nerd Font Mono"
Set FontSize 14
Set Width 1000
Set Height 600
Set Theme "Dracula"
Set TypingSpeed 40ms
Set Padding 20

Hide
Type "cd /demo/my-demo-project && clear"
Enter
Sleep 500ms
Show

Type "# View self-play personas for red-team testing"
Enter
Sleep 500ms

Type "promptarena config-inspect --section selfplay -v"
Enter
Sleep 3s

Type "echo ''"
Enter
Type "echo '🎭 Personas define how simulated users behave'"
Enter
Sleep 1s
