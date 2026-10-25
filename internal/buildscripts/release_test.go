package buildscripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseRemainsDraft(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release job runs in bash on Linux")
	}
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "build.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, step, ok := strings.Cut(string(data), "      - name: Create or update draft only\n")
	if !ok {
		t.Fatal("missing draft release step")
	}
	_, block, ok := strings.Cut(step, "        run: |\n")
	if !ok {
		t.Fatal("missing draft release script")
	}
	var script strings.Builder
	for _, line := range strings.Split(block, "\n") {
		script.WriteString(strings.TrimPrefix(line, "          ") + "\n")
	}
	for _, mode := range []string{"new", "draft", "published", "api-failure"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			mock := filepath.Join(root, "gh")
			mockScript := `#!/bin/sh
printf '%s\n' "$*" >> "$PGS_TEST_CALLS"
case "$1 $2" in
  "api --paginate")
    case "$PGS_TEST_MODE" in
      draft) printf '123\ttrue\n' ;;
      published) printf '123\tfalse\n' ;;
      api-failure) printf 'API unavailable\n' >&2; exit 7 ;;
    esac ;;
  "release create"|"release upload") ;;
  "release view")
    case "$*" in
      *url,isDraft,assets*) printf '{"url":"https://example.invalid/draft","isDraft":true,"assets":[]}\n' ;;
      *) printf 'https://example.invalid/draft\n' ;;
    esac ;;
  *) printf 'unexpected gh command\n' >&2; exit 8 ;;
esac
`
			if err := os.WriteFile(mock, []byte(mockScript), 0755); err != nil {
				t.Fatal(err)
			}
			calls := filepath.Join(root, "calls")
			cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", script.String())
			cmd.Dir = root
			cmd.Env = append(os.Environ(),
				"PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"),
				"PGS_TEST_MODE="+mode, "PGS_TEST_CALLS="+calls,
				"GITHUB_REPOSITORY=example/project", "GITHUB_REF_NAME=v1.0.0",
				"GITHUB_STEP_SUMMARY="+filepath.Join(root, "summary"),
				"RELEASE_NOTES=manual review required")
			output, runErr := cmd.CombinedOutput()
			record, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			invocations := string(record)
			switch mode {
			case "new":
				if runErr != nil || !strings.Contains(invocations, "release create v1.0.0") ||
					!strings.Contains(invocations, "--draft --verify-tag") {
					t.Fatalf("new release was not created as a verified draft: %v: %s: %s", runErr, output, record)
				}
			case "draft":
				if runErr != nil || !strings.Contains(invocations, "release upload v1.0.0") ||
					strings.Contains(invocations, "release create") || strings.Contains(invocations, "release edit") {
					t.Fatalf("draft rerun must only refresh assets: %v: %s: %s", runErr, output, record)
				}
			default:
				if runErr == nil || strings.Contains(invocations, "release create") ||
					strings.Contains(invocations, "release upload") {
					t.Fatalf("published release or API failure must stop before writes: %v: %s: %s", runErr, output, record)
				}
			}
		})
	}
}
