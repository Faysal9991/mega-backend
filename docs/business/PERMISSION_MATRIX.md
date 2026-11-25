# Step 8 identity permission matrix

| Action | Required authority | Restrictions / effect |
| --- | --- | --- |
| Challenge | Valid installation + durable abuse budget | Restricted installations denied |
| Google login/signup | Valid signed audience/issuer/expiry, nonce, fresh iat and unused challenge | Only creation path; ordinary privileges; subject binding, no contact matching |
| Password login | Existing alias + matching Argon2id credential | Generic failures; never creates; account/installation active |
| Protected current identity | Live access token + bound installation | PostgreSQL checked on every request; database failure denies |
| Identifiers | Live session + bound single-use identifiers Google proof | No ownership claim for phone; conflicts atomic |
| Password setup/recovery | Linked-subject fresh password Google proof | Account/installation active; all sessions revoked atomically |
| Password change | Same proof OR live session + current password | Credential snapshot rechecked under account lock; all sessions revoked |
| Refresh | Unused refresh token + bound active installation/account/family | Atomic rotation; reuse commits family revocation |
| Logout / all | Live bound session | Current family / all account families revoked |
| Restriction management | Internal repository interface only | No HTTP route; scoped staff permissions/workflows deferred |

Account restriction status, future staff grants and business classifications are distinct. No universal user_type. No protected request relies on Redis or a client actor/device ID as authentication.

## Step 9 approved scope

| Action | Required authority | Boundary |
| --- | --- | --- |
| Own profile read/PATCH | Live bound session | Own account from server identity; display_name/bio only |
| Public profile by ID | Live bound session | Four public fields only |
| Own effective permissions | Live bound session | Current PostgreSQL state; no Redis |
| Admin directory/basic user | users.read | Bounded ID cursor / status + public fields |
| Private contact/aliases | users.private.read AND users.read | Separate projection; no secrets/session/device/staff data |
| Admin profile PATCH | users.profile.update | Same field allowlist + atomic audit/reason |
| Restrict/lift | users.restrict | Protected owner forbidden; Identity owns write/session revocation; atomic audit |
| List/create/edit/delete staff roles | Protected owner | Four fixed permissions; role name irrelevant; bounded lists |
| Read/replace assignments | Protected owner | Self and protected-owner assignment changes denied; atomic audit |
| Bootstrap | Explicit operator + dedicated migration credential | Selected active existing Google-linked account; serialized single winner; no HTTP |

Earlier Step 8 restriction/admin deferrals describe that phase; this explicit Step 9 authorization supersedes only the rows above. Agency audio/video host classifications, premium entitlements and other business permissions remain separate/deferred. No universal user_type.


## Cohesive financial batch (explicit owner authorization)

| Action | Required authority | Additional boundary |
| --- | --- | --- |
| Own wallet/history/detail, transfer/gift, eligibility/request/history | Live bound active session | Server actor; own financial projection; explicit valid policy; no Redis authority |
| Cancel own pending withdrawal | Live bound owner session | Idempotent full reservation release; approved/terminal requests cannot be client-cancelled |
| Read policy publication history | economy.policies.read | Bounded immutable versions |
| Publish economic/activity policy | economy.policies.write + protected owner | Explicit required settings, previous version CAS, effective timestamp and atomic old/new-ID audit |
| Inspect operation | wallet.operations.read | Full postings only through this scoped admin projection |
| Review pending/approved withdrawal | withdrawals.review | Approval retains held diamonds; reject releases once; cannot settle or initiate payout |
| Issue/retire via adjustment | wallet.adjust + protected owner | Single Engine; explicit source reference/direction/amount/reason/idempotency and atomic audit |
| Manage minimum host association | agency.hosts.manage + protected owner | Existing distinct active account/agency beneficiary; nonoverlapping effective dates, history and audit |

The fixed catalog is now the four Step 9 user permissions plus these six financial permissions. Editable role names never grant authority; only protected owner manages grants. Financial critical writes still require protected owner even if the permission name appears in a staff role. Account status, host classification, premium entitlements and wallet ownership remain separate. No new generic staff or universal user_type privilege is introduced.
