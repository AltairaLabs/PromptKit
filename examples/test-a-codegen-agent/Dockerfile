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
