# ADR 0010: one diamond engine with explicitly gated financial policies

Status: owner-authorized cohesive financial batch; independent review pending.

Use the concrete ownership, accounting, policy and provider boundaries in ../architecture/FINANCIAL_ENGINE.md. One spendable wallet receives purchased/earned/transferred/commission/reward/adjustment diamonds; source provenance remains in immutable operations rather than separate spendable pots. Exact int64 balanced postings, explicit issuance contra-account, protected SQL posting ownership and atomic actor/kind idempotency preserve conservation and concurrency safety. Existing Access global locking remains unchanged.

Policies require explicit commercial settings; no fee/rate/VAT/payout defaults are silently activated. Gifts support explicit inclusive-diamond tax allocation only, with deterministic policy-selected largest remainder. Withdrawals reserve/review/release diamonds without any fiat provider settlement. Agency activity uses separate configurable audio/video/shared scopes and trusted deduplicated intervals; unavailable runtime collection fails closed. No new dependency, service, frontend, LiveKit, infrastructure or production rollout is introduced. Tests and remaining production gates are consolidated in FINANCIAL_VERIFICATION.md.

The agency activity period proposal is superseded by ADR 0011: current business calendar month through check time. Other policy and financial boundaries above remain unchanged.
