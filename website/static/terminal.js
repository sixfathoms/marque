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

  // ---- line kinds ---------------------------------------------------------
  // The prefix or sigil carries the meaning, so nothing depends on colour
  // alone. A kind may be written "<rail>:<kind>" to draw a left rule beside it:
  //   b:  neutral rail      u:  urgent rail      d:  danger rail
  //
  //   cmd   a shell command (typed out)
  //   sql   a psql prompt line
  //   in    something the operator typed at a prompt
  //   out   ordinary output
  //   dim   secondary detail
  //   lbl   a section label
  //   ok    a check that passed          ✓
  //   step  a step in progress           →
  //   warn  a loud consequence           ⚠
  //   err   an error line
  //   meas  a measured fact
  //   modl  model-produced prose
  //   rule  a box rule (neutral / urgent / danger via ruleu, ruled)
  //   gap   blank line
  const PREFIX = { ok: '✓ ', step: '→ ', warn: '⚠ ' };

  const SCENES = [
    {
      id: 'blocked',
      tab: 'Out of scope',
      caption:
        'An operator runs a statement their delegation does not cover. It does not hang and it does ' +
        'not silently do less — it returns at once (SQLSTATE 42501) with a reference, the measured ' +
        'facts, and the chain of people being asked.',
      lines: [
        ['cmd', 'marque psql -h prod-primary -U settings_writer'],
        ['dim', 'psql (marque, PostgreSQL protocol emulated)'],
        ['dim', 'Type "help" for help.'],
        ['gap', ''],
        ['sql', "prod-primary=> UPDATE accounts SET tier = 'trial'"],
        ['sql', "prod-primary->   WHERE tier = 'sandbox';"],
        ['gap', ''],
        ['err', 'ERROR:  outside your delegated scope; submitted for approval'],
        ['err', 'DETAIL:  req_01JB2Q9F3K8Z · 412 rows rehearsed, 0 outside fence'],
        ['dim', 'SQLSTATE 42501'],
        ['gap', ''],
        ['rule', '╭─ req_01JB2Q9F3K8Z ──────────────────── awaiting approval'],
        ['b:out', ''],
        ['b:out', '  submitted   dana@acme.example · 2s ago'],
        ['b:out', '  target      prod-primary / settings_writer (sensitive)'],
        ['b:out', '  scope       column accounts.tier is not in your delegation'],
        ['b:out', ''],
        ['b:meas', '  rehearsed   412 rows · 38 ms · 0 outside fence   [measured]'],
        ['b:out', ''],
        ['b:step', '  stage 1   sam@acme.example           waiting     30m'],
        ['b:dim', '    stage 2   group:data-oncall          queued       2h'],
        ['b:dim', '              prod-primary is critical'],
        ['b:out', ''],
        ['b:out', '  share       marque://acme/req_01JB2Q9F3K8Z'],
        ['b:dim', '              https://marque.acme.example/r/01JB2Q9F3K8Z'],
        ['b:dim', '              a reference is not a capability — viewing still'],
        ['b:dim', '              requires entitlement'],
        ['b:out', ''],
        ['b:out', '  watch       marque watch req_01JB2Q9F3K8Z'],
        ['b:out', ''],
        ['rule', '╰─'],
        ['gap', ''],
        ['sql', 'prod-primary=>'],
      ],
    },
    {
      id: 'review',
      tab: 'Analysis',
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
        ['meas', '  write_set      public.accounts  412 updated'],
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
        'plane. Neither party can produce a valid one alone. The per-stage requirement is written ' +
        'inside the payload every signature covers, so it cannot be stripped later.',
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
        ['out', '  objects    public.accounts'],
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
        ['out', '  approvals  stage 1 ✓ sam · stage 2 ✓ rae   (distinct principals)'],
        ['dim', '             (the per-stage requirement is signed into the payload)'],
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
        ['ok', 'approvals              every stage satisfied, distinct principals'],
        ['ok', 'window                 valid, 58m remaining'],
        ['ok', 'nonce claimed          exec_7f31c2   (budget 1 → 0)'],
        ['gap', ''],
        ['step', 'fence pre-check        0 rows outside fence'],
        ['step', "UPDATE accounts SET tier = 'trial' WHERE tier = 'sandbox';"],
        ['ok', 'fence post-assert      0 affected rows outside fence'],
        ['ok', 'max_rows               412 ≤ 500'],
        ['ok', 'write-set assert       public.accounts only'],
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
      id: 'urgent',
      tab: 'Urgent',
      caption:
        'During an incident, --urgent notifies every stage at once instead of sequentially, pages ' +
        'rather than messages, and adds the emergency approver set. It changes who is asked and how ' +
        'loudly. It does not change the scope, or how many approvals are required.',
      lines: [
        ['cmd', 'marque psql -h prod-primary -U settings_writer'],
        ['dim', '# \\urgent mid-session, or --urgent on submit. A reason is required.'],
        ['sql', 'prod-primary=> \\urgent "INC-2291 checkout 5xx — sandbox tier flag stuck"'],
        ['ok', 'urgency set for this session'],
        ['gap', ''],
        ['sql', "prod-primary=> UPDATE accounts SET tier = 'trial' WHERE tier = 'sandbox';"],
        ['gap', ''],
        ['err', 'ERROR:  outside your delegated scope; submitted for approval'],
        ['dim', 'SQLSTATE 42501'],
        ['gap', ''],
        ['ruleu', '╭─ req_01JB2QW4M7RT ─────────────────────────────────── URGENT'],
        ['u:out', ''],
        ['u:out', '  reason      INC-2291 checkout 5xx — sandbox tier flag stuck'],
        ['u:meas', '  rehearsed   412 rows · 38 ms                      [measured]'],
        ['u:out', ''],
        ['u:step', '  stage 1   sam@acme.example           paged        now'],
        ['u:step', '  stage 2   group:data-oncall          paged        now'],
        ['u:step', '        +  group:incident-command     emergency set'],
        ['u:out', ''],
        ['u:dim', '  scope and approval count are unchanged.'],
        ['u:dim', '  collapse_stages_when_urgent   off   (per target)'],
        ['u:out', ''],
        ['ruleu', '╰─'],
        ['gap', ''],
        ['step', 'paged   sam@acme.example, group:data-oncall, group:incident-command'],
        ['step', 'posted  #incidents — "req_01JB2QW4M7RT needs approval · INC-2291"'],
        ['gap', ''],
        ['cmd', 'marque watch req_01JB2QW4M7RT'],
        ['ok', 'stage 1 approved   sam@acme.example              41s'],
        ['ok', 'stage 2 approved   rae@acme.example (data-oncall) 1m 12s'],
        ['ok', 'marque mrq_01JB2QWB3F2N issued — expires in 30m'],
      ],
    },
    {
      id: 'breakglass',
      tab: 'Break glass',
      caption:
        'A break-glass grant is dormant until it is explicitly broken. It removes the wait for ' +
        'another human — not the fence, the role, the write-set assertion, the nonce or the ' +
        'logbook. It is announced the moment it happens and reviewed afterwards.',
      lines: [
        ['sql', 'prod-primary=> \\breakglass'],
        ['gap', ''],
        ['ruled', '╭─ BREAK GLASS ───────────────────────────────────────────────'],
        ['d:out', ''],
        ['d:warn', '  You hold a dormant break-glass grant on this target.'],
        ['d:out', '  Using it approves your own statement.'],
        ['d:out', ''],
        ['d:out', '  grant     bgg_01JB7K2N — dana@acme.example'],
        ['d:out', '  scope     UPDATE on public.* as settings_writer'],
        ['d:out', '  co_sign   none'],
        ['d:out', '  max_ttl   15m'],
        ['d:out', '  granted   theo@acme.example · expires 2026-11-30'],
        ['d:out', ''],
        ['d:dim', '  Everything else still applies: the fence, the write-set'],
        ['d:dim', '  assertion, the role’s own grants, the execution nonce'],
        ['d:dim', '  and the logbook.'],
        ['d:out', ''],
        ['d:warn', '  This is announced immediately and reviewed afterwards.'],
        ['d:warn', '  It is not a quiet path.'],
        ['d:out', ''],
        ['ruled', '╰─'],
        ['gap', ''],
        ['out', 'Type BREAK GLASS to continue, or anything else to stop.'],
        ['in', '> BREAK GLASS'],
        ['gap', ''],
        ['out', 'Justification (required, recorded, signed into the marque):'],
        ['in', '> INC-2291 — checkout 5xx. sandbox tier flag stuck. data-oncall'],
        ['in', '> paged 11m ago, no response. Reverting the flag now.'],
        ['gap', ''],
        ['step', 'Touch your authenticator to sign.'],
        ['ok', 'user verification confirmed'],
        ['ok', 'approver signature    dana@acme.example  (self, under grant)'],
        ['ok', 'authority signature   harbourmaster (kms)'],
        ['gap', ''],
        ['warn', 'posted   #eng-alerts — "dana@acme.example BROKE GLASS on prod-primary"'],
        ['warn', 'paged    theo@acme.example (grantor), group:security'],
        ['warn', 'logged   break_glass.used — justification recorded verbatim'],
        ['warn', 'review   mandatory post-hoc review queued, due within 24h'],
        ['gap', ''],
        ['ok', 'marque mrq_01JB2R0C8Q4M issued — auth.kind break_glass'],
        ['out', '  expires 15m · budget 1 · justification signed into the payload'],
        ['gap', ''],
        ['sql', "prod-primary=> UPDATE accounts SET tier = 'trial' WHERE tier = 'sandbox';"],
        ['ok', 'fence pre-check     0 rows outside grant'],
        ['ok', 'write-set assert    public.accounts only'],
        ['ok', 'COMMIT              412 rows, 44 ms'],
      ],
    },
    {
      id: 'queue',
      tab: 'The queue',
      caption:
        'Everything you have in flight, in one place: what is still waiting on somebody, what is ' +
        'approved and ready to run, and how long each marque has left. Then jump straight to running ' +
        'one, or to reading what a past one did.',
      lines: [
        ['cmd', 'marque requests'],
        ['gap', ''],
        ['lbl', 'REF               STATE      TARGET        AGE   WAITING ON / EXPIRES'],
        ['ok', 'req_01JB2Q9F3K8Z  approved   prod-primary   6m   expires in 54m · budget 1'],
        ['ok', 'req_01JB2QW4M7RT  approved   prod-primary   3m   expires in 27m · budget 1'],
        ['step', 'req_01JB2R1H5XKC  pending    prod-primary  12m   group:data-oncall (2 of 2)'],
        ['step', 'req_01JB2R44N8PD  pending    reporting-ro   1h   priya@acme.example (1 of 1)'],
        ['out', 'req_01JB2Q1B9K3C  executed   prod-primary   2h   1 row · committed'],
        ['err', 'req_01JB2QK9V2LM  refused    prod-primary   2h   rae@acme.example'],
        ['gap', ''],
        ['dim', '2 ready to run · 2 waiting on others · 1 refused'],
        ['warn', 'req_01JB2QW4M7RT approved and unused — expires in 27m'],
        ['gap', ''],
        ['dim', '--mine (default) · --approving (waiting on you) · --all'],
        ['gap', ''],
        ['cmd', 'marque run req_01JB2Q9F3K8Z'],
        ['ok', 'marque mrq_01JB2QF7X0ZK verified offline'],
        ['ok', 'COMMIT   412 rows, 41 ms   →  exec_7f31c2'],
        ['gap', ''],
        ['cmd', 'marque output req_01JB2Q9F3K8Z'],
        ['gap', ''],
        ['out', '  exec_7f31c2 · committed 2026-08-16T09:41:12Z · 41 ms'],
        ['gap', ''],
        ['out', '  UPDATE 412'],
        ['gap', ''],
        ['meas', '  write set   public.accounts   412 updated        [measured]'],
        ['out', '  fence       0 rows outside · max_rows 412 ≤ 500'],
      ],
    },
    {
      id: 'logbook',
      tab: 'Logbook',
      caption:
        'Months later. Every entry names who asked, who approved, what they were shown, and what ' +
        'changed — and for anything an agent did, both the actor and the human it acted for. The ' +
        'log is append-only and hash-chained.',
      lines: [
        ['cmd', 'marque log --target prod-primary --since 7d'],
        ['gap', ''],
        ['lbl', '2026-08-16 09:52:40  break_glass.used      mrq_01JB2R0C8Q4M'],
        ['warn', '  actor      dana@acme.example    (self-approved under grant)'],
        ['out', '  grant      UPDATE on public.* — granted by theo@acme.example'],
        ['out', '  reason     "INC-2291 — checkout 5xx. sandbox tier flag stuck."'],
        ['out', '  review     pending — due 2026-08-17 09:52Z'],
        ['gap', ''],
        ['lbl', '2026-08-16 09:41:12  execution.committed   mrq_01JB2QF7X0ZK'],
        ['out', '  actor      dana@acme.example'],
        ['out', "  statement  UPDATE accounts SET tier = 'trial' WHERE tier = 'sandbox';"],
        ['out', '  approved   sam@acme.example, rae@acme.example   (stages 1, 2)'],
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
        ['out', '  approved   sam@acme.example, rae@acme.example   (stages 1, 2)'],
        ['out', "  statement  UPDATE orders SET status = 'processing' WHERE id = '88213';"],
        ['out', '  result     committed, 1 row, 4 ms'],
        ['gap', ''],
        ['ok', 'chain verified to seq 918273 · anchored 2026-08-16T00:00Z'],
      ],
    },
  ];

  // ---- rendering ----------------------------------------------------------

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
      const n = document.createElement('span');
      n.className = 'term-tab-n';
      n.setAttribute('aria-hidden', 'true');
      n.textContent = String(i + 1);
      b.appendChild(n);
      b.appendChild(document.createTextNode(scene.tab));
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

    function span(cls, text, hidden) {
      const s = document.createElement('span');
      s.className = cls;
      s.textContent = text;
      if (hidden) s.setAttribute('aria-hidden', 'true');
      return s;
    }

    // "b:out" → rail "b", kind "out". Plain "out" → no rail.
    function splitKind(kind) {
      const i = kind.indexOf(':');
      if (i < 0) return { rail: null, kind: kind };
      return { rail: kind.slice(0, i), kind: kind.slice(i + 1) };
    }

    function makeLine(rawKind, text) {
      const parsed = splitKind(rawKind);
      const kind = parsed.kind;
      const el = document.createElement('span');
      el.className = 'l l-' + kind + (parsed.rail ? ' l-railed rail-' + parsed.rail : '');

      if (kind === 'gap') {
        el.className = 'l l-gap';
        el.textContent = ' ';
        return el;
      }

      if (parsed.rail) el.appendChild(span('l-rail', '│', true));

      if (kind === 'cmd') {
        el.appendChild(span('l-sigil', '$ ', true));
        el.appendChild(span('l-text', text));
        return el;
      }
      if (kind === 'in') {
        el.appendChild(span('l-text', text));
        return el;
      }
      el.appendChild(document.createTextNode((PREFIX[kind] || '') + text));
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
        const kind = splitKind(SCENES[i].lines[n][0]).kind;
        el.classList.add('is-on');
        n++;

        if (kind === 'cmd' || kind === 'in') {
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
            if (c < full.length) timers.push(setTimeout(type, 15));
            else timers.push(setTimeout(step, 300));
          })();
          return;
        }
        const at = kind === 'gap' ? 22 : kind === 'lbl' || kind === 'rule' ? 120 : 56;
        timers.push(setTimeout(step, at));
      }
      timers.push(setTimeout(step, 180));
    }

    function select(i, autoplay) {
      active = i;
      tabs.forEach(function (t, j) {
        t.setAttribute('aria-selected', j === i ? 'true' : 'false');
        t.tabIndex = j === i ? 0 : -1;
      });
      body.setAttribute('aria-labelledby', tabs[i].id);
      body.scrollTop = 0;
      body.scrollLeft = 0;
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
