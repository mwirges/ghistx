// Package display renders formatted, color-styled history lines.
//
// Color detection is done via a lipgloss Renderer bound to the reference
// writer, so output is plain text when written to a pipe and colored when
// written to a terminal.
package display

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mwirges/ghistx/internal/find"
	"github.com/mwirges/ghistx/internal/hashlet"
	"github.com/mwirges/ghistx/internal/util"
)

type styles struct {
	hashlet   lipgloss.Style
	source    lipgloss.Style
	timestamp lipgloss.Style
	command   lipgloss.Style
	cwd       lipgloss.Style
}

func newStyles(r *lipgloss.Renderer) styles {
	return styles{
		hashlet:   r.NewStyle().Foreground(lipgloss.Color("3")),          // yellow
		source:    r.NewStyle().Foreground(lipgloss.Color("4")).Faint(true), // blue/dim
		timestamp: r.NewStyle().Faint(true),
		command:   r.NewStyle().Bold(true),
		cwd:       r.NewStyle().Faint(true),
	}
}

// Render formats hits into a single string. colorRef is used solely to detect
// terminal capabilities for color output — pass os.Stdout when the result will
// be piped to a pager so that colors are based on the real terminal.
func Render(hits []find.Hit, colorRef io.Writer) string {
	if len(hits) == 0 {
		return ""
	}
	hashes := make([]string, len(hits))
	for i, h := range hits {
		hashes[i] = h.Hash
	}
	prefixLen := hashlet.MinLen(hashes)
	r := lipgloss.NewRenderer(colorRef)
	s := newStyles(r)

	var sb strings.Builder
	for _, h := range hits {
		when := util.FormatRelative(h.TS)
		line := s.hashlet.Render(h.Hash[:prefixLen]) + " "
		if h.Source != "" {
			line += s.source.Render("["+h.Source+"]") + " "
		}
		line += s.timestamp.Render("["+when+"]") + " " +
			s.command.Render(h.Cmd)
		if h.CWD != "" {
			line += " " + s.cwd.Render("("+h.CWD+")")
		}
		sb.WriteString(line + "\n")
	}
	return sb.String()
}

// PrintHits writes formatted hits to w, using w itself for color detection.
func PrintHits(w io.Writer, hits []find.Hit) error {
	_, err := fmt.Fprint(w, Render(hits, w))
	return err
}
