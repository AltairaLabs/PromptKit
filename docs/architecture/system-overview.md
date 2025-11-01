# PromptKit System Architecture

This document provides visual representations of the PromptKit system architecture and component relationships.

## System Overview

```mermaid
graph TB
    subgraph "PromptKit Components"
        Arena["🏟️ Arena CLI<br/>Testing Framework"]
        PackC["📦 PackC CLI<br/>Pack Compiler"]
        SDK["🚀 SDK<br/>Production Library"]
        Runtime["⚙️ Runtime<br/>Core Engine"]
    end
    
    subgraph "External Integrations"
        OpenAI["OpenAI<br/>GPT-4, GPT-3.5"]
        Anthropic["Anthropic<br/>Claude 3"]
        Google["Google<br/>Gemini"]
        MCP["MCP Servers<br/>Tools & Memory"]
    end
    
    subgraph "User Workflows"
        Testing["Testing Workflow"]
        Development["Development Workflow"]
        Production["Production Deployment"]
    end
    
    Testing --> Arena
    Development --> SDK
    Production --> SDK
    
    Arena --> Runtime
    SDK --> Runtime
    PackC --> Runtime
    
    Runtime --> OpenAI
    Runtime --> Anthropic
    Runtime --> Google
    Runtime --> MCP
    
    Arena --> PackC
    SDK --> PackC
```

## Component Architecture

```mermaid
graph LR
    subgraph "SDK Layer"
        Conv["Conversation API"]
        Pack["Pack Management"]
        Pipe["Pipeline API"]
    end
    
    subgraph "Runtime Layer"
        Prov["Providers"]
        Tools["Tool Execution"]
        State["State Management"]
        Prompt["Prompt Engine"]
    end
    
    subgraph "Infrastructure"
        Logger["Logging"]
        Persist["Persistence"]
        Valid["Validation"]
    end
    
    Conv --> Prov
    Pack --> Prompt
    Pipe --> Tools
    
    Prov --> Logger
    Tools --> State
    State --> Persist
    Prompt --> Valid
```

## Data Flow Architecture

```mermaid
sequenceDiagram
    participant User
    participant SDK
    participant Runtime
    participant Provider
    participant Tools
    
    User->>SDK: Initialize Conversation
    SDK->>Runtime: Load Configuration
    Runtime->>Runtime: Validate Setup
    
    User->>SDK: Send Message
    SDK->>Runtime: Process Pipeline
    Runtime->>Provider: Execute LLM Call
    Provider-->>Runtime: Response
    
    alt Tool Call Required
        Runtime->>Tools: Execute Tool
        Tools-->>Runtime: Tool Result
        Runtime->>Provider: Continue with Tool Result
        Provider-->>Runtime: Final Response
    end
    
    Runtime-->>SDK: Formatted Response
    SDK-->>User: Final Result
```

## Testing Architecture (Arena)

```mermaid
graph TD
    subgraph "Test Definition"
        Scenario["Test Scenarios<br/>YAML Config"]
        Personas["AI Personas<br/>System Prompts"]
        Providers["Provider Matrix<br/>OpenAI, Anthropic, etc."]
    end
    
    subgraph "Execution Engine"
        Runner["Test Runner"]
        Validator["Assertion Engine"]
        Reporter["Report Generator"]
    end
    
    subgraph "Output"
        Results["Test Results<br/>JSON/HTML"]
        Metrics["Performance Metrics"]
        Artifacts["Conversation Logs"]
    end
    
    Scenario --> Runner
    Personas --> Runner
    Providers --> Runner
    
    Runner --> Validator
    Validator --> Reporter
    
    Reporter --> Results
    Reporter --> Metrics
    Reporter --> Artifacts
```

## Pack Format Architecture

```mermaid
graph LR
    subgraph "Source"
        Prompts["Prompt Templates"]
        Config["Configuration"]
        Tools["Tool Definitions"]
    end
    
    subgraph "Compilation (PackC)"
        Parser["YAML Parser"]
        Validator["Schema Validator"]
        Compiler["Pack Compiler"]
    end
    
    subgraph "Output"
        Pack["Compiled Pack<br/>Binary Format"]
        Meta["Metadata"]
        Index["Tool Index"]
    end
    
    Prompts --> Parser
    Config --> Parser
    Tools --> Parser
    
    Parser --> Validator
    Validator --> Compiler
    
    Compiler --> Pack
    Compiler --> Meta
    Compiler --> Index
```