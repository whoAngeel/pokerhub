package tui

import "charm.land/bubbles/v2/key"

// keyMap is the spectator control surface (docs/TUI_PLAN.md T2): table
// pacing (space/n/+/-), the side panel (tab/1/2/3/p) and help/quit. It
// implements bubbles/help's KeyMap interface so the footer can render
// both the short one-liner and the "?"-toggled full grid from the same
// bindings -- no separate help text to keep in sync.
type keyMap struct {
	Pause       key.Binding
	Step        key.Binding
	SpeedUp     key.Binding
	SpeedDown   key.Binding
	Tab         key.Binding
	Tab1        key.Binding
	Tab2        key.Binding
	Tab3        key.Binding
	TogglePanel key.Binding
	Up          key.Binding
	Down        key.Binding
	Help        key.Binding
	Quit        key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Pause: key.NewBinding(
			key.WithKeys(" "),
			key.WithHelp("espacio", "pausa/sigue"),
		),
		Step: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "paso"),
		),
		SpeedUp: key.NewBinding(
			key.WithKeys("+", "="),
			key.WithHelp("+", "x2 velocidad"),
		),
		SpeedDown: key.NewBinding(
			key.WithKeys("-", "_"),
			key.WithHelp("-", "x0.5 velocidad"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "panel"),
		),
		Tab1: key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "chat")),
		Tab2: key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "razonamiento")),
		Tab3: key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "memoria")),
		TogglePanel: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "mostrar/ocultar panel"),
		),
		Up: key.NewBinding(
			key.WithKeys("up", "k", "pgup"),
			key.WithHelp("↑/pgup", "desplazar"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j", "pgdown"),
			key.WithHelp("↓/pgdown", "desplazar"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "ayuda"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "salir"),
		),
	}
}

// ShortHelp implements help.KeyMap: the single-line footer.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Pause, k.Step, k.SpeedUp, k.SpeedDown, k.Tab, k.TogglePanel, k.Help, k.Quit}
}

// FullHelp implements help.KeyMap: the "?"-toggled grid.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Pause, k.Step, k.SpeedUp, k.SpeedDown},
		{k.Tab, k.Tab1, k.Tab2, k.Tab3, k.TogglePanel},
		{k.Up, k.Down, k.Help, k.Quit},
	}
}
