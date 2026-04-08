import { createOpenAI } from "@ai-sdk/openai";
import { streamText } from "ai";

const openai = createOpenAI({
  baseURL: process.env.OPENAI_BASE_URL || "http://localhost:8081/v1",
  apiKey: process.env.OPENAI_API_KEY || "sk-bench-fake",
});

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

  if (!body.stream) {
    const result = await streamText({
      model: openai("gpt-4o"),
      prompt: userContent,
    });
    let text = "";
    for await (const chunk of result.textStream) {
      text += chunk;
    }
    return Response.json({
      choices: [
        {
          message: { role: "assistant", content: text },
          finish_reason: "stop",
        },
      ],
    });
  }

  // Streaming: emit OpenAI-compatible SSE
  const result = await streamText({
    model: openai("gpt-4o"),
    prompt: userContent,
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
