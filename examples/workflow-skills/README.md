# Workflow + Skills Example

Demonstrates how to combine **workflow states** with **directory-based skill filtering** and the **three-level tool scoping** model.

## What This Shows

| Feature | How it's demonstrated |
|---------|----------------------|
| **Directory-based skills** | Skills organized under `skills/billing/`, `skills/orders/`, `skills/escalation/` |
| **Workflow skill filtering** | Billing state only sees `skills/billing/`, orders state only sees `skills/orders/` |
| **Three-level tool scoping** | Pack declares all tools → prompts select baseline → skills extend on activation |
| **Preloaded skills** | `brand-voice` is always available (no activation needed) |
| **Skill assertions** | Arena scenarios assert `skill_activated` and `skill_not_activated` |

## Directory Structure

```
workflow-skills/
├── workflow-skills.pack.json       # Pack with workflow + skills config
├── config.arena.yaml               # Arena test configuration
├── mock-responses.yaml             # Deterministic mock responses
├── providers/
│   └── mock-provider.yaml
├── scenarios/
│   ├── billing-flow.scenario.yaml  # Billing path with PCI skill activation
│   └── orders-flow.scenario.yaml   # Orders path with troubleshooting skill
└── skills/
    ├── brand-voice/                # Top-level — always available
    │   └── SKILL.md
    ├── billing/                    # Scoped to billing workflow state
    │   ├── pci-compliance/
    │   │   └── SKILL.md
    │   └── refund-processing/
    │       └── SKILL.md
    ├── orders/                     # Scoped to orders workflow state
    │   └── order-troubleshooting/
    │       └── SKILL.md
    └── escalation/                 # Available in intake (all skills)
        └── escalation-policy/
            └── SKILL.md
```

## Workflow

```
intake ──RouteBilling──→ billing ──Resolve──→ closed
   │                                            ↑
   └────RouteOrders────→ orders ───Resolve──────┘
```

- **intake**: All skills available (no `skills` filter)
- **billing**: Only `skills/billing/` skills (pci-compliance, refund-processing)
- **orders**: Only `skills/orders/` skills (order-troubleshooting)
- **closed**: No skills (`skills: none`)

## Three-Level Tool Scoping

```
Pack tools (ceiling):     get_order, refund, search_orders, escalate_ticket
                              │
Billing prompt (baseline): get_order, refund
                              │
PCI skill (extension):     + refund (already available via prompt)
```

Skills can only grant tools that the pack declares. The pack is the ceiling.

## Running

```bash
# Requires PROMPTKIT_SCHEMA_SOURCE=local until remote schemas are updated
cd examples/workflow-skills
PROMPTKIT_SCHEMA_SOURCE=local promptarena run --ci --format html
```
