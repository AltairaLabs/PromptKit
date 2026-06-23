package app

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Activatable is implemented by pages that need to push async messages into
// the running bubbletea program. The App calls Activate(send) when the page
// becomes the active/top page: at root startup (in App.Init) and on push.
// send is *tea.Program.Send — a thread-safe delivery channel. The returned
// tea.Cmd is an optional startup command; it is batched with the page's Init
// cmd by the App.
type Activatable interface {
	Activate(send func(tea.Msg)) tea.Cmd
}
