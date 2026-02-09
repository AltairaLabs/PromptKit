---
title: '02: Multi-Prompt Packs'
sidebar:
  order: 2
---
Build a pack containing multiple prompts for a complete application.

## Learning Objectives

In this tutorial, you'll learn to:

- Create multiple prompt configurations
- Organize prompts by feature
- Compile all prompts into a single pack
- Reference prompts within the pack

## Time Required

**20 minutes**

## Prerequisites

- Completed [Tutorial 1: Your First Pack](01-first-pack)
- packc installed
- Basic understanding of prompt structure

## Scenario

You're building a customer service application that needs three types of prompts:

1. **Greeting** - Welcome customers
2. **Support** - Answer support questions
3. **Escalation** - Handle complex issues

## Step 1: Project Setup

Create a new project:

```bash
mkdir customer-service-pack
cd customer-service-pack
mkdir -p prompts config packs
```

## Step 2: Create Multiple Prompts

### Greeting Prompt

```bash
cat > prompts/greeting.yaml <<'EOF'
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: Customer Greeting
spec:
  task_type: greeting
  description: Welcome customers and identify their needs
  version: v1.0.0

  system_template: |
    You are a friendly customer service representative.
    Greet the customer warmly and ask how you can help them today.
    Customer: {{customer_name}}
    Previous interactions: {{previous_interactions}}

  template_engine:
    version: v1
    syntax: "{{variable}}"

  variables:
    - name: customer_name
      type: string
      required: true
    - name: previous_interactions
      type: string
      required: false
      default: "none"
EOF
```

### Support Prompt

```bash
cat > prompts/support.yaml <<'EOF'
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: Technical Support
spec:
  task_type: support
  description: Provide technical support and solutions
  version: v1.0.0

  system_template: |
    You are a knowledgeable technical support specialist.
    Help customers solve their technical issues with clear, step-by-step guidance.
    Always be patient and understanding.
    Customer: {{customer_name}}
    Issue: {{issue}}
    Product: {{product}}

  template_engine:
    version: v1
    syntax: "{{variable}}"

  variables:
    - name: customer_name
      type: string
      required: true
    - name: issue
      type: string
      required: true
    - name: product
      type: string
      required: true

  allowed_tools:
    - search_knowledge_base
    - create_ticket
EOF
```

### Escalation Prompt

```bash
cat > prompts/escalation.yaml <<'EOF'
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: Escalation Handler
spec:
  task_type: escalation
  description: Handle complex issues requiring escalation
  version: v1.0.0

  system_template: |
    You are a senior support specialist handling escalated issues.
    Document the issue thoroughly and prepare it for senior management review.
    Be empathetic and assure the customer their issue has high priority.
    Customer: {{customer_name}}
    Account Type: {{account_type}}
    Issue: {{issue}}
    Previous attempts: {{previous_attempts}}
    Urgency: {{urgency}}

  template_engine:
    version: v1
    syntax: "{{variable}}"

  variables:
    - name: customer_name
      type: string
      required: true
    - name: account_type
      type: string
      required: true
    - name: issue
      type: string
      required: true
    - name: previous_attempts
      type: string
      required: false
      default: "none"
    - name: urgency
      type: string
      required: true

  allowed_tools:
    - create_escalation_ticket
    - notify_manager
EOF
```

## Step 3: Create Arena Configuration

Create a configuration that includes all three prompts:

```bash
cat > config/arena.yaml <<'EOF'
prompts:
  - ../prompts/greeting.yaml
  - ../prompts/support.yaml
  - ../prompts/escalation.yaml

tools_directory: ./tools
EOF
```

## Step 4: Compile Multi-Prompt Pack

Compile all prompts into a single pack:

```bash
packc compile \
  --config config/arena.yaml \
  --output packs/customer-service.pack.json \
  --id customer-service
```

**Expected output:**

```
Loaded 3 prompt configs from memory repository
Compiling 3 prompts into pack 'customer-service'...
✓ Pack compiled successfully: packs/customer-service.pack.json
  Contains 3 prompts: [greeting, support, escalation]
```

## Step 5: Inspect the Pack

See all prompts in the pack:

```bash
packc inspect packs/customer-service.pack.json
```

**Key sections to notice:**

```
Prompts
=======
- ID: greeting
  Name: Customer Greeting
  ...

- ID: support
  Name: Technical Support
  Tools: [search_knowledge_base, create_ticket]
  ...

- ID: escalation
  Name: Escalation Handler
  Tools: [create_escalation_ticket, notify_manager]
  ...
```

## Step 6: Validate the Pack

Ensure all prompts are valid:

```bash
packc validate packs/customer-service.pack.json
```

## Understanding Multi-Prompt Packs

### Pack Structure

Your pack now contains:

```json
{
  "id": "customer-service",
  "version": "1.0.0",
  "prompts": {
    "greeting": { ... },
    "support": { ... },
    "escalation": { ... }
  }
}
```

### Using Multiple Prompts

In your application, you can select prompts by task type:

```go
// Load pack
pack, _ := prompt.LoadPack("./packs/customer-service.pack.json")

// Access greeting prompt
greetingPrompt := pack.GetPrompt("greeting")

// Access support prompt
supportPrompt := pack.GetPrompt("support")

// List all available prompts
promptTypes := pack.ListPrompts()
```

## Organizing Large Packs

### By Feature

```
prompts/
├── greeting/
│   ├── standard.yaml
│   └── vip.yaml
├── support/
│   ├── technical.yaml
│   └── billing.yaml
└── escalation/
    └── priority.yaml
```

### By Environment

```
config/
├── arena.dev.yaml      # Dev prompts
├── arena.staging.yaml  # Staging prompts
└── arena.prod.yaml     # Production prompts
```

## Try It Yourself

### 1. Add a Fourth Prompt

Create a follow-up prompt:

```bash
cat > prompts/followup.yaml <<'EOF'
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: Follow-up Check
spec:
  task_type: followup
  description: Check on customer satisfaction after support
  version: v1.0.0

  system_template: |
    You are conducting a satisfaction check after customer support.
    Ask if the issue was resolved and if they need any additional help.
    Customer: {{customer_name}}
    Resolution: {{resolution}}

  template_engine:
    version: v1
    syntax: "{{variable}}"

  variables:
    - name: customer_name
      type: string
      required: true
    - name: resolution
      type: string
      required: true
EOF
```

Update arena.yaml:

```yaml
prompts:
  - ../prompts/greeting.yaml
  - ../prompts/support.yaml
  - ../prompts/escalation.yaml
  - ../prompts/followup.yaml  # Add new prompt
```

Recompile:

```bash
packc compile --config config/arena.yaml --output packs/customer-service.pack.json --id customer-service
```

### 2. Create Environment-Specific Packs

```bash
# Development version (shorter responses)
cp config/arena.yaml config/arena.dev.yaml

# Production version (add more validation)
cp config/arena.yaml config/arena.prod.yaml

# Compile both
packc compile --config config/arena.dev.yaml --output packs/customer-service-dev.pack.json --id customer-service-dev
packc compile --config config/arena.prod.yaml --output packs/customer-service-prod.pack.json --id customer-service-prod
```

## Benefits of Multi-Prompt Packs

### 1. Single Distribution

One pack file contains all related prompts:

```bash
# Instead of:
app.load("greeting.pack.json")
app.load("support.pack.json")
app.load("escalation.pack.json")

# You have:
app.load("customer-service.pack.json")
```

### 2. Consistent Versioning

All prompts share the same version:

```bash
packc compile --config arena.yaml --output packs/customer-service-v1.2.0.pack.json --id customer-service
```

### 3. Simplified Deployment

Deploy one file instead of many:

```bash
# Single deployment
cp packs/customer-service.pack.json /deployment/packs/
```

## Best Practices

### 1. Related Prompts Together

Group prompts by application or feature:

```
✅ Good: customer-service.pack.json contains all customer service prompts
❌ Avoid: all-prompts.pack.json containing unrelated prompts
```

### 2. Consistent Naming

Use consistent task_type naming:

```yaml
# Good
task_type: customer-greeting
task_type: customer-support
task_type: customer-escalation

# Avoid
task_type: greeting
task_type: support_thing
task_type: esc
```

### 3. Document Prompt Relationships

```yaml
# arena.yaml with comments
prompts:
  - ../prompts/greeting.yaml      # Step 1: Initial contact
  - ../prompts/support.yaml       # Step 2: Problem solving
  - ../prompts/escalation.yaml    # Step 3: Complex issues
  - ../prompts/followup.yaml      # Step 4: Satisfaction check
```

## Next Steps

Continue learning about pack management:

- **[Tutorial 3: Validation Workflows](03-validation-workflow)** - Ensure pack quality
- **[How-To: Organize Packs](../how-to/organize-packs)** - Structure your packs
- **[Reference: Pack Format](../explanation/pack-format)** - Understanding pack structure

## Summary

You've learned to:

- ✅ Create multiple related prompts
- ✅ Compile them into a single pack
- ✅ Organize prompts by feature
- ✅ Understand multi-prompt pack structure
- ✅ Use prompts selectively in applications

Multi-prompt packs are ideal for:
- Complete applications
- Feature sets
- Workflow stages
- Related use cases

Great job! 🎉
