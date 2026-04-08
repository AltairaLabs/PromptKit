import { createOpenAI } from "@ai-sdk/openai";
import { streamText } from "ai";
import { z } from "zod";

const openai = createOpenAI({
  baseURL: process.env.OPENAI_BASE_URL || "http://localhost:8081/v1",
  apiKey: process.env.OPENAI_API_KEY || "sk-bench-fake",
});

const TOOL_URL = process.env.TOOL_URL || "http://localhost:8085";

interface ChatMessage {
  role: string;
  content: string;
}

interface ChatRequest {
  messages: ChatMessage[];
  stream?: boolean;
}

export async function POST(req: Request) {
  const body: ChatRequest = await req.json();
  const messages = body.messages ?? [];

  if (messages.length === 0) {
    return Response.json({ error: "no messages" }, { status: 400 });
  }

  const userContent = messages[messages.length - 1].content;

  const result = await streamText({
    model: openai("gpt-4o"),
    prompt: userContent,
    tools: {
      lookup_order: {
        description: "Look up an order by ID",
        parameters: z.object({ order_id: z.string() }),
        execute: async ({ order_id }) => {
          const resp = await fetch(`${TOOL_URL}/tool`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ order_id }),
          });
          return resp.json();
        },
      },
    },
    maxSteps: 2,
  });

  const encoder = new TextEncoder();

  const stream = new ReadableStream({
    async start(controller) {
      try {
        for await (const chunk of result.textStream) {
          const data = JSON.stringify({
            choices: [{ delta: { content: chunk }, finish_reason: null }],
          });
          controller.enqueue(encoder.encode(`data: ${data}\n\n`));
        }
        const stop = JSON.stringify({
          choices: [{ delta: {}, finish_reason: "stop" }],
        });
        controller.enqueue(encoder.encode(`data: ${stop}\n\n`));
        controller.enqueue(encoder.encode("data: [DONE]\n\n"));
        controller.close();
      } catch (err) {
        controller.error(err);
      }
    },
  });

  return new Response(stream, {
    headers: {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
      Connection: "keep-alive",
    },
  });
}
