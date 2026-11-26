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

## Live / agency batch permissions

| Permission / authority | Supported scope |
| --- | --- |
| agencies.manage | Inspect agency directory, approve/reject/restrict/update agency profiles, delegate managers |
| agency.hosts.manage (existing) | Inspect host association history, approve/reject host applications globally, change classification; does not grant financial writes |
| live.rooms.moderate | Admin room list/detail/close, room moderation and explicit provider reconciliation; protected account rules remain in Access |
| live.activity.read | Bounded account/SID inspection with association/classification and verified/open/invalidated timestamps |
| live.policies.manage | Read/publish immutable live capacity/seat/host-type compatibility policy with predecessor CAS and audit |
| Agency owner / delegated manager | Own agency application list/decisions; only owner or agency staff delegates managers |
| Room host | Own room close/roles/seats/moderation; never client-asserted ownership |
| Co-host / room moderator | Audience moderation and seats; cannot appoint roles, change host or moderate peer co-hosts/moderators |
| Active authenticated audience | Join/leave/token; microphone only after eligible audio-seat invitation/acceptance |

All staff grants use Step 9 authoritative persisted assignment/revocation. Privileged and ordinary domain mutations persist audit in the same transaction as state/idempotency/provider intent. Financial activity thresholds still require existing Wallet policy permissions/protected-owner boundary; the new policy permission does not bypass that authority. No admin UI is implemented.

Agency profile administration does not itself confer global host-application approval: that requires agency.hosts.manage or the target agency's intrinsic owner/delegated manager authority. Staff self-delegation, host self-approval and self-classification are denied. Account restriction atomically revokes sessions, ends local room membership/seats and records an immutable activity ceiling plus pending provider-removal intent. Unrestriction cannot revive prior sessions/activity; physical media convergence remains explicit provider work.

## Delivery batch at c749cec (ADR 0014)

| Action | Authority |
| --- | --- |
| List current active catalog, send to eligible verified publisher, own gift history | Current authenticated account; room/recipient authority additionally rechecked by Live under Wallet transaction |
| Catalog create/version/deactivate/order/category and explicit recipient mode | gifts.manage; reason, actor/operation idempotency, previous-version CAS and audit |
| Own avatar intent/completion/attachment | Current session/account, own asset/target; approved state and expected previous asset ID |
| Agency/gift upload purposes | media.manage; agency logo target additionally agencies.manage; ordinary agency ownership alone is insufficient |
| Agency logo attachment | media.manage + agencies.manage, matching asset target; audit |
| Device registration/removal and preferences/history | Own current authenticated installation/session/account; no client actor/installation override |
| Fixed-template announcement (max100 explicit accounts) | notifications.announce + atomic audit/idempotency |
| Safe delivery inspection / failed-intent replay | deliveries.read / deliveries.retry respectively; replay additionally reason/audit/idempotency, never money posting |

Protected owner inherits the fixed 19-permission catalog through existing authorization; this is not independent review acceptance. Business roles and premium/room roles never grant these scopes. Worker credentials perform only trusted persisted intent processing; network calls hold no authority transaction. Tokens, credentials and raw provider errors have no response projection.

## Consolidated dynamic capabilities and packages

Access.Resolve merges current owner/staff/room/package/direct authority once; applicable denies and mandatory account/install/live/resource restrictions dominate ordinary grants. Owner protection cannot be removed through editable roles/overrides; mandatory participation restrictions still apply. Existing /identity/me/permissions keeps its staff projection (implicit public entry/purchase omitted for nonowners); /identity/me/powers exposes complete current capabilities, optionally scoped to a room. This is one engine with different projections.

Global Moderator is an explicitly assigned staff capability set, created through the existing owner-only role system/preset API; names do not authorize. Defaults: entry/private entry/hidden entry, kick/mute/seat removal, self-call/seat join/leave and global-live-ban administration. Existing room moderator is still a separate scoped membership. Protected owner targets/ownership changes remain guarded. Hidden membership cannot publish and is excluded from ordinary lists; operational administrators retain identity/audit visibility. Private-entry provenance and all privileged self-call grants are rechecked and recovered after revocation.

capabilities.manage publishes immutable room role policy; direct overrides remain protected-owner administered. packages.manage controls versioned catalog/benefits/history/subscribers; packages.entitlements.manage controls audited direct grants/revocations; packages.purchases.read inspects bounded purchases. packages.purchase is an ordinary authenticated default, explicitly deniable before a new debit; committed receipt retries remain retrievable. Packages may select only room.enter, room.seat.self_join, room.seat.self_leave, benefit.badge and benefit.entrance. Hidden/private entry, staff delegation, moderation, global bans and financial/owner powers are never purchasable. See ADR0015 and capabilities.openapi.json.

Global live restriction removes every room.* capability, legacy live.rooms.moderate and live.participant.global_ban, including for protected owners; current persisted restriction is checked before mutation/replay. Normal APIs cannot restrict protected owners. Owner approved Room Admin defaults: kick/mute/seat-remove/self-leave only; close, role assignment and room bans require explicit audited policy/global staff assignment. Existing Host/co-host/room-moderator defaults are retained; global Moderator preset never implicitly grants room.close, room.admin.assign or room.participant.ban. Migration12 corrects only the original seed head, preserving custom policy.
