package largefile

import "testing"

func TestThresholdsFromDefaults(t *testing.T) {
	th := ThresholdsFrom(nil)
	if th.Base.MaxBytes != DefaultMaxKB*1024 || th.Base.MaxLines != DefaultMaxLines {
		t.Fatalf("nil getter must yield the base defaults, got %+v", th.Base)
	}
	for f := Feature(0); f < FeatureCount; f++ {
		if th.FeatureBytes[f] != 0 {
			t.Fatalf("per-feature thresholds default to 0 (follow base), got %d for %s", th.FeatureBytes[f], f.Label())
		}
	}
}

func TestThresholdsFromReadsFeatureKeys(t *testing.T) {
	th := ThresholdsFrom(get(map[string]string{
		"files.large_file_vcs_kb":    "2",
		"files.large_file_search_kb": "bogus",
		"files.large_file_lsp_kb":    "-5",
	}))
	if th.FeatureBytes[FeatureVCS] != 2*1024 {
		t.Fatalf("vcs threshold: got %d", th.FeatureBytes[FeatureVCS])
	}
	if th.FeatureBytes[FeatureSearch] != 0 || th.FeatureBytes[FeatureLSP] != 0 {
		t.Fatal("malformed or negative values must fall back to 0 (follow base)")
	}
}

func TestOffBaseCliffDisablesEveryFeature(t *testing.T) {
	th := ThresholdsFrom(get(map[string]string{"files.large_file_kb": "1"}))
	for f := Feature(0); f < FeatureCount; f++ {
		if !th.Off(f, 2048, 10) {
			t.Fatalf("%s must be off past the base cliff", f.Label())
		}
		if th.Off(f, 512, 10) {
			t.Fatalf("%s must stay on below every threshold", f.Label())
		}
	}
}

func TestOffPerFeatureThresholdDegradesEarlier(t *testing.T) {
	th := ThresholdsFrom(get(map[string]string{"files.large_file_vcs_kb": "1"}))
	if !th.Off(FeatureVCS, 2048, 10) {
		t.Fatal("vcs must degrade past its own threshold even below the base cliff")
	}
	if th.Off(FeatureHighlight, 2048, 10) {
		t.Fatal("other features must be untouched by the vcs threshold")
	}
	if th.Off(FeatureVCS, 512, 10) {
		t.Fatal("vcs must stay on below its threshold")
	}
}

func TestFeatureLabelsAndKeysComplete(t *testing.T) {
	for f := Feature(0); f < FeatureCount; f++ {
		if f.Label() == "" || f.Key() == "" {
			t.Fatalf("feature %d is missing a label or key", f)
		}
	}
	if Feature(-1).Label() != "" || FeatureCount.Key() != "" {
		t.Fatal("out-of-range features must yield empty strings")
	}
}
