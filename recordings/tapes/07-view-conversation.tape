# VHS Tape: View Conversation Details
# Duration: ~25 seconds
# Output: 07-view-conversation.gif

Output "/recordings/gifs/07-view-conversation.gif"

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

Type "# View past test results and drill into conversations"
Enter
Sleep 500ms

Type "cd out"
Enter
Sleep 500ms

Type "promptarena view"
Enter
Sleep 2s

# Navigate to first result
Down
Sleep 500ms
Down
Sleep 500ms
Enter
Sleep 2s

# Scroll through conversation
Down
Sleep 1s
Down
Sleep 1s
Down
Sleep 1s
Down
Sleep 1s

Type "q"
Sleep 500ms

Type "# 💬 See tool calls, arguments, and responses"
Enter
Sleep 1s
