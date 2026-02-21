---
name: escalation-policy
description: Guidelines for when and how to escalate support tickets to a manager. Activate when the customer requests a manager or the issue cannot be resolved.
allowed-tools:
  - escalate_ticket
metadata:
  tags: "escalation, manager, priority"
---

# Escalation Policy

## When to Escalate
- Customer explicitly requests a manager
- Refund amount exceeds $200
- Issue involves a safety concern
- Three or more failed resolution attempts
- Legal or regulatory compliance issue

## Escalation Priorities
- **Critical**: Safety issues, data breaches, legal threats
- **High**: Refunds over $500, VIP customers, repeated failures
- **Medium**: Refunds $200-$500, complex disputes
- **Low**: Manager requests with no urgency

## Process
1. Acknowledge the customer's frustration
2. Explain that you're escalating to a specialist/manager
3. Use the `escalate_ticket` tool with the appropriate priority
4. Provide the customer with a ticket reference number
5. Set expectations for follow-up timeline (24-48 hours)
