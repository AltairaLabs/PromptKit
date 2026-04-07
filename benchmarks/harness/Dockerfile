FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY benchmarks/go.mod benchmarks/go.sum ./benchmarks/
COPY benchmarks/harness/ ./benchmarks/harness/
COPY benchmarks/internal/ ./benchmarks/internal/
RUN go build -C benchmarks -o /harness ./harness/

FROM alpine:3.20
COPY --from=builder /harness /usr/local/bin/harness
ENTRYPOINT ["harness"]
