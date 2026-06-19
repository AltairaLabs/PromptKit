# Sandbox image for the codegen-agent test harness.
# codegen-sandbox base (MCP server + dev tools) + promptarena + packc + gate scripts.
# Build via: make build-codegen-agent-sandbox
FROM ghcr.io/altairalabs/codegen-sandbox@sha256:10628364c517c125d3f4de8fc3ab8160d45b99434d46b1b38872aed4230dbbdd

# linux/amd64 binaries, built by `make build-arena-linux build-packc-linux`
# --chmod=755 sets executable bit at COPY time (avoids a RUN chmod as non-root)
COPY --chmod=755 bin/promptarena-linux /usr/local/bin/promptarena
COPY --chmod=755 bin/packc-linux /usr/local/bin/packc

# gate + metric scripts
COPY --chmod=755 examples/test-a-codegen-agent/tools/unused-files.sh /usr/local/bin/unused-files.sh
COPY --chmod=755 examples/test-a-codegen-agent/tools/idiom-traps.sh /usr/local/bin/idiom-traps.sh

# Brief the agent exactly like a real PromptArena project: drop AGENTS.md and the
# full .claude/skills/promptarena-authoring/SKILL.md into the workspace so the
# agent has the same tooling a developer's coding agent would (not just the shim).
RUN mkdir -p /workspace && /usr/local/bin/promptarena agent-brief /workspace

# Expose the sandbox HTTP API for workspace capture (/api/download).
# The base image's entrypoint is the sandbox binary; CMD supplies its args.
# CMD replaces the base image's default args, so re-state -addr/-workspace
# explicitly, then add the API flags.
CMD ["-addr=:8080", "-workspace=/workspace", "-enable-api", "-api-addr", ":8090", "-dev-mode"]
