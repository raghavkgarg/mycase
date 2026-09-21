package stockpicker

import "testing"

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"sp500", "sp500"},
		{"SP500", "sp500"},
		{"^GSPC", "gspc"},
		{"micro,small", "micro_small"},
		{"midcap150+smallcap250", "midcap150_smallcap250"},
		{"nifty total market", "nifty_total_market"},
		{"  Trimmed  ", "trimmed"},
	}
	for _, tc := range tests {
		if got := SanitizeName(tc.in); got != tc.want {
			t.Errorf("SanitizeName(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestDisplayName_FlagPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		opts       *Options
		loadedName string
		want       string
	}{
		{
			name:       "explicit --name wins over everything",
			opts:       &Options{DisplayName: "MyPortfolio", IndexName: "sp500", FilePath: "data/microsmall.csv"},
			loadedName: "microsmall",
			want:       "MyPortfolio",
		},
		{
			name:       "--index anchors identity over a --file derived name",
			opts:       &Options{IndexName: "sp500", FilePath: "data/microsmall.csv"},
			loadedName: "microsmall",
			want:       "sp500",
		},
		{
			name:       "pure --file run falls back to the loaded universe name",
			opts:       &Options{FilePath: "data/microsmall.csv"},
			loadedName: "microsmall",
			want:       "microsmall",
		},
		{
			name:       "plain --index run uses the index",
			opts:       &Options{IndexName: "niftytotalmarket"},
			loadedName: "niftytotalmarket",
			want:       "niftytotalmarket",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DisplayName(tc.opts, tc.loadedName); got != tc.want {
				t.Errorf("DisplayName() = %q; want %q", got, tc.want)
			}
		})
	}
}

// TestPickIdentity_WriterReaderAgree pins the exact bug from the roadmap: a
// `pick --index sp500 --method us_quality_momentum` run must file under
// sp500_us_quality_momentum, never a --file-derived us_microsmall_multibagger.
func TestPickIdentity_WriterReaderAgree(t *testing.T) {
	opts := &Options{IndexName: "sp500", Method: "us_quality_momentum", FilePath: "data/microsmall.csv"}
	name := DisplayName(opts, "microsmall")
	if got, want := PickIdentity(name, opts.Method), "sp500_us_quality_momentum"; got != want {
		t.Errorf("PickIdentity = %q; want %q", got, want)
	}
}
