# Marque

**Reviewed, approved, time-bounded access to production data.**

Docs: **https://sixfathoms.github.io/marque/**

Someone needs to run a statement against production. Today that is a Slack thread, a screen share,
and a psql prompt with the whole schema behind it. Marque turns it into a *marque* — a commission to
act, in the old sense: scoped, time-bounded, revocable, and carried as a signed artefact.

You submit the statement. It is parsed for what it touches and rehearsed in a transaction that is
always rolled back, so the affected row count is measured rather than guessed. A human with
authority over that database signs a grant naming that exact statement, that role, and a validity
window. Then it runs — once, inside the window, as that role — and everything lands in an
append-only journal naming who asked, who approved, what they were shown, and what changed.

> [!NOTE]
> **Status: design.** The decision records are published; the implementation has not started.
> Nothing here runs in production anywhere yet.

## Why it is shaped this way

Four properties the design gives other things up to keep:

- **A control-plane compromise grants no database access.** The control plane holds no target
  credential, and a marque needs the approver's own key as well as the server's — so an attacker who
  owns the server can ask for things, and nothing more.
- **An issued marque works while the control plane is down.** The Pilot verifies it by computation,
  offline. The tool stays usable during exactly the incidents it exists for.
- **Nothing is silently narrowed.** A statement that would touch rows outside your delegated scope
  aborts and tells you how many. A partially-applied change nobody reviewed is worse than a refusal.
- **No model approves anything.** The analyser writes prose beside deterministic facts and holds no
  authority a configuration could widen.

## Repository layout

```
docs/
  edrs/         Engineering decision records — the source of truth for the design
  content/      Documentation pages
  changelog/    One file per changelog entry
website/        The static site generator that renders all three
```

## Building the docs

```sh
cd website
pnpm install
pnpm run build     # validates every record and changelog entry; outputs website/dist
pnpm run serve     # http://localhost:8080/marque/
```

The build is the validator: a decision record with a missing summary, a duplicate id, a changelog
entry with an unknown tag or a malformed filename all fail it.

## Reading order

1. [Introduction](https://sixfathoms.github.io/marque/overview/introduction/) — the problem and the flow.
2. [Architecture](https://sixfathoms.github.io/marque/overview/architecture/) — components, object model, trust boundaries.
3. [Scope](https://sixfathoms.github.io/marque/overview/scope/) — what is in, what is out, prior art.
4. [Decision records](https://sixfathoms.github.io/marque/edrs/) — start at EDR-0001, then 0004, 0005 and 0007.

Design references cite [Field Notes](https://zrz.io/zfn/) as `ZFN-N` — standing engineering
positions this system applies.

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md). Security reports: [SECURITY.md](./SECURITY.md).

## Licence

Apache-2.0. See [LICENSE](./LICENSE).

---

Built by [Six Fathoms](https://sixfathoms.dev/).
