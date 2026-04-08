import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: "standalone",
  async rewrites() {
    return [
      { source: "/v1/chat/completions", destination: "/api/chat" },
      { source: "/v1/chat/completions/tools", destination: "/api/chat-tools" },
      { source: "/health", destination: "/api/health" },
    ];
  },
};

export default nextConfig;
