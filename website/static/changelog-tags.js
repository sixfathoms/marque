// Progressive enhancement for the changelog tag filter.
//
// The chips are rendered as real links (?tag=cli) so the control works with
// JavaScript disabled — the page just reloads. This upgrades them to in-place
// filtering and keeps the URL in step, so a filtered view is still shareable.
(function () {
  const root = document.querySelector('[data-changelog-filter]');
  if (!root) return;
  const chips = [...root.querySelectorAll('.changelog-filter-tag')];
  const entries = [...document.querySelectorAll('.changelog-entry')];
  const status = root.querySelector('.changelog-filter-status');
  if (!chips.length || !entries.length) return;

  function apply(tag, push) {
    let shown = 0;
    for (const e of entries) {
      const match = !tag || (e.dataset.tags || '').split(' ').includes(tag);
      e.hidden = !match;
      if (match) shown++;
    }
    for (const c of chips) {
      const active = (c.dataset.tag || '') === (tag || '');
      c.classList.toggle('active', active);
      c.setAttribute('aria-pressed', String(active));
    }
    if (status) {
      status.hidden = !tag;
      status.textContent = tag ? `${shown} entr${shown === 1 ? 'y' : 'ies'} tagged ${tag}.` : '';
    }
    if (push) {
      const url = tag ? `?tag=${encodeURIComponent(tag)}` : location.pathname;
      history.pushState({ tag }, '', url);
    }
  }

  for (const c of chips) {
    c.setAttribute('role', 'button');
    c.addEventListener('click', (ev) => {
      ev.preventDefault();
      apply(c.dataset.tag || '', true);
    });
  }
  window.addEventListener('popstate', () => {
    apply(new URLSearchParams(location.search).get('tag') || '', false);
  });
  apply(new URLSearchParams(location.search).get('tag') || '', false);
})();
