/**
 * x402 Developer Examples — Playground Server
 *
 * Single Express server hosting all example APIs and the frontend playground.
 * Run with: npm run dev
 * Open:     http://localhost:3000
 */

const express = require("express");
const path = require("path");
const { simulatePayment } = require("./shared/x402-payment-simulator");

const app = express();
const PORT = process.env.PORT || 3000;

// --- Middleware ---
app.use(express.json());
app.use(express.urlencoded({ extended: false }));

// CORS for local dev
app.use((req, res, next) => {
  res.header("Access-Control-Allow-Origin", "*");
  res.header(
    "Access-Control-Allow-Headers",
    "Content-Type, X402-Proof"
  );
  res.header(
    "Access-Control-Expose-Headers",
    "X402-Challenge, X402-Accept, X402-Amount-Sats, X402-Receipt, X402-Receipt-Time, X402-Status"
  );
  res.header("Access-Control-Allow-Methods", "GET, POST, OPTIONS");
  if (req.method === "OPTIONS") return res.sendStatus(204);
  next();
});

// --- Static frontend ---
app.use(express.static(path.join(__dirname, "..", "frontend-playground", "public")));

// --- Example API Routes ---
app.use(require("./paid-api-weather/routes"));
app.use(require("./llm-summarizer/routes"));
app.use(require("./paid-website/routes"));
app.use(require("./data-marketplace/routes"));
app.use(require("./knowledge-query/routes"));
app.use(require("./middleware-demo/routes"));

// --- Payment Simulation Endpoint ---
// The frontend calls this to simulate the client-side payment flow.
// In production, the real x402 client (X402Client.fetch()) handles this automatically.
// This endpoint exists only for the playground UI to visualize each step.
app.post("/api/x402/simulate-payment", async (req, res) => {
  const { challengeHeader, method, path: reqPath, query, body: reqBody } = req.body;

  if (!challengeHeader) {
    return res.status(400).json({ error: "Missing challengeHeader" });
  }

  try {
    const result = await simulatePayment(challengeHeader, {
      method,
      path: reqPath,
      query,
      body: reqBody ? JSON.stringify(reqBody) : null,
      headers: {},
    });
    res.json(result);
  } catch (err) {
    res.status(400).json({ error: "Payment simulation failed", detail: err.message });
  }
});

// --- Playground page routes (serve index.html for SPA routes) ---
const SPA_ROUTES = ["/weather", "/summarize", "/articles", "/data", "/query", "/middleware"];
SPA_ROUTES.forEach((route) => {
  app.get(route, (req, res) => {
    res.sendFile(
      path.join(__dirname, "..", "frontend-playground", "public", "index.html")
    );
  });
});

// --- Start ---
app.listen(PORT, () => {
  console.log(`\n  x402 Developer Examples Playground`);
  console.log(`  ===================================`);
  console.log(`  http://localhost:${PORT}\n`);
  console.log(`  Example routes:`);
  console.log(`    /weather     — Weather API (3 sats)`);
  console.log(`    /summarize   — LLM Summarizer (5 sats)`);
  console.log(`    /articles    — Paid Website (2 sats)`);
  console.log(`    /data        — Data Marketplace (4 sats)`);
  console.log(`    /query       — Knowledge Query (3 sats)`);
  console.log(`    /middleware  — Express Middleware (1-10 sats)\n`);
  console.log(`  API endpoints:`);
  console.log(`    GET  /api/weather/:city`);
  console.log(`    POST /api/summarize`);
  console.log(`    GET  /api/articles`);
  console.log(`    GET  /api/articles/:slug`);
  console.log(`    GET  /api/financial/sp500/history`);
  console.log(`    POST /api/query`);
  console.log(`    GET  /api/middleware/echo`);
  console.log(`    GET  /api/middleware/premium\n`);
});
