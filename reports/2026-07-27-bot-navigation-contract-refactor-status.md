# STATUS_REPORT

## Goal

Stop grounded bots from losing valid routes because one arbitrary footprint
contact was treated as the actor's only support, then stop retreat plans from
retrying the same failed destination after every plan restart.

## Scope

- Primary owner: engine grounded-character support contract.
- Consumer: ActionGame route following and bot plan execution.
- Additive runtime fields only; no serialized schema or nav bake changes.

## Confidence

- High.
- Logs show an unblocked motor reaching the exact waypoint at
  `(16.25, -43.00, 14.35)`, followed by
  `waypoint_support_mismatch`.
- Equal-height ground selected the first corner probe because corners were
  sampled before the centre.
- Generic plan cleanup also erased the retreat candidate set, so the next plan
  selected the same failed span.

## Chosen design

- Publish the existing five physical ground probes at the selected bearing
  height as a fixed-size, allocation-free contact set with support entity IDs.
- Prefer the centre probe when valid contacts have equal height.
- Reuse `ActionGameNPCNavigationComponent` as the navigation lifecycle owner.
- Use the selected motor support for normal localization. Near a route boundary
  or mismatch, tie-break only among other live motor contacts; never treat a
  stale route span as physical support.
- Keep failed retreat spans across generic plan resets; invalidate them on
  navigation revision, threat change, material bot movement, or success.
- Require consecutive localization misses before off-graph recovery.
- Declare navigation dependency on each plan step; remove executor allowlist.
- Version navigation commands and retain one terminal result until the matching
  plan step consumes it.

## Alternatives rejected

- Widen generic graph projection: can bind the wrong stacked surface.
- Trust the expected route span without a live motor contact: hides removed or
  moving support.
- Keep patching action-name switches: every new movement action can be omitted.
- Add another navigation-agent abstraction: duplicates the lifecycle already
  present in `ActionGameNPCNavigationComponent`.

## Verification

- Engine regressions: flat equal-height ground selects centre; all five live
  contacts and moving support identity reach motor state.
- ActionGame regressions: a live centre contact completes a route when the
  selected corner belongs to the previous span; one-frame misses do not
  recover.
- Retreat regression: generic plan reset preserves attempted destination refs.
- Plan regression: duplicate and stale navigation results are rejected; an
  active required-navigation step consumes its matching result after mutable
  status changes.
- Passed:
  - `env GOCACHE=/private/tmp/gekko3d-codex-go-cache go test ./...` in
    `gekko`.
  - `env GOCACHE=/private/tmp/gekko3d-codex-go-cache go test ./...` in
    `actiongame`.
  - `env GOCACHE=/private/tmp/gekko3d-codex-go-cache
    ACTIONGAME_CROSSFIRE_NAV_TEST=1 go test ./src/modules/startup -run
    '^TestActionGameNPCNavigationCrossfireStairUsesFootprintSupport$' -count=1`
    in `actiongame`.

## Manual check

Replay the logged Crossfire stair route and confirm no
`recovering_off_nav -> off_nav_recovery_failed -> start_unsupported` sequence.

## Result

- Motor and navigation now share the complete live support set.
- Normal flat travel performs one localization; extra contact checks happen
  only at span boundaries or mismatches.
- Failed retreat destinations survive plan restarts.
- Route localization tolerates two transient misses before bounded recovery.
- Generated and single-action plans share one explicit navigation-dependency
  contract.
- Plans consume one matching terminal result instead of polling mutable
  navigation status.
- Navigation bake/schema unchanged; no rebake required.
