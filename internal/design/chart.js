// owl frontend — vanilla JS, single file, no build pipeline.
// Three responsibilities:
//   1. Theme (light/dark) handling.
//   2. Per-panel polling and chart rendering.
//   3. Hover interactions (crosshair + tooltip) over the chart.

(() => {
  // ────────────────────────────────────────────────────────────────────────
  // Theme

  var STORAGE_KEY = 'owl-theme';

  function setTheme(theme) {
    if (theme !== 'light' && theme !== 'dark') return;
    document.documentElement.setAttribute('data-theme', theme);
    try {
      localStorage.setItem(STORAGE_KEY, theme);
    } catch (_e) {
      /* ignore */
    }
  }
  function currentTheme() {
    return document.documentElement.getAttribute('data-theme') || 'light';
  }
  function bindThemeToggle() {
    var btn = document.querySelector('[data-theme-toggle]');
    if (!btn) return;
    btn.addEventListener('click', () => {
      setTheme(currentTheme() === 'dark' ? 'light' : 'dark');
    });
  }

  // ────────────────────────────────────────────────────────────────────────
  // Number formatting

  function fmt(n, unit) {
    if (n === null || n === undefined || Number.isNaN(n)) return '—';
    var abs = Math.abs(n);
    switch (unit) {
      case 'bytes':
        if (abs >= 1073741824) return (n / 1073741824).toFixed(2) + ' GB';
        if (abs >= 1048576) return (n / 1048576).toFixed(2) + ' MB';
        if (abs >= 1024) return (n / 1024).toFixed(1) + ' KB';
        return n.toFixed(0) + ' B';
      case 'Bps':
      case 'bytes/s':
        if (abs >= 1073741824) return (n / 1073741824).toFixed(2) + ' GB/s';
        if (abs >= 1048576) return (n / 1048576).toFixed(2) + ' MB/s';
        if (abs >= 1024) return (n / 1024).toFixed(1) + ' KB/s';
        return n.toFixed(0) + ' B/s';
      case 's':
        if (abs >= 3600) return (n / 3600).toFixed(2) + ' h';
        if (abs >= 60) return (n / 60).toFixed(2) + ' m';
        return n.toFixed(3) + ' s';
      case 'ms':
        if (abs >= 1000) return (n / 1000).toFixed(2) + ' s';
        return n.toFixed(1) + ' ms';
      case 'cores':
        return n.toFixed(2) + ' cores';
      case 'load':
        // Load average is unitless by convention.
        return n.toFixed(2);
      case 'percent':
        return (n * 100).toFixed(1) + ' %';
    }
    if (abs >= 1e9) return (n / 1e9).toFixed(2) + 'G';
    if (abs >= 1e6) return (n / 1e6).toFixed(2) + 'M';
    if (abs >= 1e3) return (n / 1e3).toFixed(1) + 'k';
    if (Number.isInteger(n)) return String(n);
    return n.toFixed(3);
  }

  // Compact axis-tick label, parameterised by precision (number of decimals
  // after a chosen magnitude scale).
  function fmtTickAt(n, unit, prec) {
    if (n === null || n === undefined || Number.isNaN(n)) return '—';
    var abs = Math.abs(n);
    switch (unit) {
      case 'bytes':
        if (abs >= 1073741824) return (n / 1073741824).toFixed(prec) + 'G';
        if (abs >= 1048576) return (n / 1048576).toFixed(prec) + 'M';
        if (abs >= 1024) return (n / 1024).toFixed(prec) + 'K';
        return n.toFixed(prec);
      case 'Bps':
      case 'bytes/s':
        if (abs >= 1073741824) return (n / 1073741824).toFixed(prec) + 'G/s';
        if (abs >= 1048576) return (n / 1048576).toFixed(prec) + 'M/s';
        if (abs >= 1024) return (n / 1024).toFixed(prec) + 'K/s';
        return n.toFixed(prec) + '/s';
      case 's':
        if (abs >= 60) return (n / 60).toFixed(prec) + 'm';
        return n.toFixed(Math.max(prec, 1));
      case 'ms':
        if (abs >= 1000) return (n / 1000).toFixed(prec) + 's';
        return n.toFixed(prec);
      case 'percent':
        return (n * 100).toFixed(prec) + '%';
      case 'cores':
      case 'load':
        return n.toFixed(Math.max(prec, 2));
    }
    if (abs >= 1e9) return (n / 1e9).toFixed(prec) + 'G';
    if (abs >= 1e6) return (n / 1e6).toFixed(prec) + 'M';
    if (abs >= 1e3) return (n / 1e3).toFixed(prec) + 'k';
    if (prec === 0 && Number.isInteger(n)) return String(n);
    return n.toFixed(prec);
  }

  // Format a list of tick values, increasing precision until all labels
  // are distinct. Caps at 3 decimals; collisions beyond that are
  // accepted (very tight ranges become meaningless anyway).
  function formatTicks(values, unit) {
    for (let p = 0; p <= 3; p++) {
      const labels = values.map((v) => fmtTickAt(v, unit, p));
      const seen = {};
      let dup = false;
      for (let i = 0; i < labels.length; i++) {
        if (seen[labels[i]]) {
          dup = true;
          break;
        }
        seen[labels[i]] = true;
      }
      if (!dup) return labels;
    }
    return values.map((v) => fmtTickAt(v, unit, 3));
  }

  // "5m 12s ago" / "12s ago" / "now"
  function fmtRelative(deltaMs) {
    if (deltaMs < 1500) return 'now';
    var s = Math.round(deltaMs / 1000);
    if (s < 60) return '−' + s + 's';
    var m = Math.floor(s / 60),
      rs = s % 60;
    if (m < 60) return rs > 0 && rs >= 5 ? '−' + m + 'm' + rs + 's' : '−' + m + 'm';
    var h = Math.floor(m / 60);
    return '−' + h + 'h';
  }

  // "12:34:56" absolute time of day, for the tooltip header
  function fmtTime(ms) {
    var d = new Date(ms);
    function pad(n) {
      return n < 10 ? '0' + n : String(n);
    }
    return pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds());
  }

  // ────────────────────────────────────────────────────────────────────────
  // Chart rendering

  var SVG_NS = 'http://www.w3.org/2000/svg';
  // Series palette size — twelve hues every 30° on the colour wheel.
  // Keep this in sync with the --series-1..--series-12 custom
  // properties in owl.css; the cycle wraps modulo this constant.
  var SERIES_PALETTE_SIZE = 12;
  var MIN_REFRESH_MS = 1000;
  var DEFAULT_REFRESH_MS = 5000;
  var PAD = { top: 8, right: 12, bottom: 16, left: 44 };

  // ────────────────────────────────────────────────────────────────────────
  // Time state — anchor + window.
  //
  //   anchor: number | null
  //     Right-edge timestamp in ms. null means live (anchor = Date.now()
  //     evaluated per tick).
  //   window: string
  //     One of WINDOW_KEYS; controls the duration.
  //
  // anchor lives in sessionStorage so a refresh does not strand a user
  // in the past. window lives in localStorage so the chosen zoom
  // survives across sessions, matching the legacy picker.

  var WINDOW_KEY = 'owl-range';
  var ANCHOR_KEY = 'owl-anchor';
  var WINDOW_OPTIONS = {
    '5m': 5 * 60 * 1000,
    '15m': 15 * 60 * 1000,
    '1h': 60 * 60 * 1000,
    '6h': 6 * 60 * 60 * 1000,
    '24h': 24 * 60 * 60 * 1000,
  };
  var TARGET_POINTS = 240;

  function currentWindowKey() {
    var stored;
    try {
      stored = localStorage.getItem(WINDOW_KEY);
    } catch (_e) {
      /* ignore */
    }
    return WINDOW_OPTIONS[stored] ? stored : '5m';
  }
  function currentWindowMs() {
    return WINDOW_OPTIONS[currentWindowKey()];
  }
  function setWindow(key) {
    if (!WINDOW_OPTIONS[key]) return;
    try {
      localStorage.setItem(WINDOW_KEY, key);
    } catch (_e) {
      /* ignore */
    }
    repaintAll();
  }

  function currentAnchor() {
    var raw;
    try {
      raw = sessionStorage.getItem(ANCHOR_KEY);
    } catch (_e) {
      /* ignore */
    }
    if (!raw) return null;
    var n = parseInt(raw, 10);
    return Number.isFinite(n) ? n : null;
  }
  function setAnchor(ms) {
    try {
      if (ms === null) sessionStorage.removeItem(ANCHOR_KEY);
      else sessionStorage.setItem(ANCHOR_KEY, String(ms));
    } catch (_e) {
      /* ignore */
    }
    repaintAll();
    renderTimeNav();
  }
  function isLive() {
    return currentAnchor() === null;
  }
  function effectiveTo() {
    var a = currentAnchor();
    return a === null ? Date.now() : a;
  }
  function effectiveStepMs() {
    return Math.max(Math.round(currentWindowMs() / TARGET_POINTS), 1000);
  }

  // Range cache — populated on first calendar open, kept for the
  // page's lifetime. The server-side cache is 30 s; refetching on
  // every popover open would be wasteful when the user pages
  // through months. They can always reload the tab.
  var rangeCache = null;
  function fetchRange() {
    if (rangeCache) return Promise.resolve(rangeCache);
    return fetch('/api/range')
      .then((r) => (r.ok ? r.json() : { min_ts: null, max_ts: null }))
      .then((body) => {
        rangeCache = body;
        return body;
      })
      .catch(() => ({ min_ts: null, max_ts: null }));
  }

  function repaintAll() {
    document.querySelectorAll('.panel').forEach((p) => {
      if (p.dataset.status !== 'unsupported') refreshPanel(p);
    });
  }

  // fmtAnchor formats a historic anchor timestamp as a compact
  // date + time string for display in the topbar chip.
  function fmtAnchor(ms) {
    var d = new Date(ms);
    var date = d.toLocaleDateString(undefined, { day: '2-digit', month: 'short' });
    var time = d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
    return date + ' · ' + time;
  }

  // renderTimeNav refreshes topbar button visibility and the anchor
  // label to reflect the current timeState (live vs historic).
  function renderTimeNav() {
    var root = document.querySelector('[data-time-nav]');
    if (!root) return;
    var prev = root.querySelector('[data-anchor-prev]');
    var next = root.querySelector('[data-anchor-next]');
    var now = root.querySelector('[data-anchor-now]');
    var label = root.querySelector('[data-anchor-label]');
    var anchor = currentAnchor();
    var historic = anchor !== null;
    if (historic) root.setAttribute('data-historic', '');
    else root.removeAttribute('data-historic');
    if (prev) prev.hidden = !historic;
    if (next) next.hidden = !historic;
    if (now) now.hidden = !historic;
    if (label) {
      label.hidden = !historic;
      label.textContent = historic ? fmtAnchor(anchor) : '';
    }
  }

  // stepAnchor shifts the current anchor by one window-width in the
  // given direction (+1 = forward, -1 = backward).
  function stepAnchor(direction) {
    var current = currentAnchor();
    var base = current === null ? Date.now() : current;
    setAnchor(base + direction * currentWindowMs());
  }

  // bindTimeNav wires all topbar controls: the window select, the
  // anchor open/prev/next/now buttons, and keyboard shortcuts.
  function bindTimeNav() {
    var sel = document.querySelector('[data-range]');
    if (sel) {
      sel.value = currentWindowKey();
      sel.addEventListener('change', () => {
        setWindow(sel.value);
      });
    }
    var anchorBtn = document.querySelector('[data-anchor-open]');
    if (anchorBtn)
      anchorBtn.addEventListener('click', (ev) => {
        ev.preventDefault();
        openCalendar(anchorBtn);
      });
    var prev = document.querySelector('[data-anchor-prev]');
    if (prev)
      prev.addEventListener('click', () => {
        stepAnchor(-1);
      });
    var next = document.querySelector('[data-anchor-next]');
    if (next)
      next.addEventListener('click', () => {
        stepAnchor(+1);
      });
    var nowBtn = document.querySelector('[data-anchor-now]');
    if (nowBtn)
      nowBtn.addEventListener('click', () => {
        setAnchor(null);
      });

    document.addEventListener('keydown', (e) => {
      if (e.target && /^(input|select|textarea)$/i.test(e.target.tagName)) return;
      if (e.key === 'ArrowLeft') {
        stepAnchor(-1);
      } else if (e.key === 'ArrowRight') {
        stepAnchor(+1);
      } else if (e.key === 'n' || e.key === 'N') {
        setAnchor(null);
      }
    });

    renderTimeNav();
  }

  // openCalendar mounts a one-shot popover anchored to the given
  // element. Clicking a day projects today's HH:MM onto that day and
  // sets the anchor. Clicking today's date clears the anchor (live).
  function openCalendar(anchorEl) {
    closeCalendar();
    var pop = document.createElement('div');
    pop.className = 'time-nav__popover';
    pop.dataset.popover = 'calendar';

    var anchor = currentAnchor();
    var viewing = new Date(anchor === null ? Date.now() : anchor);
    viewing.setDate(1);

    fetchRange().then((range) => {
      renderCalendar(pop, viewing, range, anchorEl);
    });

    var rect = anchorEl.getBoundingClientRect();
    pop.style.top = window.scrollY + rect.bottom + 4 + 'px';
    pop.style.left = window.scrollX + rect.left + 'px';
    document.body.appendChild(pop);

    setTimeout(() => {
      document.addEventListener('click', outsideClick, true);
      document.addEventListener('keydown', escClose, true);
    }, 0);

    function outsideClick(e) {
      if (pop.contains(e.target)) return;
      if (anchorEl.contains(e.target)) return;
      closeCalendar();
    }
    function escClose(e) {
      if (e.key === 'Escape') closeCalendar();
    }
    pop._cleanup = () => {
      document.removeEventListener('click', outsideClick, true);
      document.removeEventListener('keydown', escClose, true);
    };
  }

  // closeCalendar removes the calendar popover from the DOM and
  // detaches its event listeners.
  function closeCalendar() {
    var existing = document.querySelector('[data-popover="calendar"]');
    if (!existing) return;
    if (existing._cleanup) existing._cleanup();
    existing.remove();
  }

  // renderCalendar (re)populates the calendar popover for the given
  // month. Called initially and on prev/next month navigation.
  function renderCalendar(pop, viewing, range, anchorEl) {
    clearChildren(pop);

    var head = document.createElement('div');
    head.className = 'time-nav__popover-head';
    var prevBtn = document.createElement('button');
    prevBtn.type = 'button';
    prevBtn.textContent = '‹';
    prevBtn.addEventListener('click', () => {
      var v = new Date(viewing);
      v.setMonth(v.getMonth() - 1);
      renderCalendar(pop, v, range, anchorEl);
    });
    var title = document.createElement('span');
    title.className = 'time-nav__popover-title';
    title.textContent = viewing.toLocaleDateString(undefined, { month: 'long', year: 'numeric' });
    var nextBtn = document.createElement('button');
    nextBtn.type = 'button';
    nextBtn.textContent = '›';
    nextBtn.addEventListener('click', () => {
      var v = new Date(viewing);
      v.setMonth(v.getMonth() + 1);
      renderCalendar(pop, v, range, anchorEl);
    });
    head.appendChild(prevBtn);
    head.appendChild(title);
    head.appendChild(nextBtn);
    pop.appendChild(head);

    var grid = document.createElement('div');
    grid.className = 'time-nav__grid';

    var dow = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];
    for (let i = 0; i < 7; i++) {
      const h = document.createElement('div');
      h.className = 'time-nav__dow';
      h.textContent = dow[i];
      grid.appendChild(h);
    }

    var firstDow = (viewing.getDay() + 6) % 7; // Mon=0
    for (let p = 0; p < firstDow; p++) {
      grid.appendChild(document.createElement('div'));
    }

    var month = viewing.getMonth();
    var year = viewing.getFullYear();
    var minDate =
      range.min_ts !== null && range.min_ts !== undefined ? new Date(range.min_ts) : null;
    var maxDate =
      range.max_ts !== null && range.max_ts !== undefined ? new Date(range.max_ts) : null;
    var today = new Date();
    var todayKey = isoDate(today);
    var anchorKey = currentAnchor() !== null ? isoDate(new Date(currentAnchor())) : null;

    for (let d = 1; d <= 31; d++) {
      const dt = new Date(year, month, d);
      if (dt.getMonth() !== month) break;
      const cell = document.createElement('button');
      cell.type = 'button';
      cell.className = 'time-nav__day';
      cell.textContent = String(d);
      const key = isoDate(dt);
      const disabled = !minDate || dt < startOfDay(minDate) || dt > endOfDay(maxDate || today);
      if (disabled) cell.setAttribute('disabled', '');
      if (key === todayKey) cell.setAttribute('data-today', '');
      if (anchorKey && key === anchorKey) cell.setAttribute('data-current', '');
      cell.addEventListener(
        'click',
        ((selected) => () => {
          if (sameDay(selected, new Date())) {
            setAnchor(null);
          } else {
            const clock = new Date();
            const projected = new Date(
              selected.getFullYear(),
              selected.getMonth(),
              selected.getDate(),
              clock.getHours(),
              clock.getMinutes(),
              clock.getSeconds(),
              0,
            );
            setAnchor(projected.getTime());
          }
          closeCalendar();
        })(dt),
      );
      grid.appendChild(cell);
    }
    pop.appendChild(grid);
  }

  // isoDate returns a "YYYY-MM-DD" string for a Date, used to
  // compare calendar days without caring about time-of-day.
  function isoDate(d) {
    return (
      d.getFullYear() +
      '-' +
      String(d.getMonth() + 1).padStart(2, '0') +
      '-' +
      String(d.getDate()).padStart(2, '0')
    );
  }

  // startOfDay returns midnight at the start of the given Date's day.
  function startOfDay(d) {
    return new Date(d.getFullYear(), d.getMonth(), d.getDate(), 0, 0, 0, 0);
  }

  // endOfDay returns 23:59:59.999 at the end of the given Date's day.
  function endOfDay(d) {
    return new Date(d.getFullYear(), d.getMonth(), d.getDate(), 23, 59, 59, 999);
  }

  // sameDay returns true when two Date values fall on the same calendar day.
  function sameDay(a, b) {
    return isoDate(a) === isoDate(b);
  }

  function el(name, attrs, text) {
    var node = document.createElementNS(SVG_NS, name);
    if (attrs) {
      for (var k in attrs) {
        if (Object.hasOwn(attrs, k)) {
          node.setAttribute(k, attrs[k]);
        }
      }
    }
    if (text !== undefined && text !== null)
      node.appendChild(document.createTextNode(String(text)));
    return node;
  }

  function clearChildren(n) {
    while (n.firstChild) n.removeChild(n.firstChild);
  }

  // panelStates[svg] = { seriesList, unit, geom..., cursorMs (or null) }
  var panelStates = new WeakMap();

  function renderChart(svg, seriesList, unit, legendTemplate, domain) {
    clearChildren(svg);
    if (!seriesList.length) {
      panelStates.delete(svg);
      return;
    }
    var w = svg.clientWidth || 600;
    var h = svg.clientHeight || 140;
    svg.setAttribute('viewBox', '0 0 ' + w + ' ' + h);

    var innerW = w - PAD.left - PAD.right;
    var innerH = h - PAD.top - PAD.bottom;

    // Extents. Y is always derived from the data; X uses the query
    // window when supplied so the axis labels read "-1h … now"
    // regardless of where the actual sample timestamps landed (gauge
    // panels without rate() return raw scrape ticks that drift a few
    // seconds from the requested window).
    var minX = Infinity,
      maxX = -Infinity,
      minY = Infinity,
      maxY = -Infinity;
    for (let i = 0; i < seriesList.length; i++) {
      const pts = seriesList[i].points;
      for (let j = 0; j < pts.length; j++) {
        const px = pts[j][0],
          py = pts[j][1];
        if (px < minX) minX = px;
        if (px > maxX) maxX = px;
        if (py < minY) minY = py;
        if (py > maxY) maxY = py;
      }
    }
    if (!Number.isFinite(minX)) {
      panelStates.delete(svg);
      return;
    }
    if (
      domain &&
      Number.isFinite(domain.from) &&
      Number.isFinite(domain.to) &&
      domain.to > domain.from
    ) {
      minX = domain.from;
      maxX = domain.to;
    }

    // 5 % vertical breathing room so lines don't kiss the gridlines.
    // Clamp the lower bound to 0 when the data is non-negative, so axes
    // like memory or counts never invent negative tick values.
    if (maxY === minY) {
      maxY = minY + 1;
    }
    var ypad = (maxY - minY) * 0.05;
    var yLo = minY - ypad,
      yHi = maxY + ypad;
    if (minY >= 0 && yLo < 0) yLo = 0;
    if (maxX === minX) {
      maxX = minX + 1;
    }

    function sx(x) {
      return PAD.left + ((x - minX) / (maxX - minX)) * innerW;
    }
    function sy(y) {
      return PAD.top + innerH - ((y - yLo) / (yHi - yLo)) * innerH;
    }

    // Y gridlines and labels.
    var yTicks = [yLo, (yLo + yHi) / 2, yHi];
    var yLabels = formatTicks(yTicks, unit);
    for (let t = 0; t < yTicks.length; t++) {
      const yv = yTicks[t];
      const py2 = sy(yv);
      svg.appendChild(
        el('line', {
          x1: PAD.left,
          y1: py2.toFixed(1),
          x2: w - PAD.right,
          y2: py2.toFixed(1),
          class: 'gridline',
        }),
      );
      svg.appendChild(
        el(
          'text',
          {
            x: PAD.left - 6,
            y: py2.toFixed(1),
            class: 'axis-label axis-label--y',
          },
          yLabels[t],
        ),
      );
    }

    // Baseline (bottom of the inner area).
    svg.appendChild(
      el('line', {
        x1: PAD.left,
        y1: (PAD.top + innerH).toFixed(1),
        x2: w - PAD.right,
        y2: (PAD.top + innerH).toFixed(1),
        class: 'baseline',
      }),
    );

    // X tick labels — 3 evenly spaced positions show relative time.
    var nowMs = maxX;
    var xTicks = [minX, (minX + maxX) / 2, maxX];
    for (let ti = 0; ti < xTicks.length; ti++) {
      const xv = xTicks[ti];
      const anchor = ti === 0 ? 'start' : ti === 2 ? 'end' : 'middle';
      svg.appendChild(
        el(
          'text',
          {
            x: sx(xv).toFixed(1),
            y: (h - 4).toFixed(1),
            class: 'axis-label axis-label--x',
            'text-anchor': anchor,
          },
          fmtRelative(nowMs - xv),
        ),
      );
    }

    // Series paths and markers.
    for (let s = 0; s < seriesList.length; s++) {
      const slot = (s % SERIES_PALETTE_SIZE) + 1;
      const pp = seriesList[s].points;
      let d = '';
      for (let p = 0; p < pp.length; p++) {
        d += (p === 0 ? 'M' : 'L') + sx(pp[p][0]).toFixed(1) + ',' + sy(pp[p][1]).toFixed(1);
      }
      svg.appendChild(el('path', { d: d, class: 'series series--' + slot }));
      const last = pp[pp.length - 1];
      svg.appendChild(
        el('circle', {
          cx: sx(last[0]).toFixed(1),
          cy: sy(last[1]).toFixed(1),
          r: 2,
          class: 'marker marker--' + slot,
        }),
      );
    }

    // Hover overlay — transparent rect on top to capture mouse events.
    var overlay = el('rect', {
      x: PAD.left,
      y: PAD.top,
      width: innerW,
      height: innerH,
      class: 'hover-overlay',
    });
    svg.appendChild(overlay);

    // Hover layer — populated by the mouse handler.
    var hoverLayer = el('g', { class: 'hover-layer' });
    svg.appendChild(hoverLayer);

    var prev = panelStates.get(svg);
    var cursorMs = prev ? prev.cursorMs : null;

    panelStates.set(svg, {
      seriesList: seriesList,
      unit: unit,
      legendTemplate: legendTemplate || '',
      w: w,
      h: h,
      minX: minX,
      maxX: maxX,
      sx: sx,
      sy: sy,
      innerLeft: PAD.left,
      innerRight: w - PAD.right,
      innerTop: PAD.top,
      innerBottom: PAD.top + innerH,
      cursorMs: cursorMs,
    });

    // Restore hover overlay if the cursor was over this chart.
    if (cursorMs !== null) renderHover(svg, cursorMs);
  }

  // Convert a MouseEvent on the SVG to a viewBox-coordinate.
  function eventToSvg(svg, evt) {
    var pt = svg.createSVGPoint();
    pt.x = evt.clientX;
    pt.y = evt.clientY;
    var ctm = svg.getScreenCTM();
    if (!ctm) return null;
    return pt.matrixTransform(ctm.inverse());
  }

  // Nearest data point in a series given cursor X (already in SVG coords).
  function nearestIndex(points, cursorMs) {
    if (!points.length) return -1;
    var lo = 0,
      hi = points.length - 1;
    while (lo < hi) {
      const mid = (lo + hi) >>> 1;
      if (points[mid][0] < cursorMs) lo = mid + 1;
      else hi = mid;
    }
    // lo is the smallest index with points[i][0] >= cursorMs.
    if (lo === 0) return 0;
    var before = points[lo - 1],
      after = points[lo];
    return Math.abs(after[0] - cursorMs) < Math.abs(cursorMs - before[0]) ? lo : lo - 1;
  }

  // Derive a short legend label from a series. Honours the panel's
  // Grafana-style legendFormat (e.g. "{{name}}"). When no template is
  // set, falls back to:
  //   - the single remaining label's value (drop "job" and "instance"
  //     which are usually the same across every series of a panel)
  //   - "k=v k=v" when more than one label remains
  //   - the metric name when the series carries no labels at all
  function labelFor(ser, legendTemplate) {
    var labels = ser.labels || {};
    if (legendTemplate) {
      return legendTemplate.replace(
        /\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}/g,
        (_, k) => labels[k] || '',
      );
    }
    var keys = Object.keys(labels).filter((k) => k !== 'job' && k !== 'instance');
    if (keys.length === 0) keys = Object.keys(labels);
    if (keys.length === 0) return ser.metric;
    if (keys.length === 1) return labels[keys[0]];
    return keys.map((k) => k + '=' + labels[k]).join(' ');
  }

  function renderHover(svg, cursorMs) {
    var state = panelStates.get(svg);
    if (!state) return;
    var hoverLayer = svg.querySelector('.hover-layer');
    if (!hoverLayer) return;
    clearChildren(hoverLayer);

    var clampedMs = Math.max(state.minX, Math.min(state.maxX, cursorMs));
    var crossX = state.sx(clampedMs);

    // Vertical crosshair.
    hoverLayer.appendChild(
      el('line', {
        x1: crossX.toFixed(1),
        y1: state.innerTop,
        x2: crossX.toFixed(1),
        y2: state.innerBottom,
        class: 'crosshair',
      }),
    );

    // For each series, find the nearest data point and stamp a circle.
    var rows = [];
    for (let i = 0; i < state.seriesList.length; i++) {
      const s = state.seriesList[i];
      if (!s.points?.length) continue;
      const idx = nearestIndex(s.points, clampedMs);
      if (idx < 0) continue;
      const pt = s.points[idx];
      const slot = (i % SERIES_PALETTE_SIZE) + 1;
      hoverLayer.appendChild(
        el('circle', {
          cx: state.sx(pt[0]).toFixed(1),
          cy: state.sy(pt[1]).toFixed(1),
          r: 3,
          class: 'hover-point hover-point--' + slot,
        }),
      );
      rows.push({ slot: slot, label: labelFor(s, state.legendTemplate), value: pt[1], ts: pt[0] });
    }
    if (!rows.length) return;

    // Sort by value descending so the tooltip rows match the visual
    // top-to-bottom order of the lines at the cursor position.
    rows.sort((a, b) => b.value - a.value);

    // Tooltip — header (timestamp) + one row per series.
    var headerTs = rows[0].ts;
    var rowH = 13;
    var headerH = 14;
    var swatchW = 8;
    var swatchGap = 5;
    var rowPadX = 8;
    var rowPadY = 5;
    var multi = state.seriesList.length > 1;

    // Measure rough width using a probe text element.
    var lines = [fmtTime(headerTs)];
    for (let r = 0; r < rows.length; r++) {
      const prefix = multi ? rows[r].label + '  ' : '';
      lines.push(prefix + fmt(rows[r].value, state.unit));
    }
    var charW = 6.2; // ~ width of mono char at 10px
    var widest = 0;
    for (let li = 0; li < lines.length; li++) {
      if (lines[li].length > widest) widest = lines[li].length;
    }
    var bgW = Math.ceil(widest * charW) + (multi ? swatchW + swatchGap : 0) + rowPadX * 2;
    var bgH = headerH + rows.length * rowH + rowPadY * 2;

    // Place to the right of the cursor unless we'd overflow.
    var bgX = crossX + 8;
    if (bgX + bgW > state.innerRight) bgX = crossX - 8 - bgW;
    var bgY = state.innerTop + 4;
    if (bgY + bgH > state.innerBottom) bgY = state.innerBottom - bgH;

    hoverLayer.appendChild(
      el('rect', {
        x: bgX.toFixed(1),
        y: bgY.toFixed(1),
        width: bgW,
        height: bgH,
        rx: 3,
        ry: 3,
        class: 'tooltip-bg',
      }),
    );
    // Header (time).
    hoverLayer.appendChild(
      el(
        'text',
        {
          x: (bgX + rowPadX).toFixed(1),
          y: (bgY + rowPadY + 10).toFixed(1),
          class: 'tooltip-text tooltip-text--muted',
        },
        fmtTime(headerTs),
      ),
    );

    // Rows.
    for (let rr = 0; rr < rows.length; rr++) {
      const ry = bgY + rowPadY + headerH + rr * rowH + 9;
      let rx = bgX + rowPadX;
      if (multi) {
        hoverLayer.appendChild(
          el('rect', {
            x: rx.toFixed(1),
            y: (ry - 4).toFixed(1),
            width: swatchW,
            height: 2,
            class: 'tooltip-swatch tooltip-swatch--' + rows[rr].slot,
          }),
        );
        rx += swatchW + swatchGap;
        hoverLayer.appendChild(
          el(
            'text',
            {
              x: rx.toFixed(1),
              y: ry.toFixed(1),
              class: 'tooltip-text tooltip-text--muted',
            },
            rows[rr].label,
          ),
        );
      }
      // Value (always right-aligned to the box).
      hoverLayer.appendChild(
        el(
          'text',
          {
            x: (bgX + bgW - rowPadX).toFixed(1),
            y: ry.toFixed(1),
            class: 'tooltip-text',
            'text-anchor': 'end',
          },
          fmt(rows[rr].value, state.unit),
        ),
      );
    }
  }

  function bindChartInteractions(svg) {
    svg.addEventListener('mousemove', (evt) => {
      var pt = eventToSvg(svg, evt);
      if (!pt) return;
      var state = panelStates.get(svg);
      if (!state) return;
      var ratio = (pt.x - state.innerLeft) / (state.innerRight - state.innerLeft);
      var ts = state.minX + ratio * (state.maxX - state.minX);
      state.cursorMs = ts;
      renderHover(svg, ts);
    });
    svg.addEventListener('mouseleave', () => {
      var state = panelStates.get(svg);
      if (state) state.cursorMs = null;
      var layer = svg.querySelector('.hover-layer');
      if (layer) clearChildren(layer);
    });
  }

  // ────────────────────────────────────────────────────────────────────────
  // Polling

  function refreshPanel(panel) {
    if (panel.dataset.status === 'unsupported') return;
    var staticSrc = panel.dataset.static;
    var expr = panel.dataset.expr;
    if (!staticSrc && !expr) return;

    var unit = panel.dataset.unit || '';
    var to = effectiveTo();
    var from = to - currentWindowMs();
    var step = effectiveStepMs();

    // The docs site ships pre-baked fixture JSON next to each page.
    // Panels in those pages set data-static to the fixture URL and we
    // skip the /api/query call entirely — the response shape is the
    // same {series: [...]} envelope so the renderer is unchanged.
    var url = staticSrc
      ? staticSrc
      : '/api/query?expr=' +
        encodeURIComponent(expr) +
        '&from=' +
        from +
        '&to=' +
        to +
        '&step=' +
        step;

    fetch(url)
      .then((resp) => (resp.ok ? resp.json() : null))
      .then((body) => {
        if (!body) return;
        var seriesList = (body.series || []).filter((s) => s?.points && s.points.length > 0);
        var legendTemplate = panel.dataset.legend || '';

        var svg = panel.querySelector('.panel__chart');
        // For static fixtures the on-disk timestamps live in the past;
        // letting renderChart auto-fit the X domain from the data
        // keeps the points inside the visible area. Live panels pass
        // the picker's window so the axis reads "-Nh … now".
        var dom = staticSrc ? null : { from: from, to: to };
        if (svg) renderChart(svg, seriesList, unit, legendTemplate, dom);

        var legend = panel.querySelector('.panel__legend');
        if (legend) renderLegend(legend, seriesList, legendTemplate);

        var valueEl = panel.querySelector('.panel__value');
        if (valueEl) {
          // The headline value only makes sense when the panel has a
          // single series. In multi-series panels we'd be picking one
          // arbitrarily — the legend already carries that info, so we
          // hide the readout entirely and let the chart speak.
          const multi = seriesList.length > 1;
          valueEl.classList.toggle('panel__value--hidden', multi);
          if (!multi) {
            const first = seriesList[0];
            const lastPt = first ? first.points[first.points.length - 1] : null;
            const v = lastPt ? lastPt[1] : null;
            valueEl.textContent = fmt(v, unit);
            valueEl.classList.toggle('panel__value--placeholder', v === null);
          }
        }
      })
      .catch(() => {
        /* network error — keep last render */
      });
  }

  function renderLegend(legendEl, seriesList, legendTemplate) {
    clearChildren(legendEl);
    if (seriesList.length <= 1) return;
    for (let i = 0; i < seriesList.length; i++) {
      const slot = (i % SERIES_PALETTE_SIZE) + 1;
      const item = document.createElement('span');
      item.className = 'panel__legend-item';
      const swatch = document.createElement('span');
      swatch.className = 'panel__legend-swatch';
      swatch.style.background = 'var(--series-' + slot + ')';
      item.appendChild(swatch);
      const label = document.createElement('span');
      label.textContent = labelFor(seriesList[i], legendTemplate);
      item.appendChild(label);
      legendEl.appendChild(item);
    }
  }

  function schedulePanels() {
    document.querySelectorAll('.panel').forEach((panel) => {
      if (panel.dataset.status === 'unsupported') return;
      var svg = panel.querySelector('.panel__chart');
      if (svg) bindChartInteractions(svg);

      // data-refresh="0" means "load once, never re-poll" — used by
      // the docs site so fixture-backed panels render exactly once.
      var raw = panel.dataset.refresh;
      var parsed = parseInt(raw, 10);
      function tick() {
        if (document.visibilityState === 'hidden') return;
        if (!isLive()) return; // historic mode: data is frozen, skip the fetch
        refreshPanel(panel);
      }
      refreshPanel(panel);
      if (raw !== undefined && parsed === 0) return;
      var interval = Math.max(parsed || DEFAULT_REFRESH_MS, MIN_REFRESH_MS);
      setInterval(tick, interval);
    });
  }

  // ────────────────────────────────────────────────────────────────────────
  // Boot

  function init() {
    bindThemeToggle();
    bindTimeNav();
    schedulePanels();
  }
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
