# VHS Tape: Red-Team Security Testing
# Duration: ~35 seconds
# Output: 06-redteam-test.gif

Output "/recordings/gifs/06-redteam-test.gif"

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

Type "# Run adversarial red-team self-play test"
Enter
Sleep 500ms

Type "# This tests if the AI leaks data across tenants"
Enter
Sleep 1s

Type "promptarena run --scenario redteam-selfplay"
Enter
Sleep 2s

Enter #to select the run
Sleep 2s
Down
Sleep 2s
Down
Sleep 2s
Down
Sleep 2s
Down
Sleep 2s
Down
Sleep 2s
Down
Sleep 2s
Down
Escape
Sleep 10s

Type "q"
Sleep 500ms

Type "# 🛡️ Assertions passed. Agent blocked cross-tenant data access!"
Enter
Sleep 2s
