---
name: OpenAI Realtime API Integration
title: '[FEATURE] OpenAI Realtime API Integration for Multimodal Streaming'
labels: ['enhancement', 'needs-triage', 'streaming']
assignees: ''
---

## Feature Summary

**Brief Description**
Integrate OpenAI's Realtime API into PromptKit to enable bidirectional multimodal streaming for voice agents and real-time audio interactions. This will provide low-latency speech-to-speech interactions using GPT-4o Realtime models with seamless integration into the existing pipeline architecture.

**Component**
- [x] SDK
- [x] Runtime
- [ ] Arena CLI
- [ ] PackC CLI  
- [ ] Documentation
- [ ] Examples
- [ ] Infrastructure/Tooling

## Problem Statement

**What problem does this solve?**
PromptKit currently supports Gemini streaming but lacks OpenAI Realtime API support, limiting voice agent capabilities and real-time interaction options for users who prefer OpenAI models.

**Current Workarounds**
Users can only use Gemini for streaming voice interactions. OpenAI integration requires custom implementation outside of PromptKit.

## Proposed Solution

**Detailed Description**
Implement `OpenAIRealtimeSession` conforming to the `providers.StreamInputSession` interface. The existing `DuplexProviderStage` pipeline can orchestrate the integration without modifications. The implementation should support:

- Multimodal inputs: Audio and text
- Multimodal outputs: Audio and text
- Function calling during streaming sessions
- Server-side VAD and session controls

**Estimated Effort**: 60-80 engineering hours
**Estimated Timeline**: 2-3 weeks
**Dependencies**: Stage-based pipeline (complete), Gemini streaming implementation (complete)
