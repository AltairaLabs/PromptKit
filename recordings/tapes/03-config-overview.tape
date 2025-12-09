# VHS Tape: Inspect Configuration
# Duration: ~25 seconds
# Output: 03-config-overview.gif

Output "/recordings/gifs/03-config-overview.gif"

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

Type "# Explore the project configuration"
Enter
Sleep 500ms

Type "promptarena config-inspect"
Enter
Sleep 2s

Enter
Type "# 📋 Shows: prompts, providers, scenarios, tools, personas'"
Enter
Sleep 1s
