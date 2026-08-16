// Landing-page pseudo-terminal.
//
// Plays through the flow the decision records specify. Nothing here is a
// recording of running software — Marque is at design stage — so the component
// is labelled illustrative in the markup and this file invents no behaviour
// that a record does not describe.
//
// Vanilla JS, no dependencies. Respects prefers-reduced-motion by rendering the
// full transcript statically instead of animating.
(function () {
  'use strict';

  // Line kinds. The prefix carries the meaning, so nothing depends on colour
  // alone: `cmd` renders "$ ", ok renders "✓", step renders "→", and errors
  // keep psql's own ERROR:/DETAIL:/HINT: prefixes.
  //   cmd   a shell command
  //   sql   a psql prompt line
  //   out   ordinary output
  //   dim   secondary detail
  //   lbl   a section label
  //   ok    a check that passed
  //   step  a step in progress
  //   err   an error line
  //   meas  a measured-fact marker
  //   modl  model-produced prose
  //   gap   blank line
  const SCENES = [
    {
      id: 'blocked',
      tab: 'Out of scope',
      caption:
        'An operator runs a statement their delegation does not cover. It does not hang and it does ' +
        'not silently do less — it returns at once (SQLSTATE 42501) with the request id and who is ' +
        'being asked.',
      lines: [
        ['cmd', 'marque psql -h prod-primary -U settings_writer'],
        ['dim', 'psql (marque, PostgreSQL protocol emulated)'],
        ['dim', 'Type "help" for help.'],
        ['gap', ''],
        ['sql', "prod-primary=> UPDATE accounts SET tier = 'trial'"],
        ['sql', "prod-primary->   WHERE tier = 'sandbox';"],
        ['gap', ''],
        ['err', 'ERROR:  outside your delegated scope; submitted for approval'],
        ['err', 'DETAIL:  column accounts.tier is not in your delegation'],
        ['err', 'DETAIL:  req_01JB2Q9F3K8Z — waiting on sam@acme.example, then group:data-oncall'],
        ['err', 'HINT:  re-run this statement once approved, or watch: marque watch req_01JB2Q9F3K8Z'],
        ['gap', ''],
        ['sql', 'prod-primary=>'],
      ],
    },
    {
      id: 'review',
      tab: 'The analysis',
      caption:
        'What an approver is shown. The row count is measured by a rehearsal that ran in a ' +
        'transaction and rolled back — not a planner estimate. The model writes prose beside those ' +
        'facts and is marked as its source. There is no risk score and no recommendation.',
      lines: [
        ['cmd', 'marque watch req_01JB2Q9F3K8Z'],
        ['gap', ''],
        ['out', 'request   req_01JB2Q9F3K8Z          submitted by dana@acme.example'],
        ['out', 'target    prod-primary             role settings_writer (sensitive)'],
        ['out', 'reason    ACME-4471 — move legacy sandbox accounts to trial'],
        ['gap', ''],
        ['lbl', 'statement'],
        ['out', "  UPDATE accounts SET tier = 'trial' WHERE tier = 'sandbox';"],
        ['gap', ''],
        ['lbl', 'parser                                              [measured]'],
        ['out', '  operations   UPDATE'],
        ['out', '  tables       public.accounts'],
        ['out', '  columns      tier'],
        ['out', '  scope        no delegation covers column accounts.tier'],
        ['gap', ''],
        ['lbl', 'rehearsal   transaction rolled back                 [measured]'],
        ['meas', '  rows_affected  412'],
        ['meas', '  duration_ms    38'],
        ['out', '  plan           Index Scan on accounts_tier_idx   (estimate)'],
        ['out', '  warnings       none'],
        ['gap', ''],
        ['lbl', 'analysis                                  [model — advisory]'],
        ['modl', '  Retiers every sandbox account in one statement. 412 rows is'],
        ['modl', '  the whole sandbox population rather than a subset, so this is'],
        ['modl', '  a bulk migration, not a fix to one account.'],
        ['modl', '  Questions to ask: should accounts created in the last day be'],
        ['modl', '  excluded? Does trial billing apply immediately on retier?'],
        ['gap', ''],
        ['lbl', 'escalation'],
        ['out', '  1  sam@acme.example     approver for settings_writer     30m'],
        ['out', '  2  group:data-oncall    prod-primary is critical          2h'],
        ['gap', ''],
        ['step', 'waiting on stage 1 — sam@acme.example'],
      ],
    },
    {
      id: 'approve',
      tab: 'Approval',
      caption:
        'A marque is signed by the approver’s own hardware key and countersigned by the control ' +
        'plane. Neither party can produce a valid one alone. How many approvals were required is ' +
        'written inside the payload every signature covers, so it cannot be stripped later.',
      lines: [
        ['cmd', 'marque approve req_01JB2Q9F3K8Z'],
        ['gap', ''],
        ['out', 'You are stage 1 of 2.'],
        ['out', '  submitted by   dana@acme.example'],
        ['out', '  target/role    prod-primary / settings_writer (sensitive)'],
        ['out', "  statement      UPDATE accounts SET tier = 'trial'"],
        ['out', "                   WHERE tier = 'sandbox';"],
        ['meas', '  rehearsed      412 rows, 38 ms                   [measured]'],
        ['gap', ''],
        ['lbl', 'you are granting'],
        ['out', '  role       settings_writer on prod-primary'],
        ['out', '  not before 2026-08-16T09:14:02Z'],
        ['out', '  expires    2026-08-16T10:14:02Z   (1h)'],
        ['out', '  budget     1 execution, max_rows 500'],
        ['gap', ''],
        ['step', 'Touch your authenticator to sign.'],
        ['ok', 'user verification confirmed   webauthn, key enrolled 2026-06-02'],
        ['ok', 'approver signature            sam@acme.example'],
        ['ok', 'authority signature           harbourmaster (kms)'],
        ['gap', ''],
        ['step', 'stage 1 satisfied — advancing to stage 2 (group:data-oncall)'],
        ['gap', ''],
        ['dim', '… 4 minutes later, rae@acme.example approves stage 2 …'],
        ['gap', ''],
        ['ok', 'marque mrq_01JB2QF7X0ZK issued to dana@acme.example'],
        ['out', '  approvals  2 of 2 required, distinct principals'],
        ['dim', '             (required count is signed into the payload)'],
        ['out', '  expires    2026-08-16T10:14:02Z'],
      ],
    },
    {
      id: 'run',
      tab: 'Execution',
      caption:
        'The Pilot verifies the marque offline — it never asks the control plane. The execution ' +
        'nonce is claimed before the statement runs, so a retry returns the recorded outcome ' +
        'instead of applying the change twice.',
      lines: [
        ['cmd', 'marque run mrq_01JB2QF7X0ZK'],
        ['gap', ''],
        ['ok', 'marque verified        offline — approver + authority signatures'],
        ['ok', 'approvals              2 of 2, distinct principals'],
        ['ok', 'window                 valid, 58m remaining'],
        ['ok', 'nonce claimed          exec_7f31c2   (budget 1 → 0)'],
        ['gap', ''],
        ['step', 'fence pre-check        0 rows outside fence'],
        ['step', "UPDATE accounts SET tier = 'trial' WHERE tier = 'sandbox';"],
        ['ok', 'fence post-assert      0 affected rows outside fence'],
        ['ok', 'max_rows               412 ≤ 500'],
        ['ok', 'COMMIT                 412 rows, 41 ms'],
        ['gap', ''],
        ['out', 'execution recorded — exec_7f31c2'],
        ['gap', ''],
        ['dim', '# the connection dropped; run it again with the same nonce'],
        ['cmd', 'marque run mrq_01JB2QF7X0ZK --nonce exec_7f31c2'],
        ['ok', 'nonce already claimed — returning the recorded outcome'],
        ['out', '  committed, 412 rows, 41 ms   (statement not re-applied)'],
      ],
    },
    {
      id: 'fence',
      tab: 'The fence',
      caption:
        'A delegated statement that would touch rows outside your scope is not quietly trimmed to ' +
        'the rows you are allowed. The transaction aborts, tells you the count, and applies nothing ' +
        '— a half-applied change nobody reviewed is worse than a refusal.',
      lines: [
        ['cmd', 'marque psql -h prod-primary -U support_writer'],
        ['dim', "delegation: update orders.status where status = 'pending', max 50 rows"],
        ['gap', ''],
        ['sql', "prod-primary=> UPDATE orders SET status = 'cancelled'"],
        ['sql', 'prod-primary->   WHERE customer_id = 4471;'],
        ['gap', ''],
        ['err', 'ERROR:  aborted — 3 of 19 affected rows fall outside your delegated scope'],
        ['err', "DETAIL:  fence  status = 'pending'"],
        ['err', 'DETAIL:  nothing was applied; the transaction rolled back whole'],
        ['err', 'HINT:  narrow the statement, or submit it for approval'],
        ['gap', ''],
        ['sql', 'prod-primary=>'],
      ],
    },
    {
      id: 'logbook',
      tab: 'The logbook',
      caption:
        'Months later. Every entry names who asked, who approved, what they were shown, and what ' +
        'changed — and for anything an agent did, both the actor and the human it acted for. The ' +
        'log is append-only and hash-chained.',
      lines: [
        ['cmd', 'marque log --target prod-primary --since 7d'],
        ['gap', ''],
        ['lbl', '2026-08-16 09:41:12  execution.committed   mrq_01JB2QF7X0ZK'],
        ['out', '  actor      dana@acme.example'],
        ['out', "  statement  UPDATE accounts SET tier = 'trial' WHERE tier = 'sandbox';"],
        ['out', '  approved   sam@acme.example, rae@acme.example   (2 of 2)'],
        ['meas', '  shown      412 rows rehearsed · analysis sha256:1a7e…'],
        ['out', '  result     committed, 412 rows, 41 ms'],
        ['gap', ''],
        ['lbl', '2026-08-16 08:02:55  standing_order.invoked   unlock-account'],
        ['out', '  actor      priya@acme.example'],
        ['out', '  authority  standing order signed by sam@acme.example 2026-07-02'],
        ['dim', '             (the human signed the shape, not this instance)'],
        ['out', "  binding    account_id = 9f2c…, tier = 'sandbox'"],
        ['out', '  result     committed, 1 row, 6 ms'],
        ['gap', ''],
        ['lbl', '2026-08-15 22:17:03  execution.committed   mrq_01JAZ8T2M4QP'],
        ['out', '  actor      svc:order-bot     on behalf of  sam@acme.example'],
        ['out', '  task       tsk_01JAZ8SN — "unstick order 88213 (ACME-4471)"'],
        ['dim', "             declared scope: orders.status where id = '88213', 1 row"],
        ['out', '  approved   sam@acme.example, rae@acme.example   (2 of 2)'],
        ['out', "  statement  UPDATE orders SET status = 'processing' WHERE id = '88213';"],
        ['out', '  result     committed, 1 row, 4 ms'],
        ['gap', ''],
        ['lbl', '2026-08-15 16:30:41  marque.refused        req_01JAYW6K1D'],
        ['out', '  actor      dana@acme.example'],
        ['err', '  refused    rae@acme.example — "run this through the migration pipeline"'],
        ['gap', ''],
        ['ok', 'chain verified to seq 918273 · anchored 2026-08-16T00:00Z'],
      ],
    },
  ];

  const PREFIX = { ok: '✓ ', step: '→ ' };

  function build(root) {
    const tablist = root.querySelector('[data-term-tabs]');
    const body = root.querySelector('[data-term-body]');
    const caption = root.querySelector('[data-term-caption]');
    const status = root.querySelector('[data-term-status]');
    const replayBtn = root.querySelector('[data-term-replay]');
    const skipBtn = root.querySelector('[data-term-skip]');
    if (!tablist || !body || !caption) return;

    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)');
    let active = 0;
    let timers = [];
    let playing = false;

    SCENES.forEach(function (scene, i) {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'term-tab';
      b.id = 'term-tab-' + scene.id;
      b.setAttribute('role', 'tab');
      b.setAttribute('aria-controls', 'term-panel');
      b.setAttribute('aria-selected', i === 0 ? 'true' : 'false');
      b.tabIndex = i === 0 ? 0 : -1;
      b.textContent = String(i + 1) + ' · ' + scene.tab;
      b.addEventListener('click', function () {
        select(i, true);
      });
      tablist.appendChild(b);
    });
    const tabs = Array.prototype.slice.call(tablist.querySelectorAll('.term-tab'));

    tablist.addEventListener('keydown', function (ev) {
      let next = null;
      if (ev.key === 'ArrowRight') next = (active + 1) % SCENES.length;
      else if (ev.key === 'ArrowLeft') next = (active - 1 + SCENES.length) % SCENES.length;
      else if (ev.key === 'Home') next = 0;
      else if (ev.key === 'End') next = SCENES.length - 1;
      if (next === null) return;
      ev.preventDefault();
      select(next, true);
      tabs[next].focus();
    });

    function clearTimers() {
      timers.forEach(clearTimeout);
      timers = [];
    }

    function makeLine(kind, text) {
      const el = document.createElement('span');
      el.className = 'l l-' + kind;
      if (kind === 'gap') {
        el.textContent = ' ';
        return el;
      }
      if (kind === 'cmd') {
        const p = document.createElement('span');
        p.className = 'l-sigil';
        p.textContent = '$ ';
        el.appendChild(p);
        const t = document.createElement('span');
        t.className = 'l-text';
        t.textContent = text;
        el.appendChild(t);
        return el;
      }
      el.textContent = (PREFIX[kind] || '') + text;
      return el;
    }

    // Reveal everything at once — used for reduced motion and the skip control.
    function showAll() {
      clearTimers();
      playing = false;
      Array.prototype.forEach.call(body.children, function (el) {
        el.classList.add('is-on');
        const t = el.querySelector('.l-text');
        if (t && t.dataset.full) t.textContent = t.dataset.full;
      });
      body.removeAttribute('aria-busy');
      if (status) status.textContent = 'Full transcript shown.';
      if (skipBtn) skipBtn.disabled = true;
    }

    function render(i) {
      clearTimers();
      body.textContent = '';
      const scene = SCENES[i];
      caption.textContent = scene.caption;
      body.setAttribute('aria-label', 'Terminal transcript: ' + scene.tab);
      scene.lines.forEach(function (ln) {
        body.appendChild(makeLine(ln[0], ln[1]));
      });
    }

    function play(i) {
      render(i);
      if (reduced.matches) {
        showAll();
        return;
      }
      playing = true;
      body.setAttribute('aria-busy', 'true');
      if (skipBtn) skipBtn.disabled = false;
      if (status) status.textContent = 'Playing…';

      const kids = Array.prototype.slice.call(body.children);
      let at = 0;
      let n = 0;

      function step() {
        if (!playing || n >= kids.length) {
          if (playing) {
            playing = false;
            body.removeAttribute('aria-busy');
            if (status) status.textContent = 'Done.';
            if (skipBtn) skipBtn.disabled = true;
          }
          return;
        }
        const el = kids[n];
        const kind = SCENES[i].lines[n][0];
        el.classList.add('is-on');
        n++;

        if (kind === 'cmd') {
          const t = el.querySelector('.l-text');
          const full = t.textContent;
          t.dataset.full = full;
          t.textContent = '';
          let c = 0;
          (function type() {
            if (!playing) {
              t.textContent = full;
              return;
            }
            t.textContent = full.slice(0, ++c);
            if (c < full.length) timers.push(setTimeout(type, 16));
            else timers.push(setTimeout(step, 320));
          })();
          return;
        }
        at = kind === 'gap' ? 24 : kind === 'lbl' ? 130 : 62;
        timers.push(setTimeout(step, at));
      }
      timers.push(setTimeout(step, 200));
    }

    function select(i, autoplay) {
      active = i;
      tabs.forEach(function (t, j) {
        t.setAttribute('aria-selected', j === i ? 'true' : 'false');
        t.tabIndex = j === i ? 0 : -1;
      });
      body.setAttribute('aria-labelledby', tabs[i].id);
      if (autoplay) play(i);
      else render(i);
    }

    if (replayBtn) {
      replayBtn.addEventListener('click', function () {
        play(active);
      });
    }
    if (skipBtn) {
      skipBtn.addEventListener('click', showAll);
      skipBtn.disabled = true;
    }

    select(0, false);
    if (reduced.matches) {
      showAll();
      return;
    }

    // Start when it is actually on screen, once.
    if ('IntersectionObserver' in window) {
      const io = new IntersectionObserver(
        function (entries) {
          entries.forEach(function (e) {
            if (e.isIntersecting) {
              io.disconnect();
              play(0);
            }
          });
        },
        { threshold: 0.25 },
      );
      io.observe(root);
    } else {
      play(0);
    }
  }

  const roots = document.querySelectorAll('[data-term]');
  Array.prototype.forEach.call(roots, build);
})();
