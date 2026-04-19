package tui

// Screen identifies which screen is currently active.
type Screen int

const (
	ScreenMain        Screen = iota // Tab view: Global / Agents
	ScreenChainEditor               // Editing 4 slots of an agent
	ScreenModelPicker               // Modal: selecting provider/model
	ScreenProviders                  // Viewing detected providers
	ScreenStatus                     // System status: bridge + auth
)

// Route defines forward/backward navigation for a screen.
type Route struct {
	Backward Screen
}

// linearRoutes maps each screen to its navigation target.
var linearRoutes = map[Screen]Route{
	ScreenMain:        {},
	ScreenChainEditor: {Backward: ScreenMain},
	ScreenModelPicker: {Backward: ScreenChainEditor},
	ScreenProviders:   {Backward: ScreenMain},
	ScreenStatus:      {Backward: ScreenMain},
}

// backScreen returns the screen to navigate to when going backward.
func backScreen(current Screen) Screen {
	if route, ok := linearRoutes[current]; ok {
		return route.Backward
	}
	return ScreenMain
}
