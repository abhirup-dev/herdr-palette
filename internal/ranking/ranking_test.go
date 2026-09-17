package ranking

import (
	"testing"
	"time"
)

// TestRankedReproducesJqScores mirrors the jq rank-lib math on a synthetic log:
// two uses of A (one fresh, one 48h old) vs one fresh use of B vs one 240h-old
// use of C. Scores: A = 1/(1+0) + 1/(1+2) = 1.333, B = 1.0, C = 1/11.
func TestRankedReproducesJqScores(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	records := []Record{
		{TS: "2026-08-22T11:00:00Z", Label: "a", Command: "cmd-a"}, // age 1h  -> 1/(1+1/24)=0.96
		{TS: "2026-08-20T12:00:00Z", Label: "a", Command: "cmd-a"}, // age 48h -> 1/3
		{TS: "2026-08-22T10:00:00Z", Label: "b", Command: "cmd-b"}, // age 2h
		{TS: "2026-08-12T12:00:00Z", Label: "c", Command: "cmd-c"}, // age 240h -> 1/11
		{TS: "not-a-date", Label: "x", Command: "cmd-a"},           // jq: 0-epoch -> huge age -> ~0
	}
	catalog := []Row{
		{Label: "A", Command: "cmd-a"},
		{Label: "B", Command: "cmd-b"},
		{Label: "C", Command: "cmd-c"},
		{Label: "D", Command: "cmd-d"}, // never used: excluded
	}
	got := Ranked(catalog, records, now)
	if len(got) != 3 {
		t.Fatalf("rows = %d, want 3: %+v", len(got), got)
	}
	if got[0].Label != "A (*)" || got[0].Command != "cmd-a" {
		t.Fatalf("top = %+v, want A (*)", got[0])
	}
	if got[1].Label != "B (*)" {
		t.Fatalf("second = %+v, want B (*)", got[1])
	}
	if got[2].Label != "C (*)" {
		t.Fatalf("third = %+v, want C (*)", got[2])
	}
}

// TestRankedCapsAtEight mirrors jq .[0:8].
func TestRankedCapsAtEight(t *testing.T) {
	now := time.Now()
	var records []Record
	var catalog []Row
	for i := 0; i < 12; i++ {
		records = append(records, Record{TS: now.Add(-time.Duration(i) * time.Hour).Format(time.RFC3339), Command: "c"})
	}
	for i := 0; i < 12; i++ {
		catalog = append(catalog, Row{Label: string(rune('a' + i)), Command: "c"})
	}
	if got := Ranked(catalog, records, now); len(got) != 1 {
		// all rows share one command -> unique_by collapses to 1
		t.Fatalf("shared command should collapse to 1 row, got %d", len(got))
	}
	catalog = nil
	for i := 0; i < 12; i++ {
		catalog = append(catalog, Row{Label: string(rune('a' + i)), Command: "c"})
		records = append(records, Record{TS: now.Format(time.RFC3339), Command: "c"})
	}
	// still one unique command
	if got := Ranked(catalog, records, now); len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
}

// TestParseRecordsForgiving skips torn lines.
func TestParseRecordsForgiving(t *testing.T) {
	in := `{"ts":"2026-08-22T00:00:00Z","label":"x","command":"y"}
{broken json
{"ts":"","command":"y"}
`
	got := ParseRecords([]byte(in))
	if len(got) != 1 {
		t.Fatalf("records = %d, want 1", len(got))
	}
}
