# VHS Tape: Initialize a New Project
# Duration: ~20 seconds
# Output: 02-init-project.gif

Output "/recordings/gifs/02-init-project.gif"

Set FontFamily "JetBrainsMono Nerd Font Mono"
Set FontSize 14
Set Width 1000
Set Height 600
Set Theme "Dracula"
Set TypingSpeed 40ms
Set Padding 20

Type "# Create a new project from template"
Enter
Sleep 500ms

Type "# List available templates"
Enter
Enter
Sleep 500ms

Type "promptarena templates list"
Enter
Enter
Sleep 2s

Type "# Build an IoT maintenance assistant. Sounds useful"
Enter
Enter
Sleep 500ms

Type "promptarena init my-iot-demo --template community/iot-maintenance-demo"
Enter
Sleep 2s

Enter
Sleep 500ms

Enter
Sleep 500ms

Enter
Sleep 500ms

Enter
Sleep 5s

Type "cd my-iot-demo && ls -la"
Enter
Sleep 1500ms

Type "# ✅ Project scaffolded with prompts, tools, and scenarios"
Enter
Sleep 1s
