/**
 * Paid Website — 2 sats to unlock full articles
 *
 * GET /api/articles           → list articles (free)
 * GET /api/articles/:slug     → preview (free) or full (paid)
 *
 * Preview is always free. Full content requires 2 sats.
 */

const express = require("express");
const { x402Middleware } = require("../shared/x402-middleware");

const router = express.Router();

const ARTICLES = [
  {
    slug: "bitcoin-micropayments",
    title: "How Bitcoin Micropayments Change the Web",
    author: "Satoshi Dev",
    date: "2025-12-01",
    preview: "Micropayments have long been theorized as an alternative to advertising-funded content. With Bitcoin's low transaction fees, this vision is finally practical.",
    content: "Micropayments have long been theorized as an alternative to advertising-funded content. With Bitcoin's low transaction fees, this vision is finally practical.\n\nThe x402 protocol enables any HTTP endpoint to require payment before serving a response. Instead of subscriptions or API keys, each request settles independently using a single Bitcoin transaction.\n\nThis creates a new economic model where content creators are paid per-read, API providers are paid per-call, and AI agents can autonomously purchase data they need.\n\nThe implications are profound: no more paywalls requiring accounts, no more free tiers subsidized by enterprise contracts, and no more advertising as the default business model for the web.",
  },
  {
    slug: "machine-commerce",
    title: "Machine Commerce: When AI Agents Pay for APIs",
    author: "Agent Builder",
    date: "2025-11-15",
    preview: "As AI agents become more autonomous, they need the ability to pay for resources without human intervention. HTTP 402 makes this possible.",
    content: "As AI agents become more autonomous, they need the ability to pay for resources without human intervention. HTTP 402 makes this possible.\n\nImagine an AI research agent that needs weather data, financial datasets, and language translation — all from different providers. With x402, the agent simply makes HTTP requests. If a 402 response is returned, the agent's payment client automatically settles the transaction and retries.\n\nNo API keys to manage. No OAuth flows. No billing dashboards. Just HTTP requests and Bitcoin transactions.\n\nThis is machine commerce: autonomous economic activity between software systems, settled at the protocol level.",
  },
  {
    slug: "request-level-settlement",
    title: "Request-Level Settlement: The End of API Keys",
    author: "Protocol Engineer",
    date: "2025-10-20",
    preview: "API keys were invented because HTTP had no native payment mechanism. The x402 protocol changes this by embedding settlement into the request-response cycle.",
    content: "API keys were invented because HTTP had no native payment mechanism. The x402 protocol changes this by embedding settlement into the request-response cycle.\n\nWith request-level settlement, every API call is independently paid for. The server doesn't need to track who is calling — it only needs to verify that payment was made.\n\nThis eliminates entire categories of infrastructure: user databases, API key management, rate limiting based on identity, billing systems, and subscription management.\n\nThe 402 status code was reserved in HTTP/1.1 for exactly this purpose. Decades later, Bitcoin finally provides the settlement layer that makes it work.",
  },
];

// Free endpoint: list articles
router.get("/api/articles", (req, res) => {
  res.json(
    ARTICLES.map(({ slug, title, author, date, preview }) => ({
      slug,
      title,
      author,
      date,
      preview,
    }))
  );
});

// Free: article preview (no middleware)
// GET /api/articles/:slug → 200 OK with preview
// GET /api/articles/:slug?unlock=true → falls through to paid handler
router.get("/api/articles/:slug", (req, res, next) => {
  if (req.query.unlock === "true") return next("route");

  const article = ARTICLES.find((a) => a.slug === req.params.slug);
  if (!article) {
    return res.status(404).json({ error: "Article not found" });
  }

  res.json({
    slug: article.slug,
    title: article.title,
    author: article.author,
    date: article.date,
    preview: article.preview,
    locked: true,
    unlock_price: 2,
    unlock_url: `/api/articles/${article.slug}?unlock=true`,
  });
});

// Paid: full article content (middleware gates access)
// GET /api/articles/:slug?unlock=true + X402-Proof → 200 with full content
router.get(
  "/api/articles/:slug",
  x402Middleware({ price: 2, description: "Full article — 2 sats" }),
  (req, res) => {
    const article = ARTICLES.find((a) => a.slug === req.params.slug);
    if (!article) {
      return res.status(404).json({ error: "Article not found" });
    }

    res.json({
      ...article,
      locked: false,
      payment: req.x402,
    });
  }
);

module.exports = router;
