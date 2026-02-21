---
name: pci-compliance
description: PCI DSS compliance rules for handling payment card data. Activate when the customer mentions billing, refunds, or payment information.
allowed-tools:
  - refund
metadata:
  tags: "compliance, payments, billing"
---

# PCI Compliance Guidelines

When handling payment card data, follow these rules:

1. **Never log or display full card numbers** — use last-4 only (e.g., "card ending in 4242")
2. **Verify cardholder identity** before processing any refund or payment modification
3. **Do not ask for CVV** — this is never needed for support interactions
4. **Mask sensitive data** in all tool calls and conversation logs
5. **Refund to original payment method only** — never transfer to a different card or account

## Refund Authorization
- Refunds under $50: process immediately
- Refunds $50-$200: require order verification
- Refunds over $200: escalate to a manager
