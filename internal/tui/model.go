package tui

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/matiasblanca/opencode-fallback/internal/config"
	"github.com/matiasblanca/opencode-fallback/internal/opencode"
	"github.com/matiasblanca/opencode-fallback/internal/tui/screens"
)



// maxSlots is the number of slots in a fallback chain (Primary + 3 Fallbacks).
const maxSlots = 4

// Model is the root Bubble Tea model for the configuration TUI.
type Model struct {
	// Screen navigation (gentle-ai pattern)
	Screen         Screen
	PreviousScreen Screen
	Cursor         int

	// Terminal size
	Width  int
	Height int

	// Data layer
	Config    config.Config   // Working copy
	Original  config.Config   // Snapshot to detect changes
	Providers []ProviderInfo  // Detected providers with availability
	Agents    []AgentDisplay  // Merged from opencode.json + config

	// Tabs
	ActiveTab int // 0=Global, 1=Agents

	// Editor state
	EditingAgent string // Agent name being edited
	PickerSlot   int    // Slot index being picked (on ScreenModelPicker)
	FilterText   string // Current filter text in model picker

	// UI state
	Dirty       bool
	ConfirmQuit bool
	StatusMsg   string
	ShowHelp    bool

	// Scroll offset for lists
	ScrollOffset int

	// Add agent input mode
	AddingAgent  bool
	AddAgentInput string

	// DI
	Deps Dependencies
}

// NewModel creates a new Model with the given initial data.
func NewModel(cfg config.Config, providers []ProviderInfo, agents []opencode.AgentInfo, deps Dependencies) Model {
	m := Model{
		Screen:    ScreenMain,
		Config:    cfg,
		Original:  cfg,
		Providers: providers,
		ActiveTab: 1, // Default to Agents tab
		Deps:      deps,
	}
	m.rebuildAgents(agents)
	return m
}

// rebuildAgents merges agents from opencode.json with those in config.
func (m *Model) rebuildAgents(discovered []opencode.AgentInfo) {
	seen := make(map[string]bool)
	var agents []AgentDisplay

	// Add all discovered agents.
	for _, a := range discovered {
		seen[a.Name] = true
		_, hasOverride := m.Config.FallbackChains.Agents[a.Name]
		chain := m.resolveChain(a.Name)
		agents = append(agents, AgentDisplay{
			Name:         a.Name,
			CurrentModel: a.Model,
			Mode:         a.Mode,
			HasOverride:  hasOverride,
			Chain:        chain,
		})
	}

	// Add agents from config that are not in opencode.json.
	if m.Config.FallbackChains.Agents != nil {
		for name := range m.Config.FallbackChains.Agents {
			if !seen[name] {
				seen[name] = true
				chain := m.resolveChain(name)
				agents = append(agents, AgentDisplay{
					Name:        name,
					HasOverride: true,
					Chain:       chain,
				})
			}
		}
	}

	sort.Slice(agents, func(i, j int) bool {
		return agents[i].Name < agents[j].Name
	})

	m.Agents = agents
}

// rebuildAgentsKeepExisting rebuilds the agents list preserving existing discovered agents.
func (m *Model) rebuildAgentsKeepExisting() {
	// Extract currently discovered agents to preserve them.
	var discovered []opencode.AgentInfo
	for _, a := range m.Agents {
		if a.CurrentModel != "" || a.Mode != "" {
			discovered = append(discovered, opencode.AgentInfo{
				Name:  a.Name,
				Model: a.CurrentModel,
				Mode:  a.Mode,
			})
		}
	}
	m.rebuildAgents(discovered)
}

// resolveChain resolves the effective chain for an agent using the 3-level cascade.
func (m *Model) resolveChain(agentName string) []config.ChainEntry {
	if m.Deps.ResolveChain != nil {
		return m.Deps.ResolveChain(m.Config, agentName)
	}
	// Simple fallback: check agent-specific, then global.
	if m.Config.FallbackChains.Agents != nil {
		if chain, ok := m.Config.FallbackChains.Agents[agentName]; ok {
			return chain
		}
	}
	return m.Config.FallbackChains.Global
}

// setScreen navigates to a new screen, resetting the cursor.
func (m *Model) setScreen(s Screen) {
	m.PreviousScreen = m.Screen
	m.Screen = s
	m.Cursor = 0
	m.ScrollOffset = 0
	m.FilterText = ""
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil

	case SaveResultMsg:
		if msg.Err != nil {
			m.StatusMsg = "Error: " + msg.Err.Error()
		} else {
			m.Dirty = false
			m.Original = m.Config
			m.StatusMsg = "Saved!"
		}
		return m, clearStatusAfter(3 * time.Second)

	case ClearStatusMsg:
		m.StatusMsg = ""
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKeyPress(msg)
	}

	return m, nil
}

// handleKeyPress dispatches key events based on current screen.
func (m Model) handleKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global keybinds (always active).
	if key == keyCtrlS {
		return m.handleSave()
	}

	// Confirm quit dialog.
	if m.ConfirmQuit {
		return m.handleConfirmQuit(key)
	}

	// Help overlay.
	if m.ShowHelp {
		m.ShowHelp = false
		return m, nil
	}

	// Screen-specific handlers.
	switch m.Screen {
	case ScreenMain:
		return m.handleMainScreen(key)
	case ScreenChainEditor:
		return m.handleChainEditor(key)
	case ScreenModelPicker:
		return m.handleModelPicker(key, msg)
	case ScreenProviders:
		return m.handleProviders(key)
	}

	return m, nil
}

// handleSave saves the current config via Dependencies.
func (m Model) handleSave() (tea.Model, tea.Cmd) {
	if m.Deps.SaveConfig == nil {
		m.StatusMsg = "Error: save not available"
		return m, clearStatusAfter(3 * time.Second)
	}
	err := m.Deps.SaveConfig(m.Config)
	return m, func() tea.Msg {
		return SaveResultMsg{Err: err}
	}
}

// handleConfirmQuit handles y/n/esc in the quit confirmation dialog.
func (m Model) handleConfirmQuit(key string) (tea.Model, tea.Cmd) {
	switch key {
	case keyY:
		// Save and quit.
		if m.Deps.SaveConfig != nil {
			_ = m.Deps.SaveConfig(m.Config)
		}
		return m, tea.Quit
	case keyN:
		// Quit without saving.
		return m, tea.Quit
	case keyEsc:
		m.ConfirmQuit = false
		return m, nil
	}
	return m, nil
}

// handleMainScreen handles keys on the main screen (tabs).
func (m Model) handleMainScreen(key string) (tea.Model, tea.Cmd) {
	// When in add-agent input mode, route directly to agents tab handler.
	if m.AddingAgent {
		return m.handleAgentsTab(key)
	}

	switch key {
	case keyQuit:
		if m.Dirty {
			m.ConfirmQuit = true
			return m, nil
		}
		return m, tea.Quit

	case keyEsc:
		if m.Dirty {
			m.ConfirmQuit = true
			return m, nil
		}
		return m, tea.Quit

	case keyTab:
		m.ActiveTab = (m.ActiveTab + 1) % 2
		m.Cursor = 0
		return m, nil

	case keyShiftTab:
		m.ActiveTab = (m.ActiveTab + 1) % 2
		m.Cursor = 0
		return m, nil

	case key1:
		m.ActiveTab = 0
		m.Cursor = 0
		return m, nil

	case key2:
		m.ActiveTab = 1
		m.Cursor = 0
		return m, nil

	case keyHelp:
		m.ShowHelp = true
		return m, nil

	case keyP:
		m.setScreen(ScreenProviders)
		return m, nil
	}

	// Tab-specific navigation.
	if m.ActiveTab == 0 {
		return m.handleGlobalTab(key)
	}
	// In add-agent mode, skip global keys like 'n', 'p', '?', 'q'.
	return m.handleAgentsTab(key)
}

// handleGlobalTab handles navigation on the Global tab.
func (m Model) handleGlobalTab(key string) (tea.Model, tea.Cmd) {
	switch key {
	case keyJ, keyDown:
		if m.Cursor < maxSlots-1 {
			m.Cursor++
		}
	case keyK, keyUp:
		if m.Cursor > 0 {
			m.Cursor--
		}
	case keyEnter:
		m.EditingAgent = ""
		m.PickerSlot = m.Cursor
		m.setScreen(ScreenModelPicker)
		m.PickerSlot = m.Cursor // Restore after setScreen reset
	case keyX:
		m.clearGlobalSlot(m.Cursor)
	}
	return m, nil
}

// handleAgentsTab handles navigation on the Agents tab.
func (m Model) handleAgentsTab(key string) (tea.Model, tea.Cmd) {
	// Add agent input mode.
	if m.AddingAgent {
		return m.handleAddAgentInput(key)
	}

	switch key {
	case keyJ, keyDown:
		if m.Cursor < len(m.Agents)-1 {
			m.Cursor++
			m.adjustScrollOffset()
		}
	case keyK, keyUp:
		if m.Cursor > 0 {
			m.Cursor--
			m.adjustScrollOffset()
		}
	case keyEnter:
		if len(m.Agents) > 0 && m.Cursor < len(m.Agents) {
			m.EditingAgent = m.Agents[m.Cursor].Name
			m.setScreen(ScreenChainEditor)
		}
	case keyN:
		m.AddingAgent = true
		m.AddAgentInput = ""
	}
	return m, nil
}

// adjustScrollOffset ensures the cursor stays within the visible window.
func (m *Model) adjustScrollOffset() {
	visibleRows := m.Height - 8
	if visibleRows < 3 {
		visibleRows = 3
	}
	if m.Cursor < m.ScrollOffset {
		m.ScrollOffset = m.Cursor
	}
	if m.Cursor >= m.ScrollOffset+visibleRows {
		m.ScrollOffset = m.Cursor - visibleRows + 1
	}
}

// handleAddAgentInput handles keys when adding a new agent name.
func (m Model) handleAddAgentInput(key string) (tea.Model, tea.Cmd) {
	switch key {
	case keyEsc:
		m.AddingAgent = false
		m.AddAgentInput = ""
		return m, nil
	case keyEnter:
		name := strings.TrimSpace(m.AddAgentInput)
		if name == "" {
			// Empty input — do nothing.
			return m, nil
		}
		// Create the agent in config with empty chain.
		if m.Config.FallbackChains.Agents == nil {
			m.Config.FallbackChains.Agents = make(map[string][]config.ChainEntry)
		}
		m.Config.FallbackChains.Agents[name] = []config.ChainEntry{}
		m.Dirty = true
		m.AddingAgent = false
		m.AddAgentInput = ""
		// Rebuild agents list (pass nil for discovered — keep existing).
		m.rebuildAgentsKeepExisting()
		return m, nil
	case keyBackspace:
		if len(m.AddAgentInput) > 0 {
			m.AddAgentInput = m.AddAgentInput[:len(m.AddAgentInput)-1]
		}
		return m, nil
	}
	// Accept alphanumeric, dash, underscore.
	if len(key) == 1 {
		ch := key[0]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			m.AddAgentInput += key
		}
	}
	return m, nil
}

// handleChainEditor handles keys on the chain editor screen.
func (m Model) handleChainEditor(key string) (tea.Model, tea.Cmd) {
	switch key {
	case keyEsc:
		m.setScreen(ScreenMain)
		// Restore cursor to the editing agent's position.
		for i, a := range m.Agents {
			if a.Name == m.EditingAgent {
				m.Cursor = i
				break
			}
		}
		return m, nil

	case keyJ, keyDown:
		if m.Cursor < maxSlots-1 {
			m.Cursor++
		}
	case keyK, keyUp:
		if m.Cursor > 0 {
			m.Cursor--
		}
	case keyEnter:
		m.PickerSlot = m.Cursor
		m.setScreen(ScreenModelPicker)
		m.PickerSlot = m.Cursor // Restore
	case keyX:
		m.clearAgentSlot(m.EditingAgent, m.Cursor)
	case keyD:
		m.deleteOverride(m.EditingAgent)
	case keyO:
		m.createOverride(m.EditingAgent)
	}
	return m, nil
}

// handleModelPicker handles keys on the model picker screen.
func (m Model) handleModelPicker(key string, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key {
	case keyEsc:
		m.setScreen(ScreenChainEditor)
		if m.EditingAgent == "" {
			// Coming from global tab.
			m.Screen = ScreenMain
			m.ActiveTab = 0
			m.Cursor = m.PickerSlot
		}
		return m, nil

	case keyEnter:
		m.selectModel()
		return m, nil

	case keyBackspace:
		if len(m.FilterText) > 0 {
			m.FilterText = m.FilterText[:len(m.FilterText)-1]
			m.Cursor = 0
		}
		return m, nil

	case keyUp:
		if m.Cursor > 0 {
			m.Cursor--
		}
		return m, nil

	case keyDown:
		filtered := m.filteredModels()
		if m.Cursor < len(filtered)-1 {
			m.Cursor++
		}
		return m, nil
	}

	// ALL printable single-rune keys go to filter (including j, k).
	// Navigation is ONLY via arrow keys (fzf/telescope pattern).
	if len(key) == 1 {
		m.FilterText += key
		m.Cursor = 0
	}

	return m, nil
}

// handleProviders handles keys on the providers screen.
func (m Model) handleProviders(key string) (tea.Model, tea.Cmd) {
	switch key {
	case keyEsc, keyQuit:
		m.setScreen(ScreenMain)
	case keyJ, keyDown:
		if m.Cursor < len(m.Providers)-1 {
			m.Cursor++
		}
	case keyK, keyUp:
		if m.Cursor > 0 {
			m.Cursor--
		}
	}
	return m, nil
}

// filteredModels returns all models matching the current filter.
func (m *Model) filteredModels() []string {
	all := m.allModels()
	if m.FilterText == "" {
		return all
	}
	filter := strings.ToLower(m.FilterText)
	var filtered []string
	for _, model := range all {
		if strings.Contains(strings.ToLower(model), filter) {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

// allModels returns all available models as "provider/model" strings.
func (m *Model) allModels() []string {
	seen := make(map[string]bool)
	var models []string

	// From providers.
	for _, p := range m.Providers {
		for _, model := range p.Models {
			key := p.ID + "/" + model
			if !seen[key] {
				seen[key] = true
				models = append(models, key)
			}
		}
	}

	// From existing chains (to include custom models).
	addChainModels := func(chain []config.ChainEntry) {
		for _, e := range chain {
			key := e.Provider + "/" + e.Model
			if !seen[key] {
				seen[key] = true
				models = append(models, key)
			}
		}
	}
	addChainModels(m.Config.FallbackChains.Global)
	if m.Config.FallbackChains.Agents != nil {
		for _, chain := range m.Config.FallbackChains.Agents {
			addChainModels(chain)
		}
	}

	sort.Strings(models)
	return models
}

// selectModel applies the selected model from the picker.
func (m *Model) selectModel() {
	filtered := m.filteredModels()
	var modelStr string
	if m.Cursor < len(filtered) {
		modelStr = filtered[m.Cursor]
	} else if m.FilterText != "" {
		// Use the filter text as a custom model.
		modelStr = m.FilterText
	} else {
		return
	}

	parts := strings.SplitN(modelStr, "/", 2)
	var entry config.ChainEntry
	if len(parts) == 2 {
		entry = config.ChainEntry{Provider: parts[0], Model: parts[1]}
	} else {
		entry = config.ChainEntry{Provider: modelStr, Model: modelStr}
	}

	if m.EditingAgent == "" {
		// Global chain.
		m.setGlobalSlot(m.PickerSlot, entry)
	} else {
		// Agent chain.
		m.setAgentSlot(m.EditingAgent, m.PickerSlot, entry)
	}

	// Navigate back.
	if m.EditingAgent == "" {
		m.Screen = ScreenMain
		m.ActiveTab = 0
		m.Cursor = m.PickerSlot
	} else {
		m.Screen = ScreenChainEditor
		m.Cursor = m.PickerSlot
	}
	m.FilterText = ""
}

// setGlobalSlot sets a slot in the global chain.
func (m *Model) setGlobalSlot(slot int, entry config.ChainEntry) {
	for len(m.Config.FallbackChains.Global) <= slot {
		m.Config.FallbackChains.Global = append(m.Config.FallbackChains.Global, config.ChainEntry{})
	}
	m.Config.FallbackChains.Global[slot] = entry
	m.Dirty = true
	m.refreshAgentChains()
}

// clearGlobalSlot removes a slot from the global chain.
func (m *Model) clearGlobalSlot(slot int) {
	if slot < len(m.Config.FallbackChains.Global) {
		// Remove the entry, shift remaining entries up.
		m.Config.FallbackChains.Global = append(
			m.Config.FallbackChains.Global[:slot],
			m.Config.FallbackChains.Global[slot+1:]...,
		)
		m.Dirty = true
		m.refreshAgentChains()
	}
}

// setAgentSlot sets a slot in an agent's chain.
func (m *Model) setAgentSlot(agentName string, slot int, entry config.ChainEntry) {
	if m.Config.FallbackChains.Agents == nil {
		m.Config.FallbackChains.Agents = make(map[string][]config.ChainEntry)
	}
	chain := m.Config.FallbackChains.Agents[agentName]
	for len(chain) <= slot {
		chain = append(chain, config.ChainEntry{})
	}
	chain[slot] = entry
	m.Config.FallbackChains.Agents[agentName] = chain
	m.Dirty = true
	m.refreshAgentChains()
}

// clearAgentSlot removes a slot from an agent's chain.
func (m *Model) clearAgentSlot(agentName string, slot int) {
	if m.Config.FallbackChains.Agents == nil {
		return
	}
	chain, ok := m.Config.FallbackChains.Agents[agentName]
	if !ok || slot >= len(chain) {
		return
	}
	chain = append(chain[:slot], chain[slot+1:]...)
	if len(chain) == 0 {
		delete(m.Config.FallbackChains.Agents, agentName)
	} else {
		m.Config.FallbackChains.Agents[agentName] = chain
	}
	m.Dirty = true
	m.refreshAgentChains()
}

// deleteOverride removes the agent-specific chain override.
func (m *Model) deleteOverride(agentName string) {
	if m.Config.FallbackChains.Agents != nil {
		delete(m.Config.FallbackChains.Agents, agentName)
		m.Dirty = true
		m.refreshAgentChains()
	}
}

// createOverride creates an agent-specific chain by copying the global chain.
func (m *Model) createOverride(agentName string) {
	if m.Config.FallbackChains.Agents == nil {
		m.Config.FallbackChains.Agents = make(map[string][]config.ChainEntry)
	}
	// Copy global chain as starting point.
	global := m.Config.FallbackChains.Global
	chain := make([]config.ChainEntry, len(global))
	copy(chain, global)
	m.Config.FallbackChains.Agents[agentName] = chain
	m.Dirty = true
	m.refreshAgentChains()
}

// refreshAgentChains updates the resolved chains for all agents.
func (m *Model) refreshAgentChains() {
	for i := range m.Agents {
		_, hasOverride := m.Config.FallbackChains.Agents[m.Agents[i].Name]
		m.Agents[i].HasOverride = hasOverride
		m.Agents[i].Chain = m.resolveChain(m.Agents[i].Name)
	}
}

// View implements tea.Model.
func (m Model) View() tea.View {
	var content string
	if m.Width == 0 {
		content = "Loading..."
	} else if m.ConfirmQuit {
		content = screens.RenderConfirmQuit(m.Width)
	} else if m.ShowHelp {
		content = screens.RenderHelp(int(m.Screen), m.ActiveTab, m.Width, m.Height)
	} else {
		switch m.Screen {
		case ScreenMain:
			content = m.renderMain()
		case ScreenChainEditor:
			content = m.renderChainEditor()
		case ScreenModelPicker:
			content = m.renderModelPicker()
		case ScreenProviders:
			content = m.renderProviders()
		}
	}

	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

// viewContent returns the rendered string content (used by tests).
func (m Model) viewContent() string {
	if m.Width == 0 {
		return "Loading..."
	}
	if m.ConfirmQuit {
		return screens.RenderConfirmQuit(m.Width)
	}
	if m.ShowHelp {
		return screens.RenderHelp(int(m.Screen), m.ActiveTab, m.Width, m.Height)
	}
	switch m.Screen {
	case ScreenMain:
		return m.renderMain()
	case ScreenChainEditor:
		return m.renderChainEditor()
	case ScreenModelPicker:
		return m.renderModelPicker()
	case ScreenProviders:
		return m.renderProviders()
	}
	return ""
}

// renderMain renders the tabbed main view.
func (m Model) renderMain() string {
	header := screens.RenderHeader(m.ActiveTab, m.Dirty, m.StatusMsg, m.Width)
	var content string
	switch m.ActiveTab {
	case 0:
		content = screens.RenderGlobal(m.Config.FallbackChains.Global, m.Cursor, m.Width, m.toScreenProviders())
	case 1:
		content = screens.RenderAgents(m.toScreenAgents(), m.Cursor, m.Width, m.ScrollOffset, m.Height)
		if m.AddingAgent {
			content += screens.RenderAddAgentInput(m.AddAgentInput, m.Width)
		}
	}
	// Build scroll indicator for agents tab.
	var scrollInfo string
	if m.ActiveTab == 1 && len(m.Agents) > 0 {
		visibleRows := m.Height - 8
		if visibleRows < 3 {
			visibleRows = 3
		}
		if m.Height > 0 && len(m.Agents) > visibleRows {
			scrollInfo = screens.RenderScrollIndicator(m.ScrollOffset, len(m.Agents), visibleRows)
		}
	}
	footer := screens.RenderFooter(int(m.Screen), m.ActiveTab, m.Width, scrollInfo)
	return header + "\n" + content + "\n" + footer
}

// renderChainEditor renders the chain editor for the current agent.
func (m Model) renderChainEditor() string {
	var agent AgentDisplay
	for _, a := range m.Agents {
		if a.Name == m.EditingAgent {
			agent = a
			break
		}
	}
	return screens.RenderChainEditor(
		m.EditingAgent,
		agent.CurrentModel,
		agent.Chain,
		m.Config.FallbackChains.Global,
		agent.HasOverride,
		m.Cursor,
		m.Dirty,
		m.StatusMsg,
		m.Width,
		m.toScreenProviders(),
	)
}

// renderModelPicker renders the model picker.
func (m Model) renderModelPicker() string {
	return screens.RenderModelPicker(
		m.toScreenProviders(),
		m.filteredModels(),
		m.FilterText,
		m.Cursor,
		m.Width,
		m.Height,
	)
}

// renderProviders renders the providers view.
func (m Model) renderProviders() string {
	return screens.RenderProviders(m.toScreenProviders(), m.Cursor, m.Width)
}

// toScreenProviders converts tui.ProviderInfo to screens.ProviderInfo.
func (m Model) toScreenProviders() []screens.ProviderInfo {
	sp := make([]screens.ProviderInfo, len(m.Providers))
	for i, p := range m.Providers {
		sp[i] = screens.ProviderInfo{
			ID:          p.ID,
			DisplayName: p.DisplayName,
			BaseURL:     p.BaseURL,
			Available:   p.Available,
			Models:      p.Models,
		}
	}
	return sp
}

// toScreenAgents converts tui.AgentDisplay to screens.AgentDisplay.
func (m Model) toScreenAgents() []screens.AgentDisplay {
	sa := make([]screens.AgentDisplay, len(m.Agents))
	for i, a := range m.Agents {
		sa[i] = screens.AgentDisplay{
			Name:         a.Name,
			CurrentModel: a.CurrentModel,
			Mode:         a.Mode,
			HasOverride:  a.HasOverride,
			Chain:        a.Chain,
		}
	}
	return sa
}

// ProviderAvailable returns availability info for a provider/model combo.
func (m *Model) ProviderAvailable(providerID string) bool {
	for _, p := range m.Providers {
		if p.ID == providerID {
			return p.Available
		}
	}
	return false
}
