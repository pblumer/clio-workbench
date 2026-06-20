Test Studio Demo – Order Lifecycle
=====================================

This example gives a complete, self-contained demonstration of the Test
Studio data model. It contains:

- **draft-order-lifecycle.json** — a model definition for an order entity.
- **suite-order-lifecycle-tests.json** — a scenario suite with five test cases.

Importing
---------

Copy the draft into your Workbench `DataDir`:

  cp draft-order-lifecycle.json  <DataDir>/

Copy the suite into the scenario store (a subdirectory so it is not mistaken
for a draft):

  cp suite-order-lifecycle-tests.json  <DataDir>/scenarios/order-lifecycle-tests.json

Restart or refresh the Workbench to see them in the Studio.

Draft overview
--------------

Nodes
  cart (start) → placed → paid → shipped → completed (end)
  cancelled (end)

Transitions (event-type edges)
  order-placed      : cart       → placed
  order-paid        : placed     → paid
  order-shipped     : paid       → shipped
  order-delivered   : shipped    → completed
  order-cancelled   : cart       → cancelled
  order-cancelled   : placed     → cancelled

Event-type fields
  order-placed       — customerEmail (string, required, format email)
                       itemsCount    (integer, required)
  order-paid         — amount (number, required)
                       paidAt (datetime, required)
  order-shipped      — trackingId (string, required, format uuid)
                       carrier    (string, required, enum DHL/UPS/FedEx)
  order-delivered    — deliveredAt (datetime, required)
  order-cancelled    — reason (string, required)

Suite overview
--------------

"happy-path-standard" — valid walk ending in *completed*, with all payloads.
"cancel-before-payment" — valid cancellation from *placed*, ending in *cancelled*.
"invalid-sequence-ship-without-pay" — rejected because *order-shipped* has no
  valid transition from *placed* without *order-paid*.
"payload-missing-required-field" — rejected because the payload omits the
  required `customerEmail`.
"payload-type-mismatch" — rejected because `itemsCount` is a string instead of
  an integer.

The suite records `draftRev: c6ac54f6d18d`, so drift detection will fire if the
model is edited without updating the tests.
