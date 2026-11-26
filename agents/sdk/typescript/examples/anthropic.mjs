// Makes two real calls through a metered Anthropic client, one not streamed and
// one streamed, and sends their usage to FlexPrice.
//
// Put these three lines in agents/sdk/typescript/.env (git ignores that file):
//
//   ANTHROPIC_API_KEY=your Anthropic key
//   FLEXPRICE_API_KEY=a FlexPrice key for a sandbox environment
//   FLEXPRICE_BASE_URL=https://us.api.flexprice.io/v1   (India: https://api.cloud.flexprice.io/v1)
//
// Then, from agents/sdk/typescript:
//
//   npm run build
//   node --env-file=.env examples/anthropic.mjs
//
// Afterwards, find the two events in FlexPrice under Developers, Events
// debugger, for the customer "agent-sdk-test".
import Anthropic from "@anthropic-ai/sdk";
import { createAgentMeter } from "../dist/index.js";

for (const name of ["ANTHROPIC_API_KEY", "FLEXPRICE_API_KEY", "FLEXPRICE_BASE_URL"]) {
  if (!process.env[name]) {
    console.error(`${name} is not set. See the top of this file.`);
    process.exit(1);
  }
}

let problems = 0;
const meter = createAgentMeter({
  apiKey: process.env.FLEXPRICE_API_KEY,
  baseUrl: process.env.FLEXPRICE_BASE_URL,
  debug: true, // prints each event as it is built
  onError: (error, where) => {
    problems++;
    console.error(`[problem, ${where}] ${error.message}`);
  },
});

// The client reads ANTHROPIC_API_KEY from the environment itself.
const claude = meter.wrap(new Anthropic());

const params = {
  model: "claude-haiku-5-5",
  max_tokens: 40,
  messages: [{ role: "user", content: "Say hello in five words." }],
};

await meter.withLabels({ customerId: "agent-sdk-test", agentId: "first-real-call" }, async () => {
  console.log("\n1. A call that is not streamed");
  const message = await claude.messages.create(params);
  console.log(`   Claude said: ${message.content[0]?.text ?? "(no text)"}`);
  console.log(`   Anthropic's usage: ${JSON.stringify(message.usage)}`);

  console.log("\n2. A streamed call");
  const stream = await claude.messages.create({ ...params, stream: true });
  let text = "";
  for await (const event of stream) {
    if (event.type === "content_block_delta" && event.delta.type === "text_delta") text += event.delta.text;
  }
  console.log(`   Claude said: ${text}`);
});

console.log("\n3. Sending the events to FlexPrice");
await meter.flush();

if (problems === 0) console.log("   Done. FlexPrice accepted both events.");
else console.log(`   Done, with ${problems} problem(s) above.`);
