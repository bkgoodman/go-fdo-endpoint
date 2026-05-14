# Inventory Status Bug Analysis: Why the GUI Shows PRE_ONBOARDED After Successful TO2

## Executive Summary

After a successful FDO TO1/TO2 onboarding flow, the GUI shows the device status
as `PRE_ONBOARDED` instead of `ONBOARDING_COMPLETE`. This is a two-part bug in
the **hzp-inventory** service affecting our device -- a Dell-manufactured,
OpenSku, non-iDRAC-FDO compute node.

**Part 1 (stale ece_inventory):** Our device takes a "hybrid path" through the
event handlers: the NE handler creates the initial `ece_inventory` row
(PRE_ONBOARDED), but subsequent FDO events are routed to the compute handler
which only updates `endpoint.Extra["provisioned_state"]` and never updates
`ece_inventory.status`. The ece_inventory row stays stuck at PRE_ONBOARDED.

**Part 2 (status regression):** Even though the `endpoint` table correctly
reaches `onboarding_complete`, this value is **overwritten back to
`pre_onboarded`** whenever `UpsertNEEndpointWithServiceTag` is called. This
function reads the stale `ece_inventory.status` (PRE_ONBOARDED) and writes it
into `endpoint.Extra["provisioned_state"]`. Multiple agent lifecycle events
trigger this function: CID updates, hardware inventory reports, IP address
changes, VM events, heartbeats, and upgrade events.

The combination of these two problems means that the correct status is briefly
visible in the endpoint table after each FDO event, but is then regressed by
the next agent lifecycle event.

---

## 1. Our Device's Identity

Our device has these properties (set during manufacturing and embedded in the
voucher OVEExtra fields):

| Property | Value | Meaning |
|----------|-------|---------|
| `manufacturingType` | `MANUFACTURED_DELL` -> `"Dell"` | Dell factory-manufactured device |
| `isOpenSku` | `true` | "Free pool" compute node, not a dedicated NativeEdge appliance |
| `isIdracFdo` | `false` | Does NOT have iDRAC BMC speaking FDO; uses ECE agent on OS |

These three flags together determine how the inventory service classifies and
routes events for our device.

### 1.1 Endpoint Type Classification

```go
// ne_service.go:508-521
func getEndpointType(manufacturingType string, isOpenSku bool) (commons.Type, error) {
    switch manufacturingType {
    case commons.DellManufacturingType:   // "Dell"
        if isOpenSku {
            return endpoint.TypeCompute, nil    // ← US: Dell + OpenSku = "compute"
        }
        return endpoint.TypeNativeEdge, nil     // Dell + !OpenSku = "nativeedge"
    case commons.BrownfieldManufacturingType:
        return endpoint.TypeBrownfield, nil
    case commons.ThirdPartyManufacturingType:
        return endpoint.TypeThirdParty, nil
    }
    return endpoint.TypeEmpty, fmt.Errorf("invalid manufacturing type: %s", manufacturingType)
}
```

**Result**: Our device is classified as `TypeCompute` with asset type
`free_pool`. This is the key distinction that creates the hybrid path.

---

## 2. The Hybrid Path: How Our Device Is Uniquely Routed

### 2.1 Event Routing Decision Tree

When the inventory service receives an FDO event, it goes through this
decision tree:

```
Receive() [event_handler.go:235]
  │
  └── default case [line 331-337]:
        │
        ├── NE Decoupling feature flag enabled?
        │     ├── YES: Is isOpenSku explicitly false?
        │     │     ├── YES → SKIP (NE handles separately)
        │     │     └── NO (true or absent) → continue
        │     └── NO → continue
        │
        └── eceInventoryHandler() [line 683]:
              │
              └── determineEndpointType() [line 738]:
                    │
                    ├── Endpoint already exists in DB?
                    │     ├── YES → Is endpoint.Type == TypeCompute?
                    │     │     ├── YES → eceInventoryHandlerForCompute()
                    │     │     └── NO  → eceInventoryHandlerForNE()
                    │     │
                    │     └── NO (first event, no endpoint yet):
                    │           ├── IsIdracFdo == true?
                    │           │     └── YES → eceInventoryHandlerForCompute()
                    │           └── IsIdracFdo == false?
                    │                 └── YES → eceInventoryHandlerForNE()     ← US (first event)
```

### 2.2 The Hybrid Path

This is what makes our device unique. Because `isIdracFdo=false`:

**First event (PRE_ONBOARDED):**
- No endpoint exists yet → `determineEndpointType` checks `IsIdracFdo`
- `IsIdracFdo = false` → routes to **NE handler** (`eceInventoryHandlerForNE`)
- NE handler creates `ece_inventory` row (status=PRE_ONBOARDED)
- NE handler calls `UpsertNEEndpoint()` → creates `endpoint` row
- `getEndpointType(Dell, isOpenSku=true)` → endpoint created as **TypeCompute**

**All subsequent events (WAITING_TO_ONBOARD, ONBOARDING, ONBOARDING_COMPLETE):**
- Endpoint now exists with `Type=TypeCompute`
- `determineEndpointType` sees TypeCompute → routes to **compute handler**
- Compute handler calls `UpdateEndpoint()` → updates ONLY `endpoint.Extra["provisioned_state"]`
- Compute handler **never calls `UpdateECEInventory()`**
- `ece_inventory.status` remains stuck at `PRE_ONBOARDED`

**Result:** Two tables are now out of sync:

| Store | Our Value | Updated By |
|-------|-----------|-----------|
| `ece_inventory.status` | `PRE_ONBOARDED` (stale) | Never updated after creation |
| `endpoint.Extra["provisioned_state"]` | `onboarding_complete` (correct) | `UpdateEndpoint()` in compute handler |

### 2.3 Why Normal NativeEdge Devices Don't Hit This

Normal NativeEdge devices have:
- `manufacturingType = "Dell"`, `isOpenSku = false`, `isIdracFdo = false`
- Endpoint type: `TypeNativeEdge` ("nativeedge")
- They **always** route to `eceInventoryHandlerForNE()`

The NE handler updates **both** tables for every event, so they stay in sync.

### 2.4 Why iDRAC FDO Devices Don't Hit This

True iDRAC FDO devices have `isIdracFdo=true`, so they route to the
compute handler from the very first event. The compute handler calls
`UpsertNEEndpoint()` for PRE_ONBOARDED but **never creates an
ece_inventory row**. This means `UpsertNEEndpointWithServiceTag` (Part 2
below) would not find an ece_inventory record and would skip the
regression path. These devices have a different problem (no ece_inventory
row at all), but they don't experience the status regression.

**Our device is the worst case**: it gets an ece_inventory row (from the
NE handler on first event), but that row is never updated (because all
subsequent events go through the compute handler).

---

## 3. Part 2: Status Regression via UpsertNEEndpointWithServiceTag

Even if the endpoint table briefly shows the correct
`provisioned_state = "onboarding_complete"`, it gets **overwritten back
to "pre_onboarded"** by any agent lifecycle event that triggers
`UpsertNEEndpointWithServiceTag`.

### 3.1 The Regression Chain

```
Agent lifecycle event (CID update, HW inventory, heartbeat, etc.)
  │
  ├── UpsertNEEndpointWithServiceTag(serviceTag)  [ne_service.go:60-67]
  │     │
  │     ├── GetECEInventoryByServiceTag(serviceTag)
  │     │     └── Returns ece_inventory row with status=PRE_ONBOARDED (stale!)
  │     │
  │     └── UpsertNEEndpoint(ece)  [ne_service.go:126-168]
  │           │
  │           ├── Endpoint exists → calls existingECEToEndpoint()
  │           │
  │           └── existingECEToEndpoint()  [ne_service.go:279-297]
  │                 │
  │                 └── existingEP.Extra = generateNEEndpointExtraFromECE(ece, existingEP)
  │                       │
  │                       └── Line 450: "provisioned_state": strings.ToLower(string(ece.Status))
  │                             │
  │                             └── ece.Status = PRE_ONBOARDED
  │                                   │
  │                                   └── provisioned_state OVERWRITTEN to "pre_onboarded"
```

### 3.2 The Smoking Gun: Line 450

```go
// ne_service.go:398-470 (abbreviated)
func (s *service) generateNEEndpointExtraFromECE(ctx context.Context,
    ece *models.ECEInventory, existingEP *endpoint.Endpoint) map[string]interface{} {

    extraMap := map[string]interface{}{
        "service_tag":       ece.ServiceTag,
        // ... many other fields ...
        "provisioned_state": strings.ToLower(string(ece.Status)),  // ← THE BUG
        // ...
    }

    // Preserves update_status from existing endpoint:
    if existingEP != nil {
        if existingEP.Extra["update_status"] != nil && existingEP.Extra["update_status"] != "" {
            extraMap["update_status"] = existingEP.Extra["update_status"]
        }
    }
    // BUT: Does NOT preserve provisioned_state from existing endpoint!

    return extraMap
}
```

The function preserves `update_status` from the existing endpoint (lines
459-462) but **does not preserve `provisioned_state`**. It always
unconditionally overwrites it from the ece_inventory record.

### 3.3 All Callers That Trigger the Regression

There are 8 call sites in `event_handler.go` that call
`UpsertNEEndpointWithServiceTag`, each triggered by different agent
lifecycle events. **Any one of these is sufficient to regress the status:**

| # | Handler Function | Line | Trigger |
|---|-----------------|------|---------|
| 1 | `updateCIDHandler` | 1184 | Agent CID (Client ID) update - **fires when agent connects to NATS** |
| 2 | `updateHardwareInventory` | 395 | Agent reports hardware inventory |
| 3 | `updateIPAddressHandler` | 424 | Agent reports IP address change |
| 4 | `updateVMInventory` | 445 | Agent reports VM inventory |
| 5 | `eceInventoryHandlerForNE` | 647 | NE handler status update (WAITING_TO_ONBOARD, etc.) |
| 6 | `eceInventoryHandlerForNE` | 667 | NE handler ONBOARDING_COMPLETE |
| 7 | `eceInventoryHandlerForNE` | 674 | NE handler PROVISIONED |
| 8 | `upgradeHandler` | 1382 | Agent reports upgrade status |

### 3.4 The Primary Regression Path: updateCIDHandler

The most likely immediate regression path is `updateCIDHandler` (line
1165), which fires when the ECE agent connects to NATS and its Client ID
is updated:

```go
// event_handler.go:1165-1189
func (eventHandler *eventHandler) updateCIDHandler(ctx context.Context,
    event *commonModels.EventEnvelope, serviceTag string) {

    clientID := event.Envelope.Body
    _, err1 := eventHandler.eceInventoryService.GetECEInventoryByServiceTag(serviceTag)
    if err1 != nil && errors.Is(err1.Err, sql.ErrNoRows) {
        // no ece_inventory record → non-NE path (does NOT call UpsertNEEndpoint)
        eventHandler.updateCIDForNonNE(ctx, serviceTag, clientID)
        return
    }
    // ece_inventory record EXISTS (our device has one, created by NE handler)
    eventHandler.eceInventoryService.UpdateClientID(ctx, serviceTag, clientID, ...)
    go eventHandler.endpointService.UpsertNEEndpointWithServiceTag(serviceTag) // ← REGRESSION
}
```

For our device, the ece_inventory row **does exist** (created by the NE
handler on the first PRE_ONBOARDED event), so `GetECEInventoryByServiceTag`
succeeds, and the code falls through to line 1184 which calls
`UpsertNEEndpointWithServiceTag` → reads the stale PRE_ONBOARDED status →
overwrites `endpoint.Extra["provisioned_state"]`.

### 3.5 The NATS System Connect Path

In addition to the CID update, the NATS system `$SYS.ACCOUNT.*.CONNECT`
event also fires when the agent connects:

```go
// event_handler.go:990-1029
func (eventHandler *eventHandler) NatsSystemConnectHandler(cm *ng.Msg) {
    // Parse NATS system connect advisory
    connectEvent := evt.(*advisory.ConnectEventMsgV1)
    clientName := connectEvent.Client.Name
    clientKind := connectEvent.Client.Kind

    if clientKind == "Client" {
        eventHandler.updateEndpointOnlineStatus(ctx, clientName)
    }
}

// event_handler.go:1048-1083
func (eventHandler *eventHandler) updateEndpointOnlineStatus(ctx context.Context, clientName string) {
    ep := eventHandler.endpointService.GetByExternalID(ctx, clientName)

    switch ep.Type {
    case endpoint.TypeCompute:
        // Updates connection status ONLY (connection, state, last_seen)
        // Does NOT call UpsertNEEndpointWithServiceTag
        eventHandler.endpointService.CheckAgentClientIDDiscrepancy(ctx, clientName)
    }
}
```

The NATS connect handler itself does NOT trigger the regression -- it
only updates connection status fields. However, after the agent connects,
it will publish hardware inventory, CID updates, and other events that
DO trigger `UpsertNEEndpointWithServiceTag`.

---

## 4. The Two Data Stores and the GUI

### 4.1 Data Store Summary

| Store | Field | Our Value | Updated By | Read By GUI? |
|-------|-------|-----------|-----------|--------------|
| `ece_inventory` | `status` | `PRE_ONBOARDED` (stale) | Only NE handler (never for our device after creation) | **Yes** |
| `endpoint` | `Extra["provisioned_state"]` | `"pre_onboarded"` (regressed) | Compute handler → correct, then UpsertNEEndpoint → regressed | Indirectly (used to compute `endpoint.state`) |

The GUI reads `ece_inventory.status` for NativeEdge/compute devices. Since
our `ece_inventory.status` is stuck at PRE_ONBOARDED, the GUI shows
PRE_ONBOARDED.

### 4.2 Timeline of Events

```
Time   Event                          ece_inventory.status    endpoint.Extra[provisioned_state]
─────  ─────────────────────────────  ──────────────────────  ────────────────────────────────
T0     Voucher upload (PRE_ONBOARDED)
       → NE handler creates both      PRE_ONBOARDED           pre_onboarded
       rows

T1     TO0 complete (WAITING_TO_ONBOARD)
       → Compute handler              PRE_ONBOARDED (stale)   waiting_to_onboard
       → UpdateEndpoint only

T2     TO2 start (ONBOARDING)
       → Compute handler              PRE_ONBOARDED (stale)   onboarding
       → UpdateEndpoint only

T3     TO2 complete (ONBOARDING_COMPLETE)
       → Compute handler              PRE_ONBOARDED (stale)   onboarding_complete ✓
       → UpdateEndpoint only

T4     Agent connects to NATS
       → CID update event
       → UpsertNEEndpointWithSvcTag   PRE_ONBOARDED (stale)   pre_onboarded ✗ REGRESSED!
       → Reads stale ece_inventory

T5     Agent reports HW inventory
       → UpsertNEEndpointWithSvcTag   PRE_ONBOARDED (stale)   pre_onboarded ✗ REGRESSED!
       → (would regress again even
          if T4 didn't)
```

---

## 5. Device Classification Matrix

| Manufacturing Type | isOpenSku | isIdracFdo | Endpoint Type | First Event Handler | Subsequent Handler | ece_inventory Row | Status Regression? |
|-------------------|-----------|-----------|---------------|--------------------|--------------------|-------------------|--------------------|
| Dell | false | false | `nativeedge` | NE handler | NE handler | Created & updated | **No** |
| Dell | true | false | `compute` | **NE handler** | **Compute handler** | Created, **never updated** | **YES (OUR BUG)** |
| Dell | true | true | `compute` | Compute handler | Compute handler | **Never created** | No (different problem) |
| Brownfield | * | * | `brownfield` | NE handler | NE handler | Created & updated | **No** |
| ThirdParty | * | * | `thirdparty` | NE handler | NE handler | Created & updated | **No** |

**Only our specific combination** (`Dell + isOpenSku=true + isIdracFdo=false`)
hits both problems: stale ece_inventory AND status regression via
UpsertNEEndpointWithServiceTag.

---

## 6. Additional Issues Found

### 6.1 State Machine Mismatch

The `UpdateEndpoint()` function at `ne_service.go:74` passes an empty
string for manufacturing type:

```go
canUpdate := models.CanUpdateStatus(currStatus, newStatus, "")
```

This causes it to use the "default" (Dell) state machine, which produced
the log error we observed:

```
unable to update endpoint (service tag 'SVCTAG-BKG1254'):
  invalid new status 'PROVISIONED', current status is 'ONBOARDING_COMPLETE'
```

The Dell default state machine requires the intermediate
`PROVISIONING_OPERATING_ENVIRONMENT` step, which is never published for
our device type.

### 6.2 NE Decoupling Interaction

When the `NE Decoupling` feature flag (`ne_cap_17244`) is enabled:

- **isOpenSku=false** events: Skipped by `skipNEOnboarding()` before
  reaching the handler. Handled by a separate NE endpoint service.
- **isOpenSku=true** events (us): NOT skipped -- processed normally
  through the buggy hybrid path.

### 6.3 UpsertNEEndpoint Does Not Preserve Provisioned State

The `generateNEEndpointExtraFromECE` function (ne_service.go:398-470)
**unconditionally overwrites** `provisioned_state` from `ece.Status`.
It preserves `update_status` and `cluster_id` from the existing
endpoint, but not `provisioned_state`. This is the root cause of Part 2.

---

## 7. Potential Fixes

### Option A: Fix the Compute Handler to Update ece_inventory (Recommended)

Modify `eceInventoryHandlerForCompute()` to also maintain `ece_inventory`
records, mirroring what the NE handler does:

1. For PRE_ONBOARDED: Call `CreateECEInventoryFromVoucher()` before
   `UpsertNEEndpoint()`
2. For subsequent statuses: Call `UpdateECEInventory()` before
   `UpdateEndpoint()`

This keeps both tables in sync and prevents UpsertNEEndpointWithServiceTag
from reading a stale value.

### Option B: Preserve provisioned_state in generateNEEndpointExtraFromECE

Modify `generateNEEndpointExtraFromECE` to check the existing endpoint's
`provisioned_state` and keep the more advanced state:

```go
// ne_service.go line 450, change:
"provisioned_state": strings.ToLower(string(ece.Status)),

// To:
"provisioned_state": preserveAdvancedProvisionedState(existingEP, ece.Status),
```

Where `preserveAdvancedProvisionedState` compares the state machine
positions and keeps whichever is more advanced. This is a defensive fix
that prevents the regression regardless of which caller triggers
`UpsertNEEndpointWithServiceTag`.

### Option C: Fix the GUI/API Layer

Modify the GUI to read `endpoint.Extra["provisioned_state"]` instead of
(or in addition to) `ece_inventory.status` for compute endpoints.

### Option D: Combination Fix (Best)

Apply both Option A and Option B:
- Option A ensures the ece_inventory table stays in sync going forward
- Option B is a defensive measure that prevents regression even if other
  code paths create similar out-of-sync scenarios

---

## 8. How to Reproduce

1. Upload a voucher for a device with `isIdracFdo=false` and
   `isOpenSku=true` (Dell manufacturing type)
2. Complete the full FDO onboarding flow (TO0 → TO1 → TO2)
3. Check the GUI -- status shows `PRE_ONBOARDED`
4. Query the database:
   - `ece_inventory.status` = `PRE_ONBOARDED` (stale)
   - `endpoint.Extra['provisioned_state']` = `onboarding_complete` (briefly
     correct, then regressed to `pre_onboarded` after agent connects)

---

## 9. Key Source Files

| File | Lines | What It Does |
|------|-------|-------------|
| `hzp-inventory/server/messaging/events/event_handler.go` | 235-339 | Main event routing (Receive) |
| `hzp-inventory/server/messaging/events/event_handler.go` | 460-531 | **Compute handler (Part 1 bug)** |
| `hzp-inventory/server/messaging/events/event_handler.go` | 534-677 | NE handler (correct) |
| `hzp-inventory/server/messaging/events/event_handler.go` | 683-703 | eceInventoryHandler routing |
| `hzp-inventory/server/messaging/events/event_handler.go` | 738-748 | determineEndpointType |
| `hzp-inventory/server/messaging/events/event_handler.go` | 990-1029 | NatsSystemConnectHandler |
| `hzp-inventory/server/messaging/events/event_handler.go` | 1048-1083 | updateEndpointOnlineStatus |
| `hzp-inventory/server/messaging/events/event_handler.go` | 1165-1189 | **updateCIDHandler (Part 2 regression trigger)** |
| `hzp-inventory/server/messaging/events/event_handler.go` | 1741-1757 | skipNEOnboarding (NE decoupling) |
| `hzp-inventory/server/services/endpoint/ne_service.go` | 60-67 | **UpsertNEEndpointWithServiceTag (Part 2 entry point)** |
| `hzp-inventory/server/services/endpoint/ne_service.go` | 69-102 | UpdateEndpoint (endpoint-only) |
| `hzp-inventory/server/services/endpoint/ne_service.go` | 126-168 | UpsertNEEndpoint |
| `hzp-inventory/server/services/endpoint/ne_service.go` | 279-297 | existingECEToEndpoint |
| `hzp-inventory/server/services/endpoint/ne_service.go` | 398-470 | **generateNEEndpointExtraFromECE (Part 2 smoking gun, line 450)** |
| `hzp-inventory/server/services/endpoint/ne_service.go` | 508-521 | getEndpointType (type classification) |
| `hzp-inventory/server/services/endpoint/ne_service.go` | 1520-1591 | handleNEEndpointExtraUpdateForState |
| `hzp-inventory/server/services/endpoint/plugin_agent.go` | 111-158 | CheckAgentClientIDDiscrepancy |
| `hzp-inventory/server/services/inventory/service.go` | 231-263 | UpdateECEInventory (with state machine) |
| `hzp-inventory/server/services/inventory/service.go` | 632-673 | CheckClientIDDiscrepancy (NE online status) |
| `hzp-inventory/server/models/ece_inventory_status.go` | 88-134 | State machine transitions + CanUpdateStatus |
| `hzp-inventory/ece_inventory.go` | 555-568 | NATS system events subscription |
| `hzp-fido-protocol/.../OwnerVoucher.java` | 316-342 | PRE_ONBOARDED event publishing |
| `hzp-fido-protocol/.../StandardMessageDispatcher.java` | 643-710 | ONBOARDING event (TO2 start) |
| `hzp-fido-protocol/.../StandardMessageDispatcher.java` | 1600-1718 | ONBOARDING_COMPLETE event (TO2 done) |
| `hzp-fido-protocol/.../To0Client.java` | 122-136 | WAITING_TO_ONBOARD event (TO0 done) |

---

## 10. Conclusion

This is a **server-side inventory service bug**, not a client issue. Our FDO
client and the Java FDO Owner Service are both working correctly -- they
complete the full TO1/TO2 flow and publish all the right NATS events with
correct status values.

The bug is a two-part problem specific to our device configuration
(`Dell + isOpenSku=true + isIdracFdo=false`):

1. **Hybrid path**: The first FDO event (PRE_ONBOARDED) routes through the
   NE handler (because `isIdracFdo=false` and no endpoint exists yet),
   creating an `ece_inventory` row. All subsequent events route through
   the compute handler (because the endpoint is TypeCompute), which never
   updates `ece_inventory.status`. The ece_inventory stays at PRE_ONBOARDED.

2. **Status regression**: `generateNEEndpointExtraFromECE` (ne_service.go
   line 450) unconditionally sets `provisioned_state` from `ece.Status`,
   which is the stale PRE_ONBOARDED from ece_inventory. This function is
   called via `UpsertNEEndpointWithServiceTag`, which is triggered by at
   least 8 different agent lifecycle events (CID updates, hardware
   inventory, IP changes, VM events, heartbeats, upgrades). Even if the
   endpoint table briefly reaches `onboarding_complete`, the next agent
   lifecycle event regresses it back to `pre_onboarded`.

We are the only device type that hits this combination because:
- NativeEdge devices (`Dell + !OpenSku`) use the NE handler exclusively,
  which keeps both tables in sync
- True iDRAC FDO devices (`Dell + OpenSku + isIdracFdo=true`) never get
  an ece_inventory row created, so UpsertNEEndpointWithServiceTag exits
  early when it can't find the ece_inventory record
- Our device uniquely gets an ece_inventory row (from NE handler) that is
  then never updated (because compute handler takes over)
