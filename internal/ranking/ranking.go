// Package ranking ports jq/rank-lib.jq: score = uses × 24h recency decay
// (1 / (1 + age_hours/24)), top 8 emitted with a " (*)" recency suffix.
package ranking

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	json "github.com/bytedance/sonic"
)

// Record is one selections.jsonl line: {ts,label,command}.
type Record struct {
	TS      string `json:"ts"`
	Label   string `json:"label"`
	Command string `json:"command"`
}

// ParseRecords is forgiving like the jq jsonl_records: torn or manual bad
// lines are skipped rather than disabling the palette.
func ParseRecords(data []byte) []Record {
	var out []Record
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue
		}
		// jq parity: require object with string ts and string command.
		if r.TS == "" || r.Command == "" {
			continue
		}
		out = append(out, r)
	}
	return out
}

// score mirrors jq selection_score: sum over matching records of
// 1/(1+age_hours/24); negative ages (future ts, parse failure -> 0) score 1.
func score(records []Record, command string, now time.Time) float64 {
	total := 0.0
	for _, r := range records {
		if r.Command != command {
			continue
		}
		t, err := time.Parse(time.RFC3339, r.TS)
		if err != nil {
			t = time.Time{} // jq fromdateiso8601 failure -> 0 -> huge age
		}
		seconds := now.Sub(t).Seconds()
		if seconds < 0 {
			total += 1
			continue
		}
		total += 1 / (1 + (seconds / 3600 / 24))
	}
	return total
}

// Row is one manifest row.
type Row struct {
	Label   string
	Command string
}

// Ranked returns the top-8 scored catalog rows with a " (*)" recency suffix,
// exactly mirroring jq ranked_rows: unique by command, score > 0,
// sorted by -score then label.
func Ranked(catalog []Row, records []Record, now time.Time) []Row {
	seen := map[string]bool{}
	var uniq []Row
	for _, r := range catalog {
		if r.Command == "" || seen[r.Command] {
			continue
		}
		seen[r.Command] = true
		uniq = append(uniq, r)
	}
	type scored struct {
		row   Row
		score float64
	}
	var hits []scored
	for _, r := range uniq {
		s := score(records, r.Command, now)
		if s > 0 {
			hits = append(hits, scored{r, s})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if math.Abs(hits[i].score-hits[j].score) > 1e-12 {
			return hits[i].score > hits[j].score
		}
		return hits[i].row.Label < hits[j].row.Label
	})
	if len(hits) > 8 {
		hits = hits[:8]
	}
	out := make([]Row, len(hits))
	for i, h := range hits {
		out[i] = Row{Label: h.row.Label + " (*)", Command: h.row.Command}
	}
	return out
}

// Format renders rows as LABEL<TAB>COMMAND lines.
func Format(rows []Row) string {
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%s\t%s\n", r.Label, r.Command)
	}
	return b.String()
}
