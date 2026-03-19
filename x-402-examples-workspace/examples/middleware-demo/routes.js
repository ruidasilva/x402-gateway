/**
 * Express Middleware Example
 *
 * Demonstrates how developers add x402 payment gating to existing
 * Express APIs using a single middleware function.
 *
 * GET /api/middleware/echo    — 1 sat (cheapest possible)
 * GET /api/middleware/premium — 10 sats (premium tier)
 * GET /api/middleware/info    — free (shows middleware source)
 */

const express = require("express");
const { x402Middleware } = require("../shared/x402-middleware");

const router = express.Router();

// Free: show how the middleware works
router.get("/api/middleware/info", (req, res) => {
  res.json({
    title: "x402 Express Middleware",
    description:
      "Add payment gating to any Express route with a single middleware function.",
    usage: `
const { x402Middleware } = require("./shared/x402-middleware");

// Protect any route with a price
app.get("/api/weather/:city",
  x402Middleware({ price: 3, description: "Weather data" }),
  weatherHandler
);

// That's it. The middleware handles:
// 1. Detecting missing payment → returns 402 with challenge
// 2. Verifying proof header → passes to your handler
// 3. Attaching payment info to req.x402
    `.trim(),
    examples: [
      { endpoint: "/api/middleware/echo", price: 1, method: "GET" },
      { endpoint: "/api/middleware/premium", price: 10, method: "GET" },
    ],
  });
});

// Paid: echo endpoint (1 sat)
router.get(
  "/api/middleware/echo",
  x402Middleware({ price: 1, description: "Echo endpoint — 1 sat" }),
  (req, res) => {
    res.json({
      echo: "Payment received! This response was gated at 1 satoshi.",
      headers: {
        host: req.headers.host,
        accept: req.headers.accept,
        "user-agent": req.headers["user-agent"],
      },
      query: req.query,
      timestamp: new Date().toISOString(),
      payment: req.x402,
    });
  }
);

// Paid: premium endpoint (10 sats)
router.get(
  "/api/middleware/premium",
  x402Middleware({ price: 10, description: "Premium content — 10 sats" }),
  (req, res) => {
    res.json({
      tier: "premium",
      message:
        "This is premium content gated at 10 satoshis. In a production system, this could be a high-value API, exclusive dataset, or premium service.",
      secret: "The x402 protocol makes every HTTP endpoint monetizable.",
      timestamp: new Date().toISOString(),
      payment: req.x402,
    });
  }
);

module.exports = router;
