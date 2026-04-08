.PHONY: round1 round2 all build clean help

TIERS := 10 50 100 250 500 1000

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

all: round1 round2 ## Run both rounds

round1: ## Run Round 1: LLM streaming (PromptKit vs LangChain vs Vercel AI SDK)
	@echo "=== Round 1: LLM Streaming ==="
	docker compose --profile round1 up --build -d mockupstream promptkit-round1 langchain vercelai
	@echo "Waiting for services..."
	@sleep 3
	@for tier in $(TIERS); do \
		echo "--- PromptKit @ $$tier concurrent ---"; \
		docker compose --profile round1 run --rm benchmark-round1 \
			--round round1 --target http://promptkit-round1:8090 \
			--framework promptkit --concurrency $$tier \
			--requests $$(( $$tier * 10 )) --output /results; \
		echo "--- LangChain @ $$tier concurrent ---"; \
		docker compose --profile round1 run --rm benchmark-round1 \
			--round round1 --target http://langchain:8091 \
			--framework langchain --concurrency $$tier \
			--requests $$(( $$tier * 10 )) --output /results; \
		echo "--- VercelAI @ $$tier concurrent ---"; \
		docker compose --profile round1 run --rm benchmark-round1 \
			--round round1 --target http://vercelai:8092 \
			--framework vercelai --concurrency $$tier \
			--requests $$(( $$tier * 10 )) --output /results; \
	done
	docker compose --profile round1 down

round2: ## Run Round 2: Voice pipeline (PromptKit vs Pipecat)
	@echo "=== Round 2: Voice Pipeline ==="
	docker compose --profile round2 up --build -d mockupstream promptkit-round2 pipecat
	@echo "Waiting for services..."
	@sleep 5
	@for tier in 1 5 10 25 50 100; do \
		echo "--- PromptKit @ $$tier concurrent ---"; \
		docker compose --profile round2 run --rm benchmark-round2 \
			--round round2 --target ws://promptkit-round2:8090 \
			--framework promptkit --concurrency $$tier \
			--requests $$(( $$tier * 5 )) --output /results; \
		echo "--- Pipecat @ $$tier concurrent ---"; \
		docker compose --profile round2 run --rm benchmark-round2 \
			--round round2 --target ws://pipecat:8092 \
			--framework pipecat --concurrency $$tier \
			--requests $$(( $$tier * 5 )) --output /results; \
	done
	docker compose --profile round2 down

build: ## Build mock upstream and harness locally
	go build -o bin/mockupstream ./mockupstream/
	go build -o bin/harness ./harness/

clean: ## Remove build artifacts and results
	rm -rf bin/ results/*.json results/*.csv results/*.md
