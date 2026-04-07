FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY benchmarks/go.mod benchmarks/go.sum ./benchmarks/
COPY benchmarks/mockupstream/ ./benchmarks/mockupstream/
COPY benchmarks/internal/ ./benchmarks/internal/
RUN go build -C benchmarks -o /mockupstream ./mockupstream/

FROM alpine:3.20
COPY --from=builder /mockupstream /usr/local/bin/mockupstream
ENTRYPOINT ["mockupstream"]
