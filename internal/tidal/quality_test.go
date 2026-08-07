package tidal

import "testing"

// The UI renders badges from Quality.Label, so every tier on the ladder must
// produce a label. A tier added to qualityLadder without a matching label would
// otherwise silently render nothing.
func TestQualityLabelCoversLadder(t *testing.T) {
	for _, q := range qualityLadder {
		if q.Label() == "" {
			t.Errorf("Quality(%q).Label() is empty — tier is on the ladder but has no badge label", q)
		}
	}

	if got := QualityHiRes.Label(); got != "hi-res" {
		t.Errorf("QualityHiRes.Label() = %q, want %q", got, "hi-res")
	}
	// The empty tier means "nothing resolved yet" and must render no badge.
	if got := Quality("").Label(); got != "" {
		t.Errorf("Quality(\"\").Label() = %q, want empty", got)
	}
	// An unknown tier degrades to a readable label rather than vanishing.
	if got := Quality("HI_RES").Label(); got != "hi res" {
		t.Errorf("Quality(\"HI_RES\").Label() = %q, want %q", got, "hi res")
	}
}
