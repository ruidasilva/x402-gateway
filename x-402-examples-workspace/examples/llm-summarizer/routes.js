/**
 * LLM Summarizer — 5 sats per request
 *
 * POST /api/summarize
 * Body: { "text": "..." }
 *
 * Returns a simulated summary of the input text.
 * Payment-gated at 5 satoshis per request.
 */

const express = require("express");
const { x402Middleware } = require("../shared/x402-middleware");

const router = express.Router();

function summarize(text) {
  if (!text || text.trim().length === 0) {
    return "No text provided.";
  }

  const sentences = text
    .replace(/([.!?])\s+/g, "$1|")
    .split("|")
    .filter((s) => s.trim().length > 0);

  if (sentences.length <= 2) {
    return text.trim();
  }

  // Take first and last sentence as a simple extractive summary
  const summary = [sentences[0].trim(), sentences[sentences.length - 1].trim()]
    .join(" ")
    .trim();

  return summary;
}

router.post(
  "/api/summarize",
  x402Middleware({ price: 5, description: "Text summarization — 5 sats" }),
  (req, res) => {
    const { text } = req.body || {};

    if (!text) {
      return res.status(400).json({ error: "Missing 'text' field in request body" });
    }

    const summary = summarize(text);
    const wordCount = text.split(/\s+/).length;
    const summaryWordCount = summary.split(/\s+/).length;

    res.json({
      summary,
      original_word_count: wordCount,
      summary_word_count: summaryWordCount,
      compression_ratio: Math.round((1 - summaryWordCount / wordCount) * 100) + "%",
      model: "extractive-v1 (simulated)",
      payment: req.x402,
    });
  }
);

module.exports = router;
