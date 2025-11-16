---
layout: docs
title: "01: Your First Pack"
parent: Tutorials
grand_parent: PackC
nav_order: 1
---

# Tutorial 1: Your First Pack

Compile your first prompt pack in just 15 minutes.

## Learning Objectives

In this tutorial, you'll learn to:

- Install the packc compiler
- Create a basic prompt configuration
- Compile your first pack
- Validate the compiled pack
- Inspect pack contents

## Time Required

**15 minutes**

## Prerequisites

- Go 1.22 or higher installed
- Basic command-line knowledge
- A text editor

## Step 1: Install PackC

First, install the packc compiler:

```bash
# Install packc
go install github.com/AltairaLabs/PromptKit/tools/packc@latest

# Verify installation
packc version
```

**Expected output:**

```
packc v0.1.0
```

If packc isn't found, add Go's bin directory to your PATH:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

## Step 2: Create Project Structure

Create a directory for your first pack project:

```bash
# Create project directory
mkdir my-first-pack
cd my-first-pack

# Create subdirectories
mkdir -p prompts config packs
```

Your directory structure should look like:

```
my-first-pack/
├── prompts/     # Prompt YAML files
├── config/      # Arena configuration
└── packs/       # Compiled packs (output)
```

## Step 3: Create a Prompt

Create your first prompt file:

```bash
cat > prompts/greeting.yaml <<'EOF'
apiVersion: promptkit/v1
kind: PromptConfig
spec:
  task_type: greeting
  name: Greeting Assistant
  description: A friendly assistant that greets users
  
  system_prompt: |
    You are a friendly assistant. Greet the user warmly and ask how you can help.
  
  user_template: |
    User name: {{.name}}
    Time of day: {{.time_of_day}}
  
  template_engine: go
  
  parameters:
    temperature: 0.7
    max_tokens: 150
EOF
```

This creates a simple greeting prompt with template variables.

## Step 4: Create Arena Configuration

Create an arena.yaml configuration that references your prompt:

```bash
cat > config/arena.yaml <<'EOF'
prompts:
  - ../prompts/greeting.yaml

tools_directory: ./tools
EOF
```

## Step 5: Compile Your Pack

Now compile the prompt into a pack:

```bash
packc compile \
  --config config/arena.yaml \
  --output packs/greeting.pack.json \
  --id greeting
```

**Expected output:**

```
Loaded 1 prompt configs from memory repository
Compiling 1 prompts into pack 'greeting'...
✓ Pack compiled successfully: packs/greeting.pack.json
  Contains 1 prompts: [greeting]
```

**What happened:**

1. PackC read the arena.yaml configuration
2. Loaded the greeting.yaml prompt file
3. Compiled it into an optimized pack.json file
4. Saved it to packs/greeting.pack.json

## Step 6: Inspect Your Pack

Look at what was created:

```bash
# Check file size
ls -lh packs/greeting.pack.json

# Inspect pack contents
packc inspect packs/greeting.pack.json
```

**Expected output:**

```
Pack Information
================
ID: greeting
Name: greeting
Version: 1.0.0
Compiler Version: packc-v0.1.0

Template Engine
===============
Engine: go

Prompts
=======
- ID: greeting
  Name: Greeting Assistant
  Description: A friendly assistant that greets users
  
  System Prompt:
  You are a friendly assistant. Greet the user warmly and ask how you can help.
  
  User Template:
  User name: {{.name}}
  Time of day: {{.time_of_day}}
  
  Parameters:
  - temperature: 0.7
  - max_tokens: 150
```

## Step 7: Validate Your Pack

Ensure the pack is valid:

```bash
packc validate packs/greeting.pack.json
```

**Expected output:**

```
Validating pack: packs/greeting.pack.json
✓ Pack is valid
```

## Step 8: View the Pack JSON

Look at the compiled JSON (optional):

```bash
cat packs/greeting.pack.json
```

You'll see a structured JSON file containing your prompt in a format ready for the PromptKit SDK.

## What You Learned

Congratulations! You've successfully:

- ✅ Installed packc
- ✅ Created a prompt configuration
- ✅ Compiled your first pack
- ✅ Validated the pack
- ✅ Inspected pack contents

## Understanding the Pack

Your pack contains:

- **Prompt metadata** - Name, description, task type
- **System prompt** - Instructions for the AI
- **User template** - How to format user input with variables
- **Parameters** - Model settings (temperature, max_tokens)
- **Compiler metadata** - Version and compilation info

## Try It Yourself

Experiment with your pack:

### 1. Modify the Prompt

Edit `prompts/greeting.yaml` to change the greeting style:

```yaml
system_prompt: |
  You are a professional business assistant. Greet the user formally.
```

Then recompile:

```bash
packc compile --config config/arena.yaml --output packs/greeting.pack.json --id greeting
```

### 2. Add Parameters

Add more model parameters:

```yaml
parameters:
  temperature: 0.9
  max_tokens: 200
  top_p: 0.95
```

### 3. Inspect Changes

Check the updated pack:

```bash
packc inspect packs/greeting.pack.json
```

## Common Issues

### Issue: packc: command not found

**Solution:** Add Go bin to PATH:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

### Issue: arena.yaml not found

**Solution:** Ensure you're in the project directory:

```bash
cd my-first-pack
ls config/arena.yaml  # Should exist
```

### Issue: Invalid YAML syntax

**Solution:** Check indentation (use spaces, not tabs):

```bash
# Validate YAML
cat prompts/greeting.yaml
```

## Next Steps

Now that you've compiled your first pack, you're ready to:

- **[Tutorial 2: Multi-Prompt Packs](02-multi-prompt.md)** - Build packs with multiple prompts
- **[How-To: Compile Packs](../how-to/compile-packs.md)** - Learn more compilation patterns
- **[Reference: compile command](../reference/compile.md)** - Complete compile documentation

## Complete Code

Here's the complete prompt file for reference:

```yaml
# prompts/greeting.yaml
apiVersion: promptkit/v1
kind: PromptConfig
spec:
  task_type: greeting
  name: Greeting Assistant
  description: A friendly assistant that greets users
  
  system_prompt: |
    You are a friendly assistant. Greet the user warmly and ask how you can help.
  
  user_template: |
    User name: {{.name}}
    Time of day: {{.time_of_day}}
  
  template_engine: go
  
  parameters:
    temperature: 0.7
    max_tokens: 150
```

And the arena configuration:

```yaml
# config/arena.yaml
prompts:
  - ../prompts/greeting.yaml

tools_directory: ./tools
```

Congratulations on completing your first pack! 🎉
