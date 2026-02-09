---
title: Prompts
sidebar:
  order: 1
---
Understanding prompts and prompt engineering in PromptKit.

## What is a Prompt?

A **prompt** is the text input sent to an LLM. It tells the model what to do and provides context for generating a response.

### Basic Example

```
User: What's the capital of France?

LLM Response: The capital of France is Paris.
```

### Structured Prompt

```
System: You are a helpful assistant. Be concise.

User: What's the capital of France?

LLM Response: Paris.
```

## Prompt Components

### Messages

Prompts consist of **messages** with different roles:

- **System**: Instructions for the AI's behavior
- **User**: Input from the end user
- **Assistant**: Previous AI responses (for context)
- **Tool**: Results from function calls

Example in PromptKit:

```go
messages := []types.Message{
    {Role: "system", Content: "You are a helpful assistant"},
    {Role: "user", Content: "Hello"},
    {Role: "assistant", Content: "Hi! How can I help?"},
    {Role: "user", Content: "What's 2+2?"},
}
```

### System Prompts

System prompts set the AI's behavior:

**Good system prompt:**
```
You are a customer support agent for TechCorp.

Guidelines:
- Be professional and empathetic
- Keep responses under 100 words
- Reference documentation when possible
- Escalate complex issues to human agents

Available resources:
- Help docs: https://help.techcorp.com
- Status page: https://status.techcorp.com
```

**Poor system prompt:**
```
Be helpful.
```

## Prompt Engineering Techniques

### 1. Clear Instructions

**Bad:**
```
Write something about dogs.
```

**Good:**
```
Write a 3-sentence summary about Golden Retrievers.
Include: origin, temperament, and typical uses.
```

### 2. Few-Shot Examples

Provide examples to guide the model:

```
Convert user questions to support ticket categories.

Examples:
Q: "I can't log in" → Category: Authentication
Q: "My credit card was declined" → Category: Billing
Q: "The app crashed" → Category: Technical

Q: "I forgot my password" → Category: ?
```

### 3. Chain-of-Thought

Ask the model to explain its reasoning:

```
Solve this math problem step-by-step:
If Alice has 3 apples and Bob gives her 5 more, how many does Alice have?

Let's solve this step by step:
1. Alice starts with: 3 apples
2. Bob gives her: 5 apples
3. Total: 3 + 5 = 8 apples

Answer: 8 apples
```

### 4. Role-Based

Assign a specific role:

```
You are an experienced software architect reviewing code.
Focus on:
- Design patterns
- Scalability
- Maintainability

Review this code:
[code here]
```

## Prompts in PromptKit

### Runtime

Runtime uses prompts with templates:

```go
templates := template.NewRegistry()
templates.RegisterTemplate("support", &template.PromptTemplate{
    SystemPrompt: "You are a support agent. Be helpful.",
    Variables: map[string]string{
        "company": "TechCorp",
    },
})
```

### SDK

SDK simplifies prompt management:

```go
conv := sdk.NewConversation(provider, &sdk.ConversationConfig{
    SystemPrompt: "You are a helpful assistant",
    Model:        "gpt-4o-mini",
})

response, _ := conv.Send(ctx, "Hello")
```

### PackC

PackC packages prompts for distribution:

```yaml
# support.prompt
system: |
  You are a  for .
  Be professional and concise.

user: |
  
```

Compile and use:

```bash
packc compile --config arena.yaml --output support.pack.json --id support
```

```go
pack, _ := prompt.LoadPack("support.pack.json")
```

### PromptArena

PromptArena tests prompts:

```yaml
tests:
  - name: Support Query
    prompt: "How do I reset my password?"
    assertions:
      - type: contains
        value: "password reset"
      - type: max_length
        value: 200
```

## Best Practices

### Do's

✅ **Be specific** - Clear instructions get better results  
✅ **Provide context** - Give the model relevant information  
✅ **Use examples** - Show what you want  
✅ **Set constraints** - Specify format, length, tone  
✅ **Test variations** - Try different phrasings  
✅ **Version prompts** - Track changes over time  

### Don'ts

❌ **Don't be vague** - "Be helpful" isn't enough  
❌ **Don't assume knowledge** - Provide necessary context  
❌ **Don't skip system prompts** - They guide behavior  
❌ **Don't ignore length** - Longer ≠ better  
❌ **Don't forget to test** - Use PromptArena  

## Common Patterns

### Task-Oriented

```
Task: Extract customer email from support ticket

Input: "Hi, my email is john@example.com and I need help"
Output: john@example.com

Input: "Please contact me at support@company.com"
Output:
```

### Conversational

```
You are a friendly chatbot. Maintain context across messages.

Be:
- Conversational and warm
- Helpful and informative
- Concise (under 50 words per response)
```

### Analytical

```
Analyze the following text and provide:
1. Main topic
2. Sentiment (positive/negative/neutral)
3. Key entities mentioned
4. Confidence score (0-1)

Text: [input]
```

### Creative

```
Write a creative product description for:

Product: 
Features: 
Target audience: 

Style: Engaging and persuasive
Length: 50-100 words
```

## Prompt Management

### Development

- Store prompts in version control
- Use meaningful names
- Document prompt purpose
- Track performance metrics

### Testing

- Test with PromptArena
- Try edge cases
- Measure quality
- Compare variations

### Production

- Load from PackC files
- Use templates for flexibility
- Monitor performance
- Version prompts

## Troubleshooting

### Problem: Inconsistent Responses

**Solution**: Add more specific instructions and examples

### Problem: Wrong Format

**Solution**: Specify exact format in prompt

Before:
```
List the items.
```

After:
```
List the items in JSON format:
{"items": ["item1", "item2"]}
```

### Problem: Ignoring Instructions

**Solution**: Emphasize important instructions

```
IMPORTANT: Always include sources.
NEVER make up information.

[rest of prompt]
```

### Problem: Too Verbose

**Solution**: Set length constraints

```
Answer in exactly 2 sentences. Be concise.
```

## Summary

Prompts are the foundation of LLM applications. Good prompts:

- Provide clear instructions
- Include relevant context
- Use examples when helpful
- Set appropriate constraints
- Are tested and versioned

## Related Documentation

- [Templates](templates) - Organizing prompts
- [Validation](validation) - Content safety
- [Runtime Templates](../runtime/how-to/use-templates) - Implementation
- [PackC](../packc/index) - Prompt packaging
