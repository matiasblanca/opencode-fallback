package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/matiasblanca/opencode-fallback/internal/config"
	"github.com/matiasblanca/opencode-fallback/internal/opencode"
)

// testConfig returns a config suitable for testing.
func testConfig() config.Config {
	return config.Config{
		Version: "1",
		Providers: map[string]config.ProviderConfig{
			"anthropic": {
				BaseURL: "https://api.anthropic.com",
				Models:  []string{"claude-sonnet-4", "claude-haiku-3"},
			},
			"openai": {
				BaseURL: "https://api.openai.com",
				Models:  []string{"gpt-4o", "gpt-4o-mini"},
			},
		},
		FallbackChains: config.FallbackChainsConfig{
			Global: []config.ChainEntry{
				{Provider: "anthropic", Model: "claude-sonnet-4"},
				{Provider: "openai", Model: "gpt-4o"},
			},
			Agents: map[string][]config.ChainEntry{
				"sdd-apply": {
					{Provider: "mistral", Model: "codestral-latest"},
					{Provider: "openai", Model: "gpt-4o"},
				},
			},
		},
	}
}

// testProviders returns test provider info.
func testProviders() []ProviderInfo {
	return []ProviderInfo{
		{ID: "anthropic", DisplayName: "Anthropic", BaseURL: "https://api.anthropic.com", Available: true, Models: []string{"claude-sonnet-4", "claude-haiku-3"}},
		{ID: "openai", DisplayName: "OpenAI", BaseURL: "https://api.openai.com", Available: true, Models: []string{"gpt-4o", "gpt-4o-mini"}},
		{ID: "deepseek", DisplayName: "DeepSeek", BaseURL: "https://api.deepseek.com", Available: false, Models: []string{"deepseek-chat"}},
	}
}

// testAgents returns test agents from opencode.json.
func testAgents() []opencode.AgentInfo {
	return []opencode.AgentInfo{
		{Name: "gentleman", Model: "anthropic/claude-sonnet-4-6", Mode: "primary"},
		{Name: "sdd-apply", Model: "anthropic/claude-sonnet-4-6", Mode: "subagent"},
		{Name: "sdd-explore", Model: "openai/gpt-5.3-codex", Mode: "subagent"},
	}
}

// testDeps returns mock dependencies for testing.
func testDeps() Dependencies {
	return Dependencies{
		LoadConfig: func() (config.Config, error) { return testConfig(), nil },
		SaveConfig: func(cfg config.Config) error { return nil },
	}
}

func newTestModel() Model {
	return NewModel(testConfig(), testProviders(), testAgents(), testDeps())
}

func TestNewModel_InitialState(t *testing.T) {
	m := newTestModel()

	if m.Screen != ScreenMain {
		t.Errorf("Screen = %d, want ScreenMain (%d)", m.Screen, ScreenMain)
	}
	if m.ActiveTab != 1 {
		t.Errorf("ActiveTab = %d, want 1 (Agents)", m.ActiveTab)
	}
	if m.Cursor != 0 {
		t.Errorf("Cursor = %d, want 0", m.Cursor)
	}
	if m.Dirty {
		t.Error("Dirty should be false initially")
	}
	if m.ConfirmQuit {
		t.Error("ConfirmQuit should be false initially")
	}
	if len(m.Agents) != 3 {
		t.Errorf("Agents count = %d, want 3", len(m.Agents))
	}
}

func TestNewModel_AgentMerge(t *testing.T) {
	m := newTestModel()

	// sdd-apply should have HasOverride=true (it's in config.agents).
	var sddApply *AgentDisplay
	for i := range m.Agents {
		if m.Agents[i].Name == "sdd-apply" {
			sddApply = &m.Agents[i]
			break
		}
	}
	if sddApply == nil {
		t.Fatal("sdd-apply not found in agents")
	}
	if !sddApply.HasOverride {
		t.Error("sdd-apply should have HasOverride=true")
	}

	// gentleman should NOT have override.
	var gentleman *AgentDisplay
	for i := range m.Agents {
		if m.Agents[i].Name == "gentleman" {
			gentleman = &m.Agents[i]
			break
		}
	}
	if gentleman == nil {
		t.Fatal("gentleman not found")
	}
	if gentleman.HasOverride {
		t.Error("gentleman should NOT have HasOverride")
	}
}

func TestInit_ReturnsNil(t *testing.T) {
	m := newTestModel()
	cmd := m.Init()
	if cmd != nil {
		t.Error("Init() should return nil")
	}
}

func TestUpdate_QuitKey(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	_ = updated
	if cmd == nil {
		t.Fatal("Update('q') should return tea.Quit cmd")
	}
}

func TestUpdate_WindowSizeMsg(t *testing.T) {
	m := newTestModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	um := updated.(Model)
	if um.Width != 120 {
		t.Errorf("Width = %d, want 120", um.Width)
	}
	if um.Height != 40 {
		t.Errorf("Height = %d, want 40", um.Height)
	}
}

func TestUpdate_TabSwitch(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24

	// Default is tab 1 (Agents). Press '1' to go to Global.
	updated, _ := m.Update(tea.KeyPressMsg{Code: '1'})
	um := updated.(Model)
	if um.ActiveTab != 0 {
		t.Errorf("ActiveTab after '1' = %d, want 0", um.ActiveTab)
	}

	// Press '2' to go back to Agents.
	updated2, _ := um.Update(tea.KeyPressMsg{Code: '2'})
	um2 := updated2.(Model)
	if um2.ActiveTab != 1 {
		t.Errorf("ActiveTab after '2' = %d, want 1", um2.ActiveTab)
	}
}

func TestUpdate_TabCycles(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.ActiveTab = 0 // Start on Global.

	// Tab should cycle to Agents.
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	um := updated.(Model)
	if um.ActiveTab != 1 {
		t.Errorf("ActiveTab after Tab from 0 = %d, want 1", um.ActiveTab)
	}

	// Tab again should cycle back to Global.
	updated2, _ := um.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	um2 := updated2.(Model)
	if um2.ActiveTab != 0 {
		t.Errorf("ActiveTab after Tab from 1 = %d, want 0", um2.ActiveTab)
	}
}

func TestUpdate_TabResetsCursor(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.Cursor = 3 // Move cursor down.

	// Switch tab should reset cursor.
	updated, _ := m.Update(tea.KeyPressMsg{Code: '1'})
	um := updated.(Model)
	if um.Cursor != 0 {
		t.Errorf("Cursor after tab switch = %d, want 0", um.Cursor)
	}
}

func TestUpdate_CursorNavigation(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24

	// j moves cursor down.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	um := updated.(Model)
	if um.Cursor != 1 {
		t.Errorf("Cursor after 'j' = %d, want 1", um.Cursor)
	}

	// k moves cursor up.
	updated2, _ := um.Update(tea.KeyPressMsg{Code: 'k'})
	um2 := updated2.(Model)
	if um2.Cursor != 0 {
		t.Errorf("Cursor after 'k' = %d, want 0", um2.Cursor)
	}

	// k at 0 stays at 0.
	updated3, _ := um2.Update(tea.KeyPressMsg{Code: 'k'})
	um3 := updated3.(Model)
	if um3.Cursor != 0 {
		t.Errorf("Cursor after 'k' at 0 = %d, want 0", um3.Cursor)
	}
}

func TestUpdate_EnterOpensChainEditor(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24

	// Press enter on first agent.
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(Model)
	if um.Screen != ScreenChainEditor {
		t.Errorf("Screen = %d, want ScreenChainEditor (%d)", um.Screen, ScreenChainEditor)
	}
	if um.EditingAgent != m.Agents[0].Name {
		t.Errorf("EditingAgent = %q, want %q", um.EditingAgent, m.Agents[0].Name)
	}
}

func TestUpdate_EscFromChainEditor(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.Screen = ScreenChainEditor
	m.EditingAgent = "sdd-apply"

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	um := updated.(Model)
	if um.Screen != ScreenMain {
		t.Errorf("Screen after Esc = %d, want ScreenMain (%d)", um.Screen, ScreenMain)
	}
}

func TestUpdate_CtrlS_SaveSuccess(t *testing.T) {
	saved := false
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.Dirty = true
	m.Deps.SaveConfig = func(cfg config.Config) error {
		saved = true
		return nil
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Ctrl+S should return a cmd")
	}

	// Execute the cmd to get the SaveResultMsg.
	msg := cmd()
	result, ok := msg.(SaveResultMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want SaveResultMsg", msg)
	}
	if result.Err != nil {
		t.Errorf("SaveResultMsg.Err = %v, want nil", result.Err)
	}
	if !saved {
		t.Error("SaveConfig should have been called")
	}

	// Process the SaveResultMsg.
	updated, _ := m.Update(result)
	um := updated.(Model)
	if um.Dirty {
		t.Error("Dirty should be false after successful save")
	}
	if um.StatusMsg != "Saved!" {
		t.Errorf("StatusMsg = %q, want %q", um.StatusMsg, "Saved!")
	}
}

func TestUpdate_CtrlS_SaveError(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.Dirty = true
	m.Deps.SaveConfig = func(cfg config.Config) error {
		return errors.New("disk full")
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	msg := cmd()
	result := msg.(SaveResultMsg)

	updated, _ := m.Update(result)
	um := updated.(Model)
	if !um.Dirty {
		t.Error("Dirty should still be true after failed save")
	}
	if !strings.Contains(um.StatusMsg, "disk full") {
		t.Errorf("StatusMsg = %q, should contain 'disk full'", um.StatusMsg)
	}
}

func TestUpdate_QuitWithDirty(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.Dirty = true

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	um := updated.(Model)
	if !um.ConfirmQuit {
		t.Error("ConfirmQuit should be true when dirty")
	}
	if cmd != nil {
		t.Error("should not quit yet, just show confirmation")
	}
}

func TestUpdate_QuitConfirmY(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.ConfirmQuit = true

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y'})
	if cmd == nil {
		t.Error("'y' in confirm dialog should return tea.Quit")
	}
}

func TestUpdate_QuitConfirmN(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.ConfirmQuit = true

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'n'})
	if cmd == nil {
		t.Error("'n' in confirm dialog should return tea.Quit")
	}
}

func TestUpdate_QuitConfirmEsc(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.ConfirmQuit = true

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	um := updated.(Model)
	if um.ConfirmQuit {
		t.Error("Esc should cancel confirm quit dialog")
	}
}

func TestUpdate_GlobalTab_Navigation(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.ActiveTab = 0 // Global tab.

	// j moves down through 4 slots.
	for i := 0; i < 3; i++ {
		updated, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
		m = updated.(Model)
	}
	if m.Cursor != 3 {
		t.Errorf("Cursor after 3x 'j' = %d, want 3", m.Cursor)
	}

	// j at 3 stays at 3 (max slot).
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	m = updated.(Model)
	if m.Cursor != 3 {
		t.Errorf("Cursor after 'j' at 3 = %d, want 3", m.Cursor)
	}
}

func TestUpdate_GlobalTab_ClearSlot(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.ActiveTab = 0

	initialLen := len(m.Config.FallbackChains.Global)

	// Press 'x' to clear slot 0.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'x'})
	um := updated.(Model)

	if len(um.Config.FallbackChains.Global) != initialLen-1 {
		t.Errorf("Global chain length = %d, want %d", len(um.Config.FallbackChains.Global), initialLen-1)
	}
	if !um.Dirty {
		t.Error("Dirty should be true after clearing slot")
	}
}

func TestView_ReturnsTeaView(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24

	v := m.View()
	if v.AltScreen != true {
		t.Error("View should set AltScreen=true")
	}
}

func TestUpdate_ProvidersScreen(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24

	// Press 'p' to go to providers.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'p'})
	um := updated.(Model)
	if um.Screen != ScreenProviders {
		t.Errorf("Screen = %d, want ScreenProviders (%d)", um.Screen, ScreenProviders)
	}
}

func TestUpdate_CreateOverride(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.Screen = ScreenChainEditor
	m.EditingAgent = "gentleman"

	// Press 'o' to create override.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'o'})
	um := updated.(Model)

	chain, ok := um.Config.FallbackChains.Agents["gentleman"]
	if !ok {
		t.Fatal("gentleman should have an override after 'o'")
	}
	// Should copy global chain.
	if len(chain) != len(um.Config.FallbackChains.Global) {
		t.Errorf("override chain length = %d, want %d (copy of global)", len(chain), len(um.Config.FallbackChains.Global))
	}
	if !um.Dirty {
		t.Error("Dirty should be true after creating override")
	}
}

func TestUpdate_DeleteOverride(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.Screen = ScreenChainEditor
	m.EditingAgent = "sdd-apply"

	// sdd-apply has an override. Press 'd' to delete it.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	um := updated.(Model)

	_, ok := um.Config.FallbackChains.Agents["sdd-apply"]
	if ok {
		t.Error("sdd-apply should NOT have an override after 'd'")
	}
	if !um.Dirty {
		t.Error("Dirty should be true after deleting override")
	}
}

// --- Add Agent Tests (Paso 1) ---

func TestUpdate_NKey_EntersAddingAgent(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	// Default tab is 1 (Agents).

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'n'})
	um := updated.(Model)
	if !um.AddingAgent {
		t.Error("AddingAgent should be true after 'n'")
	}
	if um.AddAgentInput != "" {
		t.Errorf("AddAgentInput = %q, want empty", um.AddAgentInput)
	}
}

func TestUpdate_AddAgent_TypingCharacters(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.AddingAgent = true
	m.AddAgentInput = ""

	// Type "my-agent".
	keys := []rune{'m', 'y', '-', 'a', 'g', 'e', 'n', 't'}
	for _, ch := range keys {
		updated, _ := m.Update(tea.KeyPressMsg{Code: ch})
		m = updated.(Model)
	}
	if m.AddAgentInput != "my-agent" {
		t.Errorf("AddAgentInput = %q, want %q", m.AddAgentInput, "my-agent")
	}
}

func TestUpdate_AddAgent_EnterCreatesAgent(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.AddingAgent = true
	m.AddAgentInput = "my-custom-agent"

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(Model)

	if um.AddingAgent {
		t.Error("AddingAgent should be false after Enter")
	}
	if um.AddAgentInput != "" {
		t.Errorf("AddAgentInput = %q, want empty", um.AddAgentInput)
	}

	// Check agent was created.
	chain, ok := um.Config.FallbackChains.Agents["my-custom-agent"]
	if !ok {
		t.Fatal("my-custom-agent should exist in config.agents")
	}
	if len(chain) != 0 {
		t.Errorf("new agent chain length = %d, want 0 (empty)", len(chain))
	}
	if !um.Dirty {
		t.Error("Dirty should be true after creating agent")
	}
}

func TestUpdate_AddAgent_EscCancels(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.AddingAgent = true
	m.AddAgentInput = "partial-input"

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	um := updated.(Model)

	if um.AddingAgent {
		t.Error("AddingAgent should be false after Esc")
	}
	if um.AddAgentInput != "" {
		t.Errorf("AddAgentInput = %q, want empty after cancel", um.AddAgentInput)
	}

	// Agent should NOT be created.
	_, ok := um.Config.FallbackChains.Agents["partial-input"]
	if ok {
		t.Error("partial-input should NOT be in config.agents after cancel")
	}
}

func TestUpdate_AddAgent_EmptyEnterDoesNothing(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.AddingAgent = true
	m.AddAgentInput = ""
	initialCount := len(m.Agents)

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(Model)

	if !um.AddingAgent {
		t.Error("AddingAgent should remain true when Enter with empty input")
	}
	if len(um.Agents) != initialCount {
		t.Errorf("Agents count = %d, want %d (unchanged)", len(um.Agents), initialCount)
	}
}

// --- Model Picker Arrow Key Tests (Paso 8) ---

func TestModelPicker_JAddsToFilter(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.Screen = ScreenModelPicker
	m.FilterText = ""

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	um := updated.(Model)
	if um.FilterText != "j" {
		t.Errorf("FilterText = %q, want %q (j should filter, not navigate)", um.FilterText, "j")
	}
}

func TestModelPicker_KAddsToFilter(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.Screen = ScreenModelPicker
	m.FilterText = ""

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	um := updated.(Model)
	if um.FilterText != "k" {
		t.Errorf("FilterText = %q, want %q (k should filter, not navigate)", um.FilterText, "k")
	}
}

func TestModelPicker_ArrowDownMovesCursor(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.Screen = ScreenModelPicker
	m.Cursor = 0

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	um := updated.(Model)
	if um.Cursor != 1 {
		t.Errorf("Cursor = %d, want 1 (arrow down should navigate)", um.Cursor)
	}
}

func TestModelPicker_ArrowUpMovesCursor(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.Screen = ScreenModelPicker
	m.Cursor = 2

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	um := updated.(Model)
	if um.Cursor != 1 {
		t.Errorf("Cursor = %d, want 1 (arrow up should navigate)", um.Cursor)
	}
}

func TestModelPicker_TypingDeepseekFilters(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.Screen = ScreenModelPicker
	m.FilterText = ""

	for _, ch := range "deepseek" {
		updated, _ := m.Update(tea.KeyPressMsg{Code: ch})
		m = updated.(Model)
	}
	if m.FilterText != "deepseek" {
		t.Errorf("FilterText = %q, want %q", m.FilterText, "deepseek")
	}
}

func TestUpdate_AddAgent_AppearsInList(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	m.Height = 24
	m.AddingAgent = true
	m.AddAgentInput = "new-agent"

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	um := updated.(Model)

	// Find the new agent.
	found := false
	for _, a := range um.Agents {
		if a.Name == "new-agent" {
			found = true
			if !a.HasOverride {
				t.Error("new agent should have HasOverride=true")
			}
			break
		}
	}
	if !found {
		t.Error("new-agent should appear in the agents list")
	}
}
