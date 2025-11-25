# Schema Quick Start Guide

This guide helps you get started with PromptKit's JSON schema validation and discoverability features.

## 🚀 Quick Setup (VS Code)

1. **Add schema URLs to your config files:**

   ```yaml
   $schema: https://promptkit.altairalabs.ai/schemas/latest/arena.json
   apiVersion: promptkit.altairalabs.ai/v1alpha1
   kind: Arena
   metadata:
     name: my-arena
   spec:
     # Your configuration here
   ```

2. **Install the YAML extension** (if not already installed):
   - Search for "YAML" by Red Hat in VS Code extensions
   - Install and reload

3. **Enjoy IDE features:**
   - Press `Ctrl+Space` for autocomplete
   - Hover over fields for documentation
   - See validation errors in real-time

## 📁 File Naming Convention

Use typed file extensions for automatic schema detection:

```text
config.arena.yaml          # Arena configuration
openai.provider.yaml       # Provider config
support-test.scenario.yaml # Scenario config  
search.tool.yaml          # Tool definition
customer.persona.yaml     # Persona config
```

**Benefits:**
- IDEs automatically match schemas by filename
- Files are self-documenting
- Easy to find: `find . -name "*.provider.yaml"`

## 🔗 Available Schema URLs

### Latest (Recommended)

Use `/latest/` for automatic updates:

```yaml
$schema: https://promptkit.altairalabs.ai/schemas/latest/arena.json
$schema: https://promptkit.altairalabs.ai/schemas/latest/scenario.json
$schema: https://promptkit.altairalabs.ai/schemas/latest/provider.json
$schema: https://promptkit.altairalabs.ai/schemas/latest/tool.json
$schema: https://promptkit.altairalabs.ai/schemas/latest/persona.json
$schema: https://promptkit.altairalabs.ai/schemas/latest/promptconfig.json
```

### Versioned (For Stability)

Pin to specific version in production:

```yaml
$schema: https://promptkit.altairalabs.ai/schemas/v1alpha1/arena.json
```

## ✅ Command Line Validation

Validate configs before running:

```bash
# Auto-detect config type from 'kind' field
promptarena validate config.arena.yaml

# Schema-only validation (fast)
promptarena validate config.arena.yaml --schema-only

# Verbose error output
promptarena validate config.arena.yaml --verbose

# Validate specific type
promptarena validate myfile.yaml --type scenario
```

## 🔄 Migration from Old Format

### Update Arena Config

```bash
# Rename main config
mv arena.yaml config.arena.yaml

# Update schema URL inside file
sed -i '' 's|schemas/v1alpha1/|schemas/latest/|g' config.arena.yaml
```

### Rename Provider Files

```bash
cd providers/
for f in *.yaml; do
  mv "$f" "${f%.yaml}.provider.yaml"
done
```

### Rename Scenario Files

```bash
cd scenarios/
for f in *.yaml; do
  mv "$f" "${f%.yaml}.scenario.yaml"
done
```

### Update File References

In `config.arena.yaml`, update file paths:

```diff
 providers:
-  - file: providers/openai.yaml
+  - file: providers/openai.provider.yaml

 scenarios:
-  - file: scenarios/test.yaml
+  - file: scenarios/test.scenario.yaml
```

## 🎯 Example: Complete Arena Config

`config.arena.yaml`:

```yaml
$schema: https://promptkit.altairalabs.ai/schemas/latest/arena.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: customer-support-test

spec:
  providers:
    - file: providers/openai.provider.yaml
    - file: providers/claude.provider.yaml
  
  scenarios:
    - file: scenarios/basic-support.scenario.yaml
    - file: scenarios/billing-inquiry.scenario.yaml
  
  tools:
    - file: tools/search.tool.yaml
```

`providers/openai.provider.yaml`:

```yaml
$schema: https://promptkit.altairalabs.ai/schemas/latest/provider.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-gpt4

spec:
  id: "openai-gpt4"
  type: openai
  model: gpt-4
  defaults:
    temperature: 0.7
    max_tokens: 2000
```

`scenarios/basic-support.scenario.yaml`:

```yaml
$schema: https://promptkit.altairalabs.ai/schemas/latest/scenario.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: basic-support

spec:
  id: "basic-support"
  task_type: "assistant"
  description: "Basic customer support interaction"
  
  turns:
    - role: user
      content: "I need help with my account"
      assertions:
        - type: content_includes
          params:
            patterns: ["help", "assist", "support"]
```

## 🛠️ Troubleshooting

### IDE Not Showing Autocomplete

1. Check YAML extension is installed
2. Verify `$schema` field is at the top of file
3. Reload window: `Cmd+Shift+P` → "Developer: Reload Window"

### Schema Not Found (404)

- Check internet connection
- Verify schema URL is correct
- Try clearing browser cache

### Validation Errors

```bash
# See detailed error messages
promptarena validate config.arena.yaml --verbose

# Check against example
diff config.arena.yaml examples/assertions-test/config.arena.yaml
```

## 📚 Learn More

- [Schema Validation Reference](/reference/schema-validation/) - Detailed validation guide
- [Schema Versioning](/reference/schema-versioning/) - Version management details
- [Examples](/examples/) - See working examples with schemas

## 🎉 That's It!

You now have:
- ✅ Schema validation in your IDE
- ✅ Autocomplete and documentation
- ✅ Command-line validation
- ✅ File naming best practices

Happy configuring! 🚀
