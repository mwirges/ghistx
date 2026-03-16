//go:build !unix

package explore

import "fmt"

// injectTIOCSTI is not available on non-Unix platforms.
// It always returns an error so the caller falls back to printing to stdout.
func injectTIOCSTI(cmd string) error {
	return fmt.Errorf("tiocsti: not supported on this platform")
}
