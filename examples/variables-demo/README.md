# Variables Demo

This example demonstrates how to **define and use variables** in PromptKit promptconfigs and override them when running scenarios.

## 🎯 What You'll Learn

1. How to define **optional variables** with defaults in promptconfigs
2. How to define **required variables** that must be provided
3. How to **override variables** in `arena.yaml`
4. How to create **multiple configurations** from one prompt template
5. How variables flow from promptconfig → arena.yaml → scenarios

---

## 📁 Directory Structure

```
variables-demo/
├── README.md                           # This file
├── arena.yaml                          # Arena config showing variable overrides
├── prompts/
│   ├── restaurant-bot.yaml            # Example with optional_vars
│   └── product-expert.yaml            # Example with required_vars
├── scenarios/
│   ├── restaurant-default.yaml        # Tests default values
│   ├── restaurant-custom.yaml         # Tests overridden values
│   ├── product-support.yaml           # Tests required variables
│   └── product-enterprise.yaml        # Tests multiple configs from one template
└── providers/
    └── openai-gpt4o-mini.yaml
```

---

## 🔧 How Variables Work

### Variable Resolution Priority

Variables are resolved in this order (highest to lowest priority):

1. **Arena-level `vars`** (in `arena.yaml` under `prompt_configs[].vars`)
2. **PromptConfig `optional_vars`** (defaults in the prompt file)
3. **Scenario context variables** (auto-extracted: `domain`, `user_role`, etc.)

### Variable Syntax

Use `{{variable_name}}` in your `system_template`:

```yaml
system_template: |
  You are a support agent for {{company_name}}.
  Contact us at {{support_email}}.
```

---

## 📝 Example 1: Optional Variables

**File:** `prompts/restaurant-bot.yaml`

```yaml
spec:
  optional_vars:
    restaurant_name: "TastyBites"        # Default value
    cuisine_type: "Italian"              # Default value
    opening_hours: "11 AM - 10 PM daily" # Default value

  system_template: |
    You are the AI assistant for {{restaurant_name}}, 
    a {{cuisine_type}} restaurant.
    Hours: {{opening_hours}}
```

### Using Defaults (No Override)

In `arena.yaml`:

```yaml
prompt_configs:
  - id: restaurant-default
    file: prompts/restaurant-bot.yaml
    # No vars specified = uses all defaults
```

**Result:** Uses "TastyBites", "Italian", "11 AM - 10 PM daily"

### Overriding Some Variables

In `arena.yaml`:

```yaml
prompt_configs:
  - id: restaurant-custom
    file: prompts/restaurant-bot.yaml
    vars:
      restaurant_name: "Sushi Haven"      # Override
      cuisine_type: "Japanese"            # Override
      opening_hours: "12 PM - 11 PM"      # Override
      # Other vars use defaults
```

**Result:** Uses "Sushi Haven", "Japanese", "12 PM - 11 PM" but keeps default phone number, policies, etc.

---

## 📝 Example 2: Overriding Variables

**File:** `prompts/product-expert.yaml`

```yaml
spec:
  # Optional variables with defaults
  optional_vars:
    company_name: "TechCo"
    product_name: "ProductX"
    support_email: "support@techco.com"
    support_hours: "24/7"
    warranty_period: "1 year"

  system_template: |
    You are a product expert for {{company_name}}, 
    specializing in {{product_name}}.
    Contact: {{support_email}}
    Support Hours: {{support_hours}}
    Warranty: {{warranty_period}}
```

### Overriding Variables in arena.yaml

In `arena.yaml`:

```yaml
prompt_configs:
  - id: product-support
    file: prompts/product-expert.yaml
    vars:
      # Override with specific values
      company_name: "TechStart Inc"
      product_name: "CloudSync Pro"
      support_email: "support@techstart.com"
      support_hours: "9 AM - 5 PM EST"
      warranty_period: "2 years"
      # return_window uses default
```

---

## 🎭 Example 3: Multiple Configs with Similar Templates

You can create **different prompt files** with similar templates but different task types:

```yaml
prompt_configs:
  # Standard product
  - id: product-support
    file: prompts/product-expert.yaml  # task_type: "product-support"
    vars:
      company_name: "TechStart Inc"
      product_name: "CloudSync Pro"
      support_email: "support@techstart.com"
      warranty_period: "2 years"

  # Enterprise product (similar template, different task_type)
  - id: product-support-enterprise
    file: prompts/product-expert-enterprise.yaml  # task_type: "product-support-enterprise"
    vars:
      company_name: "TechStart Inc"
      product_name: "CloudSync Enterprise"
      support_email: "enterprise@techstart.com"
      support_hours: "24/7 with dedicated account manager"
      warranty_period: "3 years with SLA"
```

**Important:** Each `task_type` must be unique! You can't reuse the same prompt file twice. Instead, create separate prompt files with different `task_type` values.

---

## 🧪 Running the Examples

### Prerequisites

Set your OpenAI API key (or use mock provider for testing):

```bash
export OPENAI_API_KEY="your-api-key-here"
```

### Run All Scenarios with Mock Provider (Recommended for Testing)

```bash
cd examples/variables-demo
promptarena run --config arena.yaml --mock-provider --mock-config mock-config.yaml --provider mock-provider
```

### Run with Real LLM Provider

```bash
# Run with OpenAI
promptarena run --config arena.yaml --provider openai-gpt-4o-mini

# Run all providers
promptarena run --config arena.yaml
```

### Run Specific Scenario

```bash
# Test default variables
promptarena run --config arena.yaml --mock-provider --mock-config mock-config.yaml --provider mock-provider --scenario restaurant-default

# Test overridden variables
promptarena run --config arena.yaml --mock-provider --mock-config mock-config.yaml --provider mock-provider --scenario restaurant-custom

# Test product support variables
promptarena run --config arena.yaml --mock-provider --mock-config mock-config.yaml --provider mock-provider --scenario product-support

# Test enterprise configuration
promptarena run --config arena.yaml --mock-provider --mock-config mock-config.yaml --provider mock-provider --scenario product-enterprise
```

### View Results

```bash
open out/report.html
```

---

## 🔍 What to Observe

### In `restaurant-default.yaml` scenario:
- ✅ Bot identifies as "TastyBites"
- ✅ Mentions Italian cuisine
- ✅ States hours as "11 AM - 10 PM daily"

### In `restaurant-custom.yaml` scenario:
- ✅ Bot identifies as "Sushi Haven" (overridden)
- ✅ Mentions Japanese cuisine (overridden)
- ✅ States hours as "12 PM - 11 PM, closed Mondays" (overridden)
- ✅ Still uses default phone number (not overridden)

### In `product-support.yaml` scenario:
- ✅ Mentions "TechStart Inc" and "CloudSync Pro" (required vars)
- ✅ States support hours as "9 AM - 5 PM EST" (overridden)
- ✅ Mentions 2-year warranty (overridden)
- ✅ Still uses 30-day return (default, not overridden)

### In `product-enterprise.yaml` scenario:
- ✅ Mentions "CloudSync Enterprise" (different product)
- ✅ States "24/7 with account manager" (premium support)
- ✅ Mentions 3-year warranty with SLA (enterprise features)
- ✅ States 60-day return window (extended)

---

## 💡 Best Practices

### 1. Use Optional Variables for Common Customizations

```yaml
optional_vars:
  company_name: "ACME Corp"     # Brand name
  support_email: "help@acme.com"
  support_hours: "24/7"
```

### 2. Use Required Variables for Critical Information

```yaml
required_vars:
  - api_key          # Must be provided
  - database_url     # Must be provided
  - region           # Must be provided
```

### 3. Document Your Variables

Add comments in your promptconfig:

```yaml
optional_vars:
  # Company branding
  company_name: "TechCo"
  
  # Contact information
  support_email: "support@techco.com"
  support_phone: "(555) 123-4567"
  
  # Business hours
  opening_hours: "9 AM - 5 PM EST"
```

### 4. Use Descriptive Variable Names

✅ Good:
- `restaurant_name`
- `cuisine_type`
- `cancellation_policy`

❌ Avoid:
- `name` (too generic)
- `type` (unclear)
- `policy1` (meaningless)

### 5. Each task_type Must Be Unique

**Important:** You cannot reference the same prompt file multiple times because each file defines a `task_type`, and all task types must be unique.

❌ **This won't work:**
```yaml
- id: brand-a
  file: prompts/support-bot.yaml  # task_type: "support"

- id: brand-b
  file: prompts/support-bot.yaml  # ERROR: duplicate task_type "support"
```

✅ **Instead, create separate files:**
```yaml
- id: brand-a
  file: prompts/support-bot-brand-a.yaml  # task_type: "support-brand-a"

- id: brand-b
  file: prompts/support-bot-brand-b.yaml  # task_type: "support-brand-b"
```

---

## 🚀 Next Steps

1. **Modify variables** in `arena.yaml` and re-run scenarios
2. **Add new variables** to the prompt templates
3. **Create new scenarios** that test different variable combinations
4. **Experiment with required vs optional** variables

---

## 📚 Related Documentation

- [Configuration Schema](../../docs/src/content/arena/reference/config-schema.md)
- [How to Write Scenarios](../../docs/src/content/arena/how-to/write-scenarios.md)
- [Template Variables](../../docs/src/content/concepts/templates.md)

---

## ❓ Common Questions

### Q: What happens if I don't provide a required variable?

A: The system will fail to load the prompt and show an error message indicating which required variables are missing.

### Q: Can I use variables in assertions?

A: Not directly. Variables are resolved in the prompt template, but assertions check the LLM's response content.

### Q: Can I use the same prompt file multiple times with different variables?

A: No, each prompt file has a unique `task_type`, and you can't have duplicate task types. If you need similar prompts with different variables, create separate prompt files with different `task_type` values (e.g., `product-support` and `product-support-enterprise`).

### Q: Can I override variables at runtime via CLI?

A: Currently, variables must be set in `arena.yaml`. Runtime CLI overrides are planned for future releases.

### Q: How do I pass environment variables?

A: Use `${ENV_VAR}` syntax in your YAML files (works for API keys, URLs, etc.):

```yaml
api_key: ${OPENAI_API_KEY}
```

### Q: What if I misspell a variable name?

A: The template will render with the placeholder intact (e.g., `{{misspeled_var}}`). Check your output for unexpected `{{...}}` patterns.
