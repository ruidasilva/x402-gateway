/**
 * Knowledge Query API — 3 sats per question
 *
 * POST /api/query
 * Body: { "question": "..." }
 *
 * Returns an answer from a simulated knowledge base.
 * Payment-gated at 3 satoshis per request.
 */

const express = require("express");
const { x402Middleware } = require("../shared/x402-middleware");

const router = express.Router();

const KNOWLEDGE_BASE = {
  "what is the capital of portugal": {
    answer: "Lisbon",
    confidence: 0.99,
    source: "geography",
  },
  "what is the capital of france": {
    answer: "Paris",
    confidence: 0.99,
    source: "geography",
  },
  "what is the capital of japan": {
    answer: "Tokyo",
    confidence: 0.99,
    source: "geography",
  },
  "what is bitcoin": {
    answer:
      "Bitcoin is a decentralized digital currency that enables peer-to-peer transactions without intermediaries. It was introduced in 2009 by the pseudonymous Satoshi Nakamoto.",
    confidence: 0.95,
    source: "technology",
  },
  "what is x402": {
    answer:
      "x402 is a stateless settlement-gated HTTP protocol that uses Bitcoin transactions to gate API access. It leverages the HTTP 402 Payment Required status code to enable request-level micropayments.",
    confidence: 0.98,
    source: "protocol",
  },
  "what is a satoshi": {
    answer:
      "A satoshi is the smallest unit of Bitcoin, equal to 0.00000001 BTC. It is named after Bitcoin's creator, Satoshi Nakamoto.",
    confidence: 0.99,
    source: "cryptocurrency",
  },
  "how does http 402 work": {
    answer:
      "HTTP 402 Payment Required is a status code reserved for future use in HTTP/1.1. The x402 protocol implements it by having servers return a 402 response with a payment challenge. The client broadcasts a Bitcoin transaction, attaches proof to the retry request, and the server verifies the payment before serving the response.",
    confidence: 0.97,
    source: "protocol",
  },
};

function findAnswer(question) {
  const normalized = question.toLowerCase().trim().replace(/[?!.]+$/, "");

  // Exact match
  if (KNOWLEDGE_BASE[normalized]) {
    return KNOWLEDGE_BASE[normalized];
  }

  // Fuzzy match: find best keyword overlap
  let bestMatch = null;
  let bestScore = 0;

  const queryWords = normalized.split(/\s+/);

  for (const [key, value] of Object.entries(KNOWLEDGE_BASE)) {
    const keyWords = key.split(/\s+/);
    const overlap = queryWords.filter((w) => keyWords.includes(w)).length;
    const score = overlap / Math.max(queryWords.length, keyWords.length);

    if (score > bestScore && score > 0.3) {
      bestScore = score;
      bestMatch = value;
    }
  }

  if (bestMatch) {
    return { ...bestMatch, confidence: Math.round(bestScore * 100) / 100 };
  }

  return {
    answer: `I don't have specific information about "${question}" in my knowledge base. Try asking about capitals, Bitcoin, x402, or satoshis.`,
    confidence: 0.1,
    source: "fallback",
  };
}

router.post(
  "/api/query",
  x402Middleware({ price: 3, description: "Knowledge query — 3 sats" }),
  (req, res) => {
    const { question } = req.body || {};

    if (!question) {
      return res
        .status(400)
        .json({ error: "Missing 'question' field in request body" });
    }

    const result = findAnswer(question);

    res.json({
      question,
      ...result,
      timestamp: new Date().toISOString(),
      payment: req.x402,
    });
  }
);

module.exports = router;
