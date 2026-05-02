package sanitize

import (
	"errors"
	"strings"
	"testing"
)

func TestNewReportsMode(t *testing.T) {
	for _, m := range []Mode{ModeOff, ModeSafe, ModeStrict} {
		if got := New(m).Mode(); got != m {
			t.Errorf("New(%q).Mode() = %q", m, got)
		}
	}
}

func TestModeOffPassesContentThrough(t *testing.T) {
	input := []byte("hello\x1b[31mworld\x1b[0m")
	got, err := New(ModeOff).Process(input)
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if string(got) != string(input) {
		t.Fatalf("Process = %q, want %q", got, input)
	}
}

func TestInvalidModeFailsClosed(t *testing.T) {
	got, err := New("bogus").Process([]byte("hi"))
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
	if got != nil {
		t.Fatalf("got content %q, want nil", got)
	}
	if !strings.Contains(err.Error(), "invalid sanitize mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Dangerous classes: stripped by safe, rejected by strict.

func TestSafeStripsOSCTitle(t *testing.T) {
	input := []byte("a\x1b]0;some title\x07b")
	got, err := New(ModeSafe).Process(input)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if string(got) != "ab" {
		t.Fatalf("got %q, want %q", got, "ab")
	}
}

func TestSafeStripsOSCClipboardWrite(t *testing.T) {
	// ESC ] 52 ; c ; ... ST
	input := []byte("x\x1b]52;c;aGVsbG8=\x1b\\y")
	got, err := New(ModeSafe).Process(input)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if string(got) != "xy" {
		t.Fatalf("got %q, want %q", got, "xy")
	}
}

func TestSafeStripsDCS(t *testing.T) {
	input := []byte("a\x1bPdevice-body\x1b\\b")
	got, err := New(ModeSafe).Process(input)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if string(got) != "ab" {
		t.Fatalf("got %q, want %q", got, "ab")
	}
}

func TestSafeStripsCSIPrivateModes(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"mouse on", "before\x1b[?1000hafter"},
		{"alt screen", "before\x1b[?1049hafter"},
		{"bracketed paste", "before\x1b[?2004hafter"},
		{"private reset", "before\x1b[?25lafter"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New(ModeSafe).Process([]byte(tc.input))
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if string(got) != "beforeafter" {
				t.Fatalf("got %q, want %q", got, "beforeafter")
			}
		})
	}
}

func TestSafeStripsKeypadMode(t *testing.T) {
	got, err := New(ModeSafe).Process([]byte("a\x1b=b\x1b>c"))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if string(got) != "abc" {
		t.Fatalf("got %q, want %q", got, "abc")
	}
}

// Cosmetic classes: preserved by safe, rejected by strict.

func TestSafePreservesSGR(t *testing.T) {
	input := []byte("a\x1b[31mred\x1b[0mb")
	got, err := New(ModeSafe).Process(input)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if string(got) != string(input) {
		t.Fatalf("got %q, want %q", got, input)
	}
}

func TestSafePreservesCursorMovement(t *testing.T) {
	input := []byte("\x1b[2A\x1b[10;5Hafter")
	got, err := New(ModeSafe).Process(input)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if string(got) != string(input) {
		t.Fatalf("got %q, want %q", got, input)
	}
}

func TestSafePreservesErase(t *testing.T) {
	input := []byte("\x1b[K\x1b[2Jdone")
	got, err := New(ModeSafe).Process(input)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if string(got) != string(input) {
		t.Fatalf("got %q, want %q", got, input)
	}
}

// Strict rejects any escape sequence.

func TestStrictRejectsEachClass(t *testing.T) {
	cases := []struct {
		name  string
		input string
		class string
		off   int
	}{
		{"OSC", "abc\x1b]0;t\x07", "OSC", 3},
		{"DCS", "abc\x1bPd\x1b\\", "DCS", 3},
		{"CSI private", "x\x1b[?1000h", "CSI", 1},
		{"CSI SGR cosmetic", "x\x1b[31m", "CSI", 1},
		{"CSI cursor cosmetic", "x\x1b[2A", "CSI", 1},
		{"CSI erase cosmetic", "x\x1b[K", "CSI", 1},
		{"keypad =", "x\x1b=", "KEYPAD", 1},
		{"keypad >", "x\x1b>", "KEYPAD", 1},
		{"dangling ESC", "ab\x1b", "ESC", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New(ModeStrict).Process([]byte(tc.input))
			if got != nil {
				t.Fatalf("got content %q, want nil", got)
			}
			var sre *StrictRejectError
			if !errors.As(err, &sre) {
				t.Fatalf("got %T: %v, want *StrictRejectError", err, err)
			}
			if sre.Class != tc.class {
				t.Errorf("class = %q, want %q", sre.Class, tc.class)
			}
			if sre.Offset != tc.off {
				t.Errorf("offset = %d, want %d", sre.Offset, tc.off)
			}
			if !strings.Contains(err.Error(), "mode=strict") {
				t.Errorf("error missing mode=strict: %q", err.Error())
			}
		})
	}
}

func TestStrictErrorMessageShape(t *testing.T) {
	_, err := New(ModeStrict).Process([]byte(strings.Repeat("a", 142) + "\x1b]0;t\x07"))
	if err == nil {
		t.Fatal("expected error")
	}
	want := "content rejected by sanitizer (mode=strict): escape sequence detected at byte 142 (OSC)"
	if err.Error() != want {
		t.Fatalf("got %q, want %q", err.Error(), want)
	}
}

// StripAll: removes every ESC-initiated sequence, including cosmetic ones
// that safe mode preserves. Used for metadata rendering where any escape is a
// visual-manipulation vector.

func TestStripAllNoESCIsIdentity(t *testing.T) {
	input := []byte("plain αβγ text")
	got := StripAll(input)
	if string(got) != string(input) {
		t.Fatalf("got %q, want %q", got, input)
	}
}

func TestStripAllRemovesDangerousAndCosmeticClasses(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"OSC", "a\x1b]0;t\x07b", "ab"},
		{"DCS", "a\x1bPpayload\x1b\\b", "ab"},
		{"CSI private", "a\x1b[?1049hb", "ab"},
		{"CSI SGR cosmetic", "a\x1b[31mred\x1b[0mb", "aredb"},
		{"CSI cursor cosmetic", "a\x1b[10;5Hb", "ab"},
		{"CSI erase cosmetic", "a\x1b[2Jb\x1b[Kc", "abc"},
		{"KEYPAD", "a\x1b=b\x1b>c", "abc"},
		{"bare ESC trailing", "ab\x1b", "ab"},
		{"UTF-8 around sequence", "α\x1b[31mβ\x1b[0mγ", "αβγ"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StripAll([]byte(tc.in))
			if string(got) != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Identity: content without escape sequences passes through unchanged in all modes.

func TestIdentityNoSequences(t *testing.T) {
	input := []byte("Plain text with\nnewlines and\ttabs and unicode αβγ 🎉")
	for _, m := range []Mode{ModeOff, ModeSafe, ModeStrict} {
		t.Run(string(m), func(t *testing.T) {
			got, err := New(m).Process(input)
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if string(got) != string(input) {
				t.Fatalf("got %q, want %q", got, input)
			}
		})
	}
}

// Multi-byte UTF-8 adjacent to escape sequences must not be corrupted.

func TestUTF8BoundaryAroundSequence(t *testing.T) {
	// "α" (0xce 0xb1) then OSC then "β" (0xce 0xb2). Safe must strip only the OSC.
	input := []byte("α\x1b]0;t\x07β")
	got, err := New(ModeSafe).Process(input)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if string(got) != "αβ" {
		t.Fatalf("got %q, want %q", got, "αβ")
	}
}

func TestUTF8PreservedWithCosmeticSequence(t *testing.T) {
	// Cosmetic SGR inside multi-byte content. Safe should preserve everything.
	input := []byte("αβ\x1b[31mγ\x1b[0mδ")
	got, err := New(ModeSafe).Process(input)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if string(got) != string(input) {
		t.Fatalf("got %q, want %q", got, input)
	}
}

func TestSafeMalformedCSIDoesNotSwallowNestedESC(t *testing.T) {
	// Malformed CSI: ESC [ immediately followed by ESC ] 0 ; t BEL (an OSC,
	// which is dangerous). The inner ESC must not be consumed as a "final
	// byte" of the first CSI — the outer scanner must re-enter on it and
	// recognize the OSC so both sequences are stripped. Without the fix,
	// scanCSI would swallow the ESC and the OSC would leak into output as
	// literal "]0;t<BEL>".
	got, err := New(ModeSafe).Process([]byte("a\x1b[\x1b]0;t\x07b"))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if string(got) != "ab" {
		t.Fatalf("got %q, want %q", got, "ab")
	}
}

func TestStrictOffsetCountsBytesNotRunes(t *testing.T) {
	// "αβ" is 4 bytes; ESC lands at byte 4.
	_, err := New(ModeStrict).Process([]byte("αβ\x1b[31m"))
	var sre *StrictRejectError
	if !errors.As(err, &sre) {
		t.Fatalf("want *StrictRejectError, got %v", err)
	}
	if sre.Offset != 4 {
		t.Fatalf("offset = %d, want 4 (byte index)", sre.Offset)
	}
}

// AUR-162: bracketed-paste protocol terminators are dangerous regardless of
// the generic cosmetic-CSI rule, because tprompt's own delivery wrapper uses
// `paste-buffer -p` (load-buffer + ESC[200~ … ESC[201~). An embedded ESC[201~
// closes the wrapper early and lets subsequent bytes execute as raw keystrokes.

func TestSafeStripsBracketedPasteEnd(t *testing.T) {
	input := []byte("before\x1b[201~after")
	got, err := New(ModeSafe).Process(input)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if string(got) != "beforeafter" {
		t.Fatalf("got %q, want %q", got, "beforeafter")
	}
}

func TestSafeStripsBracketedPasteStart(t *testing.T) {
	input := []byte("a\x1b[200~b")
	got, err := New(ModeSafe).Process(input)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if string(got) != "ab" {
		t.Fatalf("got %q, want %q", got, "ab")
	}
}

func TestSafeStripsBracketedPasteRoundTrip(t *testing.T) {
	// Mirrors the AUR-162 manual repro payload: the bracketed-paste end
	// terminator embedded mid-string, an OSC set-window-title, and a bare BEL.
	// safe must strip the CSI 201~ and the OSC; the bare BEL is preserved
	// (non-ESC control bytes are out of scope for the sanitizer's denylist).
	input := []byte("before \x1b[201~ after \x1b]0;evil\x07 osc-end \x07 bell-alone\n")
	got, err := New(ModeSafe).Process(input)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	want := "before  after  osc-end \x07 bell-alone\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSafePreservesOtherTildeFinalCSI(t *testing.T) {
	// `~`-final CSI sequences other than the two bracketed-paste terminators
	// are function-key / editing-key reports (Home, End, F-keys) emitted from
	// the keyboard. They are cosmetic and must be preserved.
	cases := []struct {
		name  string
		input string
	}{
		{"Home", "before\x1b[1~after"},
		{"End", "before\x1b[4~after"},
		{"F5", "before\x1b[15~after"},
		{"three-digit non-paste", "before\x1b[202~after"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New(ModeSafe).Process([]byte(tc.input))
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if string(got) != tc.input {
				t.Fatalf("got %q, want %q (sequence must be preserved)", got, tc.input)
			}
		})
	}
}

func TestStrictStillRejectsBracketedPasteTerminators(t *testing.T) {
	// strict rejects on any ESC; the bracketed-paste terminators must surface
	// as CSI rejections at the ESC byte's offset, no special-casing.
	cases := []struct {
		name  string
		input string
	}{
		{"start", "abc\x1b[200~rest"},
		{"end", "abc\x1b[201~rest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(ModeStrict).Process([]byte(tc.input))
			var sre *StrictRejectError
			if !errors.As(err, &sre) {
				t.Fatalf("want *StrictRejectError, got %v", err)
			}
			if sre.Class != "CSI" {
				t.Errorf("class = %q, want CSI", sre.Class)
			}
			if sre.Offset != 3 {
				t.Errorf("offset = %d, want 3", sre.Offset)
			}
		})
	}
}
