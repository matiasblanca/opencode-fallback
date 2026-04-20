# E2E Test Results — opencode-fallback v0.6.0

Date: YYYY-MM-DD HH:MM
Environment: Windows 11, Go 1.25, OpenCode latest

## Environment

| Check | Status | Notes |
|-------|--------|-------|
| auth.json present | ✅/❌ | |
| auth.json valid JSON | ✅/❌ | |
| Anthropic OAuth valid | ✅/❌ | |
| Token not expired | ✅/❌ | Expires in: |
| go build succeeds | ✅/❌ | |
| Port 18888 available | ✅/❌ | |
| Bridge token present | ✅/❌/⏭️ | |
| Copilot configured | ✅/❌/⏭️ | |

## Test Results

| Test | Status | Duration | Notes |
|------|--------|----------|-------|
| **Phase 2: Proxy Startup** | | | |
| 2.1 Proxy starts with OAuth provider | ✅/❌ | | |
| 2.2 Provider registered as subscription | ✅/❌ | | |
| **Phase 3: Live Requests** | | | |
| 3.1 Non-streaming request (Anthropic) | ✅/❌ | | |
| 3.2 Streaming request (Anthropic) | ✅/❌ | | |
| 3.3 Fallback scenario (fake→Anthropic) | ✅/❌ | | |
| 3.4 Token refresh | ✅/❌/⏭️ | | |
| **Phase 4: Bridge Integration** | | | |
| 4.1 Bridge health check | ✅/❌/⏭️ | | |
| 4.2 Bridge auth retrieval | ✅/❌/⏭️ | | |
| 4.3 Bridge transform | ✅/❌/⏭️ | | |
| 4.4 Bridge full proxy request | ✅/❌/⏭️ | | |
| **Phase 5: TUI Status (manual)** | | | |
| 5.1 Status bar visible | ✅/❌ | | |
| 5.2 Status screen opens | ✅/❌ | | |
| 5.3 Bridge status accurate | ✅/❌ | | |
| 5.4 Auth table accurate | ✅/❌ | | |
| 5.5 Refresh works | ✅/❌ | | |
| 5.6 Navigation works | ✅/❌ | | |

Legend: ✅ = passed, ❌ = failed, ⏭️ = skipped (prerequisites not met)

## SSE Streaming Details (Test 3.2)

- Total SSE events received: ___
- Content assembled: ___
- Stream terminated with: [DONE] / message_stop / other
- Latency to first token: ___ms

## Fallback Details (Test 3.3)

- First provider failure reason: ___
- Fallback triggered: yes/no
- Final response from: ___
- Total latency: ___ms

## Issues Found

1. (describe any failures, unexpected behaviors, or edge cases)
2. 
3. 

## Token/Auth Observations

- Token expiry at test start: ___
- Token refreshed during test: yes/no
- auth.json updated on disk: yes/no/n/a

## Performance Observations

- Non-streaming request latency: ___ms
- Streaming first-token latency: ___ms
- Proxy startup time: ___ms
- Fallback overhead: ___ms

## Recommendations

1. (list any improvements, bugs to file, or follow-up work)
2. 
3. 

## Next Steps

- [ ] File GitHub issues for any failures found
- [ ] Update README with "tested with Claude Max subscription" note
- [ ] Consider automated E2E in CI (with secrets) for regression testing
- [ ] Plan next feature phase based on real-world findings
