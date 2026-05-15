// Tiny single-file vanilla JS layer. No frameworks, no build pipeline.
// Polls /api/query per panel and renders a minimal SVG sparkline.

const REFRESH_MS = 2000;
const WINDOW_MS = 5 * 60 * 1000; // 5 minutes

function fmt(n) {
  if (n === null || n === undefined || Number.isNaN(n)) return "—";
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
  const metric = panel.dataset.metric;
  const to = Date.now();
  const from = to - WINDOW_MS;
  let resp;
  try {
    resp = await fetch(`/api/query?metric=${encodeURIComponent(metric)}&from=${from}&to=${to}`);
  } catch (e) {
    return;
  }
  if (!resp.ok) return;
  const body = await resp.json();
  const series = (body.series || [])[0];
  const points = series ? series.points : [];
  renderSparkline(panel.querySelector("svg"), points);
  const last = points.length ? points[points.length - 1][1] : null;
  panel.querySelector(".last").textContent = fmt(last);
}

function tick() {
  if (document.visibilityState === "hidden") return;
  document.querySelectorAll(".panel").forEach(refreshPanel);
}

tick();
setInterval(tick, REFRESH_MS);
