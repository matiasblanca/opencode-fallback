# TUI Status Verification Checklist — Manual E2E Test

## Prerequisites
- [ ] `opencode-fallback` binary is built and in PATH (or run from project root)
- [ ] auth.json has valid Anthropic OAuth credentials
- [ ] Terminal window is at least 80x24

## Main Screen Verification

1. **Run the configurator:**
   ```
   opencode-fallback configure
   ```

2. **Status bar (bottom of main screen):**
   - [ ] Status bar is visible at the bottom
   - [ ] Shows bridge status (connected/disconnected)
   - [ ] Shows auth status summary
   - [ ] If terminal height < 15: status bar should be hidden

## Status Screen Verification

3. **Open Status screen:**
   - [ ] Press `s` → Status screen opens

4. **Bridge section:**
   - [ ] Shows bridge connection status (Connected ✓ / Disconnected ✗)
   - [ ] Shows correct bridge port number
   - [ ] If bridge IS running: shows "Connected" + "Plugin transforms"
   - [ ] If bridge is NOT running: shows "Disconnected" + "Local transforms"

5. **Subscription Auth table:**
   - [ ] Shows "anthropic" provider row
   - [ ] Shows auth type ("oauth")
   - [ ] Shows validity status (Valid ✓ / Expired ✗ / Not configured)
   - [ ] Shows expiry time (e.g., "3h 42m")
   - [ ] If github-copilot is configured: shows a second row

6. **Refresh action:**
   - [ ] Press `r` → data refreshes
   - [ ] Expiry time may update slightly (a few seconds/minutes less)
   - [ ] Bridge status re-checks (may briefly flicker)

7. **Navigation:**
   - [ ] Press `Esc` → returns to main screen
   - [ ] Status bar on main screen still shows updated info

## Edge Cases

8. **Narrow terminal (< 80 cols):**
   - [ ] Status information is still readable (truncated but not broken)

9. **Tall terminal (> 40 rows):**
   - [ ] Layout uses available space appropriately

10. **No auth.json present:**
    - [ ] Status shows "not configured" for all providers
    - [ ] No crashes or panics

## Results

| Check | Status | Notes |
|-------|--------|-------|
| Status bar visible | ✅/❌ | |
| Bridge status correct | ✅/❌ | |
| Auth status correct | ✅/❌ | |
| Status screen opens (s) | ✅/❌ | |
| Bridge section accurate | ✅/❌ | |
| Auth table accurate | ✅/❌ | |
| Refresh works (r) | ✅/❌ | |
| Navigation (Esc) works | ✅/❌ | |
| Terminal size handling | ✅/❌ | |

**Tester:** _______________
**Date:** _______________
**OS/Terminal:** _______________
