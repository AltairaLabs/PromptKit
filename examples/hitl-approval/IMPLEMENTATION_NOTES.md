# Implementation Notes

## Overview

This example demonstrates the Human-in-the-Loop (HITL) feature implementation for PromptKit. It shows how tools can opt-in to asynchronous execution with approval workflows.

## Key Implementation Details

### 1. Executor Registration

The `AsyncEmailTool` must use the name `"mock-static"` to integrate with the existing registry lookup logic:

```go
func (t *AsyncEmailTool) Name() string {
    return "mock-static" // Required to work with ExecuteAsync
}
```

This is because `Registry.ExecuteAsync()` hardcodes executor name lookups based on tool mode:
- `mode="mock"` → looks for "mock-static" executor
- `mode="mcp"` → looks for "mcp" executor  
- `mode="live"` → looks for "http" executor

### 2. Tool Descriptor Mode

The tool descriptor should use `mode="mock"` to trigger the mock-static executor lookup:

```go
toolDescriptor := &tools.ToolDescriptor{
    Name: "send_email",
    Mode: "mock", // This causes ExecuteAsync to look for "mock-static" executor
    //...
}
```

### 3. Risk Assessment Logic

The example implements simple risk-based approval logic:

```go
func (t *AsyncEmailTool) isHighRisk(req EmailRequest) bool {
    // Emails to executives require approval
    highRiskDomains := []string{"ceo@", "board@", "investor@", "press@"}
    
    for _, domain := range highRiskDomains {
        if strings.HasPrefix(req.To, domain) {
            return true
        }
    }
    
    // Large emails also require approval
    if len(req.Body) > 1000 {
        return true
    }
    
    return false
}
```

### 4. Pending Tool Information

When approval is needed, the tool returns comprehensive metadata:

```go
return &tools.ToolExecutionResult{
    Status: tools.ToolStatusPending,
    PendingInfo: &tools.PendingToolInfo{
        Reason:      "requires_approval",
        Message:     fmt.Sprintf("Email to %s requires approval", req.To),
        ToolName:    "send_email",
        Args:        args, // Original arguments for resumption
        ExpiresAt:   &expiresAt,
        CallbackURL: "http://localhost:8080/approve/email_123",
        Metadata: map[string]interface{}{
            "risk_level": "critical",
            "recipient":  req.To,
            "subject":    req.Subject,
        },
    },
}, nil
```

## Testing the Example

Run the example to see:

1. **High-risk email** (ceo@example.com) - Returns pending status with approval metadata
2. **Low-risk email** (support@example.com) - Executes immediately without approval
3. **Approval simulation** - Shows how approved tools complete execution

## Integration with Pipeline Middleware

In a real application, the provider middleware automatically:

1. Detects pending tool results via `ExecuteAsync()`
2. Stores pending info in `ExecutionContext.PendingToolCalls`
3. Adds to result metadata as `metadata["pending_tools"]`
4. Application can:
   - Save conversation state
   - Show approval UI
   - Resume execution after approval

## Future Enhancements

- **Custom executor modes**: Allow tools to specify custom executor names without hardcoded lookups
- **Approval callbacks**: Implement actual HTTP endpoints for approval workflows
- **State persistence**: Show complete save/load cycle with state store
- **Multi-tool approval**: Handle multiple pending tools in one turn
- **Timeout handling**: Demonstrate expiry and cleanup of stale approvals
