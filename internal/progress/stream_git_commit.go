package progress

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	liptable "charm.land/lipgloss/v2/table"
	"github.com/iw2rmb/amata/internal/jsonutil"
)

func gitCommitRenderData(step Step) (string, int, int, int, []commitFileDescriptor, bool) {
	metadataValue, ok := jsonutil.MapField(step.Value, "metadata")
	if !ok {
		return "", 0, 0, 0, nil, false
	}

	shortCommit, _ := jsonutil.StringField(metadataValue, "shortCommit")
	changedFiles, _ := jsonutil.IntField(metadataValue, "changedFileCount")
	insertions, _ := jsonutil.IntField(metadataValue, "insertions")
	deletions, _ := jsonutil.IntField(metadataValue, "deletions")
	return shortCommit, changedFiles, insertions, deletions, fileStats(metadataValue), shortCommit != ""
}

func gitCommitMessage(step Step) string {
	data := cloneDescriptorData(step.Descriptor)
	if data == nil || len(data.DetailText) == 0 {
		return ""
	}
	return strings.TrimSpace(data.DetailText[0])
}

func gitCommitDiffColumnWidths(insertions int, deletions int, files []commitFileDescriptor) (int, int) {
	plusWidth := lipgloss.Width(fmt.Sprintf("+%d", insertions))
	minusWidth := lipgloss.Width(fmt.Sprintf("-%d", deletions))
	for _, file := range files {
		plusWidth = max(plusWidth, lipgloss.Width(fmt.Sprintf("+%d", file.Insertions)))
		minusWidth = max(minusWidth, lipgloss.Width(fmt.Sprintf("-%d", file.Deletions)))
	}
	return plusWidth, minusWidth
}

func renderGitCommitTotalsLine(
	insertions int,
	deletions int,
	changedFiles int,
	plusWidth int,
	minusWidth int,
	width int,
	styles streamStyles,
) []string {
	plusText := fmt.Sprintf("%*s", plusWidth, fmt.Sprintf("+%d", insertions))
	minusText := fmt.Sprintf("%*s", minusWidth, fmt.Sprintf("-%d", deletions))
	words := []styledWord{
		{text: plusText},
		{text: minusText},
		{text: "files:"},
		{text: strconv.Itoa(changedFiles)},
	}
	if styles.colorize {
		words[0].render = func(text string) string { return styles.diffPlus.Render(text) }
		words[1].render = func(text string) string { return styles.diffMinus.Render(text) }
	}
	return wrapStyledWords(words, width)
}

func renderGitCommitFileTable(step Step, files []commitFileDescriptor, plusWidth int, minusWidth int, width int, styles streamStyles) []string {
	if len(files) == 0 {
		return nil
	}

	pathWidth := 0
	type gitCommitTableRow struct {
		plus  string
		minus string
		path  string
	}
	tableRows := make([]gitCommitTableRow, 0, len(files))
	for _, file := range files {
		plusText := fmt.Sprintf("+%d", file.Insertions)
		minusText := fmt.Sprintf("-%d", file.Deletions)
		pathWidth = max(pathWidth, lipgloss.Width(file.Path))
		tableRows = append(tableRows, gitCommitTableRow{
			plus:  plusText,
			minus: minusText,
			path:  file.Path,
		})
	}

	rows := make([][]string, 0, len(tableRows))
	for _, row := range tableRows {
		rows = append(rows, []string{
			fmt.Sprintf("%*s", plusWidth, row.plus),
			fmt.Sprintf("%*s", minusWidth, row.minus),
			row.path,
		})
	}

	availablePathWidth := pathWidth
	if width > 0 {
		availablePathWidth = max(1, width-plusWidth-minusWidth-2)
		if pathWidth < availablePathWidth {
			availablePathWidth = pathWidth
		}
	}

	table := liptable.New().
		Rows(rows...).
		BorderTop(false).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false).
		BorderColumn(false).
		BorderHeader(false).
		BorderRow(false).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := lipgloss.NewStyle()
			switch col {
			case 0:
				style = style.PaddingRight(1)
				if styles.colorize {
					style = style.Inherit(styles.diffPlus)
				}
			case 1:
				style = style.PaddingRight(1)
				if styles.colorize {
					style = style.Inherit(styles.diffMinus)
				}
			case 2:
				style = style.Width(availablePathWidth)
				if styles.colorize {
					if link := gitCommitFileLink(step, files[row].Path); link != "" {
						style = style.Hyperlink(link)
					}
					switch {
					case files[row].Deletions == 0 && files[row].Insertions > 0:
						style = style.Inherit(styles.pathAdded)
					case files[row].Insertions == 0 && files[row].Deletions > 0:
						style = style.Inherit(styles.pathDeleted)
					}
				}
			}
			return style
		})

	rendered := strings.Split(table.String(), "\n")
	lines := make([]string, 0, len(rendered))
	for _, line := range rendered {
		lines = append(lines, strings.TrimRight(line, " "))
	}
	return lines
}

func gitCommitFileLink(step Step, path string) string {
	repoRoot, ok := jsonutil.StringField(step.Value, "repoRoot")
	if !ok || repoRoot == "" || path == "" {
		return ""
	}

	target := filepath.Join(repoRoot, filepath.FromSlash(path))
	return (&url.URL{
		Scheme: "file",
		Path:   filepath.Clean(target),
	}).String()
}
