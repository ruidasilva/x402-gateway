#!/usr/bin/env node
/**
 * x402 CLI Demo
 *
 * Demonstrates the full x402 payment flow from the terminal:
 *   request → 402 challenge → payment → proof → response
 *
 * Usage:
 *   node examples/cli-demo.js weather london
 *   node examples/cli-demo.js weather tokyo
 *   node examples/cli-demo.js summarize "Bitcoin enables micropayments on the web."
 *   node examples/cli-demo.js articles bitcoin-micropayments
 *   node examples/cli-demo.js data sp500
 *   node examples/cli-demo.js query "What is x402?"
 *   node examples/cli-demo.js middleware echo
 *
 * The server must be running: npm run dev
 */

const BASE = process.env.X402_SERVER || "http://localhost:3000";

// ── Route mapping ────────────────────────────────────

function buildRequest(command, arg) {
  switch (command) {
    case "weather":
      return { method: "GET", url: `${BASE}/api/weather/${arg || "london"}` };
    case "summarize":
      return {
        method: "POST",
        url: `${BASE}/api/summarize`,
        body: { text: arg || "Bitcoin enables micropayments on the web." },
      };
    case "articles":
      return { method: "GET", url: `${BASE}/api/articles/${arg || "bitcoin-micropayments"}?unlock=true` };
    case "data":
      return {
        method: "GET",
        url: `${BASE}/api/financial/${arg === "nasdaq" ? "nasdaq" : "sp500"}/history`,
      };
    case "query":
      return {
        method: "POST",
        url: `${BASE}/api/query`,
        body: { question: arg || "What is x402?" },
      };
    case "middleware":
      return { method: "GET", url: `${BASE}/api/middleware/${arg || "echo"}` };
    default:
      return null;
  }
}

// ── Logging helpers ──────────────────────────────────

const dim = (s) => `\x1b[2m${s}\x1b[0m`;
const bold = (s) => `\x1b[1m${s}\x1b[0m`;
const green = (s) => `\x1b[32m${s}\x1b[0m`;
const yellow = (s) => `\x1b[33m${s}\x1b[0m`;
const cyan = (s) => `\x1b[36m${s}\x1b[0m`;
const red = (s) => `\x1b[31m${s}\x1b[0m`;

function step(arrow, msg) {
  console.log(`  ${arrow} ${msg}`);
}

// ── Main flow ────────────────────────────────────────

async function main() {
  const [command, ...rest] = process.argv.slice(2);
  const arg = rest.join(" ") || undefined;

  if (!command || command === "help" || command === "--help") {
    console.log(`
  ${bold("x402 CLI Demo")}

  Usage:
    node examples/cli-demo.js <command> [argument]

  Commands:
    weather <city>        Weather API (3 sats)
    summarize <text>      LLM Summarizer (5 sats)
    articles <slug>       Paid Website (2 sats)
    data <sp500|nasdaq>   Data Marketplace (4 sats)
    query <question>      Knowledge Query (3 sats)
    middleware <endpoint>  Middleware Demo (1-10 sats)

  The server must be running: npm run dev
`);
    process.exit(0);
  }

  const req = buildRequest(command, arg);
  if (!req) {
    console.error(red(`  Unknown command: ${command}`));
    console.error(dim("  Run with --help to see available commands."));
    process.exit(1);
  }

  console.log();
  step(cyan("→"), `Requesting ${bold(req.method)} ${dim(req.url)}`);

  // ── Step 1: Send original request (no proof) ──

  const fetchOpts = {
    method: req.method,
    headers: { Accept: "application/json", "Content-Type": "application/json" },
  };
  if (req.body) fetchOpts.body = JSON.stringify(req.body);

  let res;
  try {
    res = await fetch(req.url, fetchOpts);
  } catch (err) {
    step(red("✗"), `Connection failed: ${err.message}`);
    step(dim(""), "Is the server running? Start it with: npm run dev");
    process.exit(1);
  }

  if (res.status !== 402) {
    // No payment required — show response directly
    const data = await res.json().catch(() => ({}));
    step(green("←"), `${res.status} ${res.statusText}`);
    console.log(dim(JSON.stringify(data, null, 2)));
    return;
  }

  // ── Step 2: 402 Payment Required ──

  const challengeHeader = res.headers.get("X402-Challenge");
  const amountSats = res.headers.get("X402-Amount-Sats");
  step(yellow("←"), `${bold("402 Payment Required")}`);
  step(dim(""), `Amount: ${amountSats} sats`);

  // ── Step 3: Simulate payment ──

  step(cyan("→"), `Paying ${amountSats} sats ${dim("(simulated)")}`);

  const simRes = await fetch(`${BASE}/api/x402/simulate-payment`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      challengeHeader,
      method: req.method,
      path: new URL(req.url).pathname,
      query: new URL(req.url).search.replace("?", ""),
    }),
  });

  if (!simRes.ok) {
    const err = await simRes.json().catch(() => ({}));
    step(red("✗"), `Payment failed: ${err.error || simRes.statusText}`);
    process.exit(1);
  }

  const payment = await simRes.json();

  step(cyan("→"), `Broadcasting transaction ${dim(payment.txid.slice(0, 16) + "...")}`);
  step(green("←"), `Payment accepted`);

  // ── Step 4: Retry with proof ──

  const retryOpts = {
    method: req.method,
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
      "X402-Proof": payment.proofEncoded,
    },
  };
  if (req.body) retryOpts.body = JSON.stringify(req.body);

  const finalRes = await fetch(req.url, retryOpts);
  const data = await finalRes.json().catch(() => ({}));

  if (!finalRes.ok) {
    step(red("✗"), `${finalRes.status} ${finalRes.statusText}`);
    console.log(dim(JSON.stringify(data, null, 2)));
    process.exit(1);
  }

  // ── Step 5: Display result ──

  const receipt = finalRes.headers.get("X402-Receipt");
  step(green("←"), `${bold("200 OK")} ${dim(`receipt: ${receipt ? receipt.slice(0, 16) + "..." : "n/a"}`)}`);
  console.log();

  // Pretty-print based on command
  switch (command) {
    case "weather":
      console.log(`  ${bold(data.city)}: ${data.temp}°C, ${data.condition}`);
      console.log(dim(`  Humidity: ${data.humidity}% · Wind: ${data.wind_kph} kph`));
      break;
    case "summarize":
      console.log(`  ${bold("Summary:")}`);
      console.log(`  ${data.summary}`);
      console.log(dim(`  ${data.original_word_count} words → ${data.summary_word_count} words (${data.compression_ratio} compression)`));
      break;
    case "articles":
      console.log(`  ${bold(data.title)}`);
      console.log(dim(`  By ${data.author} · ${data.date}`));
      console.log();
      console.log(`  ${data.content ? data.content.slice(0, 200) + "..." : data.preview}`);
      break;
    case "data":
      console.log(`  ${bold(data.dataset)} — ${data.period} (${data.records} records)`);
      if (data.data && data.data.length > 0) {
        const last = data.data[data.data.length - 1];
        console.log(dim(`  Latest: ${last.date} close=${last.close}`));
      }
      break;
    case "query":
      console.log(`  ${bold("Q:")} ${data.question}`);
      console.log(`  ${bold("A:")} ${data.answer}`);
      console.log(dim(`  Confidence: ${data.confidence} · Source: ${data.source}`));
      break;
    case "middleware":
      console.log(`  ${data.echo || data.message || JSON.stringify(data)}`);
      break;
    default:
      console.log(dim(JSON.stringify(data, null, 2)));
  }

  console.log();
}

main().catch((err) => {
  console.error(red(`  Error: ${err.message}`));
  process.exit(1);
});
