# ADR-0006: Support Only Through a WhatsApp Number

- **Status**: Accepted
- **Date**: 2026-09-29
- **Related Commit SHA**: none (decision only, no implementation yet)
- **Related Audit Finding**: n/a

## Context

Student support needs a channel, and every channel has an operating cost.
An in-app system (tickets, chat, complaints) means building queues,
assignment, notifications, and staffing them inside the platform. A single
external number keeps the cost at one answered phone. As of this ADR, no
code exists for support in the app or backend.

## Decision

1. Student support happens only through a WhatsApp number.
2. There are no in-app tickets, no in-app chat, and no complaint system.
3. The number comes from configuration, never hardcoded in the app.

## Consequences

### Positive

- Near-zero build and operating cost for the support channel.
- No moderation, queue, or assignment machinery to build or staff.

### Negative and Tradeoffs

- Response-time expectations are set socially (profile/status wording),
  not enforced by any system: there are no SLAs, no escalation, and no
  audit trail of who was answered when.
- Support history lives outside the platform and is not queryable for
  product insight.
- The number is shown wherever a stuck student looks (settings/support
  surface and anywhere else help is offered); every new surface must
  remember to include it, since nothing central renders it automatically.

## Alternatives Considered

- **In-app tickets/chat/complaints**: rejected for build and staffing cost.
