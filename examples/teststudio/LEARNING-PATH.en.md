Learning Path — Test Studio
===========================

> 🌐 **Language:** English · [Deutsch](LEARNING-PATH.md)

A guided, hands-on introduction to the Clio Workbench **Test Studio**. You work
your way up from the smallest building block (checking one payload) to the full
loop (pushing to an instance and comparing the design against reality).

Throughout, the path uses the bundled demo model **Order Lifecycle**
(`draft-order-lifecycle.json`) and the demo suite
(`suite-order-lifecycle-tests.json`) from this directory. Each lesson builds on
the previous one and follows the four kinds of checks and the stages from
[`../../docs/TESTSTUDIO.md`](../../docs/TESTSTUDIO.md) (the concept paper is in
German; section numbers match below).

> **Conceptual background:** The *why* behind each lesson lives in the idea paper
> [`TESTSTUDIO.md`](../../docs/TESTSTUDIO.md). This path is the *how* — the
> keyboard to go with it. Every lesson points at the matching section.

> **Ready-to-run practice material:** The [`exercises/`](exercises/) folder holds
> finished, importable starter artifacts for the individual lessons — payload
> sets for the schema tests and a runnable practice suite with the solutions to
> the lesson exercises. Details in [`exercises/README.md`](exercises/README.md).

Overview
--------

| # | Lesson | Kind of check / stage | Instance needed? | Time |
|---|--------|-----------------------|------------------|------|
| 0 | Set up & import                  | —                       | no            | 5 min |
| 1 | Schema test: does the payload fit?| Schema (§3.1, T0)      | no            | 10 min |
| 2 | Sequence test: is the path valid?| Transition (§3.2, T0/T1)| no            | 15 min |
| 3 | Scenarios & suites               | Scenario (§3.3, T1)     | no            | 15 min |
| 4 | Generator & report               | Generator (§3.4, T2)    | no            | 15 min |
| 5 | Generate producer code           | Producer (§9, T3)       | no            | 10 min |
| 6 | Push & round-trip                | Instance (§7, T4)       | **yes** (throwaway)| 20 min |
| 7 | Counter-check: design vs. reality| Consolidation (T5)      | **yes** (throwaway)| 15 min |

Lessons 0–5 run **fully offline** on the draft — no Clio instance required. Only
6 and 7 need a server, and deliberately only a **throwaway instance** (see
Lesson 6).

Prerequisites
-------------

- Go installed (for `go run ./cmd/clio-workbench`), or a built binary.
- This repository checked out.
- For Lessons 6/7, additionally a **throwaway Clio** (its own container, its own
  data store) — *never* a production instance, see Lesson 6.

---

Lesson 0 — Set up & import
--------------------------

**Goal:** Start the Workbench and get the demo model plus suite into the Studio.

1. Pick a data directory and copy the artifacts into it. The draft goes into the
   data directory itself, the suite into the `scenarios/` subdirectory (so it
   isn't mistaken for a draft):

   ```sh
   export WORKBENCH_DATA=./workbench-data
   mkdir -p "$WORKBENCH_DATA/scenarios"
   cp examples/teststudio/draft-order-lifecycle.json        "$WORKBENCH_DATA/"
   cp examples/teststudio/suite-order-lifecycle-tests.json  "$WORKBENCH_DATA/scenarios/order-lifecycle-tests.json"
   ```

2. Start the Workbench (offline is enough for the first lessons):

   ```sh
   go run ./cmd/clio-workbench
   # open http://localhost:8080
   ```

3. In the **activity bar** on the left, open the **"Teststudio"** activity.

**Expected result:** The Studio opens and **Order Lifecycle** appears in the
model picker. The scenario sidebar shows the suite **"Order Lifecycle –
Demo-Testsuite"**.

> **Tip:** You can also load the draft and suite via *import from URL* if your
> Workbench offers it — the result is the same.

**Go deeper:** The model itself (nodes, edges, event-type fields) is described
compactly in this directory's [`README.md`](README.md). Take a look before
moving on — it is the yardstick for everything that follows.

---

Lesson 1 — Schema test: does the payload fit?
---------------------------------------------

**Kind of check:** Schema tests (`TESTSTUDIO.md` §3.1) · **Stage:** T0

**Goal:** Understand how a single `data` payload is checked against the schema of
**one** event type — the smallest, most local check.

**What you learn:** Required fields, types and formats. Why a red test *shows you
why* it is red, field by field.

> The payloads below are also in
> [`exercises/lesson1-payloads.json`](exercises/lesson1-payloads.json), labelled
> valid/invalid — copy them straight into the schema-test input.

**Steps:**

1. Open the **Schema Test** editor tab. Pick model **Order Lifecycle** and event
   type **`order-placed`**. You see the schema preview: `customerEmail` (string,
   required, format email) and `itemsCount` (integer, required).

2. **Green case** — enter a valid payload and check it:

   ```json
   { "customerEmail": "alice@example.com", "itemsCount": 3 }
   ```

   → **accept**, all fields green.

3. **Missing required field** — remove `customerEmail`:

   ```json
   { "itemsCount": 3 }
   ```

   → **reject**. The error names the field `customerEmail` and the rule
   *required*.

4. **Type error** — `itemsCount` as a string:

   ```json
   { "customerEmail": "alice@example.com", "itemsCount": "three" }
   ```

   → **reject**, field `itemsCount`, rule *type* (integer expected).

5. **Format error** — broken email:

   ```json
   { "customerEmail": "not-an-email", "itemsCount": 3 }
   ```

   → **reject**, field `customerEmail`, rule *format* (email).

**Exercise:** Switch to `order-shipped` and provoke an **enum** error on the
`carrier` field (allowed: `DHL`, `UPS`, `FedEx`). Then build a **valid**
`order-shipped` payload with a correct UUID in `trackingId`.

**Self-check:** For every event type you can name *which* field and *which* rule
triggers an error — not just "invalid".

---

Lesson 2 — Sequence test: is the path valid?
--------------------------------------------

**Kind of check:** Transition & sequence tests (`TESTSTUDIO.md` §3.2) ·
**Stage:** T0/T1

**Goal:** No longer a single payload, but a **sequence** of event types checked
against the lifecycle graph.

**What you learn:** A sequence must begin at a **start state**, every step must
be an **existing edge**, and the result is the **path taken** — not just
green/red.

Reminder of the graph (from the model):

```
cart ──order-placed──▶ placed ──order-paid──▶ paid ──order-shipped──▶ shipped ──order-delivered──▶ completed
  │                       │
  └──order-cancelled──────┴──order-cancelled──▶ cancelled
```

**Steps:** In the **Scenarios** tab, create a new empty scenario (or use the
sequence field) and enter the event-type sequence as a `→` or comma-separated
list.

1. **Valid full path:**
   `order-placed → order-paid → order-shipped → order-delivered`
   → **accept**, end state **completed**. The path lights up
   `cart→placed→paid→shipped→completed`.

2. **Valid cancellation:**
   `order-placed → order-cancelled`
   → **accept**, end state **cancelled**.

3. **Invalid order** (ship without payment):
   `order-placed → order-shipped`
   → **reject**. The first deviation is step 2: there is **no edge**
   `order-shipped` from `placed`. The path visibly ends at `placed`.

4. **Wrong start:** `order-paid → …`
   → **reject**: `order-paid` is not a transition out of a start state.

**Exercise:** Find the cancellation variant the model forbids: can you still
cancel **after** shipping? Try
`order-placed → order-paid → order-shipped → order-cancelled` and explain the
rejection (there is no edge `order-cancelled` out of `shipped`).

**Self-check:** For every rejection you can say *at which step* and *out of which
state* the sequence breaks out.

---

Lesson 3 — Scenarios & suites
-----------------------------

**Kind of check:** Scenario tests (`TESTSTUDIO.md` §3.3) · **Stage:** T1

**Goal:** Turn loose sequences into **named, versionable** cases with an
**expectation** — the Studio's actual artifact, the one you put under git.

**What you learn:** The structure of a suite (`Case`/`Step`/`Expectation`), the
difference between the expectations `accept` / `reject` / `endState`, and **drift
detection** via `draftRev`.

**Steps:**

1. In the sidebar, select the suite **"Order Lifecycle – Demo-Testsuite"** and
   click **"Alle prüfen" (Check all)**. You see five cases:

   | Case | Expectation | Why |
   |------|-------------|-----|
   | `happy-path-standard`               | accept, end state `completed` | valid full path with all payloads |
   | `cancel-before-payment`             | accept, end state `cancelled` | valid cancellation from `placed` |
   | `invalid-sequence-ship-without-pay` | reject | `order-shipped` has no edge from `placed` |
   | `payload-missing-required-field`    | reject | `customerEmail` is missing |
   | `payload-type-mismatch`             | reject | `itemsCount` is a string, not an integer |

   → All five should **pass** (the "reject" cases pass *because* they are
   correctly rejected — the expectation holds).

2. Look at the source [`suite-order-lifecycle-tests.json`](suite-order-lifecycle-tests.json).
   Note: `draftId` references the model, `draftRev` (`c6ac54f6d18d`) records
   *which state* the suite was written against. The suite does **not copy** the
   model — it references it (leading principle §5).

3. **Experience drift (optional):** Change something test-relevant in the model
   (e.g. rename the event type `order-paid`) and save. The Studio shows a **drift
   warning**: the suite was written against a different state and should be
   re-checked. Undo the change afterwards.

**Exercise:** Add a case of your own to the suite that pins down exactly the
forbidden cancel-after-ship from Lesson 2 as a **reject**:

```json
{
  "id": "cancel-after-ship-forbidden",
  "name": "Storno nach Versand ist verboten",
  "seed": 47,
  "steps": [
    { "type": "order-placed",  "subject": "/orders/ORD-2025-006",
      "data": { "customerEmail": "erin@example.com", "itemsCount": 1 } },
    { "type": "order-paid",     "data": { "amount": 10.0, "paidAt": "2025-06-20T10:00:00Z" } },
    { "type": "order-shipped",  "data": { "trackingId": "770e8400-e29b-41d4-a716-446655440002", "carrier": "FedEx" } },
    { "type": "order-cancelled","data": { "reason": "too late" } }
  ],
  "expect": { "outcome": "reject" }
}
```

Run the suite again — your new case should **pass** (correctly rejected). The
finished version, alongside two more practice cases, is in
[`exercises/suite-exercises.json`](exercises/suite-exercises.json).

**Self-check:** You understand why a "reject" scenario is a *passing* test, and
what `draftRev` is for.

---

Lesson 4 — Generator & report
-----------------------------

**Kind of check:** Generator / simulator (`TESTSTUDIO.md` §3.4, §4) ·
**Stage:** T2

**Goal:** Instead of writing cases by hand, **walk the graph yourself** and
generate schema-conformant streams — reproducibly, via a **seed**.

**What you learn:** Determinism (same seed ⇒ same stream), property-based samples
("N random paths, all green"), **edge coverage**, and the **report** as an
artifact (Markdown/JSON).

**Steps:**

1. Open the **Generator** tab. Set a **seed** (e.g. `42`) and a **sample count**
   (e.g. `50`). Click **"Generieren & prüfen" (Generate & check)**.

2. Read the result:
   - **N/N valid** — every generated stream passes the engine from Lessons 1+2.
   - **Edge coverage** — which transitions of the graph occurred at least once.
     (Rare edges like `order-cancelled` may need more samples or a different
     seed.)
   - **Negative checks** — deliberately **mutated** streams (drop required field,
     corrupt type, insert a non-edge, swap the order) must be **rejected**. This
     proves the check *also rejects*, it doesn't just wave things through.

3. **Verify determinism:** Run the same seed twice — result and report are
   identical bit for bit. Change the seed — the stream changes.

4. **Download the report:** Export the report as **Markdown** (for repo/wiki) and
   as **JSON** (for further processing). The header records model + rev, seed and
   timestamp — making the run a **regression record**.

**Exercise:** Find a seed (or raise the sample count) where edge coverage reaches
**100 %** — i.e. both `order-cancelled` edges are walked at least once.

**Self-check:** You can explain why a seed is recorded in the report and what the
negative checks prove.

---

Lesson 5 — Generate producer code
---------------------------------

**Kind of check:** Producer code (`TESTSTUDIO.md` §9) · **Stage:** T3

**Goal:** Generate **example producer code** from the same model — the code that
will later actually append the events to Clio.

**What you learn:** Producer code is generated *from the design* (like schemas
and docs) and is **scaffolding, not an SDK** — a starting point to copy, not a
maintained client library.

**Steps:**

1. Open the **Producer-Code** tab. Pick model **Order Lifecycle**.

2. Switch between languages — v1 ships **Go**, **TypeScript (fetch)**, **Python**
   and **curl**. Per event type you see:
   - a **typed payload carrier** from the `data` schema,
   - a **CloudEvents POST** to Clio's public append endpoint (`/api/v1/events`),
   - a **subject helper** from the subject style `/orders/{id}`,
   - **auth** via a bearer token from the **environment** (`CLIO_*`), never
     hard-coded.

3. **Copy / download** the snippet in your favourite language.

**Exercise:** Compare the Go and the curl variant for `order-placed`. Both fill
the same CloudEvents envelope (`type`, `subject`, `data`) — the curl form doubles
as the smallest smoke test against a running Clio.

**Self-check:** You can name where the token in the generated code comes from
(the environment, not inline) and why the code is "scaffolding, not an SDK".

---

Lesson 6 — Push & round-trip *(needs a throwaway instance)*
-----------------------------------------------------------

**Kind of check:** Instance integration (`TESTSTUDIO.md` §7) · **Stage:** T4

> ⚠️ **The append-only trap — please read first.** Clio is **append-only** and
> schemas are **immutable**. Written test data **cannot be deleted**, and a
> registered test schema **stays**. So push **only against a throwaway instance**
> (its own container, its own data store) — *never* against a production Clio.
> The Studio enforces this with a hard gate (see below); do not ignore the
> warning.

**Goal:** Check the full path — **push** generated streams and **read them back**
via read queries, including Clio's *own* server-side accept/reject (the one layer
that fundamentally cannot be reproduced locally).

**Steps:**

1. Start the Workbench against your throwaway instance:

   ```sh
   CLIO_URL=http://localhost:3000 CLIO_API_TOKEN=<token> \
     WORKBENCH_DATA=./workbench-data go run ./cmd/clio-workbench
   ```

   The connection pill in the header should show **UPLINK** (green).

2. Open the **Push** tab. Note the **gate**: pushing is locked until you
   **explicitly confirm the active instance as throwaway**. Switching servers
   *disarms* the gate again automatically.

3. Confirm the throwaway instance and push a generated stream. The Studio
   automatically prefixes **all** subjects under `/_test/<run-id>/…`, so the test
   run occupies a clearly isolated namespace.

4. **Round-trip:** The Studio reads the pushed events back, groups them per
   subject, and re-checks each sequence with the engine from Lesson 2.

**Expected result:** Push accepted, round-trip green. Without throwaway
confirmation the Studio **refuses** the push — that is intentional.

**Self-check:** You can explain why push tests may only run against throwaway
instances and what the `/_test/<run-id>/` prefix is for.

---

Lesson 7 — Counter-check: design vs. reality *(needs a throwaway instance)*
---------------------------------------------------------------------------

**Kind of check:** Consolidation / counter-check (`TESTSTUDIO.md` §6, T5) ·
**Stage:** T5

**Goal:** Close the loop — hold the **design (Soll)** against the **real events
(Ist)** of an instance, using the *same* engine that checked the design in
Lessons 1–6.

**What you learn:** The design side and the reality side share
`internal/validate`. The counter-check answers three questions: are there
**deviating subjects** (sequences the design does not allow)? Is the design
**dead** (edges that never occur in reality)? Are there **unknown types** (events
the design doesn't know)?

**Steps:**

1. Make sure your throwaway instance contains events under the model's subject
   prefix (`/orders/…` or the `/_test/…` prefix from Lesson 6) — e.g. from the
   previous lesson's push.

2. Open the **Gegenprobe (counter-check)** (Soll/Ist) and run it against the
   active instance. It reads the real events scoped to the prefix, groups them
   per subject, and checks each sequence with `validate.CheckSequence`.

3. Read the three findings: deviating subjects, dead design parts, unknown types.

**Exercise:** In Lesson 6, deliberately push a **mutated** (red) stream and watch
the counter-check flag the affected subject as a deviation from the design.

**Self-check:** You can explain why the design tests (scenarios/generator) and
the reality check (counter-check) use the same engine — and what that means for
the reliability of both.

---

Done — what you can do now
--------------------------

- Check a **payload** against a schema field by field (§3.1).
- Hold a **sequence** against the lifecycle graph and locate deviations (§3.2).
- Write **scenarios** and **suites** as versionable artifacts and detect drift
  (§3.3, §5).
- Use the **generator** to produce reproducible streams, measure edge coverage
  and file a **report** as a regression record (§4, §8).
- Generate **producer code** from the model and place it correctly (§9).
- Push against a **throwaway instance**, run a **round-trip** and avoid the
  **append-only trap** (§7).
- Hold the design against real events via the **counter-check** — design and
  reality on the same engine (§6, T5).

Further reading
---------------

- [`TESTSTUDIO.md`](../../docs/TESTSTUDIO.md) — architecture & idea paper (the
  *why*).
- [`TESTSTUDIO-IMPLEMENTATION.md`](../../docs/TESTSTUDIO-IMPLEMENTATION.md) — the
  work packages (WP-1…WP-9) and what each one delivers.
- [`README.md`](README.md) — compact description of the demo model and the suite.
- [`FRAMEWORK.md`](../../docs/FRAMEWORK.md) — how the Studio fits into the
  VS-Code shell.
</content>
