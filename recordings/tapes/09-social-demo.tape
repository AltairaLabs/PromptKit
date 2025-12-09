# VHS Tape: PromptKit Complete Demo (Social Media)
# Duration: ~90 seconds
# Output: MP4 for LinkedIn, Twitter, etc.
# Combines: init, config-inspect, run scenario, red-team, view, SDK

Output "/recordings/videos/promptkit-demo.mp4"

Set FontFamily "JetBrainsMono Nerd Font Mono"
Set FontSize 16
Set Width 1280
Set Height 720
Set Theme "Dracula"
Set TypingSpeed 30ms
Set Padding 20
Set Framerate 30

# === SECTION 1: Install & Initialize ===
Hide
Type "clear"
Enter
Sleep 300ms
Show

Type "# PromptKit: Test & Deploy agentic assistants with confidence"
Enter
Sleep 1s

Type "# 1️⃣  Create a new project from a template"
Enter
Sleep 500ms

Type "promptarena templates list"
Enter
Sleep 2s

Type "promptarena init my-iot-demo --template community/iot-maintenance-demo"
Enter
Sleep 2s
Enter
Sleep 300ms
Enter
Sleep 300ms
Enter
Sleep 300ms
Enter
Sleep 4s

# === SECTION 2: Inspect Configuration ===
Hide
Type "cd my-demo-project && clear"
Enter
Sleep 300ms
Show

Type "# 2️⃣  Inspect your prompt configuration"
Enter
Sleep 500ms

Type "promptarena config-inspect"
Enter
Sleep 4s

# === SECTION 3: Run a Test Scenario ===
Hide
Type "clear"
Enter
Sleep 300ms
Show

Type "# 3️⃣  Run automated multi-turn test scenarios"
Enter
Sleep 500ms

Type "# Using LLM providers to simulate a real users"
Enter

Type "promptarena run --scenario hardware-faults"
Enter
Sleep 15s

Type "q"
Sleep 500ms

# === SECTION 4: Red-Team Security Testing ===
Hide
Type "clear"
Enter
Sleep 300ms
Show

Type "# 4️⃣  Red-team testing with adversarial personas"
Enter
Sleep 500ms

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
Sleep 2s

# === SECTION 5: View Results ===
Hide
Type "clear"
Enter
Sleep 300ms
Show

Type "# 5️⃣  Drill into conversation details"
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
Sleep 300ms

Enter
Type "# Results can be output as html, markdown, JSON and JUnit for use in CI Pipelines"
Enter
Sleep 3s


# === SECTION 6: Compile & Deploy with SDK ===
Hide
Type "cd .. && clear"
Enter
Sleep 300ms
Show

Type "# 6️⃣  Compile prompts for production deployment"
Enter
Enter
Sleep 500ms

Type "packc compile -c config.arena.yaml -o iot-demo.pack.json"
Enter
Sleep 2s

Enter
Type "# Since compiled packs are well-defined JSON, they can be run with any common framework"
Enter
Sleep 3s

Type "# Run with the PromptKit Go SDK"
Enter
Sleep 500ms

Type "cd sdk-demo && go run . ../iot-demo.pack.json troubleshooting 'PUMP-002 is vibrating heavily'"
Enter
Sleep 20s

# === CLOSING ===
Hide
Type "cd .. && clear"
Enter
Sleep 300ms
Show

Type "# ✨ PromptKit: From prototype to production"
Enter
Sleep 500ms
Type "# 📦 github.com/AltairaLabs/PromptKit"
Enter
Sleep 3s
