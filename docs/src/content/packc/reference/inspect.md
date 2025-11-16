---
title: inspect
docType: reference
order: 4
---
# packc inspect

Display detailed information about a compiled pack file.

## Synopsis

```bash
packc inspect <pack-file>
```

## Description

The `inspect` command displays comprehensive information about a pack's structure, contents, and metadata. Use this to understand what's inside a pack without manually parsing the JSON.

## Arguments

**`<pack-file>`** (required)
- Path to the pack file to inspect
- Must be a `.pack.json` file

## Examples

### Basic Inspection

```bash
packc inspect packs/app.pack.json
```

**Output:**

```
=== Pack Information ===
Pack: Customer Support Assistant
ID: customer-support
Version: 1.0.0
Description: AI-powered customer support system

Template Engine: go

=== Prompts (3) ===

1. customer-support
   System: You are a helpful customer support agent...
   Variables: customer_name, order_id, issue_type
   Tools: search_kb, create_ticket
   Parameters: temperature=0.7, max_tokens=1000

2. sales-assistant
   System: You are a sales assistant...
   Variables: product_name, price
   Tools: search_products, get_inventory
   Parameters: temperature=0.8, max_tokens=800

3. technical-expert
   System: You are a technical expert...
   Variables: product_id, error_code
   Tools: search_docs, run_diagnostics
   Parameters: temperature=0.5, max_tokens=1500

Shared Fragments (2): greeting, signature

Pack Metadata:
  Domain: customer-service
  Language: en
  Tags: [support, production]

Compilation: packc-v0.1.0, 2024-01-15T10:30:00Z, Schema: 1.0
```

### Pipe to File

```bash
packc inspect packs/app.pack.json > pack-info.txt
```

### Inspect Multiple Packs

```bash
for pack in packs/*.pack.json; do
  echo "=== $pack ==="
  packc inspect "$pack"
  echo ""
done
```

## Output Sections

### Pack Information

Basic pack metadata:

```
Pack: Name of the pack
ID: unique-identifier
Version: 1.0.0
Description: Pack description
```

### Template Engine

Template system used:

```
Template Engine: go
```

Currently only `go` templates are supported.

### Prompts

Detailed information for each prompt:

```
1. prompt-name
   System: System prompt text (truncated)
   Variables: var1, var2, var3
   Tools: tool1, tool2
   Parameters: temperature=0.7, max_tokens=1000
```

### Shared Fragments

Reusable prompt fragments:

```
Shared Fragments (2): greeting, signature
```

### Pack Metadata

Additional pack information:

```
Pack Metadata:
  Domain: customer-service
  Language: en
  Tags: [support, production]
```

### Compilation Info

Build details:

```
Compilation: packc-v0.1.0, 2024-01-15T10:30:00Z, Schema: 1.0
```

## Use Cases

### 1. Quick Pack Summary

```bash
packc inspect packs/app.pack.json | head -n 10
```

### 2. Find Available Prompts

```bash
packc inspect packs/app.pack.json | grep -A 5 "=== Prompts"
```

### 3. Check Pack Version

```bash
packc inspect packs/app.pack.json | grep "Version:"
```

### 4. List All Tools

```bash
packc inspect packs/app.pack.json | grep "Tools:" | sort -u
```

### 5. Compare Packs

```bash
diff <(packc inspect packs/v1.pack.json) <(packc inspect packs/v2.pack.json)
```

### 6. Document Pack Contents

```bash
# Generate documentation
packc inspect packs/prod.pack.json > docs/pack-contents.md
```

## Exit Codes

- `0` - Success
- `1` - Error (pack not found, invalid format, etc.)

## Common Errors

### Pack Not Found

```bash
$ packc inspect missing.pack.json
Error loading pack: open missing.pack.json: no such file or directory
```

**Solution:** Check file path is correct

### Invalid Pack Format

```bash
$ packc inspect invalid.json
Error loading pack: invalid pack format
```

**Solution:** Ensure file is a valid pack (use `validate` first)

## Integration Examples

### Pre-deployment Check

```bash
#!/bin/bash
# Check pack before deploying

echo "Inspecting pack..."
packc inspect packs/prod.pack.json

read -p "Deploy this pack? (y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
  ./deploy.sh packs/prod.pack.json
fi
```

### Pack Comparison Script

```bash
#!/bin/bash
# Compare two pack versions

OLD_PACK=$1
NEW_PACK=$2

echo "=== Old Pack ==="
packc inspect "$OLD_PACK"

echo ""
echo "=== New Pack ==="
packc inspect "$NEW_PACK"

echo ""
echo "=== Differences ==="
diff <(packc inspect "$OLD_PACK") <(packc inspect "$NEW_PACK")
```

### Pack Inventory

```bash
#!/bin/bash
# Generate inventory of all packs

echo "# Pack Inventory" > inventory.md
echo "" >> inventory.md

for pack in packs/*.pack.json; do
  echo "## $(basename $pack)" >> inventory.md
  packc inspect "$pack" | head -n 20 >> inventory.md
  echo "" >> inventory.md
done
```

## Best Practices

### 1. Inspect Before Using

Always inspect new packs:

```bash
packc inspect unknown.pack.json
```

### 2. Document Pack Contents

Keep pack documentation up-to-date:

```bash
packc inspect packs/app.pack.json > docs/PACK.md
git add docs/PACK.md
git commit -m "docs: update pack documentation"
```

### 3. Verify After Download

Inspect downloaded packs:

```bash
curl -O https://example.com/packs/app.pack.json
packc inspect app.pack.json
packc validate app.pack.json
```

### 4. Compare Environments

```bash
# Compare dev vs prod
diff <(packc inspect packs/app.dev.pack.json) \
     <(packc inspect packs/app.prod.pack.json)
```

## See Also

- [compile](compile) - Compile packs
- [validate](validate) - Validate packs
- [How to Work with Packs](../how-to/work-with-packs)
