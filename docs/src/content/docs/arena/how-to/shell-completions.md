---
title: Configure Shell Completions
---
Enable tab completion for `promptarena` and `packc` commands in your shell. Shell completions provide intelligent suggestions for commands, flags, and even dynamic values like scenario names and providers.

## Features

PromptKit's shell completions include:

- **Command and subcommand completion** - Tab through available commands
- **Flag completion** - Complete flag names with `--` prefix
- **Dynamic value completion** - Context-aware suggestions for:
  - `--scenario` - Lists scenarios from your `config.arena.yaml`
  - `--provider` - Lists configured providers
  - `--role` - Lists self-play role configurations
  - `--region` - Suggests prompt regions
  - `--format` - Output format options (json, junit, html, markdown)
  - `--section` - Config sections for `config-inspect`
  - `--template` - Available project templates for `init`

## Bash

### Generate and Install

```bash
# Create completions directory if needed
mkdir -p ~/.local/share/bash-completion/completions

# Generate completion scripts
promptarena completion bash > ~/.local/share/bash-completion/completions/promptarena
packc completion bash > ~/.local/share/bash-completion/completions/packc

# Reload your shell or source the files
source ~/.local/share/bash-completion/completions/promptarena
source ~/.local/share/bash-completion/completions/packc
```

### Alternative: System-wide Installation

```bash
# Requires sudo
sudo promptarena completion bash > /etc/bash_completion.d/promptarena
sudo packc completion bash > /etc/bash_completion.d/packc
```

### Add to .bashrc (Permanent)

Add to your `~/.bashrc`:

```bash
# PromptKit completions
if command -v promptarena &> /dev/null; then
    source <(promptarena completion bash)
fi
if command -v packc &> /dev/null; then
    source <(packc completion bash)
fi
```

## Zsh

### Generate and Install

```bash
# Create completions directory
mkdir -p ~/.zsh/completions

# Generate completion scripts
promptarena completion zsh > ~/.zsh/completions/_promptarena
packc completion zsh > ~/.zsh/completions/_packc
```

### Add to .zshrc

Add to your `~/.zshrc` (before `compinit`):

```zsh
# Add custom completions directory to fpath
fpath=(~/.zsh/completions $fpath)

# Initialize completions
autoload -Uz compinit && compinit
```

### Alternative: Oh My Zsh

If using Oh My Zsh, place completions in the custom completions directory:

```bash
promptarena completion zsh > ${ZSH_CUSTOM:-~/.oh-my-zsh/custom}/plugins/promptarena/_promptarena
packc completion zsh > ${ZSH_CUSTOM:-~/.oh-my-zsh/custom}/plugins/packc/_packc
```

## Fish

```bash
# Generate completion scripts
promptarena completion fish > ~/.config/fish/completions/promptarena.fish
packc completion fish > ~/.config/fish/completions/packc.fish
```

Fish will automatically load completions from this directory.

## PowerShell

### Generate and Load

```powershell
# Generate completion scripts
promptarena completion powershell > promptarena.ps1
packc completion powershell > packc.ps1

# Load completions (current session)
. ./promptarena.ps1
. ./packc.ps1
```

### Add to Profile (Permanent)

Add to your PowerShell profile (`$PROFILE`):

```powershell
# PromptKit completions
promptarena completion powershell | Out-String | Invoke-Expression
packc completion powershell | Out-String | Invoke-Expression
```

## Docker

If you're using the demo Docker image, completions are pre-configured. For custom Docker images, add to your Dockerfile:

```dockerfile
# Install bash-completion
RUN apk add --no-cache bash-completion

# Generate and install completions
RUN mkdir -p /etc/bash_completion.d && \
    promptarena completion bash > /etc/bash_completion.d/promptarena && \
    packc completion bash > /etc/bash_completion.d/packc

# Source completions in bashrc
RUN echo 'source /etc/profile.d/bash_completion.sh' >> /root/.bashrc && \
    echo 'source /etc/bash_completion.d/promptarena' >> /root/.bashrc && \
    echo 'source /etc/bash_completion.d/packc' >> /root/.bashrc
```

## Using Completions

Once configured, use Tab to complete:

```bash
# Complete commands
promptarena r<Tab>        # → promptarena run

# Complete flags  
promptarena run --sc<Tab> # → promptarena run --scenario

# Complete dynamic values (reads from config.arena.yaml)
promptarena run --scenario <Tab>
# → basic-chat  edge-cases  multi-turn  ...

promptarena run --provider <Tab>
# → openai  anthropic  google  mock

promptarena config-inspect --section <Tab>
# → prompts  providers  scenarios  tools  selfplay  judges  defaults  validation

promptarena init --template <Tab>
# → quick-start  customer-support  code-assistant  multimodal  mcp-integration
```

## Troubleshooting

### Completions Not Working

1. **Verify the binary is in PATH:**
   ```bash
   which promptarena
   ```

2. **Check completion script was generated:**
   ```bash
   promptarena completion bash | head -20
   ```

3. **Ensure bash-completion is installed:**
   ```bash
   # macOS
   brew install bash-completion@2
   
   # Ubuntu/Debian
   apt install bash-completion
   
   # Alpine
   apk add bash-completion
   ```

4. **Reload your shell:**
   ```bash
   exec $SHELL
   ```

### Dynamic Completions Not Showing Values

Dynamic completions (scenarios, providers, roles) require a valid `config.arena.yaml` in the current directory or parent directories. If no config is found, completions will return empty results.

```bash
# Check if config is found
cd /path/to/your/project
ls config.arena.yaml

# Test completion
promptarena __complete run --scenario ""
```

## See Also

- [Install PromptArena](installation) - Installation options
- [CLI Commands Reference](/arena/reference/cli-commands) - Full command documentation
