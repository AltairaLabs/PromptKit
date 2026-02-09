---
title: '01: Your First Pack'
sidebar:
  order: 1
---

Create your first [PromptPack](https://promptpack.org)-compliant prompt package in 15 minutes.

## What You'll Build

A `.pack.json` file that conforms to the [PromptPack open standard](https://promptpack.org)—a vendor-neutral format that works with any AI framework, not just PromptKit.

## Learning Objectives

In this tutorial, you'll learn to:

- Install the PackC compiler
- Create a prompt source file
- Compile to [PromptPack](https://promptpack.org) format
- Validate against the specification
- Inspect pack contents

## Time Required

**15 minutes**

## Prerequisites

- Basic command-line knowledge
- A text editor

## Step 1: Install PackC

Choose your preferred installation method:

**Go Install**
```bash
go install github.com/AltairaLabs/PromptKit/tools/packc@latest
```

**Verify installation:**

```bash
packc version
```

**Expected output:**

```
packc v0.1.0
```

If packc isn't found after using Go install, add Go's bin directory to your PATH:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
source ~/.zshrc  # or ~/.bashrc
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
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: Greeting Assistant
spec:
  task_type: greeting
  description: A friendly assistant that greets users
  version: v1.0.0

  system_template: |
    You are a friendly assistant. Greet the user warmly and ask how you can help.
    User name: {{user_name}}
    Time of day: {{time_of_day}}

  template_engine:
    version: v1
    syntax: "{{variable}}"

  variables:
    - name: user_name
      type: string
      required: true
      description: The user's name
    - name: time_of_day
      type: string
      required: false
      default: morning
      description: Time of day for greeting
EOF
```

This creates a simple greeting prompt with template variables.

## Step 4: Create Arena Configuration

Create an arena.yaml configuration that references your prompt:

```bash
cat > config/arena.yaml <<'EOF'
prompt_configs:
  - id: greeting
    file: ../prompts/greeting.yaml
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
=== Pack Information ===
Pack: greeting
ID: greeting
Version: v1.0.0

Template Engine: v1 ({{variable}})

=== Prompts (1) ===

[greeting]
  Name: Greeting Assistant
  Description: A friendly assistant that greets users
  Version: v1.0.0
  Variables: 1 required, 1 optional
    Required: [user_name]

Compilation: packc-v0.1.0, 2025-01-15T10:30:00Z, Schema: v1
```

## Step 7: Validate Your Pack

Ensure the pack is valid:

```bash
packc validate packs/greeting.pack.json
```

**Expected output:**

```
Validating pack: packs/greeting.pack.json
Validating against PromptPack schema...
✓ Schema validation passed
✓ Pack structure is valid
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

- **Prompt metadata** - Name, description, task type, version
- **System template** - Instructions for the AI with template variables
- **Variables** - Defined template variables with types and defaults
- **Template engine** - Template engine configuration (version and syntax)
- **Compilation info** - Compiler version, timestamp, and schema version

## Try It Yourself

Experiment with your pack:

### 1. Modify the Prompt

Edit `prompts/greeting.yaml` to change the greeting style:

```yaml
system_template: |
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

- **[Tutorial 2: Multi-Prompt Packs](02-multi-prompt)** - Build packs with multiple prompts
- **[How-To: Compile Packs](../how-to/compile-packs)** - Learn more compilation patterns
- **[Reference: compile command](../reference/compile)** - Complete compile documentation

## Complete Code

Here's the complete prompt file for reference:

```yaml
# prompts/greeting.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: Greeting Assistant
spec:
  task_type: greeting
  description: A friendly assistant that greets users
  version: v1.0.0

  system_template: |
    You are a friendly assistant. Greet the user warmly and ask how you can help.
    User name: {{user_name}}
    Time of day: {{time_of_day}}

  template_engine:
    version: v1
    syntax: "{{variable}}"

  variables:
    - name: user_name
      type: string
      required: true
      description: The user's name
    - name: time_of_day
      type: string
      required: false
      default: morning
      description: Time of day for greeting
```

And the arena configuration:

```yaml
# config/arena.yaml
prompt_configs:
  - id: greeting
    file: ../prompts/greeting.yaml
```

Congratulations on completing your first pack!

## What You've Achieved

You've created a [PromptPack](https://promptpack.org)-compliant package that:

- **Is portable** — Works with any PromptPack-compatible runtime, not just PromptKit
- **Is validated** — Conforms to the open specification
- **Is production-ready** — Can be versioned, deployed, and shared

Learn more about the PromptPack standard at [promptpack.org](https://promptpack.org).
