#!/usr/bin/env node
// Static documentation site generator for Marque.
//
// Renders three kinds of markdown into one small, fast, dependency-light site:
//
//   docs/content/**      prose pages, grouped into the sidebar by directory
//   docs/edrs/*.md       engineering decision records, numbered and validated
//   docs/changelog/*.md  one file per entry, spliced into the changelog page
//
// Everything is authored as plain markdown that also renders correctly on
// GitHub, so the repository is a usable copy of the docs even with no build.
import { readdir, readFile, writeFile, mkdir, rm, cp } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { join, dirname, posix, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import matter from 'gray-matter';
import { unified } from 'unified';
import remarkParse from 'remark-parse';
import remarkGfm from 'remark-gfm';
import remarkRehype from 'remark-rehype';
import rehypeSlug from 'rehype-slug';
import rehypeAutolinkHeadings from 'rehype-autolink-headings';
import rehypeHighlight from 'rehype-highlight';
import rehypeStringify from 'rehype-stringify';
import { visit } from 'unist-util-visit';

const SITE_DIR = dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = dirname(SITE_DIR);
const DOCS_DIR = join(REPO_ROOT, 'docs');
const CONTENT_DIR = join(DOCS_DIR, 'content');
const EDRS_DIR = join(DOCS_DIR, 'edrs');
const CHANGELOG_DIR = join(DOCS_DIR, 'changelog');
const TEMPLATES_DIR = join(SITE_DIR, 'templates');
const STATIC_DIR = join(SITE_DIR, 'static');
const DIST_DIR = join(SITE_DIR, 'dist');

// Published at https://sixfathoms.github.io/marque/ — every emitted link and
// asset carries this prefix. `serve.mjs` strips it for local preview, and
// BASE_PATH='' builds a site rooted at /.
const BASE = process.env.BASE_PATH ?? '/marque';
const REPO_URL = 'https://github.com/sixfathoms/marque';
const GITHUB_SOURCE_BASE = `${REPO_URL}/blob/main`;
const SITE_NAME = 'Marque';

const VALID_STATUSES = new Set(['proposed', 'accepted', 'deprecated', 'superseded']);
// Statuses whose decision no longer stands, so their record is not work outstanding.
const RETIRED_STATUSES = new Set(['deprecated', 'superseded']);

// `status` records what was DECIDED. `implementation` records what EXISTS, and
// the two axes are orthogonal on purpose: a record is routinely `accepted` and
// `none` at once — the decision is settled, and not a line of it is written.
// Folding this into the status vocabulary would push every settled-but-unbuilt
// decision back to `proposed`, which in this repository arms the
// `proposed_until` build-failure timer.
//
// The vocabulary is CLOSED, and this is the only place it is written down: the
// build validates against it and the roadmap page derives its groups, their
// order and their blurbs from it. Each entry is [state, blurb, note-required],
// where note-required is the phrasing of what the note must say, or null when
// no note is compelled. Ordered most-built to least; the roadmap reverses it so
// a reader meets the outstanding work first. Adding a state means editing this
// array and docs/edrs/README.md together — and assertVocabularies() below fails
// the build if you edit only one.
const IMPLEMENTATION_STATES = [
  ['shipped', 'Built and running — the whole decision, not the easy half.', null],
  ['partial', 'Some of it runs, some does not.', 'saying which half is missing'],
  ['in-flight', 'Implemented somewhere that is not the main branch.', 'naming the branch'],
  ['none', 'Nothing implements it.', null],
];
const IMPLEMENTATION_STATE_NAMES = IMPLEMENTATION_STATES.map(([s]) => s);
const IMPLEMENTATION_NOTE_REQUIRED = new Map(
  IMPLEMENTATION_STATES.filter(([, , required]) => required).map(([s, , required]) => [s, required]),
);

const EDR_FILE_RE = /^(\d{4})-([a-z0-9][a-z0-9-]*)\.md$/;
const ALIAS_RE = /^[a-z0-9][a-z0-9-]*$/;
const EDR_SLUG_RE = /^\d{4}-[a-z0-9-]+$/;
const SUMMARY_MAX = 240;

// The changelog tag vocabulary is CLOSED: the build validates entries against
// it and renders the filter bar from it. An open set degrades into a synonym
// pile nobody can filter on. It is written down twice — here and in
// docs/changelog/README.md, which people read — and assertVocabularies() below
// is what stops the two drifting apart.
const CHANGELOG_TAGS = [
  ['product', 'A user-visible capability'],
  ['policy', 'Approval policy, delegation and scope'],
  ['cli', 'The marque command-line client'],
  ['console', 'The web console'],
  ['security', 'Authentication, signing, isolation and hardening'],
  ['ops', 'Deployment, relays, runbooks and observability'],
  ['docs', 'Documentation and decision records'],
];
const CHANGELOG_TAG_NAMES = CHANGELOG_TAGS.map(([t]) => t);
const CHANGELOG_FILE_RE = /^(\d{4}-\d{2}-\d{2})-([a-z0-9][a-z0-9-]*)\.md$/;
const ENTRIES_MARKER = '<!-- @entries -->';

// Doc sidebar: categories in display order. Pages within a category sort by
// frontmatter `sidebar_position`, then title.
const CATEGORIES = [
  { dir: 'overview', label: 'Overview' },
  { dir: 'concepts', label: 'Concepts' },
  { dir: 'guides', label: 'Guides' },
  { dir: 'operations', label: 'Operations' },
  { dir: 'reference', label: 'Reference' },
];

const NAV = [
  { key: 'home', label: 'Overview', href: `${BASE}/` },
  { key: 'docs', label: 'Docs', href: `${BASE}/overview/introduction/` },
  { key: 'edrs', label: 'Decisions', href: `${BASE}/edrs/` },
  { key: 'roadmap', label: 'Roadmap', href: `${BASE}/roadmap/` },
  { key: 'changelog', label: 'Changelog', href: `${BASE}/changelog/` },
  { key: 'github', label: 'GitHub', href: REPO_URL },
];
function renderNav(activeKey) {
  return NAV.map(
    (n) => `<a href="${n.href}"${n.key === activeKey ? ' class="active"' : ''}>${escapeHtml(n.label)}</a>`,
  ).join('\n        ');
}

// Turn ```mermaid fences into <pre class="mermaid"> so mermaid.js renders them
// client-side. rehype-highlight would otherwise syntax-colour the source.
function rehypeMermaid() {
  return (tree) => {
    visit(tree, 'element', (node) => {
      if (node.tagName !== 'pre') return;
      const code = node.children?.find((c) => c.tagName === 'code');
      if (!code) return;
      const classes = code.properties?.className ?? [];
      if (!classes.includes('language-mermaid')) return;
      const text = code.children?.map((c) => c.value ?? '').join('') ?? '';
      node.properties = { className: ['mermaid'] };
      node.children = [{ type: 'text', value: text }];
    });
  };
}

// Loaded only on pages that actually contain a diagram (see scriptsFor).
const MERMAID_SCRIPT = `<script type="module">
  import mermaid from 'https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs';
  const dark = window.matchMedia('(prefers-color-scheme: dark)').matches;
  mermaid.initialize({ startOnLoad: true, theme: dark ? 'dark' : 'neutral', fontFamily: 'inherit', securityLevel: 'strict' });
</script>`;
const CHANGELOG_TAGS_SCRIPT = `<script src="${BASE}/changelog-tags.js" defer></script>`;

function scriptsFor(html) {
  const parts = [];
  if (html.includes('class="mermaid"')) parts.push(MERMAID_SCRIPT);
  if (html.includes('data-changelog-filter')) parts.push(CHANGELOG_TAGS_SCRIPT);
  return parts.join('\n');
}

const processor = unified()
  .use(remarkParse)
  .use(remarkGfm)
  .use(remarkRehype, { allowDangerousHtml: true })
  .use(rehypeSlug)
  .use(rehypeAutolinkHeadings, { behavior: 'wrap', properties: { className: ['heading-link'] } })
  .use(rehypeMermaid)
  .use(rehypeHighlight, { detect: true, ignoreMissing: true })
  .use(rehypeStringify, { allowDangerousHtml: true });

function escapeHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}
function formatDate(d) {
  if (!d) return '';
  if (d instanceof Date) return d.toISOString().slice(0, 10);
  return String(d);
}

// ---- link rewriting -----------------------------------------------------
// Pages are authored with relative `.md` links so they resolve on GitHub;
// rewrite them to the site's base-prefixed pretty URLs. `webDir` is the doc's
// directory within content/ (posix, '' at the root).
function rewriteRelativeMd(html, webDir) {
  return html.replace(
    /href="(?!https?:|\/|#)([^"]+?)\.md([#?][^"]*)?"/g,
    (_m, path, suffix = '') => {
      const resolved = posix.normalize(posix.join(webDir, path)).replace(/^\.\//, '');
      if (resolved.startsWith('../edrs/')) {
        return `href="${BASE}/edrs/${resolved.slice('../edrs/'.length)}/${suffix}"`;
      }
      return `href="${BASE}/${resolved}/${suffix}"`;
    },
  );
}
function rewriteSiteAbsolute(html) {
  return html.replace(/href="(\/(?!\/)[^"#?]*)([#?][^"]*)?"/g, (m, path, suffix = '') => {
    if (BASE && (path === BASE || path.startsWith(BASE + '/'))) return m;
    if (/\.[a-z0-9]{1,5}$/i.test(path)) return `href="${BASE}${path}${suffix}"`;
    const clean = path.endsWith('/') ? path : path + '/';
    return `href="${BASE}${clean}${suffix}"`;
  });
}
function rewriteLinks(html, webDir) {
  return rewriteSiteAbsolute(rewriteRelativeMd(html, webDir));
}
// EDR prose: `./NNNN-slug.md` is a sibling record; anything else .md points at
// the GitHub source so the link is never dead.
function rewriteEdrLinks(html) {
  html = html.replace(
    /href="(\.\.?)\/([^"#?]+?)\.md([#?][^"]*)?"/g,
    (_m, prefix, path, suffix = '') => {
      if (prefix === '.' && EDR_SLUG_RE.test(path)) return `href="${BASE}/edrs/${path}/${suffix}"`;
      if (prefix === '..' && path.startsWith('content/')) {
        return `href="${BASE}/${path.slice('content/'.length)}/${suffix}"`;
      }
      const resolved = prefix === '..' ? `docs/${path}` : `docs/edrs/${path}`;
      return `href="${GITHUB_SOURCE_BASE}/${resolved}.md${suffix}"`;
    },
  );
  return rewriteSiteAbsolute(html);
}

function extractToc(html) {
  const items = [];
  const re = /<h([23])[^>]*\bid="([^"]+)"[^>]*>([\s\S]*?)<\/h\1>/g;
  let m;
  while ((m = re.exec(html)) !== null) {
    items.push({ level: Number(m[1]), id: m[2], text: m[3].replace(/<[^>]+>/g, '').trim() });
  }
  return items;
}
function renderToc(items) {
  if (!items.length) return '';
  return items
    .map((i) => `<li class="level-${i.level}"><a href="#${escapeHtml(i.id)}">${escapeHtml(i.text)}</a></li>`)
    .join('');
}

// A record shows two badges side by side, and they answer different questions:
// what was decided, and what exists. They are drawn differently on purpose —
// status filled, implementation outlined — so the pair is never read as one
// badge saying one thing. Both go through this helper so the axis is announced
// to a screen reader from one place rather than from two copies that drift;
// "accepted / none" read aloud with no axis names is worse than either alone.
function badge(axis, value, classes) {
  return (
    `<span class="badge ${classes}">` +
    `<span class="badge-axis">${escapeHtml(axis)}: </span>${escapeHtml(value)}</span>`
  );
}
function statusBadge(s) {
  return badge('Status', s, `badge-${escapeHtml(s)}`);
}
function implementationBadge(i) {
  return badge('Implementation', i, `badge-impl impl-${escapeHtml(i)}`);
}
function tagBadges(fm) {
  const t = Array.isArray(fm.tags) ? fm.tags : [];
  return t.map((x) => `<span class="tag">${escapeHtml(x)}</span>`).join(' ');
}
function authorNames(fm) {
  const l = Array.isArray(fm.authors) ? fm.authors : [];
  // Strip the <email> part; the site is public, the source file keeps it.
  return l.map((a) => escapeHtml(String(a).replace(/\s*<[^>]*>\s*$/, ''))).join(', ');
}

async function renderTemplate(name, vars) {
  const tpl = await readFile(join(TEMPLATES_DIR, name), 'utf8');
  return tpl.replace(/\{\{\s*([\w.-]+)\s*\}\}/g, (_, k) => (k in vars ? vars[k] : ''));
}
function applyLayout(layout, { title, nav, body, scripts = '' }) {
  return layout
    .replaceAll('{{title}}', title)
    .replaceAll('{{nav}}', nav)
    .replaceAll('{{body}}', body)
    .replaceAll('{{scripts}}', scripts)
    .replaceAll('{{base}}', BASE)
    .replaceAll('{{repo_url}}', REPO_URL);
}

async function writePageAt(relPath, content) {
  const full = join(DIST_DIR, relPath, 'index.html');
  await mkdir(dirname(full), { recursive: true });
  await writeFile(full, content);
}
function redirectHtml(toPath) {
  const safe = escapeHtml(toPath);
  return `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta http-equiv="refresh" content="0; url=${safe}"><link rel="canonical" href="${safe}">
<meta name="robots" content="noindex, nofollow, noarchive, nosnippet"><title>Redirecting…</title></head>
<body><p>Redirecting to <a href="${safe}">${safe}</a>…</p></body></html>\n`;
}

// ---- docs ---------------------------------------------------------------
async function walkMd(dir, baseRel = '') {
  const out = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const rel = baseRel ? posix.join(baseRel, entry.name) : entry.name;
    if (entry.isDirectory()) out.push(...(await walkMd(join(dir, entry.name), rel)));
    else if (entry.name.endsWith('.md') && entry.name !== 'README.md') out.push(rel);
  }
  return out;
}

async function loadDocs() {
  if (!existsSync(CONTENT_DIR)) return [];
  const docs = [];
  for (const rel of await walkMd(CONTENT_DIR)) {
    const { data, content } = matter(await readFile(join(CONTENT_DIR, rel), 'utf8'));
    if (!data.title) throw new Error(`content/${rel}: frontmatter 'title' is required`);
    const slug = rel.replace(/\.md$/, '');
    const dir = posix.dirname(slug) === '.' ? '' : posix.dirname(slug);
    if (dir && !CATEGORIES.some((c) => c.dir === dir)) {
      throw new Error(
        `content/${rel}: '${dir}' is not a sidebar category. Add it to CATEGORIES in website/build.mjs ` +
          `or move the page under one of: ${CATEGORIES.map((c) => c.dir).join(', ')}.`,
      );
    }
    docs.push({
      rel,
      slug,
      dir,
      title: data.title,
      order: data.sidebar_position ?? 100,
      markdown: content,
    });
  }
  return docs;
}

function renderSidebar(docs, activeSlug) {
  const byDir = new Map();
  for (const d of docs) byDir.set(d.dir, [...(byDir.get(d.dir) || []), d]);
  const blocks = [];
  for (const cat of CATEGORIES) {
    const items = (byDir.get(cat.dir) || []).sort(
      (a, b) => a.order - b.order || a.title.localeCompare(b.title),
    );
    if (!items.length) continue;
    const links = items
      .map(
        (d) =>
          `<li><a href="${BASE}/${d.slug}/"${d.slug === activeSlug ? ' class="active"' : ''}>${escapeHtml(d.title)}</a></li>`,
      )
      .join('');
    blocks.push(`<div class="sb-group"><p class="sb-label">${escapeHtml(cat.label)}</p><ul>${links}</ul></div>`);
  }
  return blocks.join('\n');
}

async function buildDocPage(doc, docs, layout) {
  const html = rewriteLinks(String(await processor.process(doc.markdown)), doc.dir);
  const body = await renderTemplate('doc.html', {
    title: escapeHtml(doc.title),
    content: html,
    toc: renderToc(extractToc(html)),
    sidebar: renderSidebar(docs, doc.slug),
    source_path: `docs/content/${doc.rel}`,
    base: BASE,
    repo_url: REPO_URL,
  });
  await writePageAt(
    doc.slug,
    applyLayout(layout, {
      title: `${escapeHtml(doc.title)} · ${SITE_NAME}`,
      nav: renderNav(doc.slug === 'changelog' ? 'changelog' : 'docs'),
      body,
      scripts: scriptsFor(html),
    }),
  );
}

// ---- EDRs ---------------------------------------------------------------
function validateEdr(filename, fm) {
  const errs = [];
  if (typeof fm.id !== 'number' || !Number.isInteger(fm.id) || fm.id < 1) {
    errs.push(`id must be a positive integer (got ${JSON.stringify(fm.id)})`);
  }
  if (!fm.title || typeof fm.title !== 'string') errs.push('title is required');
  // The summary is the abstract on the index and is read far more often than
  // the record. Bounded, because unbounded summaries grow into the wall of
  // text that makes an index unreadable.
  if (!fm.summary || typeof fm.summary !== 'string') {
    errs.push('summary is required (one or two sentences saying what the decision is)');
  } else if (fm.summary.length > SUMMARY_MAX) {
    errs.push(`summary is ${fm.summary.length} characters; the limit is ${SUMMARY_MAX}`);
  }
  if (!fm.status || !VALID_STATUSES.has(fm.status)) {
    errs.push(`status must be one of ${[...VALID_STATUSES].join('|')}`);
  }
  // Required, and rejected here exactly as a missing `summary` is — for the
  // same kind of reason. The roadmap page is derived from this field and from
  // nothing else, so an OPTIONAL field is one that future records omit, and the
  // page then quietly under-reports the work outstanding. A roadmap that
  // under-reports is worse than no roadmap at all, because it is read as
  // complete. Required is what makes the derivation trustworthy.
  if (!fm.implementation || !IMPLEMENTATION_STATE_NAMES.includes(fm.implementation)) {
    errs.push(
      `implementation must be one of ${IMPLEMENTATION_STATE_NAMES.join('|')} — what EXISTS, as ` +
        `opposed to what status records was decided (got ${JSON.stringify(fm.implementation)})`,
    );
  } else if (
    IMPLEMENTATION_NOTE_REQUIRED.has(fm.implementation) &&
    // Trimmed, not merely present. `partial` and `in-flight` carry their
    // whole meaning in the note, and a blank one renders as an empty
    // paragraph — invisible, where a blank summary would at least be a gap.
    !String(fm.implementation_note ?? '').trim()
  ) {
    errs.push(
      `implementation: ${fm.implementation} requires implementation_note ` +
        `${IMPLEMENTATION_NOTE_REQUIRED.get(fm.implementation)}`,
    );
  }
  if (fm.implementation_note != null && typeof fm.implementation_note !== 'string') {
    errs.push('implementation_note must be a string');
  }
  if (!fm.date) errs.push('date is required (YYYY-MM-DD)');
  if (fm.status === 'superseded' && !fm.superseded_by) errs.push('status=superseded requires superseded_by');
  if (fm.status === 'proposed' && !fm.proposed_until) errs.push('status=proposed requires proposed_until');
  // A `proposed` record that nobody came back to is the failure mode this
  // guards: the date passing fails the build rather than ageing silently.
  if (fm.status === 'proposed' && fm.proposed_until) {
    const until = formatDate(fm.proposed_until);
    if (until < new Date().toISOString().slice(0, 10)) {
      errs.push(`proposed_until (${until}) has passed — accept, supersede, or extend this record`);
    }
  }
  if (fm.aliases != null && (!Array.isArray(fm.aliases) || fm.aliases.some((a) => !ALIAS_RE.test(a)))) {
    errs.push('aliases must be slug strings');
  }
  const m = filename.match(EDR_FILE_RE);
  if (m && Number(m[1]) !== fm.id) errs.push(`filename number ${m[1]} does not match id ${fm.id}`);
  if (errs.length) throw new Error(`edrs/${filename}:\n  - ${errs.join('\n  - ')}`);
}

async function loadEdrs() {
  if (!existsSync(EDRS_DIR)) return [];
  const all = (await readdir(EDRS_DIR)).filter((f) => f.endsWith('.md'));
  const stray = all.filter((f) => !EDR_FILE_RE.test(f) && f !== 'README.md' && f !== 'template.md');
  if (stray.length) {
    throw new Error(`edrs/: ${stray.join(', ')} — records must be named NNNN-kebab-slug.md`);
  }
  const edrs = [];
  for (const file of all.filter((f) => EDR_FILE_RE.test(f)).sort()) {
    const { data, content } = matter(await readFile(join(EDRS_DIR, file), 'utf8'));
    validateEdr(file, data);
    edrs.push({ file, slug: file.replace(/\.md$/, ''), frontmatter: data, markdown: content });
  }
  const seen = new Map();
  for (const e of edrs) {
    if (seen.has(e.frontmatter.id)) {
      throw new Error(`edrs/: id ${e.frontmatter.id} used twice (${seen.get(e.frontmatter.id)}, ${e.file})`);
    }
    seen.set(e.frontmatter.id, e.file);
  }
  return edrs;
}

function edrUrl(slug) {
  return `${BASE}/edrs/${slug}/`;
}
function edrLink(id, byId) {
  const t = byId.get(id);
  const padded = String(id).padStart(4, '0');
  return t ? `<a href="${edrUrl(t.slug)}">EDR-${padded}: ${escapeHtml(t.frontmatter.title)}</a>` : `EDR-${padded}`;
}
function renderRelations(fm, byId) {
  const p = [];
  if (fm.supersedes != null) p.push(`<li>Supersedes ${edrLink(fm.supersedes, byId)}</li>`);
  if (fm.superseded_by != null) p.push(`<li>Superseded by ${edrLink(fm.superseded_by, byId)}</li>`);
  return p.length ? `<ul class="relations">${p.join('')}</ul>` : '';
}

async function buildEdrPage(edr, byId, layout) {
  const fm = edr.frontmatter;
  const html = rewriteEdrLinks(String(await processor.process(edr.markdown)));
  const idPadded = String(fm.id).padStart(4, '0');
  const body = await renderTemplate('edr.html', {
    id_padded: idPadded,
    title: escapeHtml(fm.title),
    summary: escapeHtml(fm.summary),
    status_badge: statusBadge(fm.status),
    implementation_badge: implementationBadge(fm.implementation),
    implementation_note: fm.implementation_note
      ? `<p class="edr-impl-note"><strong>What exists:</strong> ${escapeHtml(fm.implementation_note)}</p>`
      : '',
    date: escapeHtml(formatDate(fm.date)),
    authors: authorNames(fm),
    tags: tagBadges(fm),
    relations: renderRelations(fm, byId),
    content: html,
    toc: renderToc(extractToc(html)),
    source_path: `docs/edrs/${edr.file}`,
    base: BASE,
    repo_url: REPO_URL,
  });
  await writePageAt(
    `edrs/${edr.slug}`,
    applyLayout(layout, {
      title: `EDR-${idPadded}: ${escapeHtml(fm.title)} · ${SITE_NAME}`,
      nav: renderNav('edrs'),
      body,
      scripts: scriptsFor(html),
    }),
  );
  // Redirects so EDR-7, EDR-0007 and any prior slug all resolve.
  const targets = new Set([idPadded, String(fm.id), ...(fm.aliases ?? [])]);
  targets.delete(edr.slug);
  for (const t of targets) await writePageAt(`edrs/${t}`, redirectHtml(edrUrl(edr.slug)));
}

async function buildEdrIndex(edrs, layout) {
  const sorted = [...edrs].sort((a, b) => a.frontmatter.id - b.frontmatter.id);
  const rows = sorted
    .map((e) => {
      const fm = e.frontmatter;
      const href = edrUrl(e.slug);
      return (
        `<li class="edr-row">` +
        `<a class="edr-num" href="${href}">${String(fm.id).padStart(4, '0')}</a>` +
        `<div class="edr-body"><p class="edr-title"><a href="${href}">${escapeHtml(fm.title)}</a>` +
        ` ${statusBadge(fm.status)} ${implementationBadge(fm.implementation)}</p>` +
        `<p class="edr-summary">${escapeHtml(fm.summary)}</p>` +
        `<p class="edr-meta"><span class="date">${escapeHtml(formatDate(fm.date))}</span> ${tagBadges(fm)}</p>` +
        `</div></li>`
      );
    })
    .join('\n');
  const body = await renderTemplate('edr-index.html', {
    count: String(sorted.length),
    rows,
    base: BASE,
    repo_url: REPO_URL,
  });
  await writePageAt(
    'edrs',
    applyLayout(layout, { title: `Decision records · ${SITE_NAME}`, nav: renderNav('edrs'), body }),
  );
}

// ---- roadmap ------------------------------------------------------------
// Derived from the records' own `implementation` frontmatter and from nothing
// else. There is no manifest and no second list, for the same reason the
// changelog has no index: a registry only moves the line that goes stale. What
// makes the derivation trustworthy is that the field cannot be absent —
// validateEdr rejects a record without one — so this page cannot under-report
// by silently skipping a record that never declared its state.
async function buildRoadmap(edrs, layout) {
  const byState = new Map(IMPLEMENTATION_STATE_NAMES.map((s) => [s, []]));
  // A superseded or deprecated record is not outstanding work: whatever
  // replaced it carries that, and counting both would report the same work
  // twice. They keep the field — what exists is still a fact about them —
  // they simply do not appear here.
  const live = edrs.filter((e) => !RETIRED_STATUSES.has(e.frontmatter.status));
  for (const e of live) byState.get(e.frontmatter.implementation).push(e);

  // Most outstanding first: a roadmap opens on the work, not on the wins.
  const order = [...IMPLEMENTATION_STATES].reverse();

  // Every state is tallied, including the empty ones — "0 in-flight" is a fact
  // a reader wants. An empty state renders no group, though, so it is not a
  // link: an anchor to a section that was filtered out goes nowhere.
  const tally = order
    .map(([state]) => {
      const n = byState.get(state).length;
      const inner = `<span class="roadmap-tally-count">${n}</span> ${escapeHtml(state)}`;
      return n
        ? `<a class="roadmap-tally-item" href="#${state}">${inner}</a>`
        : `<span class="roadmap-tally-item is-empty">${inner}</span>`;
    })
    .join('\n    ');

  const groups = order
    .filter(([state]) => byState.get(state).length)
    .map(([state, blurb]) => {
      const rows = byState
        .get(state)
        .sort((a, b) => a.frontmatter.id - b.frontmatter.id)
        .map((e) => {
          const fm = e.frontmatter;
          const href = edrUrl(e.slug);
          return (
            `<li class="edr-row">` +
            `<a class="edr-num" href="${href}">${String(fm.id).padStart(4, '0')}</a>` +
            `<div class="edr-body"><p class="edr-title"><a href="${href}">${escapeHtml(fm.title)}</a>` +
            ` ${statusBadge(fm.status)}</p>` +
            (fm.implementation_note
              ? `<p class="edr-impl-note"><strong>What exists:</strong> ${escapeHtml(fm.implementation_note)}</p>`
              : '') +
            `</div></li>`
          );
        })
        .join('\n');
      return (
        `<section class="roadmap-group">\n` +
        `<h2 id="${state}">${implementationBadge(state)}` +
        `<span class="roadmap-count">${byState.get(state).length} of ${live.length}</span></h2>\n` +
        `<p class="roadmap-blurb">${escapeHtml(blurb)}</p>\n` +
        `<ul class="edr-list">\n${rows}\n</ul>\n</section>`
      );
    })
    .join('\n');

  const body = await renderTemplate('roadmap.html', {
    count: String(live.length),
    tally,
    groups,
    base: BASE,
    repo_url: REPO_URL,
  });
  await writePageAt(
    'roadmap',
    applyLayout(layout, { title: `Roadmap · ${SITE_NAME}`, nav: renderNav('roadmap'), body }),
  );
}

// ---- changelog ----------------------------------------------------------
// One file per entry, globbed here and spliced into content/changelog.md at its
// marker. One file per entry is what keeps two pull requests merging on the
// same day from conflicting — a single file conflicts at line 1 every time.
// Nothing enumerates the entries; do not add an index or the conflicting line
// simply moves into the index.
async function loadChangelogEntries() {
  if (!existsSync(CHANGELOG_DIR)) return [];
  const files = (await readdir(CHANGELOG_DIR)).filter((f) => f.endsWith('.md') && f !== 'README.md' && f !== 'template.md');
  const entries = [];
  for (const file of files) {
    const m = file.match(CHANGELOG_FILE_RE);
    if (!m) {
      throw new Error(`changelog/${file}: filename must be YYYY-MM-DD-slug.md (lowercase, digits and dashes)`);
    }
    // The date comes from the filename and nowhere else, so it cannot drift
    // from a frontmatter copy of itself.
    const date = m[1];
    if (Number.isNaN(Date.parse(date))) throw new Error(`changelog/${file}: '${date}' is not a real date`);
    const { data, content } = matter(await readFile(join(CHANGELOG_DIR, file), 'utf8'));
    if (!data.title || typeof data.title !== 'string') {
      throw new Error(`changelog/${file}: frontmatter 'title' is required (a string)`);
    }
    const tags = data.tags;
    if (!Array.isArray(tags) || tags.length === 0) {
      throw new Error(
        `changelog/${file}: frontmatter 'tags' is required (a non-empty array).\n` +
          `Pick from: ${CHANGELOG_TAG_NAMES.join(', ')}. See docs/changelog/README.md.`,
      );
    }
    const unknown = tags.filter((t) => !CHANGELOG_TAG_NAMES.includes(t));
    if (unknown.length) {
      throw new Error(
        `changelog/${file}: unknown tag${unknown.length === 1 ? '' : 's'} ${unknown.join(', ')}. ` +
          `The vocabulary is closed — pick from: ${CHANGELOG_TAG_NAMES.join(', ')}, or add one to ` +
          `CHANGELOG_TAGS in website/build.mjs and document it in docs/changelog/README.md.`,
      );
    }
    if (new Set(tags).size !== tags.length) throw new Error(`changelog/${file}: 'tags' contains a duplicate`);
    if (data.order != null && !Number.isInteger(data.order)) {
      throw new Error(`changelog/${file}: 'order' must be an integer`);
    }
    if (/^## /m.test(content)) {
      throw new Error(
        `changelog/${file}: an entry must not contain a '## ' heading — the date heading is generated. Use '### '.`,
      );
    }
    entries.push({ file, date, title: data.title, tags, order: data.order ?? 0, markdown: content });
  }
  entries.sort((a, b) => (a.date !== b.date ? b.date.localeCompare(a.date) : b.order - a.order || a.file.localeCompare(b.file)));
  return entries;
}

function renderChangelogFilter(entries) {
  const counts = new Map(CHANGELOG_TAG_NAMES.map((t) => [t, 0]));
  for (const e of entries) for (const t of e.tags) counts.set(t, (counts.get(t) ?? 0) + 1);
  // Rendered as real links so the control works with no JavaScript; the
  // deferred script upgrades them to in-place filtering.
  const chip = (tag, label, count) =>
    `<a class="changelog-filter-tag" href="${tag ? `?tag=${tag}` : '?'}" data-tag="${tag}">` +
    `${escapeHtml(label)}<span class="changelog-filter-count">${count}</span></a>`;
  const chips = [chip('', 'All', entries.length), ...CHANGELOG_TAGS.filter(([t]) => counts.get(t)).map(([t]) => chip(t, t, counts.get(t)))];
  return (
    '<div class="changelog-filter" data-changelog-filter>\n' +
    '  <div class="changelog-filter-chips" role="group" aria-label="Filter entries by tag">\n    ' +
    chips.join('\n    ') +
    '\n  </div>\n  <p class="changelog-filter-status" role="status" hidden></p>\n</div>\n'
  );
}

async function renderChangelogEntries(entries) {
  const out = [];
  for (const e of entries) {
    const html = rewriteLinks(String(await processor.process(e.markdown)), 'changelog');
    out.push(
      `<section class="changelog-entry" data-tags="${e.tags.join(' ')}">\n` +
        `<h2 id="${e.date}-${e.file.replace(CHANGELOG_FILE_RE, '$2')}">` +
        `<span class="changelog-date">${e.date}</span> — ${escapeHtml(e.title)}</h2>\n` +
        `<p class="changelog-tags">${e.tags.map((t) => `<span class="tag">${escapeHtml(t)}</span>`).join(' ')}</p>\n` +
        html +
        `\n</section>`,
    );
  }
  return out.join('\n');
}

async function spliceChangelog(docs, entries) {
  const doc = docs.find((d) => d.slug === 'changelog');
  if (!doc) {
    if (entries.length) throw new Error('docs/changelog/ has entries but docs/content/changelog.md is missing');
    return;
  }
  const occurrences = doc.markdown.split(ENTRIES_MARKER).length - 1;
  if (occurrences !== 1) {
    throw new Error(
      `content/changelog.md must contain exactly one '${ENTRIES_MARKER}' marker (found ${occurrences}) — ` +
        'it is where docs/changelog/*.md are spliced in.',
    );
  }
  const strays = [...doc.markdown.matchAll(/^## (\d{4}-\d{2}-\d{2})/gm)].map((m) => m[1]);
  if (strays.length) {
    throw new Error(
      `content/changelog.md: dated entries are not written in this file (found ${strays.join(', ')}). ` +
        'Move each one to its own docs/changelog/YYYY-MM-DD-slug.md — that is what keeps two pull requests ' +
        'on the same day from conflicting. See docs/changelog/README.md.',
    );
  }
  doc.markdown = doc.markdown.replace(
    ENTRIES_MARKER,
    renderChangelogFilter(entries) + '\n' + (await renderChangelogEntries(entries)),
  );
}

async function buildHome(layout, edrs) {
  const body = await renderTemplate('home.html', {
    base: BASE,
    repo_url: REPO_URL,
    edr_count: String(edrs.length),
  });
  await writeFile(
    join(DIST_DIR, 'index.html'),
    applyLayout(layout, {
      title: `${SITE_NAME} · Approved access to production data`,
      nav: renderNav('home'),
      body,
      scripts: scriptsFor(body),
    }),
  );
}

async function copyStatic() {
  if (!existsSync(STATIC_DIR)) return;
  for (const e of await readdir(STATIC_DIR)) {
    await cp(join(STATIC_DIR, e), join(DIST_DIR, e), { recursive: true });
  }
}

async function main() {
  await rm(DIST_DIR, { recursive: true, force: true });
  await mkdir(DIST_DIR, { recursive: true });
  const layout = await readFile(join(TEMPLATES_DIR, 'layout.html'), 'utf8');

  await assertVocabularies();

  const docs = await loadDocs();
  const edrs = await loadEdrs();
  const entries = await loadChangelogEntries();
  await spliceChangelog(docs, entries);

  for (const doc of docs) await buildDocPage(doc, docs, layout);

  const byId = new Map(edrs.map((e) => [e.frontmatter.id, e]));
  for (const edr of edrs) await buildEdrPage(edr, byId, layout);
  await buildEdrIndex(edrs, layout);
  await buildRoadmap(edrs, layout);

  await buildHome(layout, edrs);
  await copyStatic();
  // GitHub Pages serves /404.html for unknown paths at the domain root.
  if (existsSync(join(DIST_DIR, '404', 'index.html'))) {
    await cp(join(DIST_DIR, '404', 'index.html'), join(DIST_DIR, '404.html'));
  }

  await assertNotIndexable();

  console.log(
    `Built landing + ${docs.length} doc page(s) + ${edrs.length} EDR(s) + ${entries.length} changelog entr${entries.length === 1 ? 'y' : 'ies'} → ${DIST_DIR}`,
  );
}

// Three closed vocabularies are written down twice: once as a constant here,
// which the build enforces, and once as a table in a README, which people
// actually read. CLAUDE.md states the coupling as a rule — "adding a tag means
// editing both" — which is the honest admission that nothing enforced it, and
// this repository's own position is that a rule depending on someone
// remembering it is not a rule.
//
// So the build parses the tables back out and compares. The README is located
// by an explicit marker rather than by guessing which table is the vocabulary,
// because a heuristic that silently matches the wrong table is worse than no
// check: it would pass while comparing nothing.
//
// Only the VALUES are compared, in ORDER. Descriptions are prose and are
// allowed to differ in wording — asserting those would make the check a
// nuisance that gets deleted. Order is compared because both orders are
// load-bearing: implementation states run most-built to least and the roadmap
// reverses them, and the changelog filter bar renders tags in array order.
const VOCABULARIES = [
  {
    marker: 'implementation',
    file: 'edrs/README.md',
    constant: 'IMPLEMENTATION_STATES in website/build.mjs',
    values: () => IMPLEMENTATION_STATE_NAMES,
  },
  {
    marker: 'status',
    file: 'edrs/README.md',
    constant: 'VALID_STATUSES in website/build.mjs',
    // A Set has no meaningful order, so this one is compared as a sorted set.
    values: () => [...VALID_STATUSES].sort(),
    sorted: true,
  },
  {
    marker: 'changelog-tags',
    file: 'changelog/README.md',
    constant: 'CHANGELOG_TAGS in website/build.mjs',
    values: () => CHANGELOG_TAG_NAMES,
  },
];

// Pull the first column out of the markdown table following a marker comment.
// Values are the backticked cell contents, so a row whose first cell is not
// `like this` is not a vocabulary row and is skipped — which is what lets the
// table carry a header and a separator without special-casing them.
function readVocabularyTable(text, marker, file) {
  const at = text.indexOf(`<!-- @vocabulary:${marker} -->`);
  if (at === -1) {
    throw new Error(
      `${file}: no <!-- @vocabulary:${marker} --> marker.\n` +
        `  The build compares that table against a constant, and it locates the\n` +
        `  table by this marker. Put it on the line before the table.`,
    );
  }
  const rest = text.slice(at).split('\n').slice(1);
  const values = [];
  let started = false;
  for (const line of rest) {
    if (!line.startsWith('|')) {
      if (started) break;
      continue;
    }
    started = true;
    const first = line.split('|')[1]?.trim() ?? '';
    const m = /^`([^`]+)`$/.exec(first);
    if (m) values.push(m[1]);
  }
  if (!values.length) {
    throw new Error(`${file}: the table after <!-- @vocabulary:${marker} --> has no \`backticked\` values in its first column`);
  }
  return values;
}

async function assertVocabularies() {
  const cache = new Map();
  const problems = [];
  for (const v of VOCABULARIES) {
    if (!cache.has(v.file)) cache.set(v.file, await readFile(join(DOCS_DIR, v.file), 'utf8'));
    const documented = readVocabularyTable(cache.get(v.file), v.marker, `docs/${v.file}`);
    const enforced = v.values();
    const inTable = v.sorted ? [...documented].sort() : documented;

    const missing = enforced.filter((x) => !inTable.includes(x));
    const extra = inTable.filter((x) => !enforced.includes(x));
    if (missing.length || extra.length) {
      const lines = [`${v.constant} and docs/${v.file} disagree:`];
      for (const x of missing) lines.push(`  '${x}' is enforced but undocumented — add it to docs/${v.file}`);
      for (const x of extra) lines.push(`  '${x}' is documented but not enforced — add it to ${v.constant}, or remove the row`);
      problems.push(lines.join('\n'));
    } else if (!v.sorted && inTable.join('\u0000') !== enforced.join('\u0000')) {
      problems.push(
        `${v.constant} and docs/${v.file} list the same values in a different order:\n` +
          `  enforced:   ${enforced.join(', ')}\n` +
          `  documented: ${inTable.join(', ')}\n` +
          `  The order is load-bearing — it is the order the site renders — so make the table match.`,
      );
    }
  }
  if (problems.length) throw new Error(problems.join('\n\n'));
}

// Marque is at design stage and this site is deliberately kept out of search
// results and web archives. A robots.txt cannot do it: robots.txt is honoured at
// a domain ROOT, and this is a project site served at a subpath, so one in this
// repository would land at /marque/robots.txt and be ignored. The per-page meta
// directives in templates/layout.html are therefore the only lever — which makes
// them exactly the kind of thing that gets dropped by an innocent template edit.
// So the build refuses to produce a page without them.
async function assertNotIndexable() {
  const offenders = [];
  const walk = async (dir) => {
    for (const e of await readdir(dir, { withFileTypes: true })) {
      const p = join(dir, e.name);
      if (e.isDirectory()) await walk(p);
      else if (e.name.endsWith('.html')) {
        const html = await readFile(p, 'utf8');
        const meta = html.match(/<meta[^>]+name="robots"[^>]*>/i)?.[0] ?? '';
        if (!/noindex/i.test(meta) || !/noarchive/i.test(meta)) {
          offenders.push(relative(DIST_DIR, p));
        }
      }
    }
  };
  await walk(DIST_DIR);
  if (offenders.length) {
    throw new Error(
      `${offenders.length} emitted page(s) lack a robots meta with noindex+noarchive:\n  ` +
        offenders.slice(0, 10).join('\n  ') +
        '\n\nThis site is deliberately not indexable while Marque is at design stage, and a\n' +
        'robots.txt cannot enforce it from a project subpath. Restore the directives in\n' +
        'website/templates/layout.html (and redirectHtml in this file) rather than removing\n' +
        'this check.',
    );
  }
}

main().catch((err) => {
  console.error(`\nBuild failed:\n${err.message}\n`);
  process.exit(1);
});
