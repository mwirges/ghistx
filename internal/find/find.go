// Package find implements the "find" subcommand.
//
// Search strategy mirrors the C histx find_cmd:
//   - If all keywords are ≤ 2 chars (or no ngrams hit), fall back to an
//     Aho-Corasick scan of all commands ordered by ts DESC.
//   - If any keyword is > 2 chars, use n-gram lookup against cmdlut,
//     ranked by n-gram match count.
package find

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/cloudflare/ahocorasick"
	"github.com/mwirges/ghistx/internal/ngram"
)

// Hit is a single search result.
type Hit struct {
	Hash           string
	Cmd            string // decoded from base64
	CWD            string // decoded from base64, may be empty
	TS             int64  // milliseconds since epoch
	AnnotationType int    // 0=normal, 1=prune-marked
}

// Result carries search hits plus a flag indicating whether a CWD-local
// search returned no results and fell back to the full global history.
type Result struct {
	Hits     []Hit
	IsGlobal bool // true when cwdFilter was set but returned nothing; fell back
}

// Cmd searches the database for commands matching the given keywords.
// It returns up to limit results.
//
// cwdFilter is the raw (decoded) cwd path to restrict results to. When empty,
// all results are returned (global search). When non-empty and no local results
// are found, the search automatically falls back to global history and
// Result.IsGlobal is set to true.
func Cmd(db *sql.DB, keywords []string, limit int, cwdFilter string) (Result, error) {
	if limit <= 0 {
		limit = 5
	}

	if cwdFilter != "" {
		hits, err := search(db, keywords, limit, cwdFilter)
		if err != nil || len(hits) > 0 {
			return Result{Hits: hits}, err
		}
		// No local results — fall back to global.
		hits, err = search(db, keywords, limit, "")
		return Result{Hits: hits, IsGlobal: true}, err
	}

	hits, err := search(db, keywords, limit, "")
	return Result{Hits: hits}, err
}

// search is the internal routing layer: it chooses ngram vs ACS path and
// applies the optional CWD filter.
func search(db *sql.DB, keywords []string, limit int, cwdFilter string) ([]Hit, error) {
	universe := true
	allEmpty := true
	for _, kw := range keywords {
		if len(kw) > 0 {
			allEmpty = false
		}
		if len(kw) > 2 {
			universe = false
		}
	}

	if allEmpty {
		return nil, nil
	}

	if universe {
		return acsSearch(db, keywords, limit, cwdFilter)
	}

	hits, err := ngramSearch(db, keywords, limit, cwdFilter)
	if err != nil {
		return nil, err
	}
	// If ngram search returned no results, fall back to ACS (handles ≤2-char
	// keywords that were mixed in with long keywords but had no ngram hits).
	if len(hits) == 0 {
		return acsSearch(db, keywords, limit, cwdFilter)
	}
	return hits, nil
}

// ngramSearch uses the cmdlut index for keywords longer than 2 characters.
func ngramSearch(db *sql.DB, keywords []string, limit int, cwdFilter string) ([]Hit, error) {
	// Collect all n-grams from all keywords.
	var grams []uint32
	for _, kw := range keywords {
		if len(kw) > 2 {
			grams = append(grams, ngram.Gen(kw)...)
		}
	}
	if len(grams) == 0 {
		return nil, nil
	}

	// Build: SELECT ... WHERE ngram IN (?,?,?) [AND r.cwd = ?] GROUP BY hash ORDER BY rank DESC ...
	placeholders := strings.Repeat("?,", len(grams))
	placeholders = placeholders[:len(placeholders)-1] // trim trailing comma

	query := fmt.Sprintf(`
		SELECT l.hash, COUNT(l.hash) AS rank, r.cmd, r.ts,
		       COALESCE(a.type, 0) AS atype, r.cwd
		FROM cmdlut AS l
		INNER JOIN cmdraw AS r ON l.hash = r.hash
		LEFT OUTER JOIN cmdan AS a ON r.hash = a.hash
		WHERE l.ngram IN (%s)
	`, placeholders)

	args := make([]any, len(grams))
	for i, g := range grams {
		args[i] = g
	}

	if cwdFilter != "" {
		b64filter := base64.StdEncoding.EncodeToString([]byte(cwdFilter))
		query += " AND r.cwd = ?"
		args = append(args, b64filter)
	}

	query += fmt.Sprintf(`
		GROUP BY l.hash
		ORDER BY rank DESC, r.ts DESC
		LIMIT %d
	`, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("find: ngram query: %w", err)
	}
	defer rows.Close()

	return scanHits(rows)
}

// acsSearch performs a full table scan using Aho-Corasick string matching.
func acsSearch(db *sql.DB, keywords []string, limit int, cwdFilter string) ([]Hit, error) {
	strs := keywordsToStrings(keywords)
	var matcher *ahocorasick.Matcher
	if len(strs) > 0 {
		matcher = ahocorasick.NewStringMatcher(strs)
	}

	query := `
		SELECT r.hash, r.cmd, r.ts, COALESCE(a.type, 0) AS atype, r.cwd
		FROM cmdraw AS r
		LEFT OUTER JOIN cmdan AS a ON r.hash = a.hash`

	var args []any
	if cwdFilter != "" {
		b64filter := base64.StdEncoding.EncodeToString([]byte(cwdFilter))
		query += " WHERE r.cwd = ?"
		args = append(args, b64filter)
	}
	query += " ORDER BY r.ts DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("find: acs query: %w", err)
	}
	defer rows.Close()

	var hits []Hit
	for rows.Next() {
		var hash, b64cmd string
		var b64cwd sql.NullString
		var ts int64
		var atype int
		if err := rows.Scan(&hash, &b64cmd, &ts, &atype, &b64cwd); err != nil {
			return nil, fmt.Errorf("find: acs scan: %w", err)
		}
		cmd, err := base64.StdEncoding.DecodeString(b64cmd)
		if err != nil {
			continue
		}

		if matcher != nil {
			hits2 := matcher.Match(cmd)
			if len(hits2) == 0 {
				continue
			}
		}

		cwd := ""
		if b64cwd.Valid && b64cwd.String != "" {
			c, err := base64.StdEncoding.DecodeString(b64cwd.String)
			if err == nil {
				cwd = string(c)
			}
		}

		hits = append(hits, Hit{
			Hash:           hash,
			Cmd:            string(cmd),
			CWD:            cwd,
			TS:             ts,
			AnnotationType: atype,
		})
		if len(hits) >= limit {
			break
		}
	}
	return hits, rows.Err()
}

func keywordsToStrings(kws []string) []string {
	out := make([]string, 0, len(kws))
	for _, kw := range kws {
		if len(kw) > 0 {
			out = append(out, kw)
		}
	}
	return out
}

// scanHits decodes rows from the ngram SELECT into []Hit.
func scanHits(rows *sql.Rows) ([]Hit, error) {
	var hits []Hit
	for rows.Next() {
		var hash, b64cmd string
		var rank int
		var b64cwd sql.NullString
		var ts int64
		var atype int
		if err := rows.Scan(&hash, &rank, &b64cmd, &ts, &atype, &b64cwd); err != nil {
			return nil, fmt.Errorf("find: scan row: %w", err)
		}

		cmd, err := base64.StdEncoding.DecodeString(b64cmd)
		if err != nil {
			continue
		}
		cwd := ""
		if b64cwd.Valid && b64cwd.String != "" {
			c, err := base64.StdEncoding.DecodeString(b64cwd.String)
			if err == nil {
				cwd = string(c)
			}
		}
		hits = append(hits, Hit{
			Hash:           hash,
			Cmd:            string(cmd),
			CWD:            cwd,
			TS:             ts,
			AnnotationType: atype,
		})
	}
	return hits, rows.Err()
}
