FROM node:22-slim AS builder
WORKDIR /app
COPY benchmarks/frameworks/vercelai/package.json benchmarks/frameworks/vercelai/package-lock.json* ./
RUN npm ci
COPY benchmarks/frameworks/vercelai/ .
RUN npm run build

FROM node:22-slim
WORKDIR /app
COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static
ENV PORT=8092
EXPOSE 8092
CMD ["node", "server.js"]
