/**
 * Data Marketplace — 4 sats per dataset request
 *
 * GET /api/financial/sp500/history
 *
 * Returns simulated historical S&P 500 data.
 * Payment-gated at 4 satoshis per request.
 */

const express = require("express");
const { x402Middleware } = require("../shared/x402-middleware");

const router = express.Router();

function generateSP500History() {
  const data = [];
  let price = 4500;
  const startDate = new Date("2025-01-01");

  for (let i = 0; i < 30; i++) {
    const date = new Date(startDate);
    date.setDate(date.getDate() + i);

    const change = (Math.random() - 0.48) * 50;
    price = Math.round((price + change) * 100) / 100;

    data.push({
      date: date.toISOString().split("T")[0],
      open: Math.round((price - Math.random() * 20) * 100) / 100,
      high: Math.round((price + Math.random() * 30) * 100) / 100,
      low: Math.round((price - Math.random() * 30) * 100) / 100,
      close: price,
      volume: Math.floor(Math.random() * 5000000000) + 2000000000,
    });
  }

  return data;
}

// List available datasets (free)
router.get("/api/financial", (req, res) => {
  res.json({
    datasets: [
      {
        id: "sp500-history",
        name: "S&P 500 Historical Data",
        description: "30-day simulated historical OHLCV data",
        price_sats: 4,
        endpoint: "/api/financial/sp500/history",
      },
      {
        id: "nasdaq-history",
        name: "NASDAQ Historical Data",
        description: "30-day simulated historical OHLCV data",
        price_sats: 4,
        endpoint: "/api/financial/nasdaq/history",
      },
    ],
  });
});

// Paid endpoint: S&P 500 data
router.get(
  "/api/financial/sp500/history",
  x402Middleware({ price: 4, description: "S&P 500 historical data — 4 sats" }),
  (req, res) => {
    res.json({
      dataset: "S&P 500",
      period: "30 days",
      records: 30,
      data: generateSP500History(),
      payment: req.x402,
    });
  }
);

// Paid endpoint: NASDAQ data
router.get(
  "/api/financial/nasdaq/history",
  x402Middleware({ price: 4, description: "NASDAQ historical data — 4 sats" }),
  (req, res) => {
    const data = generateSP500History().map((d) => ({
      ...d,
      close: Math.round(d.close * 3.2 * 100) / 100,
      open: Math.round(d.open * 3.2 * 100) / 100,
      high: Math.round(d.high * 3.2 * 100) / 100,
      low: Math.round(d.low * 3.2 * 100) / 100,
    }));

    res.json({
      dataset: "NASDAQ Composite",
      period: "30 days",
      records: 30,
      data,
      payment: req.x402,
    });
  }
);

module.exports = router;
