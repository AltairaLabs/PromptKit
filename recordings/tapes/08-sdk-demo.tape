# VHS Tape: SDK Demo with PackC
# Duration: ~25 seconds
# Output: 08-sdk-demo.gif

Output "/recordings/gifs/08-sdk-demo.gif"

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

Type "# Compile prompts into a portable pack file"
Enter
Sleep 500ms

Type "packc compile -c config.arena.yaml -o iot-demo.pack.json"
Enter
Sleep 2s

Type "# Run the SDK demo with the compiled pack"
Enter
Sleep 500ms

Type "cd sdk-demo && go run . ../iot-demo.pack.json troubleshooting 'PUMP-002 is vibrating heavily'"
Enter
Sleep 20s

Enter
Type "# 📦 Pack files are portable - deploy prompts anywhere!"
Enter
Sleep 2s
