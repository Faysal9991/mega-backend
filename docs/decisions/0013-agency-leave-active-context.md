# ADR 0013: clear active agency context, retain history on leave

Status: owner-confirmed clarification, 2026-11-25; implementation remains independent review pending.

Asked whether the current-month agency-host withdrawal activity rule continues after a host leaves, the owner answered: “agency cara r por tar data clear hy jabe but history takbe” (after leaving the agency, current data clears but history remains).

Leaving closes the active agency-host association and clears its current agency/classification/eligibility context. Preserve the closed membership association, verified activity, prior policy/withdrawal decisions and financial history. This is logical effective-date closure, not deletion of the historical row or an instruction to erase Wallet balances/ledger. The existing Wallet's current-association selection remains in force: when there is no active association, it does not display or enforce an active agency-host activity context. Other current withdrawal/funds/review/configuration rules still apply.

The implemented leave transition invalidates open qualifying evidence and queues provider corrections while retaining completed/history records. Reauthentication/reconnect and a new approved association are required to establish later qualifying activity.

The owner additionally confirmed: **“নতুন agency-তে শূন্য থেকে শুরু”** — joining a new agency starts qualifying day/minute calculation from zero. An append-only activity-context journal records the current agency and the new effective floor; leaving appends a NULL agency context. The trusted source reads only the latest context and compatible intervals starting within it. All older association/activity/context records remain available as history. Classification transitions within the same agency retain that agency context; a join after a leave or a switch to another agency starts a fresh context, including within the same calendar month. The applicable current-month policy/timezone and all other Wallet rules remain unchanged.

Association writes and context changes share the same authorized transaction and write owner. A prior eligibility snapshot cannot accept a changed association; historical withdrawal decisions are not recalculated. No wallet balance, ledger or lifetime financial counter is cleared. Migration 8 initializes compatible current association floors; no trusted LiveKit collector existed at baseline 645c75c, so no legacy verified live history is imported.

Historical classification-transition proration and independent-host/ownership-transfer policies remain separate decisions. The leave/rejoin clearing rule above is owner-confirmed and is not a remaining ambiguity.
