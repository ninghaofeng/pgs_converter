package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArguments(t *testing.T) {
	for _, args := range [][]string{
		{"convert", "x.srt", "-o", "out.sup"},
		{"convert", "x.srt", "--size", "640x360", "--fps", "24", "-o", "out.sup", "--subtitle-white-nits", "203"},
		{"convert", "x.srt", "--size", "0x360", "--fps", "24", "-o", "out.sup"},
		{"batch", ".", "--size", "640x360", "--fps", "24", "--jobs", "0"},
	} {
		var out, err bytes.Buffer
		if got := Run(context.Background(), args, &out, &err); got != 2 {
			t.Errorf("%v: got %d: %s", args, got, err.String())
		}

	}
}

func TestHDRDefaultAndWarningSummary(t *testing.T) {
	// Verify the CLI default reaches the encoded bytes, not just flag parsing.
	// The experimental-profile notice must also count in the final summary.
	root := t.TempDir()
	var generated [][]byte
	for i, extra := range [][]string{nil, {"--subtitle-white-nits", "203"}} {
		output := filepath.Join(root, []string{"default.sup", "explicit.sup"}[i])
		args := []string{"convert", "../../testdata/sample.srt", "--size", "1280x720", "--fps", "24", "--color-profile", "hdr10", "-o", output}
		args = append(args, extra...)
		var out, stderr bytes.Buffer
		if code := Run(context.Background(), args, &out, &stderr); code != 0 || !strings.Contains(stderr.String(), "1 warnings") {
			t.Fatalf("HDR CLI status=%d: %s", code, stderr.String())
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		generated = append(generated, data)
	}
	if !bytes.Equal(generated[0], generated[1]) {
		t.Fatal("default HDR white differs from explicit 203 nits")
	}
}

func TestLiveProgress(t *testing.T) {
	for _, quiet := range []bool{false, true} {
		output := filepath.Join(t.TempDir(), "out.sup")
		args := []string{"convert", "../../testdata/sample.srt", "--size", "1280x720", "--fps", "24", "-o", output}
		if quiet {
			args = append(args, "--no-progress")
		}
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), args, &stdout, &stderr); code != 0 {
			t.Fatalf("status=%d: %s", code, stderr.String())
		}
		log := stderr.String()
		if stdout.Len() != 0 || !strings.Contains(log, "OK ") {
			t.Fatalf("progress changed output streams or results: %s", log)
		}
		if quiet {
			if strings.Contains(log, "progress:") || strings.Contains(log, "START ") {
				t.Fatalf("--no-progress did not disable live output: %s", log)
			}
		} else {
			initial := strings.Index(log, "0.0%")
			final := strings.Index(log, "100.0%")
			done := strings.Index(log, "OK ")
			if !strings.Contains(log, "START ") || initial < 0 || final <= initial || done <= final {
				t.Fatalf("missing or unordered progress: %s", log)
			}
		}
	}
}

func TestConcurrentProgress(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	inputs := []string{"../../testdata/sample.srt", "../../testdata/decoder.ass"}
	args := append([]string{"batch"}, inputs...)
	args = append(args, "--size", "1280x720", "--fps", "24", "--output-dir", root, "--jobs", "2")
	if code := Run(context.Background(), args, &stdout, &stderr); code != 0 {
		t.Fatalf("status=%d: %s", code, stderr.String())
	}
	log := stderr.String()
	for _, input := range inputs {
		for _, suffix := range []string{": 0.0%", ": 100.0%"} {
			if !strings.Contains(log, "progress: "+input+suffix) {
				t.Fatalf("missing per-file batch progress for %s: %s", input, log)
			}
		}
	}
	if !strings.Contains(log, "2 succeeded, 0 failed") || stdout.Len() != 0 {
		t.Fatalf("unexpected batch summary/streams: %s", log)
	}
}
func TestBatchContinueAndCollision(t *testing.T) {
	root := t.TempDir()
	input, output := filepath.Join(root, "input"), filepath.Join(root, "output")
	if err := os.MkdirAll(filepath.Join(input, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	// A malformed file and both colliding stems must fail while the unrelated
	// valid input succeeds, even with concurrent workers.
	for path, content := range map[string]string{
		"ok.srt":               "1\n00:00:00,000 --> 00:00:01,000\nHello",
		"bad.srt":              "broken",
		"nested/collision.srt": "bad", "nested/collision.ass": "bad",
	} {
		if err := os.WriteFile(filepath.Join(input, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"batch", input, "--size", "640x360", "--fps", "24", "--output-dir", output, "--recursive", "--jobs", "2"}, &out, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "1 succeeded, 3 failed") {
		t.Fatalf("batch status=%d: %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(output, "ok.sup")); err != nil {
		t.Fatal("successful file was not written:", err)
	}
	if _, err := os.Stat(filepath.Join(output, "nested/collision.sup")); !os.IsNotExist(err) {
		t.Fatal("colliding output was written")
	}
}
