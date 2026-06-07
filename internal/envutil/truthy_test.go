package envutil_test

import (
	"testing"

	"github.com/aurokin/tprompt/internal/envutil"
)

func TestTruthy(t *testing.T) {
	cases := []struct {
		name string
		val  string
		want bool
	}{
		{"empty", "", false},
		{"unset_returns_empty_string", "", false},
		{"one", "1", true},
		{"true_lower", "true", true},
		{"true_mixed", "True", true},
		{"true_upper", "TRUE", true},
		{"yes_lower", "yes", true},
		{"yes_upper", "YES", true},
		{"on_lower", "on", true},
		{"on_upper", "ON", true},
		{"trimmed_one", "  1  ", true},
		{"trimmed_true", "\ttrue\n", true},
		{"zero", "0", false},
		{"false", "false", false},
		{"no", "no", false},
		{"off", "off", false},
		{"typo", "yep", false},
		{"unknown", "enabled", false},
		{"whitespace_only", "   ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			get := func(string) string { return tc.val }
			if got := envutil.Truthy(get, "TPROMPT_TEST"); got != tc.want {
				t.Fatalf("Truthy(%q) = %v, want %v", tc.val, got, tc.want)
			}
		})
	}
}

func TestTruthy_NilGetenv(t *testing.T) {
	if envutil.Truthy(nil, "TPROMPT_TEST") {
		t.Fatal("Truthy with nil getenv should return false")
	}
}

func TestTruthy_KeyForwarded(t *testing.T) {
	var seen string
	get := func(k string) string {
		seen = k
		return "1"
	}
	if !envutil.Truthy(get, "TPROMPT_X") {
		t.Fatal("Truthy should return true for value '1'")
	}
	if seen != "TPROMPT_X" {
		t.Fatalf("getenv received key %q, want TPROMPT_X", seen)
	}
}

func TestTruthy_GetenvCalledOnce(t *testing.T) {
	calls := 0
	get := func(string) string {
		calls++
		return "1"
	}
	envutil.Truthy(get, "TPROMPT_X")
	if calls != 1 {
		t.Fatalf("getenv invoked %d times, want 1", calls)
	}
}

func TestTruthyOf(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"", false},
		{"   ", false},
		{"1", true},
		{"  on  ", true},
		{"True", true},
		{"YES", true},
		{"0", false},
		{"yep", false},
	}
	for _, tc := range cases {
		t.Run(tc.val, func(t *testing.T) {
			if got := envutil.TruthyOf(tc.val); got != tc.want {
				t.Fatalf("TruthyOf(%q) = %v, want %v", tc.val, got, tc.want)
			}
		})
	}
}
