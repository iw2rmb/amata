package progress

import (
	"reflect"
	"strings"
	"testing"
)

func TestRenderGitCommitFileTableAlignsDiffColumns(t *testing.T) {
	t.Parallel()

	files := []commitFileDescriptor{
		{Path: "docs/schemas/address.schema.json", Insertions: 53, Deletions: 2},
		{Path: "deleted.txt", Insertions: 0, Deletions: 24},
	}
	plusWidth, minusWidth := gitCommitDiffColumnWidths(53, 24, files)
	got := renderGitCommitFileTable(Step{}, files, plusWidth, minusWidth, 60, newStreamStyles(false))

	want := []string{
		"+53  -2 docs/schemas/address.schema.json",
		" +0 -24 deleted.txt",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("file table = %#v, want %#v", got, want)
	}
}

func TestRenderGitCommitFileTableAddsHyperlinksWhenRepoRootIsAvailable(t *testing.T) {
	t.Parallel()

	files := []commitFileDescriptor{
		{Path: "notes/todo.txt", Insertions: 1, Deletions: 0},
	}
	plusWidth, minusWidth := gitCommitDiffColumnWidths(1, 0, files)
	got := strings.Join(renderGitCommitFileTable(Step{
		Value: map[string]any{
			"repoRoot": "/repo",
		},
	}, files, plusWidth, minusWidth, 60, newStreamStyles(true)), "\n")

	if !strings.Contains(got, "file:///repo/notes/todo.txt") {
		t.Fatalf("file table = %q, want file hyperlink", got)
	}
}

func TestRenderGitCommitTotalsLineSharesWidthsWithFileRows(t *testing.T) {
	t.Parallel()

	files := []commitFileDescriptor{
		{Path: "docs/refactor.md", Insertions: 1, Deletions: 1},
	}
	plusWidth, minusWidth := gitCommitDiffColumnWidths(2611, 89, files)

	totals := renderGitCommitTotalsLine(2611, 89, 21, plusWidth, minusWidth, 80, newStreamStyles(false))
	rows := renderGitCommitFileTable(Step{}, files, plusWidth, minusWidth, 80, newStreamStyles(false))
	if got, want := totals, []string{"+2611 -89 files: 21"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("totals = %#v, want %#v", got, want)
	}
	if got, want := rows, []string{"   +1  -1 docs/refactor.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %#v, want %#v", got, want)
	}

	colored := strings.Join(renderGitCommitTotalsLine(2611, 89, 21, plusWidth, minusWidth, 80, newStreamStyles(true)), "\n")
	if !strings.Contains(colored, "\x1b[") {
		t.Fatalf("colored totals = %q, want ANSI colors", colored)
	}
}
