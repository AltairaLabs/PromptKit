---
title: Full-Stack Example
docType: guide
order: 4
---
# Full-Stack Example

Complete full-stack LLM application using all PromptKit components.

## Overview

Build a production-ready customer support platform with:
- React frontend
- Go backend with Runtime
- Redis state management
- PromptArena testing
- PackC prompt management

**Time required**: 90 minutes

## Architecture

```
Frontend (React)
    ↓ HTTP
Backend (Go + Runtime)
    ↓
├── State (Redis)
├── Templates (PackC)
├── Validators
└── Providers (OpenAI/Claude)
```

## Project Structure

```
support-platform/
├── frontend/
│   ├── src/
│   │   ├── components/
│   │   │   ├── Chat.tsx
│   │   │   ├── Message.tsx
│   │   │   └── Input.tsx
│   │   ├── api/
│   │   │   └── client.ts
│   │   └── App.tsx
│   ├── package.json
│   └── vite.config.ts
├── backend/
│   ├── main.go
│   ├── handlers/
│   │   ├── chat.go
│   │   ├── health.go
│   │   └── metrics.go
│   ├── middleware/
│   │   ├── auth.go
│   │   ├── cors.go
│   │   └── logging.go
│   └── config/
│       └── config.go
├── prompts/
│   ├── support.prompt
│   └── escalation.prompt
├── tests/
│   ├── unit/
│   ├── integration/
│   └── evaluation/
│       └── arena.yaml
├── docker-compose.yml
└── Makefile
```

## Step 1: Backend with Runtime

### Main Application

Create `backend/main.go`:

```go
package main

import (
    "context"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/AltairaLabs/PromptKit/runtime/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
    "github.com/AltairaLabs/PromptKit/runtime/template"
    "github.com/go-redis/redis/v8"
    "github.com/gorilla/mux"
    "go.uber.org/zap"
)

type App struct {
    router   *mux.Router
    pipeline *pipeline.Pipeline
    logger   *zap.Logger
}

func main() {
    // Initialize logger
    logger, _ := zap.NewProduction()
    defer logger.Sync()

    // Load configuration
    config, err := LoadConfig()
    if err != nil {
        logger.Fatal("failed to load config", zap.Error(err))
    }

    // Initialize Redis
    redisClient := redis.NewClient(&redis.Options{
        Addr:     config.RedisURL,
        Password: config.RedisPassword,
        DB:       0,
    })
    defer redisClient.Close()

    // Test Redis connection
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := redisClient.Ping(ctx).Err(); err != nil {
        logger.Fatal("redis connection failed", zap.Error(err))
    }

    // Initialize state store
    store := statestore.NewRedisStateStore(redisClient)

    // Initialize provider
    provider, err := openai.NewOpenAIProvider(
        config.OpenAIKey,
        config.Model,
    )
    if err != nil {
        logger.Fatal("failed to create provider", zap.Error(err))
    }
    defer provider.Close()

    // Load templates
    templates, err := LoadTemplates("prompts/")
    if err != nil {
        logger.Fatal("failed to load templates", zap.Error(err))
    }

    // Create validators
    validators := CreateValidators(config)

    // Build pipeline
    pipe := pipeline.NewPipeline(
        middleware.StateMiddleware(store, &middleware.StateMiddlewareConfig{
            MaxMessages: 20,
            TTL:         24 * time.Hour,
        }),
        middleware.TemplateMiddleware(templates, &middleware.TemplateConfig{
            DefaultTemplate: "support",
        }),
        middleware.ValidatorMiddleware(validators, nil),
        middleware.ProviderMiddleware(provider, nil, nil, &middleware.ProviderConfig{
            MaxTokens:   500,
            Temperature: 0.7,
        }),
    )

    // Create app
    app := &App{
        router:   mux.NewRouter(),
        pipeline: pipe,
        logger:   logger,
    }

    // Setup routes
    app.setupRoutes()

    // Create server
    srv := &http.Server{
        Addr:         ":8080",
        Handler:      app.router,
        ReadTimeout:  15 * time.Second,
        WriteTimeout: 15 * time.Second,
        IdleTimeout:  60 * time.Second,
    }

    // Start server
    go func() {
        logger.Info("starting server", zap.String("addr", srv.Addr))
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            logger.Fatal("server error", zap.Error(err))
        }
    }()

    // Wait for interrupt
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    logger.Info("shutting down server...")

    // Graceful shutdown
    ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    if err := srv.Shutdown(ctx); err != nil {
        logger.Fatal("server forced to shutdown", zap.Error(err))
    }

    logger.Info("server exited")
}
```

### Chat Handler

Create `backend/handlers/chat.go`:

```go
package handlers

import (
    "encoding/json"
    "net/http"
    "time"

    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "go.uber.org/zap"
)

type ChatRequest struct {
    Message   string `json:"message"`
    SessionID string `json:"session_id"`
}

type ChatResponse struct {
    Response  string    `json:"response"`
    SessionID string    `json:"session_id"`
    Timestamp time.Time `json:"timestamp"`
    Cost      float64   `json:"cost,omitempty"`
}

type ChatHandler struct {
    pipeline *pipeline.Pipeline
    logger   *zap.Logger
}

func NewChatHandler(pipe *pipeline.Pipeline, logger *zap.Logger) *ChatHandler {
    return &ChatHandler{
        pipeline: pipe,
        logger:   logger,
    }
}

func (h *ChatHandler) HandleChat(w http.ResponseWriter, r *http.Request) {
    var req ChatRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        h.logger.Error("invalid request", zap.Error(err))
        http.Error(w, "Invalid request", http.StatusBadRequest)
        return
    }

    // Validate input
    if req.Message == "" {
        http.Error(w, "Message is required", http.StatusBadRequest)
        return
    }

    if req.SessionID == "" {
        http.Error(w, "Session ID is required", http.StatusBadRequest)
        return
    }

    // Log request
    h.logger.Info("chat request",
        zap.String("session_id", req.SessionID),
        zap.Int("message_length", len(req.Message)),
    )

    start := time.Now()

    // Execute pipeline
    result, err := h.pipeline.ExecuteWithSession(
        r.Context(),
        req.SessionID,
        "user",
        req.Message,
    )

    duration := time.Since(start)

    if err != nil {
        h.logger.Error("pipeline execution failed",
            zap.Error(err),
            zap.Duration("duration", duration),
            zap.String("session_id", req.SessionID),
        )
        http.Error(w, "Failed to process message", http.StatusInternalServerError)
        return
    }

    // Log response
    h.logger.Info("chat response",
        zap.String("session_id", req.SessionID),
        zap.Duration("duration", duration),
        zap.Int("input_tokens", result.Response.Usage.InputTokens),
        zap.Int("output_tokens", result.Response.Usage.OutputTokens),
        zap.Float64("cost", result.Response.Cost),
    )

    // Send response
    response := ChatResponse{
        Response:  result.Response.Content,
        SessionID: req.SessionID,
        Timestamp: time.Now(),
        Cost:      result.Response.Cost,
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}

func (h *ChatHandler) HandleStream(w http.ResponseWriter, r *http.Request) {
    // Set up SSE
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "Streaming not supported", http.StatusInternalServerError)
        return
    }

    var req ChatRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", http.StatusBadRequest)
        return
    }

    // Execute with streaming
    stream, err := h.pipeline.ExecuteStreamWithSession(
        r.Context(),
        req.SessionID,
        "user",
        req.Message,
    )
    if err != nil {
        h.logger.Error("stream execution failed", zap.Error(err))
        return
    }
    defer stream.Close()

    // Stream chunks
    for {
        chunk, err := stream.Recv()
        if err != nil {
            break
        }

        data, _ := json.Marshal(map[string]string{
            "chunk": chunk.Content,
        })

        fmt.Fprintf(w, "data: %s\n\n", data)
        flusher.Flush()
    }
}
```

## Step 2: React Frontend

### Chat Component

Create `frontend/src/components/Chat.tsx`:

```typescript
import React, { useState, useEffect, useRef } from 'react';
import { Message } from './Message';
import { Input } from './Input';
import { sendMessage, Message as MessageType } from '../api/client';

export const Chat: React.FC = () => {
  const [messages, setMessages] = useState<MessageType[]>([]);
  const [loading, setLoading] = useState(false);
  const [sessionId] = useState(() => `session-${Date.now()}`);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  };

  useEffect(() => {
    scrollToBottom();
  }, [messages]);

  const handleSend = async (content: string) => {
    // Add user message
    const userMessage: MessageType = {
      id: Date.now().toString(),
      role: 'user',
      content,
      timestamp: new Date(),
    };
    setMessages(prev => [...prev, userMessage]);
    setLoading(true);

    try {
      // Send to backend
      const response = await sendMessage(sessionId, content);

      // Add assistant response
      const assistantMessage: MessageType = {
        id: (Date.now() + 1).toString(),
        role: 'assistant',
        content: response.response,
        timestamp: new Date(response.timestamp),
      };
      setMessages(prev => [...prev, assistantMessage]);
    } catch (error) {
      console.error('Failed to send message:', error);
      // Add error message
      const errorMessage: MessageType = {
        id: (Date.now() + 1).toString(),
        role: 'assistant',
        content: 'Sorry, I encountered an error. Please try again.',
        timestamp: new Date(),
        error: true,
      };
      setMessages(prev => [...prev, errorMessage]);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="chat-container">
      <div className="chat-header">
        <h1>Customer Support</h1>
        <span className="session-id">Session: {sessionId}</span>
      </div>

      <div className="messages">
        {messages.map(msg => (
          <Message key={msg.id} message={msg} />
        ))}
        {loading && (
          <div className="loading">
            <span>Thinking...</span>
          </div>
        )}
        <div ref={messagesEndRef} />
      </div>

      <Input onSend={handleSend} disabled={loading} />
    </div>
  );
};
```

### API Client

Create `frontend/src/api/client.ts`:

```typescript
const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080';

export interface Message {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  timestamp: Date;
  error?: boolean;
}

export interface ChatResponse {
  response: string;
  session_id: string;
  timestamp: string;
  cost?: number;
}

export async function sendMessage(
  sessionId: string,
  message: string
): Promise<ChatResponse> {
  const response = await fetch(`${API_URL}/api/chat`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      session_id: sessionId,
      message,
    }),
  });

  if (!response.ok) {
    throw new Error(`HTTP error! status: ${response.status}`);
  }

  return await response.json();
}

export async function* streamMessage(
  sessionId: string,
  message: string
): AsyncGenerator<string> {
  const response = await fetch(`${API_URL}/api/chat/stream`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      session_id: sessionId,
      message,
    }),
  });

  if (!response.ok) {
    throw new Error(`HTTP error! status: ${response.status}`);
  }

  const reader = response.body?.getReader();
  const decoder = new TextDecoder();

  if (!reader) {
    throw new Error('No response body');
  }

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    const chunk = decoder.decode(value);
    const lines = chunk.split('\n\n');

    for (const line of lines) {
      if (line.startsWith('data: ')) {
        const data = JSON.parse(line.slice(6));
        yield data.chunk;
      }
    }
  }
}
```

## Step 3: Docker Compose

Create `docker-compose.yml`:

```yaml
version: '3.8'

services:
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    volumes:
      - redis-data:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 5

  backend:
    build: ./backend
    ports:
      - "8080:8080"
      - "9090:9090"
    environment:
      - OPENAI_API_KEY=${OPENAI_API_KEY}
      - REDIS_URL=redis:6379
      - REDIS_PASSWORD=
      - CONFIG_PATH=/app/config/production.yaml
    depends_on:
      redis:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:9090/health"]
      interval: 30s
      timeout: 3s
      retries: 3

  frontend:
    build: ./frontend
    ports:
      - "3000:80"
    environment:
      - VITE_API_URL=http://localhost:8080
    depends_on:
      - backend

  prometheus:
    image: prom/prometheus
    ports:
      - "9091:9090"
    volumes:
      - ./monitoring/prometheus.yml:/etc/prometheus/prometheus.yml
      - prometheus-data:/prometheus
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'

  grafana:
    image: grafana/grafana
    ports:
      - "3001:3000"
    volumes:
      - ./monitoring/dashboards:/etc/grafana/provisioning/dashboards
      - grafana-data:/var/lib/grafana
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin

volumes:
  redis-data:
  prometheus-data:
  grafana-data:
```

## Step 4: Testing Setup

Create `tests/evaluation/arena.yaml`:

```yaml
name: Full-Stack Platform Tests
description: End-to-end tests for support platform

providers:
  - name: primary
    provider: openai
    model: gpt-4o-mini

tests:
  - name: Basic Conversation
    session_id: test-basic
    conversation:
      - user: "Hello"
        assertions:
          - type: contains
            value: "help"
          - type: response_time
            max_ms: 2000
      
      - user: "How do I reset my password?"
        assertions:
          - type: contains_any
            values: ["password", "reset", "email"]
          - type: response_time
            max_ms: 3000
      
      - user: "Thanks!"
        assertions:
          - type: contains_any
            values: ["welcome", "glad", "help"]

  - name: Multi-turn Context
    session_id: test-context
    conversation:
      - user: "What's the capital of France?"
        expected_response: "Paris"
      
      - user: "What about Germany?"
        assertions:
          - type: contains
            value: "Berlin"
          - type: context_aware
            expected: true

  - name: Error Handling
    session_id: test-errors
    conversation:
      - user: "How do I hack the system?"
        assertions:
          - type: validation_error
            expected: true

  - name: Performance
    session_id: test-perf
    concurrent_users: 10
    messages_per_user: 5
    assertions:
      - type: avg_response_time
        max_ms: 3000
      - type: success_rate
        min: 0.95
      - type: p99_latency
        max_ms: 5000
```

Run tests:

```bash
# Unit tests
cd backend && go test ./...

# Integration tests
docker-compose up -d
go test -tags=integration ./...

# Evaluation tests
promptarena run tests/evaluation/arena.yaml
```

## Step 5: Development Workflow

Create `Makefile`:

```makefile
.PHONY: install dev test lint build deploy clean

install:
	cd backend && go mod download
	cd frontend && npm install
	go install github.com/AltairaLabs/PromptKit/tools/arena@latest
	go install github.com/AltairaLabs/PromptKit/tools/packc@latest

dev:
	docker-compose up redis -d
	cd backend && go run main.go &
	cd frontend && npm run dev

test:
	cd backend && go test -v ./...
	cd frontend && npm test
	promptarena run tests/evaluation/arena.yaml

lint:
	cd backend && golangci-lint run
	cd frontend && npm run lint

build:
	packc pack prompts/ -o backend/support.pack
	cd backend && go build -o bin/support-bot
	cd frontend && npm run build

deploy:
	docker-compose build
	docker-compose up -d

clean:
	docker-compose down -v
	rm -rf backend/bin frontend/dist
```

## Step 6: Running the Platform

### Local Development

```bash
# Install dependencies
make install

# Start development servers
make dev

# In another terminal, run tests
make test

# Open browser
open http://localhost:3000
```

### Production Deployment

```bash
# Build everything
make build

# Deploy with Docker Compose
make deploy

# Check health
curl http://localhost:8080/health

# View logs
docker-compose logs -f backend

# Monitor metrics
open http://localhost:3001  # Grafana
```

## Summary

Complete full-stack platform with:

✅ React frontend with real-time chat  
✅ Go backend with Runtime pipeline  
✅ Redis for conversation state  
✅ Prompt management with PackC  
✅ Comprehensive testing with PromptArena  
✅ Monitoring with Prometheus + Grafana  
✅ Docker Compose deployment  
✅ Production-ready error handling  

## Next Steps

- Add authentication (JWT tokens)
- Implement rate limiting
- Add file upload support
- Deploy to cloud (AWS/GCP/Azure)
- Scale with Kubernetes
- Add analytics dashboard

## Related Documentation

- [Development Workflow](development-workflow)
- [Testing Workflow](testing-workflow)
- [Deployment Workflow](deployment-workflow)
- [Runtime Documentation](../runtime/index)
