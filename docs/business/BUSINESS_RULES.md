# Approved identity rules — Step 8

Source: owner Step 8 request; no legacy authentication behavior is an implementation requirement. Concrete contracts/defaults are in ../architecture/IDENTITY_AUTH.md.

- IDN-001: only nonce-bound, server-verified Google login creates an ordinary account. Provider google + verified sub are authoritative. Returning login never overwrites ANY profile/contact field; no email/phone linking, phone signup, email/password login, OTP or public password signup.
- IDN-002: username/phone aliases are unique and set only with authenticated, fresh Google identifiers proof. Canonical phones are UNVERIFIED aliases, not ownership or recovery evidence. Conflicts never transfer aliases.
- IDN-003: initial password and recovery require fresh single-use proof from the linked Google subject; changes require that proof or current password plus live session. All sessions are atomically revoked after password mutation. Argon2id and durable pre-work budgets are mandatory.
- IDN-004: opaque hashed access/refresh secrets; single-use refresh rotation; reuse revokes its family. Clients serialize refresh and reauthenticate after ambiguous failure. Session/account/installation state is PostgreSQL-authoritative on every protected request. Redis absence cannot weaken security.
- IDN-005: installation IDs bind requests/sessions and restrictions, but are resettable, not physical-device attestation. Restricted accounts retain their Google binding and cannot sign up again through the same subject; restricted installations cannot create another account. Reset-resistant physical bans require a separately approved attestation design.

Defaults (not new business roles): 15-minute access, 30-day absolute family, 5 live families/account, 5-minute nonce/proof, exact normalization, rate and hashing budgets documented in IDENTITY_AUTH.md. No staff grants/business classification is inferred from Google/client input. Flexible permissions, agency/host workflows, broader profiles and push-token delivery remain deferred.
