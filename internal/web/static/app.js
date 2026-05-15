// Tiny single-file vanilla JS layer. No frameworks, no build pipeline.
// Polls /api/query per panel and renders a minimal SVG sparkline.

const WINDOW_MS = 5 * 60 * 1000; // 5 minutes
const MIN_REFRESH_MS = 1000;
const DEFAULT_REFRESH_MS = 5000;

function fmt(n, unit) {
  if (n === null || n === undefined || Number.isNaN(n)) return "—";
  if (unit === "bytes") {
    const abs = Math.abs(n);
    if (abs >= 1073741824) return (n / 1073741824).toFixed(2) + " GB";
    if (abs >= 1048576) return (n / 1048576).toFixed(2) + " MB";
    if (abs >= 1024) return (n / 1024).toFixed(2) + " KB";
    return n.toFixed(0) + " B";
  }
  if (unit === "s") {
    const abs = Math.abs(n);
    if (abs >= 3600) return (n / 3600).toFixed(2) + " h";
    if (abs >= 60) return (n / 60).toFixed(2) + " m";
    return n.toFixed(3) + " s";
  }
  if (unit === "ms") {
    if (Math.abs(n) >= 1000) return (n / 1000).toFixed(2) + " s";
    return n.toFixed(1) + " ms";
  }
  // Generic fallback
  if (Math.abs(n) >= 1e9) return (n / 1e9).toFixed(2) + "G";
  if (Math.abs(n) >= 1e6) return (n / 1e6).toFixed(2) + "M";
  if (Math.abs(n) >= 1e3) return (n / 1e3).toFixed(2) + "k";
  if (Number.isInteger(n)) return String(n);
  return n.toFixed(3);
}

function renderSparkline(svg, points) {
  while (svg.firstChild) svg.removeChild(svg.firstChild);
  if (!points.length) return;
  const w = svg.clientWidth || 600;
  const h = svg.clientHeight || 80;
  const xs = points.map(p => p[0]);
  const ys = points.map(p => p[1]);
  const minX = xs[0], maxX = xs[xs.length - 1];
  const minY = Math.min(...ys), maxY = Math.max(...ys);
  const spanX = (maxX - minX) || 1;
  const spanY = (maxY - minY) || 1;
  const path = points.map((p, i) => {
    const x = ((p[0] - minX) / spanX) * (w - 2) + 1;
    const y = h - 1 - ((p[1] - minY) / spanY) * (h - 2);
    return (i === 0 ? "M" : "L") + x.toFixed(1) + "," + y.toFixed(1);
  }).join(" ");
  const ns = "http://www.w3.org/2000/svg";
  svg.setAttribute("viewBox", `0 0 ${w} ${h}`);
  const el = document.createElementNS(ns, "path");
  el.setAttribute("d", path);
  el.setAttribute("fill", "none");
  el.setAttribute("stroke", "currentColor");
  el.setAttribute("stroke-width", "1.25");
  el.setAttribute("vector-effect", "non-scaling-stroke");
  svg.appendChild(el);
}

async function refreshPanel(panel) {
  const status = panel.dataset.status;
  if (status === "unsupported") return; // server already rendered the reason

  const expr = panel.dataset.expr;
  if (!expr) return;

  const unit = panel.dataset.unit || "";
  const step = Math.max(
    Math.floor((parseInt(panel.dataset.refresh, 10) || DEFAULT_REFRESH_MS) / 2),
    1000
  );
  const to = Date.now();
  const from = to - WINDOW_MS;

  let resp;
  try {
    resp = await fetch(
      `/api/query?expr=${encodeURIComponent(expr)}&from=${from}&to=${to}&step=${step}`
    );
  } catch (e) {
    return;
  }
  if (!resp.ok) return;

  const body = await resp.json();
  const series = (body.series || [])[0];
  const points = series ? series.points : [];

  const svg = panel.querySelector("svg");
  if (svg) renderSparkline(svg, points);

  const lastEl = panel.querySelector(".last");
  if (lastEl) {
    const last = points.length ? points[points.length - 1][1] : null;
    lastEl.textContent = fmt(last, unit);
  }
}

// Schedule per-panel refresh using each panel's own data-refresh interval.
function schedulePanels() {
  document.querySelectorAll(".panel").forEach(panel => {
    if (panel.dataset.status === "unsupported") return;

    const refreshMs = Math.max(
      parseInt(panel.dataset.refresh, 10) || DEFAULT_REFRESH_MS,
      MIN_REFRESH_MS
    );

    // Immediate first fetch (only when visible)
    if (document.visibilityState !== "hidden") {
      refreshPanel(panel);
    }

    setInterval(() => {
      if (document.visibilityState !== "hidden") {
        refreshPanel(panel);
      }
    }, refreshMs);
  });
}

schedulePanels();
