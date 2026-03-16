package explore

import "github.com/charmbracelet/lipgloss"

// Terminal color codes matching the C histx ANSI definitions:
//   PRETTY_GREY   "\e[0;37m"  -> color 7
//   PRETTY_SELECT "\e[46m"    -> bg color 6 (cyan)
//   PRETTY_PRUNE  "\e[45m"    -> bg color 5 (magenta)
//   PRETTY_PRUNEHOV "\e[4;45m"-> bg color 5 + underline
//   PRETTY_DIM    "\e[2m"

var (
	styleNormal = lipgloss.NewStyle().
			Foreground(lipgloss.Color("7"))

	styleSelected = lipgloss.NewStyle().
			Background(lipgloss.Color("6")).
			Foreground(lipgloss.Color("0"))

	stylePrune = lipgloss.NewStyle().
			Background(lipgloss.Color("5")).
			Foreground(lipgloss.Color("0"))

	stylePruneSelected = lipgloss.NewStyle().
				Background(lipgloss.Color("5")).
				Foreground(lipgloss.Color("0")).
				Underline(true)

	styleDim = lipgloss.NewStyle().Faint(true)

	stylePrompt = lipgloss.NewStyle().Bold(true)
)

// renderLine returns a styled line for result index i.
// isPrune=true means the entry is marked for pruning.
// isSelected=true means the cursor is on this entry.
func renderLine(text string, isPrune, isSelected bool) string {
	switch {
	case isPrune && isSelected:
		return stylePruneSelected.Render(text)
	case isPrune:
		return stylePrune.Render(text)
	case isSelected:
		return styleSelected.Render(text)
	default:
		return styleNormal.Render(text)
	}
}
