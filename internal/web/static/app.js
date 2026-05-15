// owl frontend — vanilla JS, single file, no build pipeline.
// Two responsibilities:
//   1. Theme (light/dark) handling.
//   2. Per-panel polling and sparkline rendering.

(function () {
  "use strict";

  // ────────────────────────────────────────────────────────────────────────
  // Theme

  var STORAGE_KEY = "owl-theme";
  // The pre-paint inline script in the document head has already set
  // data-theme; this is the runtime API for the toggle button.

  function setTheme(theme) {
    if (theme !== "light" && theme !== "dark") return;
    document.documentElement.setAttribute("data-theme", theme);
    try { localStorage.setItem(STORAGE_KEY, theme); } catch (e) { /* ignore */ }
  }

  function currentTheme() {
    return document.documentElement.getAttribute("data-theme") || "light";
  }

  function bindThemeToggle() {
    var btn = document.querySelector("[data-theme-toggle]");
    if (!btn) return;
    btn.addEventListener("click", function () {
      setTheme(currentTheme() === "dark" ? "light" : "dark");
    });
  }

  // ────────────────────────────────────────────────────────────────────────
  // Number formatting (unit-aware)

  function fmt(n, unit) {
    if (n === null || n === undefined || Number.isNaN(n)) return "—";
    var abs = Math.abs(n);
    switch (unit) {
      case "bytes":
        if (abs >= 1073741824) return (n / 1073741824).toFixed(2) + " GB";
        if (abs >= 1048576)    return (n / 1048576).toFixed(2)    + " MB";
        if (abs >= 1024)       return (n / 1024).toFixed(2)       + " KB";
        return n.toFixed(0) + " B";
      case "s":
        if (abs >= 3600) return (n / 3600).toFixed(2) + " h";
        if (abs >= 60)   return (n / 60).toFixed(2)   + " m";
        return n.toFixed(3) + " s";
      case "ms":
        if (abs >= 1000) return (n / 1000).toFixed(2) + " s";
        return n.toFixed(1) + " ms";
    }
    if (abs >= 1e9) return (n / 1e9).toFixed(2) + "G";
    if (abs >= 1e6) return (n / 1e6).toFixed(2) + "M";
    if (abs >= 1e3) return (n / 1e3).toFixed(2) + "k";
    if (Number.isInteger(n)) return String(n);
    return n.toFixed(3);
  }

  // ────────────────────────────────────────────────────────────────────────
  // Chart rendering

  var SVG_NS = "http://www.w3.org/2000/svg";
  var SERIES_PALETTE_SIZE = 5;     // matches --series-1..--series-5 in CSS
  var GRID_LINES = 3;              // horizontal gridlines inside the chart
  var WINDOW_MS = 5 * 60 * 1000;   // 5 minutes
  var MIN_REFRESH_MS = 1000;
  var DEFAULT_REFRESH_MS = 5000;

  function createSvgEl(name, attrs) {
    var el = document.createElementNS(SVG_NS, name);
    if (attrs) {
      for (var k in attrs) {
        if (Object.prototype.hasOwnProperty.call(attrs, k)) {
          el.setAttribute(k, attrs[k]);
        }
      }
    }
    return el;
  }

  function clearChildren(node) {
    while (node.firstChild) node.removeChild(node.firstChild);
  }

  function renderChart(svg, seriesList) {
    clearChildren(svg);
    if (!seriesList.length) return;

    var w = svg.clientWidth || 600;
    var h = svg.clientHeight || 110;
    svg.setAttribute("viewBox", "0 0 " + w + " " + h);

    // Compute global y range across all series.
    var minY = Infinity, maxY = -Infinity, minX = Infinity, maxX = -Infinity;
    for (var i = 0; i < seriesList.length; i++) {
      var pts = seriesList[i].points;
      for (var j = 0; j < pts.length; j++) {
        var x = pts[j][0], y = pts[j][1];
        if (x < minX) minX = x;
        if (x > maxX) maxX = x;
        if (y < minY) minY = y;
        if (y > maxY) maxY = y;
      }
    }
    if (!isFinite(minX)) return;
    if (maxY === minY) { maxY = minY + 1; }
    if (maxX === minX) { maxX = minX + 1; }

    // Padding inside the chart so lines don't touch the edge.
    var padTop = 6;
    var padBottom = 6;
    var padX = 2;
    var innerH = h - padTop - padBottom;
    var innerW = w - padX * 2;

    function scaleY(v) {
      return padTop + innerH - ((v - minY) / (maxY - minY)) * innerH;
    }
    function scaleX(v) {
      return padX + ((v - minX) / (maxX - minX)) * innerW;
    }

    // Gridlines (horizontal only).
    for (var g = 1; g <= GRID_LINES; g++) {
      var gy = padTop + (innerH * g) / (GRID_LINES + 1);
      svg.appendChild(createSvgEl("line", {
        x1: 0, y1: gy.toFixed(1), x2: w, y2: gy.toFixed(1), class: "gridline"
      }));
    }
    // Baseline at the bottom (dashed).
    svg.appendChild(createSvgEl("line", {
      x1: 0, y1: (h - 1).toFixed(1), x2: w, y2: (h - 1).toFixed(1), class: "baseline"
    }));

    // Series paths.
    for (var s = 0; s < seriesList.length; s++) {
      var slot = (s % SERIES_PALETTE_SIZE) + 1;
      var d = "";
      var pts = seriesList[s].points;
      for (var p = 0; p < pts.length; p++) {
        var px = scaleX(pts[p][0]);
        var py = scaleY(pts[p][1]);
        d += (p === 0 ? "M" : "L") + px.toFixed(1) + "," + py.toFixed(1);
      }
      svg.appendChild(createSvgEl("path", {
        d: d, class: "series series--" + slot
      }));
      // Marker at the latest point of each series.
      var last = pts[pts.length - 1];
      svg.appendChild(createSvgEl("circle", {
        cx: scaleX(last[0]).toFixed(1),
        cy: scaleY(last[1]).toFixed(1),
        r: 2,
        class: "marker marker--" + slot
      }));
    }
  }

  // Build the legend below the chart from the series labels.
  function renderLegend(legendEl, seriesList) {
    clearChildren(legendEl);
    if (seriesList.length <= 1) return; // single series doesn't need a legend
    for (var i = 0; i < seriesList.length; i++) {
      var slot = (i % SERIES_PALETTE_SIZE) + 1;
      var ser = seriesList[i];
      var item = document.createElement("span");
      item.className = "panel__legend-item";
      var swatch = document.createElement("span");
      swatch.className = "panel__legend-swatch";
      swatch.style.background = "var(--series-" + slot + ")";
      item.appendChild(swatch);
      var label = document.createElement("span");
      label.textContent = labelFor(ser);
      item.appendChild(label);
      legendEl.appendChild(item);
    }
  }

  // Derive a short legend label from a series. Prefer a non-job label
  // since "job" is usually identical across all series of one panel.
  function labelFor(ser) {
    var labels = ser.labels || {};
    var keys = Object.keys(labels).filter(function (k) {
      return k !== "job" && k !== "instance";
    });
    if (keys.length === 0) keys = Object.keys(labels);
    if (keys.length === 0) return ser.metric;
    return keys.map(function (k) { return k + "=" + labels[k]; }).join(" ");
  }

  // ────────────────────────────────────────────────────────────────────────
  // Per-panel polling

  function refreshPanel(panel) {
    if (panel.dataset.status === "unsupported") return;
    var expr = panel.dataset.expr;
    if (!expr) return;

    var unit = panel.dataset.unit || "";
    var refresh = parseInt(panel.dataset.refresh, 10) || DEFAULT_REFRESH_MS;
    var step = Math.max(Math.floor(refresh / 2), 1000);
    var to = Date.now();
    var from = to - WINDOW_MS;

    fetch("/api/query?expr=" + encodeURIComponent(expr) +
          "&from=" + from + "&to=" + to + "&step=" + step)
      .then(function (resp) { return resp.ok ? resp.json() : null; })
      .then(function (body) {
        if (!body) return;
        var seriesList = (body.series || []).filter(function (s) {
          return s && s.points && s.points.length > 0;
        });

        var svg = panel.querySelector(".panel__chart");
        if (svg) renderChart(svg, seriesList);

        var legend = panel.querySelector(".panel__legend");
        if (legend) renderLegend(legend, seriesList);

        var valueEl = panel.querySelector(".panel__value");
        if (valueEl) {
          var firstSeries = seriesList[0];
          var lastPoint = firstSeries
            ? firstSeries.points[firstSeries.points.length - 1]
            : null;
          var lastValue = lastPoint ? lastPoint[1] : null;
          valueEl.textContent = fmt(lastValue, unit);
          valueEl.classList.toggle("panel__value--placeholder", lastValue === null);
        }
      })
      .catch(function () { /* network error — keep last render */ });
  }

  function schedulePanels() {
    var panels = document.querySelectorAll(".panel");
    panels.forEach(function (panel) {
      if (panel.dataset.status === "unsupported") return;
      var interval = Math.max(
        parseInt(panel.dataset.refresh, 10) || DEFAULT_REFRESH_MS,
        MIN_REFRESH_MS
      );

      function tick() {
        if (document.visibilityState !== "hidden") refreshPanel(panel);
      }
      tick(); // immediate
      setInterval(tick, interval);
    });
  }

  // ────────────────────────────────────────────────────────────────────────
  // Boot

  function init() {
    bindThemeToggle();
    schedulePanels();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
