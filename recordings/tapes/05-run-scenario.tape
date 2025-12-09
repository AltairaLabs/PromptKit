# VHS Tape: Run a Test Scenario
# Duration: ~30 seconds
# Output: 05-run-scenario.gif

Output "/recordings/gifs/05-run-scenario.gif"

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

Type "# Run the normal operations scenario"
Enter
Sleep 500ms

Type "promptarena run --scenario normal-operations"
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
Sleep 3s

Type "q"
Sleep 500ms

Type "echo '✅ All assertions passed!'"
Enter
Sleep 2s
