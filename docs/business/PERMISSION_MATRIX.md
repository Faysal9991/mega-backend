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
