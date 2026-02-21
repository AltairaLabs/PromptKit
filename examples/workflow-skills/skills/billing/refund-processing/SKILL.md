---
name: refund-processing
description: Refund procedures and policies. Activate when processing a refund for a customer.
allowed-tools:
  - refund
metadata:
  tags: "refund, billing, payments"
---

# Refund Processing Procedures

## Eligibility
- Orders within 30 days of delivery are eligible for full refund
- Orders 30-90 days: partial refund (50%) or store credit
- Orders over 90 days: store credit only

## Process
1. Look up the order using `get_order`
2. Verify the customer's identity and order details
3. Determine refund amount based on eligibility
4. Process using the `refund` tool
5. Confirm the refund amount and timeline with the customer

## Timeline
- Credit card refunds: 3-5 business days
- Debit card refunds: 5-10 business days
- Store credit: immediate
