(function () {
  const form = document.querySelector("[data-search]");
  if (!form) return;
  const input = form.querySelector("[data-search-input]");
  const results = form.querySelector("[data-search-results]");
  let index = null;
  let loadStarted = false;

  function loadIndex() {
    if (loadStarted) return;
    loadStarted = true;
    fetch("/search-index.json")
      .then((r) => r.json())
      .then((data) => { index = data; render(input.value); })
      .catch(() => {});
  }

  function tokenise(q) {
    return q.toLowerCase().split(/[^\p{L}\p{N}]+/u).filter(Boolean);
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

  function highlight(snippet, tokens) {
    if (!snippet) return "";
    let escaped = snippet
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
    for (const t of tokens) {
      const re = new RegExp("(" + t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + ")", "ig");
      escaped = escaped.replace(re, "<mark>$1</mark>");
    }
    return escaped;
  }

  function render(query) {
    if (!index || !query.trim()) {
      results.hidden = true;
      results.innerHTML = "";
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
      .map(({ r }) =>
        `<a class="search__result" href="${r.url}">
           <div class="search__crumb">${r.breadcrumb || ""}${r.breadcrumb ? " · " : ""}<strong>${r.title}</strong></div>
           <div class="search__snippet">${highlight(r.snippet, tokens)}</div>
         </a>`
      )
      .join("");
  }

  input.addEventListener("focus", loadIndex);
  input.addEventListener("input", () => render(input.value));
  input.addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      input.value = "";
      input.blur();
      render("");
    }
  });
  document.addEventListener("keydown", (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
      e.preventDefault();
      input.focus();
    }
  });
  document.addEventListener("click", (e) => {
    if (!form.contains(e.target)) {
      results.hidden = true;
    }
  });
})();
