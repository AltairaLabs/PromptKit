---
layout: default
title: Real-World Applications
nav_order: 5
parent: Arena Examples
grand_parent: PromptArena
---

# Real-World Application Examples

Production-ready examples demonstrating complete LLM applications with comprehensive testing.

## Examples in this Category

### [customer-support](customer-support/)

**Purpose**: Complete customer support chatbot with multi-provider testing

**What you'll learn:**
- Production chatbot structure
- Multi-provider testing strategy
- Scenario-based conversation testing
- Pack-based prompt organization
- Support domain best practices

**Difficulty**: Advanced  
**Estimated time**: 60 minutes

**Featured capabilities:**
- Customer support scenarios (refunds, orders, account issues)
- Multi-turn conversation handling
- Provider comparison (OpenAI, Claude, Gemini)
- PromptPack organization
- Support tone validation

**Production patterns:**
- Comprehensive scenario coverage
- Provider fallback strategies
- Quality consistency validation
- Cost optimization approaches

### [customer-support-integrated](customer-support-integrated/)

**Purpose**: Integrated customer support system with external tool calls

**What you'll learn:**
- Tool integration in production
- Customer data retrieval
- Support ticket creation
- Order history access
- Subscription management
- Multi-persona testing

**Difficulty**: Advanced  
**Estimated time**: 75 minutes

**Featured capabilities:**
- External API integration
- Tool calling patterns
- Data retrieval and updates
- Transaction processing
- State management across sessions

**Production patterns:**
- Tool error handling
- Data consistency validation
- Security testing for data access
- Integration test patterns

## Getting Started

### Prerequisites

```bash
# Install PromptArena
make install-arena

# Set up provider API keys
export OPENAI_API_KEY="your-key"
export ANTHROPIC_API_KEY="your-key"
export GOOGLE_API_KEY="your-key"

# For integrated example: Set up mock services
cd customer-support-integrated
npm install  # If using Node.js mock services
```

### Understanding Production Testing

These examples demonstrate real-world application testing:

**Key differences from tutorials:**
- Comprehensive scenario coverage
- Production-quality prompts
- Multi-provider strategy
- Error handling patterns
- Performance considerations
- Security validation

### Running Real-World Examples

```bash
# Navigate to an example
cd docs/arena/examples/real-world/customer-support

# Review the structure
ls -la prompts/ scenarios/ providers/

# Run complete test suite
promptarena run

# Run specific scenario category
promptarena run --tag refunds

# Compare providers
promptarena run --all-providers --report
```

## Key Concepts

### Comprehensive Scenario Coverage

Production applications need extensive testing:

```yaml
scenarios:
  # Happy paths
  - refund-request-valid.yaml
  - order-status-check.yaml
  
  # Edge cases
  - refund-outside-window.yaml
  - order-not-found.yaml
  
  # Error handling
  - invalid-order-number.yaml
  - system-unavailable.yaml
  
  # Multi-turn complexity
  - account-issue-escalation.yaml
  - subscription-modification.yaml
```

### Provider Strategy

Test across providers for resilience:

```yaml
# Primary provider
primary:
  provider: openai-gpt4o
  use_for: ["production", "quality baseline"]

# Fallback provider
fallback:
  provider: claude-sonnet
  use_for: ["openai downtime", "quality comparison"]

# Cost-effective provider
budget:
  provider: gemini-flash
  use_for: ["high volume", "simple queries"]
```

### Prompt Organization

Use PromptPacks for maintainability:

```yaml
# prompts/customer-support-pack.yaml
name: customer-support
version: 1.0.0

prompts:
  system:
    content: |
      You are a helpful customer support agent...
      
  refund-specialist:
    content: |
      You specialize in refund requests...
  
  technical-support:
    content: |
      You provide technical assistance...
```

### Tool Integration

Test external system interactions:

```yaml
tools:
  customer-lookup:
    type: function
    function:
      name: get_customer_info
      parameters:
        customer_id: string
  
  create-ticket:
    type: function
    function:
      name: create_support_ticket
      parameters:
        issue: string
        priority: string
```

## Production Testing Patterns

### Multi-Turn Conversations

Test realistic conversation flows:

```yaml
test_cases:
  - name: "Refund Request Flow"
    tags: [refunds, multi-turn]
    
    turns:
      # Turn 1: Initial request
      - user: "I want to return my order"
        expected:
          - type: contains
            value: ["order number", "reason"]
      
      # Turn 2: Provide details
      - user: "Order #12345, item is defective"
        expected:
          - type: tool_called
            value: "lookup_order"
          - type: contains
            value: ["refund", "process"]
      
      # Turn 3: Confirm action
      - user: "Yes, please process the refund"
        expected:
          - type: tool_called
            value: "issue_refund"
          - type: contains
            value: ["confirmation", "processed"]
```

### Error Handling

Test graceful failure:

```yaml
test_cases:
  - name: "System Unavailable"
    turns:
      - user: "Check my order status"
        
        # Simulate tool failure
        tool_responses:
          lookup_order:
            error: "Service temporarily unavailable"
        
        expected:
          # Should handle gracefully
          - type: contains
            value: ["temporarily", "try again", "apologize"]
          
          # Should not expose technical details
          - type: not_contains
            value: ["500 error", "exception", "stack trace"]
```

### Security Testing

Validate data access controls:

```yaml
test_cases:
  - name: "Unauthorized Data Access"
    turns:
      # Attempt to access another customer's data
      - user: "Show me order details for customer ID 99999"
        expected:
          # Should refuse
          - type: not_tool_called
            value: "get_customer_info"
          
          # Should require authentication
          - type: contains
            value: ["verify", "authenticate", "cannot access"]
```

### Performance Testing

Validate response times:

```yaml
test_cases:
  - name: "Response Time Check"
    tags: [performance]
    
    turns:
      - user: "What's the status of order #12345?"
        expected:
          # Fast response required
          - type: response_time
            max_seconds: 3
          
          # Tool should be efficient
          - type: tool_call_time
            max_seconds: 1
```

### Quality Consistency

Ensure consistent quality across providers:

```yaml
test_cases:
  - name: "Cross-Provider Quality"
    providers: [openai-gpt4o, claude-sonnet, gemini-pro]
    
    turns:
      - user: "I need help with a billing issue"
        expected:
          # All must be helpful
          - type: sentiment
            value: helpful
          
          # All must maintain tone
          - type: tone
            value: professional
          
          # Similar semantic quality
          - type: semantic_similarity
            baseline: "I understand you're having a billing issue. Let me help you resolve this."
            threshold: 0.85
```

## Advanced Patterns

### Multi-Persona Testing

Test different customer scenarios:

```yaml
personas:
  frustrated_customer:
    context: "Customer has been waiting 3 days"
    tone: "frustrated, impatient"
    scenario: "delayed-order.yaml"
  
  technical_user:
    context: "Technical user, precise questions"
    tone: "direct, technical"
    scenario: "api-integration-issue.yaml"
  
  confused_user:
    context: "New customer, unfamiliar with process"
    tone: "uncertain, needs guidance"
    scenario: "first-time-order.yaml"
```

### Integration Testing

Test complete workflows:

```yaml
test_cases:
  - name: "End-to-End Order Issue"
    tags: [integration, e2e]
    
    setup:
      # Initialize test data
      - create_test_order: "ORDER123"
      - set_order_status: "delayed"
    
    turns:
      # Customer contacts support
      - user: "My order ORDER123 hasn't arrived"
        expected:
          - type: tool_called
            value: "lookup_order"
      
      # Agent investigates
      - user: "What's the current status?"
        expected:
          - type: contains
            value: "delayed"
      
      # Agent takes action
      - user: "Can you expedite it?"
        expected:
          - type: tool_called
            value: "update_order_priority"
          - type: contains
            value: ["expedited", "updated"]
    
    teardown:
      # Clean up test data
      - delete_test_order: "ORDER123"
```

### State Management

Test conversation state:

```yaml
test_cases:
  - name: "State Preservation"
    turns:
      - user: "I'm calling about order #12345"
        expected:
          - type: state_stored
            key: "order_id"
            value: "12345"
      
      - user: "What's the shipping address?"
        expected:
          # Should remember order from turn 1
          - type: tool_called
            value: "get_order_details"
          - type: tool_args_match
            order_id: "12345"
```

### Cost Optimization

Test with cost-effective strategies:

```yaml
# Route by complexity
routing_rules:
  simple_queries:
    provider: gemini-flash
    examples: ["order status", "hours", "contact info"]
  
  complex_queries:
    provider: openai-gpt4o
    examples: ["refund dispute", "technical issue", "escalation"]
  
  tool_required:
    provider: openai-gpt4o
    reason: "Best tool calling support"
```

## Best Practices

### Scenario Organization

```
scenarios/
├── README.md
├── refunds/
│   ├── happy-path.yaml
│   ├── outside-window.yaml
│   └── damaged-item.yaml
├── orders/
│   ├── status-check.yaml
│   ├── modification.yaml
│   └── cancellation.yaml
├── account/
│   ├── password-reset.yaml
│   ├── profile-update.yaml
│   └── subscription-change.yaml
└── escalation/
    ├── unresolved-issue.yaml
    └── manager-request.yaml
```

### Testing Strategy

```yaml
# Development: Fast iteration
development:
  provider: mock
  scenarios: smoke_tests
  cost: $0

# Integration: Validate integration
integration:
  provider: openai-mini
  scenarios: all_scenarios
  cost: ~$1

# Staging: Pre-production validation
staging:
  providers: [openai-gpt4o, claude-sonnet]
  scenarios: critical_paths
  cost: ~$5

# Production: Continuous monitoring
production:
  provider: production_provider
  scenarios: health_checks
  frequency: hourly
  cost: ~$10/month
```

### Documentation

Each production example should include:

```
example/
├── README.md              # Overview and setup
├── ARCHITECTURE.md        # System design
├── TESTING_STRATEGY.md    # Test approach
├── DEPLOYMENT.md          # Production deployment
└── RUNBOOK.md            # Operational procedures
```

## Troubleshooting

### High Failure Rate

If tests are failing frequently:

1. Check provider availability
2. Review assertion thresholds
3. Test with mock provider first
4. Verify tool integrations
5. Check for prompt issues

### Inconsistent Quality

If quality varies across runs:

1. Use semantic similarity assertions
2. Lower temperature in provider config
3. Add more specific prompts
4. Test across multiple providers
5. Implement retry logic

### Tool Call Issues

If tool calls fail or incorrect:

1. Verify tool schemas are clear
2. Test providers' function calling support
3. Add explicit tool usage instructions
4. Check tool response handling
5. Validate tool error handling

## Production Deployment Checklist

Before deploying chatbot to production:

- [ ] All critical scenarios pass
- [ ] Provider fallback configured
- [ ] Error handling tested
- [ ] Security validation complete
- [ ] Performance within targets
- [ ] Cost optimization implemented
- [ ] Monitoring configured
- [ ] Documentation complete
- [ ] Runbook prepared
- [ ] Team trained

## Next Steps

### Enhancing Production Applications

1. **Add Monitoring**: Implement production monitoring with Arena
2. **Optimize Costs**: Fine-tune provider routing
3. **Improve Quality**: Iterate on prompts and scenarios
4. **Scale Testing**: Automate with CI/CD
5. **User Feedback**: Integrate real user conversations

### Building Your Application

Use these examples as templates:

1. Copy relevant example
2. Adapt to your domain
3. Create comprehensive scenarios
4. Test across providers
5. Implement monitoring
6. Deploy with confidence

## Additional Resources

- [Tutorial: CI Integration](../../tutorials/05-ci-integration.md)
- [How-To: Integrate CI/CD](../../how-to/integrate-ci-cd.md)
- [Explanation: Testing Philosophy](../../explanation/testing-philosophy.md)
- [Explanation: Provider Comparison](../../explanation/provider-comparison.md)
- [Reference: Scenario Format](../../reference/scenario-format.md)
