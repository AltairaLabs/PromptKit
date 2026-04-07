FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY benchmarks/frameworks/genkit/ ./
RUN go mod tidy && go build -o /bench-genkit .

FROM alpine:3.20
COPY --from=builder /bench-genkit /usr/local/bin/bench-genkit
ENTRYPOINT ["bench-genkit"]
