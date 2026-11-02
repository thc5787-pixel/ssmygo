# ssmygo P1-D Implementation Plan: geosite update, user rules, auto-reload

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Update geosite online (manual + weekly), apply `user-rule.txt` (ABP subset) ahead of geosite, and reload it automatically.

**Architecture:** MyGo-free packages `geosite` (download + verify) and `rules` (user-rule parse, extra domain sets, route order) feed controller operations; `ui` adds the PAC submenu.

**Tech Stack:** Go 1.27.3, no cgo, MyGo v0.7.4, vendored shadowsocks-go. Branch `feat/d-rules` from `feat/c-sources`.

**Spec:** `docs/superpowers/specs/2026-11-02-ssmygo-d-rules-design.md`

## Global Constraints

- All earlier constraints (no cgo; MyGo only in `cmd/` and `internal/ui`; no secrets in logs; commit trailer `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_0197wVrsfVcKv9s9u4vo2grA`).
- Default geosite URL `https://github.com/v2fly/domain-list-community/raw/release/dlc.dat`, sum URL `…/dlc.dat.sha256sum`. Limit 20 MB, timeout 60 s. Weekly = 7 × 24 h since `ssmygoGeositeUpdatedAt`.
- Fetch path: reuse C's `subscribe` CONNECT dialer (export it as `subscribe.Client(proxyPort int, timeout time.Duration) *http.Client`) and controller `fetchPort()`.
- Original strings: "Update Local PAC from Geosite", "Edit User Rule for Geosite...", "PAC" (in i18n.csv); add missing ones as ssmygo strings.
- i18n JSON is generated: add keys in `internal/i18n/gen/main.go` and run `go run ./internal/i18n/gen -csv C:\Users\a\Desktop\shadowsocks-windows\shadowsocks-csharp\Data\i18n.csv -resx-en …\Localization\Strings.resx -resx-zh …\Localization\Strings.zh-Hans.resx -out internal/i18n`.
- Never touch the user's live system proxy, registry or processes in tests (use the existing fakes).

## Review Focus

1. **A broken or hostile download** (truncated, HTML, wrong groups, 20 MB+): current rules kept, nothing half-written. → Task 1.
2. **user-rule.txt edits while a rebuild runs / saved by editors that write twice or via rename**: no crash, final content wins, no lost rules. → Task 4.
3. **Route order**: a user `@@` rule beats a geosite proxied domain and vice versa. → Task 3.
4. **Rules mode vs global**: rules rebuilt in both, core re-applied only when it matters, system proxy untouched. → Task 4.
5. **Original gui-config.json** with `geositeUrl`/`geositeSha256sumUrl` round-trips. → Task 1.

---

### Task 1: `geosite` download and verify; config fields

**Files:** Create `internal/geosite/{update.go,update_test.go}`; modify `internal/config/guiconfig.go` (+ test), `internal/subscribe/fetch.go` (export `Client`).

**Produces:** `config.Config` fields `GeositeURL string json:"geositeUrl"`, `GeositeSha256sumURL string json:"geositeSha256sumUrl"`, `GeositeUpdatedAt time.Time json:"ssmygoGeositeUpdatedAt,omitzero"`; `geosite.Fetch(ctx, url, sumURL string, client *http.Client, groups []string) (data []byte, err error)` (applies default URL/sum rules, limit, parse, groups check); `geosite.DefaultURL`, `geosite.DefaultSumURL`.

- [ ] Failing tests: OK; sha mismatch; custom URL without sum not verified; >20 MB; not a dlc; missing group (error names it); error text has host only; config round trip of the three fields.
- [ ] Implement; PASS; commit `feat(geosite): download and verify dlc.dat`

### Task 2: user-rule parser

**Files:** Create `internal/rules/userrule.go`, `userrule_test.go`.

**Produces:** `ParseUserRules(text string) (direct, proxied []Domain, skipped []int)` (line numbers 1-based); `Domain` from `dlc.go` with type domain-suffix.

- [ ] Failing table test covering every row of spec §3 (incl. `||a.com^$third-party` skipped, `/re/` skipped, `|https://x.com/path` → x.com, `@@` variants, comments, CRLF, BOM).
- [ ] Implement; PASS; commit `feat(rules): parse user-rule.txt`

### Task 3: rules with user sets; route order

**Files:** Modify `internal/rules/build.go` (+test), `internal/core/build.go` (+goldens), `internal/controller/controller.go`.

**Produces:** `rules.Build(dlc []byte, direct, proxied []string, user string /* user-rule text */, outDir string) (Result, error)` with `Result.UserDirect`, `Result.UserProxied` (gob paths, empty when none) and `Result.UserSkipped []int`; `core.Build` routes: private → user direct → user proxied → geosite direct → geosite proxied.

- [ ] Failing tests: user sets written and keyed into the cache key; core golden `rules-user` showing the order; runner test: user `@@` for a domain in geosite proxied goes direct (through a local echo server reached by name via a test resolver is not possible — assert via the built router config order and one end-to-end with an IP-literal private route unchanged).
- [ ] Implement; update existing callers; PASS; commit `feat(rules): user rules ahead of geosite`

### Task 4: controller — update, weekly check, auto-reload

**Files:** Create `internal/controller/rules.go`, `rules_test.go`; modify `controller.go`.

**Produces:** `UpdateGeosite(ctx) (changed bool, err error)`; startup weekly check in the background (same pattern as subscriptions, `bgCancel`); a 2 s poller of `user-rule.txt` (mtime+size) started in `Start`, stopped in `Shutdown`; `UserRulePath() string`; `EnsureUserRuleFile() (path string, err error)` (writes the template when missing); typed notices `GeositeUpdatedNotice`, `UserRulesSkippedError{Lines []int}`, failures as errors; the DLC used = `<root>/dlc.dat` if it parses, else `Deps.DLC`.

- [ ] Failing tests: manual update applies (new group present afterwards); same sha → `changed=false`, no rebuild; bad download keeps old rules; weekly check runs only when stale (inject clock); user-rule change picked up within 3 s (poll interval injectable); unreadable file keeps previous rules; rename-save (write temp, rename over) picked up; rebuild in global mode does not touch sysproxy fake.
- [ ] Implement; PASS; commit `feat(controller): geosite update and user-rule reload`

### Task 5: UI — PAC submenu, notices, acceptance

**Files:** Modify `internal/ui/{tray.go,tray_test.go,messages.go}`, i18n; create `docs/acceptance-d.md`.

- [ ] Failing tests: menu has "PAC" submenu with ids `pac.update`, `pac.userRule`; clicking update runs off the main thread via `runAction`; `Message()` texts for the new notices/errors.
- [ ] Implement (user-rule item: `EnsureUserRuleFile` then `mygo.Shell.OpenPath`); build exe to `build/ssmygo-d.exe`; write acceptance list; PASS; commit `feat(ui): PAC menu for geosite update and user rules; D acceptance list`
