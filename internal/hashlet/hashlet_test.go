package hashlet

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/mwirges/ghistx/internal/db"
	"github.com/mwirges/ghistx/internal/index"
)

// --- MinLen tests (pure unit, no DB) ---

func TestMinLenEmpty(t *testing.T) {
	if got := MinLen(nil); got != 4 {
		t.Errorf("MinLen(nil) = %d, want 4", got)
	}
}

func TestMinLenSingle(t *testing.T) {
	h := strings.Repeat("a", 64)
	if got := MinLen([]string{h}); got != 4 {
		t.Errorf("MinLen(single) = %d, want 4", got)
	}
}

func TestMinLenDistinctAt4(t *testing.T) {
	// Two hashes that differ at position 3 (0-indexed) — prefix of length 4 disambiguates.
	h1 := "aaa0" + strings.Repeat("0", 60)
	h2 := "aaa1" + strings.Repeat("0", 60)
	if got := MinLen([]string{h1, h2}); got != 4 {
		t.Errorf("MinLen(differ@3) = %d, want 4", got)
	}
}

func TestMinLenCollisionAt4(t *testing.T) {
	// Two hashes sharing the first 5 characters — needs length 6.
	h1 := "aaaaa0" + strings.Repeat("0", 58)
	h2 := "aaaaa1" + strings.Repeat("0", 58)
	if got := MinLen([]string{h1, h2}); got != 6 {
		t.Errorf("MinLen(collision@5) = %d, want 6", got)
	}
}

func TestMinLenIdentical(t *testing.T) {
	// Two identical hashes — no prefix can disambiguate; returns 64.
	h := strings.Repeat("a", 64)
	if got := MinLen([]string{h, h}); got != 64 {
		t.Errorf("MinLen(identical) = %d, want 64", got)
	}
}

func TestMinLenMany(t *testing.T) {
	// Ten hashes all unique from char 0 — result is still >= 4.
	hashes := make([]string, 10)
	for i := range hashes {
		hashes[i] = string(rune('0'+i)) + strings.Repeat("0", 63)
	}
	got := MinLen(hashes)
	if got < 4 {
		t.Errorf("MinLen(many) = %d, want >= 4", got)
	}
}

// --- FindByPrefix tests (require DB) ---

func TestFindByPrefixFound(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	if err := index.Cmd(d, "git status", "/"); err != nil {
		t.Fatalf("index.Cmd: %v", err)
	}

	// Retrieve the real hash so we can build a valid prefix.
	rows, err := d.Query(`SELECT hash FROM cmdraw LIMIT 1`)
	if err != nil {
		t.Fatalf("query hash: %v", err)
	}
	var hash string
	if rows.Next() {
		rows.Scan(&hash)
	}
	rows.Close()

	prefix := hash[:6]
	hit, err := FindByPrefix(d, prefix)
	if err != nil {
		t.Fatalf("FindByPrefix: %v", err)
	}
	if hit.Cmd != "git status" {
		t.Errorf("Cmd = %q, want \"git status\"", hit.Cmd)
	}
}

func TestFindByPrefixNotFound(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	_, err = FindByPrefix(d, "abcd1234")
	if err == nil {
		t.Fatal("expected error for not-found prefix")
	}
	if !strings.Contains(err.Error(), "no command found") {
		t.Errorf("error = %q, want to contain \"no command found\"", err.Error())
	}
}

func TestFindByPrefixAmbiguous(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	// Insert two rows with hand-crafted hashes sharing a known prefix.
	b64cmd1 := base64.StdEncoding.EncodeToString([]byte("cmd one"))
	b64cmd2 := base64.StdEncoding.EncodeToString([]byte("cmd two"))
	h1 := "aabb0000" + strings.Repeat("0", 56)
	h2 := "aabb1111" + strings.Repeat("1", 56)
	_, err = d.Exec(`INSERT INTO cmdraw(hash, ts, cmd) VALUES(?, 1, ?)`, h1, b64cmd1)
	if err != nil {
		t.Fatalf("insert h1: %v", err)
	}
	_, err = d.Exec(`INSERT INTO cmdraw(hash, ts, cmd) VALUES(?, 2, ?)`, h2, b64cmd2)
	if err != nil {
		t.Fatalf("insert h2: %v", err)
	}

	_, err = FindByPrefix(d, "aabb")
	if err == nil {
		t.Fatal("expected error for ambiguous prefix")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error = %q, want to contain \"ambiguous\"", err.Error())
	}
}

func TestFindByPrefixInvalidHex(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	_, err = FindByPrefix(d, "xyz!")
	if err == nil {
		t.Fatal("expected error for non-hex prefix")
	}
}

func TestFindByPrefixUppercase(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	if err := index.Cmd(d, "make build", "/"); err != nil {
		t.Fatalf("index.Cmd: %v", err)
	}

	var hash string
	rows, _ := d.Query(`SELECT hash FROM cmdraw LIMIT 1`)
	if rows.Next() {
		rows.Scan(&hash)
	}
	rows.Close()

	// Pass uppercase version of the prefix.
	prefix := strings.ToUpper(hash[:6])
	hit, err := FindByPrefix(d, prefix)
	if err != nil {
		t.Fatalf("FindByPrefix (uppercase): %v", err)
	}
	if hit.Cmd != "make build" {
		t.Errorf("Cmd = %q, want \"make build\"", hit.Cmd)
	}
}

func TestFindByPrefixEmptyPrefix(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	_, err = FindByPrefix(d, "")
	if err == nil {
		t.Fatal("expected error for empty prefix")
	}
}
