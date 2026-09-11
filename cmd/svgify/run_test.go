package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdbtq/svgify/internal/testfixtures"
)

// writeFixture puts a fixture PNG in a temp dir and returns its path.
func writeFixture(t *testing.T, f testfixtures.Fixture) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), f.Name+".png")
	if err := os.WriteFile(path, f.PNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultOutputPath(t *testing.T) {
	in := writeFixture(t, testfixtures.BlackOnWhite())
	var out, errb bytes.Buffer

	if err := run([]string{in}, &out, &errb); err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, errb.String())
	}

	want := strings.TrimSuffix(in, ".png") + ".svg"
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected output at %s: %v", want, err)
	}
	// Successful runs print nothing.
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want empty on success", out.String())
	}
}

func TestExplicitOutputPath(t *testing.T) {
	in := writeFixture(t, testfixtures.BlackOnWhite())
	out := filepath.Join(t.TempDir(), "custom.svg")

	if err := run([]string{in, "-o", out}, io_Discard(), io_Discard()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected output at %s: %v", out, err)
	}
}

// Flags must be accepted before or after the input operand.
func TestFlagsAfterOperand(t *testing.T) {
	in := writeFixture(t, testfixtures.BlackOnWhite())
	out := filepath.Join(t.TempDir(), "a.svg")

	if err := run([]string{in, "--padding", "5", "-o", out}, io_Discard(), io_Discard()); err != nil {
		t.Fatalf("flags after the operand were rejected: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
}

// The input must never be overwritten, even when named as the output.
func TestRefusesToOverwriteInput(t *testing.T) {
	in := writeFixture(t, testfixtures.BlackOnWhite())
	before, _ := os.ReadFile(in)

	err := run([]string{in, "-o", in}, io_Discard(), io_Discard())
	if err == nil {
		t.Fatal("overwriting the input was allowed")
	}
	if !strings.Contains(err.Error(), "input") {
		t.Errorf("error = %q, want it to mention the input file", err)
	}

	after, _ := os.ReadFile(in)
	if !bytes.Equal(before, after) {
		t.Error("the input file was modified")
	}
}

// An existing output is protected unless -f is given.
func TestDoesNotClobberExistingOutput(t *testing.T) {
	in := writeFixture(t, testfixtures.BlackOnWhite())
	out := filepath.Join(t.TempDir(), "exists.svg")
	if err := os.WriteFile(out, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := run([]string{in, "-o", out}, io_Discard(), io_Discard()); err == nil {
		t.Fatal("an existing output file was silently overwritten")
	}
	if b, _ := os.ReadFile(out); string(b) != "original" {
		t.Error("the existing file was modified")
	}

	if err := run([]string{in, "-o", out, "-f"}, io_Discard(), io_Discard()); err != nil {
		t.Fatalf("-f did not permit overwriting: %v", err)
	}
	if b, _ := os.ReadFile(out); string(b) == "original" {
		t.Error("-f did not overwrite the file")
	}
}

func TestStdoutOutput(t *testing.T) {
	in := writeFixture(t, testfixtures.BlackOnWhite())
	var out bytes.Buffer

	if err := run([]string{in, "-o", "-"}, &out, io_Discard()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.HasPrefix(out.String(), "<svg") {
		t.Errorf("stdout does not contain an SVG: %q", out.String())
	}
}

func TestVersion(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"--version"}, &out, io_Discard()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "svgify ") {
		t.Errorf("version output = %q", out.String())
	}
}

// No arguments is a usage error, which must exit 2.
func TestNoArgumentsIsUsageError(t *testing.T) {
	var errb bytes.Buffer
	err := run(nil, io_Discard(), &errb)
	if !errors.Is(err, errUsage) {
		t.Fatalf("err = %v, want a usage error", err)
	}
	if exitCode(err) != exitUsage {
		t.Errorf("exit code = %d, want %d", exitCode(err), exitUsage)
	}
	if !strings.Contains(errb.String(), "Usage:") {
		t.Error("usage was not printed to stderr")
	}
}

func TestInvalidFlagValues(t *testing.T) {
	in := writeFixture(t, testfixtures.BlackOnWhite())
	tests := []struct {
		name string
		args []string
	}{
		{"threshold too high", []string{in, "--threshold", "300"}},
		{"negative simplify", []string{in, "--simplify", "-1"}},
		{"precision out of range", []string{in, "--precision", "99"}},
		{"two inputs", []string{in, in}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(tt.args, io_Discard(), io_Discard())
			if !errors.Is(err, errUsage) {
				t.Errorf("err = %v, want a usage error", err)
			}
		})
	}
}

// A missing file is a runtime failure (exit 1), not a usage error.
func TestMissingInputFile(t *testing.T) {
	err := run([]string{filepath.Join(t.TempDir(), "nope.png")}, io_Discard(), io_Discard())
	if err == nil {
		t.Fatal("a missing input file was accepted")
	}
	if got := exitCode(err); got != exitFailure {
		t.Errorf("exit code = %d, want %d", got, exitFailure)
	}
}

// A blank image has nothing to trace and must say so rather than emitting an
// empty SVG.
func TestBlankImageIsAnError(t *testing.T) {
	blank := testfixtures.Fixture{Name: "blank", Img: testfixtures.BlankWhite()}
	in := writeFixture(t, blank)

	err := run([]string{in}, io_Discard(), io_Discard())
	if err == nil {
		t.Fatal("a blank image produced output")
	}
	if !errors.Is(err, errNoArtwork) {
		t.Errorf("err = %v, want errNoArtwork", err)
	}
}

// Tracing the same input twice must produce identical files.
func TestDeterministicOutput(t *testing.T) {
	in := writeFixture(t, testfixtures.Logo())
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.svg"), filepath.Join(dir, "b.svg")

	for _, out := range []string{a, b} {
		if err := run([]string{in, "-o", out}, io_Discard(), io_Discard()); err != nil {
			t.Fatal(err)
		}
	}
	ab, _ := os.ReadFile(a)
	bb, _ := os.ReadFile(b)
	if !bytes.Equal(ab, bb) {
		t.Error("two runs over the same input produced different output")
	}
}

func io_Discard() *bytes.Buffer { return new(bytes.Buffer) }
