package analyze

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const numTabs = 5

var tabNames = [numTabs]string{"Overview", "Commands", "Categories", "Directories", "Activity"}

var (
	styleTabActive   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styleTabInactive = lipgloss.NewStyle().Faint(true)
	styleHeader      = lipgloss.NewStyle().Bold(true)
	styleBar         = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleDim         = lipgloss.NewStyle().Faint(true)
	styleSep         = lipgloss.NewStyle().Faint(true)
)

// heatmapColors maps activity levels to 256-color terminal colors.
var heatmapColors = []struct {
	min   int
	color lipgloss.Color
	cell  string
}{
	{0, "235", "· "},
	{1, "22", "██"},
	{3, "28", "██"},
	{6, "34", "██"},
	{11, "40", "██"},
	{21, "46", "██"},
}

type viewModel struct {
	stats     Stats
	byProgram bool
	activeTab int
	offsets   [numTabs]int
	width     int
	height    int
}

// Run launches the analyze TUI in altscreen mode.
func Run(stats Stats, byProgram bool) error {
	m := viewModel{
		stats:     stats,
		byProgram: byProgram,
		width:     80,
		height:    24,
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithInputTTY())
	_, err := p.Run()
	return err
}

func (m viewModel) Init() tea.Cmd { return nil }

func (m viewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "Q", "ctrl+c":
			return m, tea.Quit
		case "1", "2", "3", "4", "5":
			m.activeTab = int(msg.String()[0]-'1')
		case "tab":
			m.activeTab = (m.activeTab + 1) % numTabs
		case "shift+tab":
			m.activeTab = (m.activeTab - 1 + numTabs) % numTabs
		case "j", "down":
			m.offsets[m.activeTab]++
		case "k", "up":
			if m.offsets[m.activeTab] > 0 {
				m.offsets[m.activeTab]--
			}
		case "g":
			m.offsets[m.activeTab] = 0
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return m, nil
}

func (m viewModel) View() string {
	var b strings.Builder
	b.WriteString(m.renderTabBar())
	b.WriteByte('\n')
	contentHeight := m.height - 3 // tab bar + blank line + help
	switch m.activeTab {
	case 0:
		b.WriteString(m.renderOverview(contentHeight))
	case 1:
		b.WriteString(m.renderCommands(contentHeight))
	case 2:
		b.WriteString(m.renderCategories(contentHeight))
	case 3:
		b.WriteString(m.renderDirectories(contentHeight))
	case 4:
		b.WriteString(m.renderActivity(contentHeight))
	}
	b.WriteByte('\n')
	b.WriteString(m.renderHelp())
	return b.String()
}

func (m viewModel) renderTabBar() string {
	var b strings.Builder
	for i, name := range tabNames {
		label := fmt.Sprintf(" %d:%s ", i+1, name)
		if i == m.activeTab {
			b.WriteString(styleTabActive.Render(label))
		} else {
			b.WriteString(styleTabInactive.Render(label))
		}
		if i < numTabs-1 {
			b.WriteString(styleSep.Render("│"))
		}
	}
	return b.String()
}

func (m viewModel) renderHelp() string {
	return styleDim.Render("  q:quit  tab:next  1-5:jump  j/k:scroll  g:top")
}

// renderOverview renders the Overview tab.
func (m viewModel) renderOverview(height int) string {
	s := m.stats
	var b strings.Builder

	if s.TotalCommands == 0 {
		b.WriteString("\n  (no data)\n")
		return b.String()
	}

	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  %-22s %d\n", "Total commands:", s.TotalCommands))
	b.WriteString(fmt.Sprintf("  %-22s %d\n", "Unique commands:", s.UniqueCommands))
	b.WriteString(fmt.Sprintf("  %-22s %d\n", "Unique programs:", s.UniquePrograms))
	b.WriteString(fmt.Sprintf("  %-22s %s\n", "First seen:", s.FirstSeen.Format("Jan 02, 2006")))
	b.WriteString(fmt.Sprintf("  %-22s %s\n", "Last seen:", s.LastSeen.Format("Jan 02, 2006")))
	b.WriteString(fmt.Sprintf("  %-22s %.1f\n", "Avg commands/day:", s.AvgPerDay))

	b.WriteString("\n")
	b.WriteString(styleHeader.Render("  Hourly Distribution") + "\n\n")
	b.WriteString(m.renderHourly())

	return b.String()
}

// renderHourly renders a compact hourly sparkline.
func (m viewModel) renderHourly() string {
	maxVal := 1
	for _, v := range m.stats.HourlyDist {
		if v > maxVal {
			maxVal = v
		}
	}

	barChars := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	b.WriteString("  ")
	for _, v := range m.stats.HourlyDist {
		idx := 0
		if maxVal > 0 && v > 0 {
			idx = 1 + int(float64(v)/float64(maxVal)*float64(len(barChars)-1))
			if idx >= len(barChars) {
				idx = len(barChars) - 1
			}
		}
		b.WriteRune(barChars[idx])
	}
	b.WriteByte('\n')
	b.WriteString("  0                   12                  23\n")
	return b.String()
}

// renderCommands renders the Commands tab (top commands or programs).
func (m viewModel) renderCommands(height int) string {
	var b strings.Builder
	b.WriteString("\n")

	items := m.stats.TopCommands
	title := "Top Commands"
	labelWidth := 40
	if m.byProgram {
		items = m.stats.TopPrograms
		title = "Top Programs"
		labelWidth = 20
	}

	b.WriteString(styleHeader.Render("  "+title) + "\n\n")
	if len(items) == 0 {
		b.WriteString("  (no data)\n")
		return b.String()
	}

	barMax := m.width - labelWidth - 20
	if barMax < 10 {
		barMax = 10
	}
	offset := m.offsets[1]
	if offset > len(items) {
		offset = len(items)
	}
	visible := items[offset:]
	maxShow := height - 4
	if maxShow < 1 {
		maxShow = 1
	}
	if len(visible) > maxShow {
		visible = visible[:maxShow]
	}

	b.WriteString(m.renderBarList(visible, offset, m.stats.TotalCommands, labelWidth, barMax))
	return b.String()
}

// renderCategories renders the Categories tab.
func (m viewModel) renderCategories(height int) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(styleHeader.Render("  Top Categories") + "\n\n")

	items := m.stats.TopCategories
	if len(items) == 0 {
		b.WriteString("  (no data)\n")
		return b.String()
	}

	barMax := m.width - 40
	if barMax < 10 {
		barMax = 10
	}
	offset := m.offsets[2]
	if offset > len(items) {
		offset = len(items)
	}
	visible := items[offset:]
	maxShow := height - 4
	if maxShow < 1 {
		maxShow = 1
	}
	if len(visible) > maxShow {
		visible = visible[:maxShow]
	}

	b.WriteString(m.renderBarList(visible, offset, m.stats.TotalCommands, 15, barMax))
	return b.String()
}

// renderDirectories renders the Directories tab.
func (m viewModel) renderDirectories(height int) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(styleHeader.Render("  Top Directories") + "\n\n")

	items := m.stats.TopDirs
	if len(items) == 0 {
		b.WriteString("  (no data)\n")
		return b.String()
	}

	barMax := m.width - 50
	if barMax < 10 {
		barMax = 10
	}
	offset := m.offsets[3]
	if offset > len(items) {
		offset = len(items)
	}
	visible := items[offset:]
	maxShow := height - 4
	if maxShow < 1 {
		maxShow = 1
	}
	if len(visible) > maxShow {
		visible = visible[:maxShow]
	}

	b.WriteString(m.renderBarList(visible, offset, m.stats.TotalCommands, 35, barMax))
	return b.String()
}

// renderBarList renders a ranked bar chart from a slice of Freq entries.
// offset is the rank of the first item (0-indexed), used for display numbering.
func (m viewModel) renderBarList(items []Freq, offset, total, labelWidth, barMax int) string {
	if len(items) == 0 {
		return ""
	}
	maxCount := items[0].Count

	var b strings.Builder
	for i, f := range items {
		rank := fmt.Sprintf("%3d.", offset+i+1)

		label := f.Label
		if len(label) > labelWidth {
			label = label[:labelWidth-1] + "…"
		}
		label = fmt.Sprintf("%-*s", labelWidth, label)

		barLen := 0
		if maxCount > 0 {
			barLen = int(float64(f.Count) / float64(maxCount) * float64(barMax))
		}
		bar := styleBar.Render(strings.Repeat("█", barLen))

		pct := 0.0
		if total > 0 {
			pct = float64(f.Count) / float64(total) * 100
		}

		b.WriteString(fmt.Sprintf("  %s %s %s %d (%.1f%%)\n",
			rank, label, bar, f.Count, pct))
	}
	return b.String()
}

// renderActivity renders the Activity tab with a GitHub-style heatmap.
func (m viewModel) renderActivity(height int) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(styleHeader.Render("  Activity Heatmap") + "\n\n")

	// 3 chars for day labels, 2 chars per week cell.
	maxWeeks := (m.width - 3) / 2
	if maxWeeks > 52 {
		maxWeeks = 52
	}
	if maxWeeks < 1 {
		b.WriteString("  (terminal too narrow)\n")
		return b.String()
	}

	// Align start to the Monday maxWeeks weeks ago.
	now := time.Now()
	weekday := int(now.Weekday()) // 0=Sun
	if weekday == 0 {
		weekday = 7
	}
	monday := now.AddDate(0, 0, -(weekday - 1))
	monday = time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, time.Local)
	start := monday.AddDate(0, 0, -(maxWeeks-1)*7)

	// Month label row.
	b.WriteString(renderMonthRow(start, maxWeeks))

	// Day rows: Mon=0 … Sun=6.
	dayLabels := [7]string{"Mo ", "Tu ", "We ", "Th ", "Fr ", "Sa ", "Su "}
	for day := 0; day < 7; day++ {
		b.WriteString(dayLabels[day])
		for week := 0; week < maxWeeks; week++ {
			d := start.AddDate(0, 0, week*7+day)
			count := m.stats.DailyDist[d.Format("2006-01-02")]
			b.WriteString(heatmapCell(count))
		}
		b.WriteByte('\n')
	}

	// Legend.
	b.WriteString("\n  Less ")
	for _, h := range heatmapColors {
		b.WriteString(lipgloss.NewStyle().Foreground(h.color).Render(h.cell))
	}
	b.WriteString(" More\n")

	return b.String()
}

// renderMonthRow builds the month-label header line for the heatmap.
func renderMonthRow(start time.Time, numWeeks int) string {
	// Buffer: 3 (day col) + 2*numWeeks chars, all spaces.
	buf := make([]byte, 3+2*numWeeks)
	for i := range buf {
		buf[i] = ' '
	}
	lastMonth := -1
	for w := 0; w < numWeeks; w++ {
		d := start.AddDate(0, 0, w*7)
		if int(d.Month()) != lastMonth {
			abbr := d.Format("Jan")
			pos := 3 + w*2
			for i := 0; i < len(abbr) && pos+i < len(buf); i++ {
				buf[pos+i] = abbr[i]
			}
			lastMonth = int(d.Month())
		}
	}
	return string(buf) + "\n"
}

// heatmapCell returns a lipgloss-colored 2-char cell string for the given count.
func heatmapCell(count int) string {
	selected := heatmapColors[0]
	for _, h := range heatmapColors {
		if count >= h.min {
			selected = h
		}
	}
	return lipgloss.NewStyle().Foreground(selected.color).Render(selected.cell)
}
