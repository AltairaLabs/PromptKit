---
layout: docs
title: Templates
parent: Concepts
nav_order: 2
---

# Templates

Understanding template systems in PromptKit.

## What is a Template?

A **template** is a reusable prompt structure with placeholders for dynamic content. Templates separate prompt logic from data.

### Without Template

```go
prompt := "You are a support agent for " + company + ". Help the user with: " + question
```

### With Template

```go
template := "You are a support agent for {{.company}}. Help the user with: {{.question}}"
result := ExecuteTemplate(template, map[string]string{
    "company":  "TechCorp",
    "question": "password reset",
})
```

## Why Use Templates?

**Consistency**: Same structure across all uses  
**Maintainability**: Update once, apply everywhere  
**Reusability**: Share templates across projects  
**Testing**: Test templates independently  
**Versioning**: Track template changes  

## Template Structure

### System and User Prompts

```yaml
system: |
  You are a {{.role}} for {{.company}}.
  
  Guidelines:
  - Be {{.tone}}
  - Keep responses under {{.max_words}} words
  
user: |
  {{.user_input}}
```

### Variables

Variables are placeholders replaced with actual values:

```go
variables := map[string]string{
    "role":      "customer support agent",
    "company":   "TechCorp",
    "tone":      "professional",
    "max_words": "100",
}
```

## Templates in PromptKit Components

### Runtime Templates

```go
import "github.com/AltairaLabs/PromptKit/runtime/template"

// Create registry
templates := template.NewRegistry()

// Register template
templates.RegisterTemplate("support", &template.PromptTemplate{
    SystemPrompt: "You are a {{.role}} for {{.company}}.",
    Variables: map[string]string{
        "role":    "support agent",
        "company": "TechCorp",
    },
})

// Use in pipeline
pipe := pipeline.NewPipeline(
    middleware.TemplateMiddleware(templates, &middleware.TemplateConfig{
        DefaultTemplate: "support",
    }),
    // ... other middleware
)
```

### PackC Templates

PackC manages templates as files:

```bash
prompts/
├── support.prompt
├── sales.prompt
└── technical.prompt
```

Example `support.prompt`:

```yaml
name: support
version: 1.0.0
system: |
  You are a customer support agent for {{.company}}.
  
  Your responsibilities:
  - Answer questions clearly
  - Be empathetic and professional
  - Escalate complex issues
  
  Company info:
  - Website: {{.website}}
  - Support hours: {{.hours}}

user: |
  Customer question: {{.question}}
```

Package templates:

```bash
packc pack prompts/ -o templates.pack
```

Load in code:

```go
pack, _ := packc.LoadPack("templates.pack")
template := pack.Templates["support"]
```

### SDK Templates

SDK uses simpler template approach:

```go
conv := sdk.NewConversation(provider, &sdk.ConversationConfig{
    SystemPrompt: "You are a helpful assistant for {{.company}}.",
    Variables: map[string]string{
        "company": "TechCorp",
    },
})
```

## Template Syntax

### Variable Substitution

Basic:
```
Hello, {{.name}}!
```

With defaults:
```
Hello, {{.name | default "Guest"}}!
```

### Conditionals

```yaml
system: |
  You are a support agent.
  {{if .premium}}
  Provide priority support.
  {{end}}
```

### Loops

```yaml
system: |
  Available commands:
  {{range .commands}}
  - {{.}}
  {{end}}
```

### Functions

```yaml
system: |
  User: {{.email | lowercase}}
  Role: {{.role | uppercase}}
```

## Template Best Practices

### Structure

✅ **Separate system and user prompts**
```yaml
system: |
  Instructions for AI
user: |
  User input: {{.input}}
```

✅ **Use clear variable names**
```yaml
{{.customer_name}} not {{.cn}}
{{.ticket_id}} not {{.id}}
```

✅ **Provide defaults**
```yaml
tone: {{.tone | default "professional"}}
```

✅ **Document variables**
```yaml
# Variables:
# - company: Company name (required)
# - role: Agent role (default: "support agent")
# - hours: Support hours (default: "24/7")
```

### Organization

✅ **One template per use case**
```
support.prompt     # Customer support
sales.prompt       # Sales inquiries
technical.prompt   # Technical support
```

✅ **Version templates**
```
support-v1.prompt
support-v2.prompt
```

✅ **Use descriptive names**
```
customer-support.prompt  # Clear
template1.prompt         # Unclear
```

## Common Patterns

### Role-Based Templates

```yaml
name: role-based
system: |
  You are a {{.role}}.
  
  {{if eq .role "support"}}
  Focus on helping customers resolve issues.
  {{else if eq .role "sales"}}
  Focus on understanding customer needs.
  {{end}}
```

### Multi-Language Templates

```yaml
name: multilingual
system: |
  {{if eq .language "en"}}
  You are a helpful assistant.
  {{else if eq .language "es"}}
  Eres un asistente útil.
  {{else if eq .language "fr"}}
  Vous êtes un assistant utile.
  {{end}}
```

### Contextual Templates

```yaml
name: contextual
system: |
  You are a support agent.
  
  User tier: {{.user_tier}}
  {{if eq .user_tier "premium"}}
  Provide white-glove support.
  {{else}}
  Provide standard support.
  {{end}}
  
  Previous issues: {{.issue_count}}
  {{if gt .issue_count 5}}
  This is a returning customer. Be extra helpful.
  {{end}}
```

## Testing Templates

### With PromptArena

```yaml
name: Template Tests

templates:
  support:
    file: support.prompt
    variables:
      company: TechCorp
      role: support agent

tests:
  - name: Test Support Template
    template: support
    variables:
      question: "How do I reset my password?"
    assertions:
      - type: contains
        value: "TechCorp"
      - type: contains
        value: "password"
```

### With Runtime

```go
func TestTemplate(t *testing.T) {
    templates := template.NewRegistry()
    templates.RegisterTemplate("test", &template.PromptTemplate{
        SystemPrompt: "You are a {{.role}}",
        Variables: map[string]string{"role": "tester"},
    })
    
    // Test template rendering
    rendered := templates.Get("test").Render()
    assert.Contains(t, rendered, "tester")
}
```

## Template Management

### Development

1. **Create templates** in `prompts/` directory
2. **Test locally** with sample data
3. **Version control** templates with code
4. **Document variables** and usage

### Production

1. **Package with PackC**: `packc pack prompts/ -o prod.pack`
2. **Deploy with app**: Include `.pack` file
3. **Load at runtime**: `packc.LoadPack("prod.pack")`
4. **Monitor usage**: Track which templates are used

### Updates

1. **Create new version**: `support-v2.prompt`
2. **Test thoroughly**: Use PromptArena
3. **Deploy gradually**: Canary or blue-green
4. **Monitor metrics**: Compare performance
5. **Rollback if needed**: Switch back to v1

## Troubleshooting

### Problem: Variable Not Substituted

```
Output: "Hello, {{.name}}!"  // Variable not replaced
```

**Solution**: Check variable is provided

```go
// Missing variable
variables := map[string]string{}  // ❌

// Correct
variables := map[string]string{
    "name": "Alice",
}  // ✅
```

### Problem: Template Not Found

```
Error: template "support" not found
```

**Solution**: Ensure template is registered

```go
templates.RegisterTemplate("support", promptTemplate)
```

### Problem: Syntax Error

```
Error: unexpected "}" in template
```

**Solution**: Check template syntax

```yaml
# Wrong
{{.name}  # Missing closing brace

# Correct
{{.name}}
```

## Advanced Features

### Nested Variables

```yaml
system: |
  Company: {{.company.name}}
  Support: {{.company.support_email}}
```

```go
variables := map[string]any{
    "company": map[string]string{
        "name":          "TechCorp",
        "support_email": "help@techcorp.com",
    },
}
```

### Template Inheritance

Base template:
```yaml
# base.prompt
system: |
  You are a {{.role}}.
  Be professional and helpful.
```

Extended template:
```yaml
# support.prompt
extends: base.prompt
system: |
  {{.base_system}}
  
  Additional guidelines:
  - Focus on customer satisfaction
  - Escalate when needed
```

## Summary

Templates provide:

✅ **Reusability** - Write once, use many times  
✅ **Consistency** - Same structure everywhere  
✅ **Maintainability** - Update in one place  
✅ **Testability** - Test independently  
✅ **Flexibility** - Customize with variables  

## Related Documentation

- [Prompts](prompts.md) - Prompt engineering basics
- [Runtime Templates](../runtime/how-to/use-templates.md) - Implementation guide
- [PackC Documentation](../packc/index.md) - Template packaging
- [Template Reference](../runtime/reference/middleware.md#templatemiddleware) - API details
