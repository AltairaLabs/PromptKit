FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -C benchmarks/frameworks/promptkit/round1 -o /bench-promptkit-round1 .

FROM alpine:3.20
COPY --from=builder /bench-promptkit-round1 /usr/local/bin/bench-promptkit-round1
COPY benchmarks/frameworks/promptkit/round1/chat.pack.json /app/chat.pack.json
ENTRYPOINT ["bench-promptkit-round1", "--pack", "/app/chat.pack.json"]
