/**
 * x402 Developer Playground — Frontend Application
 *
 * Single-page app that demonstrates the x402 payment flow
 * for each example API. Connects to the backend server
 * and visualizes each step of the protocol.
 */

// ─── Base Path Detection ─────────────────────────────
// Auto-detect base path from URL. When served under /playground/, all
// API calls and pushState URLs need this prefix.
const BASE_PATH = (() => {
  const path = window.location.pathname;
  const knownRoutes = ["weather", "summarize", "articles", "data", "query", "middleware"];
  // Check if we're under a subpath like /playground/
  for (const route of knownRoutes) {
    const idx = path.indexOf("/" + route);
    if (idx > 0) return path.substring(0, idx);
  }
  // Check if path itself is the base (e.g. /playground/ or /playground)
  const clean = path.replace(/\/$/, "");
  if (clean && clean !== "/" && !knownRoutes.includes(clean.substring(1))) {
    return clean;
  }
  return "";
})();

function apiUrl(path) {
  return BASE_PATH + path;
}

// ─── Routing ──────────────────────────────────────────

const ROUTES = ["weather", "summarize", "articles", "data", "query", "middleware"];

function navigate(route) {
  // Hide all pages
  document.querySelectorAll(".page").forEach((p) => (p.style.display = "none"));
  document.querySelectorAll(".page").forEach((p) => p.classList.remove("active"));

  // Remove active from nav
  document.querySelectorAll(".nav-link").forEach((l) => l.classList.remove("active"));

  if (ROUTES.includes(route)) {
    const page = document.getElementById("page-" + route);
    if (page) {
      page.style.display = "block";
      page.classList.add("active");
    }
    const navLink = document.querySelector(`[data-route="${route}"]`);
    if (navLink) navLink.classList.add("active");
    history.pushState(null, "", BASE_PATH + "/" + route);
  } else {
    const landing = document.getElementById("landing");
    landing.style.display = "block";
    landing.classList.add("active");
    history.pushState(null, "", BASE_PATH + "/");
  }
}

// Handle browser back/forward
window.addEventListener("popstate", () => {
  const path = window.location.pathname.replace(BASE_PATH, "").replace(/^\//, "");
  navigate(path || "landing");
});

// Handle nav clicks
document.querySelectorAll(".nav-link").forEach((link) => {
  link.addEventListener("click", (e) => {
    e.preventDefault();
    navigate(link.dataset.route);
  });
});

// Initial route
(function initRoute() {
  const path = window.location.pathname.replace(BASE_PATH, "").replace(/^\//, "");
  if (ROUTES.includes(path)) {
    navigate(path);
  }
})();

// ─── Payment Flow Inspector ───────────────────────────

function createInspector(containerId, append = false) {
  const container = document.getElementById(containerId);
  if (!append) {
    container.innerHTML = `<div class="inspector-title">Payment Flow Inspector</div>`;
  }
  return {
    container,
    addStep(icon, label, detail, rawHttp) {
      const step = document.createElement("div");
      step.className = "inspector-step";
      step.innerHTML = `
        <div class="step-icon ${icon}">${iconChar(icon)}</div>
        <div class="step-content">
          <div class="step-label">${label}</div>
          ${detail ? `<div class="step-detail">${detail}</div>` : ""}
          ${rawHttp ? `<div class="raw-http">${rawHttp}</div>` : ""}
        </div>
      `;
      this.container.appendChild(step);
    },
  };
}

function iconChar(type) {
  switch (type) {
    case "active": return "...";
    case "done":   return "\u2713";
    case "error":  return "\u2717";
    default:       return "\u00B7";
  }
}

function kvLine(key, value) {
  return `<div class="kv"><span class="key">${key}:</span> <span class="val">${truncate(value, 64)}</span></div>`;
}

function truncate(str, len) {
  if (!str) return "";
  str = String(str);
  return str.length > len ? str.slice(0, len) + "..." : str;
}

function delay(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

// ─── Core x402 Flow ───────────────────────────────────

/**
 * Executes the full x402 payment flow for a given endpoint.
 *
 * 1. Send original request → get 402
 * 2. Simulate payment (via server-side mock)
 * 3. Retry with proof → get 200
 *
 * Updates the inspector in real-time.
 */
async function executeX402Flow({
  method = "GET",
  url,
  body = null,
  inspectorId,
  responseId,
  appendInspector = false,
}) {
  const inspector = createInspector(inspectorId, appendInspector);
  const responsePanel = document.getElementById(responseId);
  if (!appendInspector) responsePanel.innerHTML = "";

  // Step 1: Send original request
  inspector.addStep(
    "done",
    "Request Sent",
    kvLine("method", method) + kvLine("url", url),
    `<span class="method">${method}</span> ${url}\nAccept: application/json${body ? "\nContent-Type: application/json\n\n" + truncate(JSON.stringify(body), 200) : ""}`
  );

  await delay(300);

  // Make the actual request (no proof)
  const fetchOpts = {
    method,
    headers: { Accept: "application/json", "Content-Type": "application/json" },
  };
  if (body) fetchOpts.body = JSON.stringify(body);

  let res;
  try {
    res = await fetch(url, fetchOpts);
  } catch (err) {
    inspector.addStep("error", "Request Failed", kvLine("error", err.message));
    return;
  }

  if (res.status !== 402) {
    // No payment required (free endpoint or error)
    const data = await res.json().catch(() => ({}));
    inspector.addStep(
      res.ok ? "done" : "error",
      `${res.status} ${res.statusText}`,
      kvLine("status", res.status)
    );
    showResponse(responsePanel, data, res.status);
    return;
  }

  // Step 2: 402 Payment Required
  const challengeHeader = res.headers.get("X402-Challenge");
  const amountSats = res.headers.get("X402-Amount-Sats");
  const challengeBody = await res.json().catch(() => ({}));

  inspector.addStep(
    "done",
    "402 Payment Required",
    kvLine("amount", amountSats + " sats") +
      kvLine("scheme", "bsv-tx-v1") +
      kvLine("challenge_hash", challengeBody.challenge_hash),
    `<span class="status-402">HTTP/1.1 402 Payment Required</span>\n<span class="header-name">X402-Challenge:</span> ${truncate(challengeHeader, 80)}\n<span class="header-name">X402-Accept:</span> bsv-tx-v1\n<span class="header-name">X402-Amount-Sats:</span> ${amountSats}`
  );

  await delay(400);

  // Step 3: Simulate payment (build tx, delegate, broadcast)
  inspector.addStep("active", "Processing Payment...", kvLine("status", "building transaction"));

  let paymentResult;
  try {
    const simRes = await fetch(apiUrl("/api/x402/simulate-payment"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        challengeHeader,
        method,
        path: new URL(url, window.location.origin).pathname,
        query: new URL(url, window.location.origin).search.replace("?", ""),
      }),
    });
    paymentResult = await simRes.json();
  } catch (err) {
    inspector.addStep("error", "Payment Failed", kvLine("error", err.message));
    return;
  }

  // Remove the "Processing" step
  const processingStep = inspector.container.querySelector(".inspector-step:last-child");
  if (processingStep) processingStep.remove();

  // Show payment broadcast step
  await delay(200);
  inspector.addStep(
    "done",
    "Payment Broadcast",
    kvLine("txid", paymentResult.txid) +
      kvLine("amount", amountSats + " sats") +
      kvLine("network", "simulated"),
    `Transaction broadcast to network\n<span class="header-name">txid:</span> ${paymentResult.txid}`
  );

  await delay(300);

  // Step 4: Proof attached
  inspector.addStep(
    "done",
    "Proof Attached",
    kvLine("txid", paymentResult.txid) +
      kvLine("challenge_hash", paymentResult.challengeHash),
    `<span class="method">${method}</span> ${url}\n<span class="header-name">X402-Proof:</span> ${truncate(paymentResult.proofEncoded, 80)}`
  );

  await delay(300);

  // Step 5: Retry request with proof
  const retryOpts = {
    method,
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
      "X402-Proof": paymentResult.proofEncoded,
    },
  };
  if (body) retryOpts.body = JSON.stringify(body);

  let finalRes;
  try {
    finalRes = await fetch(url, retryOpts);
  } catch (err) {
    inspector.addStep("error", "Retry Failed", kvLine("error", err.message));
    return;
  }

  const finalData = await finalRes.json().catch(() => ({}));
  const receipt = finalRes.headers.get("X402-Receipt");
  const status = finalRes.headers.get("X402-Status");

  inspector.addStep(
    finalRes.ok ? "done" : "error",
    "Response Received",
    kvLine("status", finalRes.status) +
      kvLine("x402-status", status || "n/a") +
      kvLine("receipt", receipt),
    `<span class="status-200">HTTP/1.1 ${finalRes.status} ${finalRes.statusText}</span>\n<span class="header-name">X402-Receipt:</span> ${receipt || "n/a"}\n<span class="header-name">X402-Status:</span> ${status || "n/a"}`
  );

  showResponse(responsePanel, finalData, finalRes.status);
}

function showResponse(panel, data, status, titleOverride) {
  const isOk = status >= 200 && status < 300;
  const title = titleOverride || (isOk ? "Response — " + status + " OK" : "Error — " + status);
  panel.innerHTML = `
    <div class="response-title">${title}</div>
    <div class="response-body">${JSON.stringify(data, null, 2)}</div>
  `;
}

// ─── Example Handlers ─────────────────────────────────

function runWeather() {
  const city = document.getElementById("weather-city").value;
  executeX402Flow({
    method: "GET",
    url: apiUrl(`/api/weather/${city}`),
    inspectorId: "weather-inspector",
    responseId: "weather-response",
  });
}

function runSummarize() {
  const text = document.getElementById("summarize-text").value;
  executeX402Flow({
    method: "POST",
    url: apiUrl("/api/summarize"),
    body: { text },
    inspectorId: "summarize-inspector",
    responseId: "summarize-response",
  });
}

async function runArticle() {
  const slug = document.getElementById("article-slug").value;
  const inspector = createInspector("articles-inspector");
  const responsePanel = document.getElementById("articles-response");
  responsePanel.innerHTML = "";

  // Phase 1: Fetch free preview
  inspector.addStep(
    "done",
    "Preview Request",
    kvLine("url", apiUrl(`/api/articles/${slug}`)),
    `<span class="method">GET</span> ${apiUrl(`/api/articles/${slug}`)}`
  );

  await delay(300);

  let previewData;
  try {
    const previewRes = await fetch(apiUrl(`/api/articles/${slug}`), {
      headers: { Accept: "application/json" },
    });
    previewData = await previewRes.json();
  } catch (err) {
    inspector.addStep("error", "Preview Failed", kvLine("error", err.message));
    return;
  }

  inspector.addStep(
    "done",
    "Preview Received (Free)",
    kvLine("title", previewData.title) +
      kvLine("locked", "true") +
      kvLine("unlock_price", previewData.unlock_price + " sats"),
    `<span class="status-200">HTTP/1.1 200 OK</span>\nFree preview — no payment required`
  );

  showResponse(responsePanel, previewData, 200, "Preview — Free Content");

  await delay(500);

  // Phase 2: Unlock full content via payment flow
  await executeX402Flow({
    method: "GET",
    url: apiUrl(`/api/articles/${slug}?unlock=true`),
    inspectorId: "articles-inspector",
    responseId: "articles-response",
    appendInspector: true,
  });
}

function runData() {
  const dataset = document.getElementById("data-dataset").value;
  const endpoint =
    dataset === "nasdaq"
      ? apiUrl("/api/financial/nasdaq/history")
      : apiUrl("/api/financial/sp500/history");
  executeX402Flow({
    method: "GET",
    url: endpoint,
    inspectorId: "data-inspector",
    responseId: "data-response",
  });
}

function runQuery() {
  const question = document.getElementById("query-question").value;
  executeX402Flow({
    method: "POST",
    url: apiUrl("/api/query"),
    body: { question },
    inspectorId: "query-inspector",
    responseId: "query-response",
  });
}

function runMiddleware() {
  const endpoint = document.getElementById("middleware-endpoint").value;
  executeX402Flow({
    method: "GET",
    url: apiUrl(`/api/middleware/${endpoint}`),
    inspectorId: "middleware-inspector",
    responseId: "middleware-response",
  });
}
