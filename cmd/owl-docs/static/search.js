(() => {
  const form = document.querySelector('[data-search]');
  if (!form) return;
  const input = form.querySelector('[data-search-input]');
  const results = form.querySelector('[data-search-results]');
  // The base URL prefix is published in the page head by layout.html.
  // It is "" when the site is rooted at "/", or e.g. "/owl" when
  // hosted under a GitHub Pages subdirectory.
  const baseMeta = document.querySelector('meta[name="owl-docs-base"]');
  const base = baseMeta?.getAttribute('content') || '';
  let index = null;
  let loadStarted = false;

  function loadIndex() {
    if (loadStarted) return;
    loadStarted = true;
    fetch(base + '/search-index.json')
      .then((r) => r.json())
      .then((data) => {
        index = data;
        render(input.value);
      })
      .catch(() => {});
  }

  function tokenise(q) {
    return q
      .toLowerCase()
      .split(/[^\p{L}\p{N}]+/u)
      .filter(Boolean);
  }

  function rank(record, tokens) {
    for (const t of tokens) {
      if (!record.terms.includes(t)) return -1;
    }
    let score = 0;
    const titleL = record.title.toLowerCase();
    for (const t of tokens) {
      if (titleL.includes(t)) score += 5;
      if (record.terms.includes(t)) score += 1;
    }
    return score;
  }

  function escRegex(s) {
    return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  }

  function excerpt(text, hit) {
    const window = 180;
    const start = Math.max(0, hit - Math.floor(window / 3));
    const end = Math.min(text.length, start + window);
    let out = text.slice(start, end);
    if (start > 0) out = '… ' + out;
    if (end < text.length) out = out + ' …';
    return out;
  }

  // Prefer the verbatim query: if the snippet doesn't contain it but
  // the body does, slice an excerpt around the body hit. Falls back to
  // the first-paragraph snippet when the verbatim query is absent, so
  // pure-token matches keep showing their familiar opening sentence.
  function pickSnippet(record, query, tokens) {
    const snippet = record.snippet || '';
    const body = record.body || '';
    const q = query.trim().toLowerCase();
    if (q) {
      if (snippet.toLowerCase().includes(q)) return snippet;
      const i = body.toLowerCase().indexOf(q);
      if (i !== -1) return excerpt(body, i);
    }
    const snippetL = snippet.toLowerCase();
    if (tokens.every((t) => snippetL.includes(t))) return snippet;
    if (!body) return snippet;
    const bodyL = body.toLowerCase();
    let hit = -1;
    for (const t of tokens) {
      const i = bodyL.indexOf(t);
      if (i !== -1 && (hit === -1 || i < hit)) hit = i;
    }
    if (hit === -1) return snippet;
    return excerpt(body, hit);
  }

  function highlight(snippet, query, tokens) {
    if (!snippet) return '';
    let escaped = snippet.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
    const marks = [];
    const q = query.trim();
    if (q) marks.push(q);
    for (const t of tokens) marks.push(t);
    marks.sort((a, b) => b.length - a.length);
    const escapedL = escaped.toLowerCase();
    const kept = [];
    for (const m of marks) {
      const mL = m.toLowerCase();
      if (!escapedL.includes(mL)) continue;
      if (kept.some((k) => k.toLowerCase().includes(mL))) continue;
      kept.push(m);
    }
    for (const m of kept) {
      const re = new RegExp('(' + escRegex(m) + ')', 'ig');
      escaped = escaped.replace(re, '<mark>$1</mark>');
    }
    return escaped;
  }

  function render(query) {
    if (!index || !query.trim()) {
      results.hidden = true;
      results.innerHTML = '';
      return;
    }
    const tokens = tokenise(query);
    const matches = [];
    for (const r of index) {
      const s = rank(r, tokens);
      if (s >= 0) matches.push({ r, s });
    }
    matches.sort((a, b) => b.s - a.s);
    const top = matches.slice(0, 10);
    if (top.length === 0) {
      results.hidden = false;
      results.innerHTML = `<div class="search__empty">No matches.</div>`;
      return;
    }
    results.hidden = false;
    results.innerHTML = top
      .map(({ r }) => {
        const snippet = pickSnippet(r, query, tokens);
        return `<a class="search__result" href="${r.url}">
           <div class="search__crumb">${r.breadcrumb || ''}${r.breadcrumb ? ' · ' : ''}<strong>${r.title}</strong></div>
           <div class="search__snippet">${highlight(snippet, query, tokens)}</div>
         </a>`;
      })
      .join('');
  }

  input.addEventListener('focus', loadIndex);
  input.addEventListener('input', () => render(input.value));
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      input.value = '';
      input.blur();
      render('');
    }
  });
  document.addEventListener('keydown', (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      input.focus();
    }
  });
  document.addEventListener('click', (e) => {
    if (!form.contains(e.target)) {
      results.hidden = true;
    }
  });
})();
