---
title: ci cd diagrams
description: DevOps and release management documentation
docType: guide
---

# CI/CD Architecture Diagram

Visual overview of the PromptKit CI/CD pipeline structure.

## Pipeline Flow

```mermaid
graph TB
    subgraph "Code Changes"
        A[Developer Push/PR]
    end
    
    subgraph "CI Pipeline (ci.yml)"
        B[Test Job]
        C[Coverage Job]
        D[Lint Job]
        E[Build Job]
        
        B --> F[gotestsum Tests]
        B --> G[Race Detector]
        B --> H[JUnit Reports]
        
        C --> I[Coverage Reports]
        C --> J[SonarCloud Scan]
        
        D --> K[go vet/fmt]
        D --> L[golangci-lint]
        
        E --> M[Build Validation]
    end
    
    subgraph "Documentation Pipeline (docs.yml)"
        N[Docs Change Detection]
        N --> O[Jekyll Build]
        O --> P[GitHub Pages Deploy]
    end
    
    subgraph "Release Testing (release-test.yml)"
        Q[Manual Trigger / Test Branch]
        Q --> R[Simulate Release Prep]
        R --> S[Generate Checklist]
        R --> T[Validate Build]
    end
    
    subgraph "External Services"
        U[SonarCloud Dashboard]
        V[GitHub Pages Site]
        W[GitHub Checks]
    end
    
    A -->|Code Change| B
    A -->|Code Change| C
    A -->|Code Change| D
    A -->|Code Change| E
    
    A -->|Doc Change| N
    
    Q -.Manual.-> R
    
    H --> W
    I --> U
    P --> V
    
    style A fill:#e1f5ff
    style U fill:#ffe1e1
    style V fill:#e1ffe1
    style W fill:#fff4e1
```

## Trigger Paths

```mermaid
graph LR
    A[Git Events] --> B{File Changed?}
    
    B -->|*.go, go.mod| C[CI Pipeline]
    B -->|docs/**| D[Docs Pipeline]
    B -->|release-test/*| E[Release Test]
    
    C --> F[All Jobs in Parallel]
    D --> G[Build → Deploy]
    E --> H[Validation Only]
    
    I[Manual Workflow Dispatch] -.-> D
    I -.-> E
    
    style A fill:#e1f5ff
    style C fill:#ffe1e1
    style D fill:#e1ffe1
    style E fill:#fff4e1
```

## Job Dependencies

```mermaid
graph TD
    subgraph "CI Pipeline"
        A[Checkout Code]
        
        A --> B[Test Job]
        A --> C[Coverage Job]
        A --> D[Lint Job]
        A --> E[Build Job]
        
        B -.-> F[No Dependencies]
        C -.-> F
        D -.-> F
        E -.-> F
        
        C --> G[SonarCloud]
    end
    
    subgraph "Docs Pipeline"
        H[Checkout Code]
        H --> I[Build Job]
        I --> J[Deploy Job]
    end
    
    style F fill:#e1f5ff,stroke:#333,stroke-dasharray: 5 5
    style J fill:#e1ffe1
```

## Coverage Flow

```mermaid
graph LR
    A[Run Tests] --> B[Generate .out Files]
    
    B --> C[runtime/runtime-coverage.out]
    B --> D[sdk/sdk-coverage.out]
    B --> E[tools/arena/arena-coverage.out]
    
    C --> F[Merge Coverage Files]
    D --> F
    E --> F
    
    F --> G[coverage.out]
    G --> H[SonarCloud Scan]
    
    H --> I[Quality Dashboard]
    H --> J[PR Comments]
    H --> K[Quality Gate Check]
    
    style G fill:#fff4e1
    style I fill:#e1ffe1
```

## Release Test Flow

```mermaid
graph TD
    A[Trigger Release Test] --> B{Input Type}
    
    B -->|Manual Dispatch| C[Use Form Inputs]
    B -->|Test Branch| D[Extract from Branch]
    B -->|Test Tag| E[Extract from Tag]
    
    C --> F[Determine Tool & Version]
    D --> F
    E --> F
    
    F --> G[Backup go.mod]
    G --> H[Remove Replace Directives]
    H --> I[Check Remote Dependencies]
    I --> J[Test Build]
    J --> K[Generate Diff]
    K --> L[Restore go.mod]
    L --> M[Upload Checklist]
    
    M --> N[Summary Report]
    
    style A fill:#e1f5ff
    style M fill:#e1ffe1
    style N fill:#fff4e1
```

## Secret & Permission Flow

```mermaid
graph TD
    A[GitHub Actions] --> B{Required Permissions}
    
    B --> C[contents: read]
    B --> D[actions: read]
    B --> E[checks: write]
    B --> F[pull-requests: write]
    
    G[GitHub Secrets] --> H[SONAR_TOKEN]
    G --> I[GITHUB_TOKEN]
    
    H --> J[SonarCloud Scan]
    I --> K[Git Operations]
    I --> L[API Calls]
    
    style G fill:#ffe1e1
    style H fill:#fff4e1
    style I fill:#fff4e1
```

## Deployment Architecture

```mermaid
graph TB
    subgraph "Repository"
        A[docs/ folder]
    end
    
    subgraph "GitHub Actions"
        B[Jekyll Build Job]
        C[Deploy Job]
    end
    
    subgraph "GitHub Pages"
        D[Static Site]
    end
    
    subgraph "CDN"
        E[Global Distribution]
    end
    
    A -->|Push to main| B
    B -->|Upload Artifact| C
    C -->|Deploy| D
    D -->|Serve via| E
    E -->|Users Access| F[altairalabs.github.io/PromptKit]
    
    style A fill:#e1f5ff
    style D fill:#e1ffe1
    style E fill:#fff4e1
```

## Quality Gate Flow

```mermaid
graph LR
    A[Code Push] --> B[Run Tests]
    B --> C[Generate Coverage]
    C --> D[SonarCloud Analysis]
    
    D --> E{Quality Gate}
    
    E -->|Pass| F[Green Check]
    E -->|Fail| G[Red X]
    
    F --> H[Can Merge]
    G --> I[Fix Required]
    
    I --> J[Developer Fixes]
    J --> A
    
    style E fill:#fff4e1
    style F fill:#e1ffe1
    style G fill:#ffe1e1
```

## Legend

### Node Colors

- 🔵 **Light Blue** - Triggers/Inputs
- 🔴 **Light Red** - External Services
- 🟢 **Light Green** - Outputs/Success
- 🟡 **Light Yellow** - Important/Decision Points

### Line Styles

- **Solid Line** (→) - Direct flow/dependency
- **Dashed Line** (⇢) - Optional/manual trigger
- **Dotted Line** (···>) - No dependency (parallel)

## Diagram Usage

These diagrams can be:
- Viewed in GitHub (Mermaid support built-in)
- Rendered in VS Code (with Mermaid extension)
- Exported to PNG/SVG for presentations
- Embedded in documentation sites

## Updating Diagrams

When workflows change, update the relevant diagram:

1. Edit the Mermaid code block
2. Test rendering locally or on GitHub
3. Update corresponding pipeline documentation
4. Commit changes together

## Tools

- **Mermaid Live Editor:** https://mermaid.live/
- **VS Code Extension:** Markdown Preview Mermaid Support
- **GitHub:** Native Mermaid rendering in markdown files

---

*Last Updated: 2 November 2025*
