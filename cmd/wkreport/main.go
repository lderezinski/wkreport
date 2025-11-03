package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"html"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"wkreport/internal/config"
	"wkreport/internal/jira"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	normalizedArgs := normalizeFilterFlag(args)

	flags := flag.NewFlagSet("wkreport", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)

	var filterRef string
	var configPath string
	var listFilters bool
	var tabDelimited bool
	var docsOutput bool
	var slidesOutput bool

	flags.StringVar(&filterRef, "f", "", "Jira filter identifier (name or numeric id, supports -f123 shorthand)")
	flags.StringVar(&configPath, "config", "cfg/config.yaml", "Path to configuration file")
	flags.BoolVar(&listFilters, "ls", false, "List available Jira filters and exit")
	flags.BoolVar(&tabDelimited, "tabs", false, "Output report using tab-separated fields")
	flags.BoolVar(&docsOutput, "docs", false, "Output report formatted for Google Docs tables")
	flags.BoolVar(&slidesOutput, "slides", false, "Output report formatted for Google Slides bullets")

	if err := flags.Parse(normalizedArgs); err != nil {
		return err
	}

	if docsOutput && slidesOutput {
		return errors.New("choose either -docs or -slides, not both")
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	client, err := jira.NewClient(cfg.Jira.URL, cfg.Jira.Email, cfg.Jira.APIToken)
	if err != nil {
		return fmt.Errorf("create jira client: %w", err)
	}

	if listFilters {
		return displayFilters(ctx, client)
	}

	if strings.TrimSpace(filterRef) == "" {
		return errors.New("filter identifier (-f) is required")
	}

	filter, err := client.ResolveFilter(ctx, strings.TrimSpace(filterRef))
	if err != nil {
		return fmt.Errorf("resolve filter %q: %w", filterRef, err)
	}

	issues, err := client.SearchByFilter(ctx, filter)
	if err != nil {
		return fmt.Errorf("search jira issues: %w", err)
	}

	if len(issues) == 0 {
		fmt.Println("No issues found.")
		return nil
	}

	if docsOutput {
		sortIssues(issues, false)
		tableHTML := buildDocsHTML(issues)
		rtfPayload, rtfErr := convertHTMLToRTF(tableHTML)

		if isTerminal(os.Stdout) {
			if rtfErr == nil {
				if err := copyToClipboard("rtf", rtfPayload); err == nil {
					fmt.Fprintln(os.Stderr, "Google Docs table copied to clipboard. Paste directly into your document.")
					return nil
				}
			}

			if err := copyToClipboard("html", []byte(tableHTML)); err != nil {
				fmt.Println(tableHTML)
				fmt.Fprintf(os.Stderr, "Warning: failed to copy table to clipboard (%v).\n", err)
				fmt.Fprintln(os.Stderr, "Tip: run `wkreport -docs ... | pbcopy -Prefer html` manually.")
			} else {
				fmt.Fprintln(os.Stderr, "Table (HTML) copied to clipboard. If Google Docs shows raw markup, use Paste special > Paste HTML.")
			}
		} else {
			if rtfErr == nil {
				os.Stdout.Write(rtfPayload)
				fmt.Fprintln(os.Stderr, "Hint: pipe into `pbcopy -Prefer rtf` to preserve table formatting.")
			} else {
				fmt.Println(tableHTML)
				fmt.Fprintln(os.Stderr, "Hint: pipe into `pbcopy -Prefer html` to preserve table formatting.")
			}
		}
		return nil
	}

	if slidesOutput {
		sortIssues(issues, true)
		plainOutput, htmlContent := buildSlidesContent(issues)

		if plainOutput == "" && htmlContent == "" {
			fmt.Println("No slide content generated.")
			return nil
		}

		rtfPayload, rtfErr := convertHTMLToRTF(htmlContent)
		if isTerminal(os.Stdout) {
			copied := false

			if rtfErr == nil {
				if err := copyToClipboard("rtf", rtfPayload); err == nil {
					fmt.Fprintln(os.Stderr, "Slides summary copied to clipboard with formatting. Paste directly into your slide notes or text box.")
					copied = true
				} else {
					fmt.Fprintf(os.Stderr, "Warning: failed to copy slides summary as RTF (%v).\n", err)
				}
			}

			if !copied {
				if err := copyToClipboard("html", []byte(htmlContent)); err == nil {
					fmt.Fprintln(os.Stderr, "Slides summary copied as HTML to clipboard. Paste directly into your slide notes or text box.")
					copied = true
				} else {
					fmt.Fprintf(os.Stderr, "Warning: failed to copy slides summary to clipboard (%v).\n", err)
					fmt.Fprintln(os.Stderr, "Tip: run `wkreport -slides ... | pbcopy -Prefer html` manually.")
				}
			}

			if rtfErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: unable to convert slides summary to RTF (%v). Falling back to HTML clipboard behavior.\n", rtfErr)
			}

			if !copied {
				fmt.Println(plainOutput)
			}
		} else {
			if rtfErr == nil {
				if _, err := os.Stdout.Write(rtfPayload); err != nil {
					fmt.Fprintf(os.Stderr, "Error writing slides RTF payload: %v\n", err)
					return nil
				}
				fmt.Fprintln(os.Stderr, "Hint: pipe into `pbcopy -Prefer rtf` to preserve hyperlinks in slides.")
			} else {
				fmt.Println(htmlContent)
				fmt.Fprintln(os.Stderr, "Hint: pipe into `pbcopy -Prefer html` to preserve hyperlinks in slides.")
			}
			return nil
		}

		return nil
	}

	sortIssues(issues, false)

	if tabDelimited {
		tabContent := buildTabDelimited(issues)
		if isTerminal(os.Stdout) {
			if err := copyToClipboard("", []byte(tabContent)); err == nil {
				fmt.Fprintln(os.Stderr, "Tab-delimited report copied to clipboard. Paste into your spreadsheet or text editor.")
				return nil
			} else {
				fmt.Print(tabContent)
				fmt.Fprintf(os.Stderr, "Warning: failed to copy tab-delimited report to clipboard (%v).\n", err)
				fmt.Fprintln(os.Stderr, "Tip: run `wkreport -tabs ... | pbcopy` manually.")
			}
		} else {
			fmt.Print(tabContent)
			fmt.Fprintln(os.Stderr, "Hint: pipe into `pbcopy` to copy the tab-delimited report.")
		}
		return nil
	} else {
		fmt.Printf("%-12s %-150s %-20s %-12s %-16s\n", "KEY", "SUMMARY", "STATUS", "PARENT", "RESOLVED")
		for _, issue := range issues {
			rawSummary := truncate(issue.Summary, 150)
			parent := strings.TrimSpace(issue.Parent)
			displaySummary := rawSummary
			if parent != "" {
				displaySummary = truncate(fmt.Sprintf("%s / %s", parent, rawSummary), 150)
			}
			parentCol := truncate(parent, 12)
			fmt.Printf("%-12s %-150s %-20s %-12s %-16s\n", issue.Key, displaySummary, issue.Status, parentCol, issue.Resolved)
		}
	}

	return nil
}

func normalizeFilterFlag(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.HasPrefix(arg, "-f") && len(arg) > 2 && !strings.Contains(arg, "=") {
			if _, err := strconv.Atoi(arg[2:]); err == nil {
				out = append(out, "-f", arg[2:])
				continue
			}
		}
		out = append(out, arg)
	}
	return out
}

func truncate(input string, width int) string {
	if len([]rune(input)) <= width {
		return input
	}
	runes := []rune(input)
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}

func buildDocsHTML(issues []jira.Issue) string {
	var b strings.Builder
	b.WriteString("<table border=\"1\" cellspacing=\"0\" cellpadding=\"4\">\n")
	b.WriteString("  <tr><td>KEY</td><td>SUMMARY</td><td>STATUS</td><td>PARENT</td><td>RESOLVED</td></tr>\n")
	for _, issue := range issues {
		key := html.EscapeString(issue.Key)
		url := html.EscapeString(strings.TrimSpace(issue.URL))
		rawSummary := truncate(issue.Summary, 150)
		parentSource := strings.TrimSpace(issue.Parent)
		if parentSource != "" {
			rawSummary = truncate(fmt.Sprintf("%s / %s", parentSource, rawSummary), 150)
		}
		summary := html.EscapeString(rawSummary)
		status := html.EscapeString(issue.Status)
		parent := html.EscapeString(strings.TrimSpace(issue.Parent))
		resolved := html.EscapeString(issue.Resolved)
		b.WriteString("  <tr>")
		b.WriteString("<td>")
		if url != "" {
			b.WriteString("<a href=\"")
			b.WriteString(url)
			b.WriteString("\">")
			b.WriteString(key)
			b.WriteString("</a>")
		} else {
			b.WriteString(key)
		}
		b.WriteString("</td><td>")
		b.WriteString(summary)
		b.WriteString("</td><td>")
		b.WriteString(status)
		b.WriteString("</td><td>")
		b.WriteString(parent)
		b.WriteString("</td><td>")
		b.WriteString(resolved)
		b.WriteString("</td></tr>\n")
	}
	b.WriteString("</table>")
	return b.String()
}

func buildTabDelimited(issues []jira.Issue) string {
	var b strings.Builder
	b.WriteString("KEY\tSUMMARY\tSTATUS\tPARENT\tRESOLVED\n")
	for _, issue := range issues {
		parent := strings.TrimSpace(issue.Parent)
		summary := truncate(issue.Summary, 150)
		if parent != "" {
			summary = truncate(fmt.Sprintf("%s / %s", parent, summary), 150)
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\n", issue.Key, summary, issue.Status, parent, issue.Resolved)
	}
	return b.String()
}

func buildSlidesContent(issues []jira.Issue) (string, string) {
	if len(issues) == 0 {
		return "", ""
	}

	var plain strings.Builder
	var htmlBuilder strings.Builder

	htmlBuilder.WriteString("<html><body>\n")

	currentStatus := ""
	firstStatus := true

	for _, issue := range issues {
		status := strings.TrimSpace(issue.Status)
		if status == "" {
			status = "Unknown"
		}

		if status != currentStatus {
			if !firstStatus {
				htmlBuilder.WriteString("</ul>\n")
				plain.WriteString("\n")
			}
			firstStatus = false
			currentStatus = status

			if plain.Len() > 0 {
				plain.WriteString("\n")
			}
			plain.WriteString(status)
			plain.WriteString("\n")

			htmlBuilder.WriteString("<h2>")
			htmlBuilder.WriteString(html.EscapeString(status))
			htmlBuilder.WriteString("</h2>\n<ul>\n")
		}

		key := strings.TrimSpace(issue.Key)
		rawSummary := truncate(strings.TrimSpace(issue.Summary), 150)
		parentSource := strings.TrimSpace(issue.Parent)
		if parentSource != "" {
			rawSummary = truncate(fmt.Sprintf("%s / %s", parentSource, rawSummary), 150)
		}
		summary := rawSummary
		url := strings.TrimSpace(issue.URL)

		plain.WriteString("- ")
		plain.WriteString(key)
		if summary != "" {
			plain.WriteString(": ")
			plain.WriteString(summary)
		}
		plain.WriteString("\n")

		htmlBuilder.WriteString("  <li>")
		if url != "" {
			htmlBuilder.WriteString("<a href=\"")
			htmlBuilder.WriteString(html.EscapeString(url))
			htmlBuilder.WriteString("\">")
			htmlBuilder.WriteString(html.EscapeString(key))
			htmlBuilder.WriteString("</a>")
		} else {
			htmlBuilder.WriteString(html.EscapeString(key))
		}
		if summary != "" {
			htmlBuilder.WriteString(": ")
			htmlBuilder.WriteString(html.EscapeString(summary))
		}
		htmlBuilder.WriteString("</li>\n")
	}

	if !firstStatus {
		htmlBuilder.WriteString("</ul>\n")
	}
	htmlBuilder.WriteString("</body></html>")

	return strings.TrimRight(plain.String(), "\n"), htmlBuilder.String()
}

func sortIssues(issues []jira.Issue, byStatus bool) {
	if byStatus {
		sort.SliceStable(issues, func(i, j int) bool {
			statusI := strings.TrimSpace(strings.ToLower(issues[i].Status))
			statusJ := strings.TrimSpace(strings.ToLower(issues[j].Status))
			if statusI == statusJ {
				return strings.TrimSpace(strings.ToLower(issues[i].Key)) < strings.TrimSpace(strings.ToLower(issues[j].Key))
			}
			return statusI < statusJ
		})
		return
	}

	sort.SliceStable(issues, func(i, j int) bool {
		parentI := strings.TrimSpace(strings.ToLower(issues[i].Parent))
		parentJ := strings.TrimSpace(strings.ToLower(issues[j].Parent))
		if parentI == parentJ {
			statusI := strings.TrimSpace(strings.ToLower(issues[i].Status))
			statusJ := strings.TrimSpace(strings.ToLower(issues[j].Status))
			if statusI == statusJ {
				return strings.TrimSpace(strings.ToLower(issues[i].Key)) < strings.TrimSpace(strings.ToLower(issues[j].Key))
			}
			return statusI < statusJ
		}
		return parentI < parentJ
	})
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func copyToClipboard(prefer string, data []byte) error {
	if runtime.GOOS != "darwin" {
		return errors.New("clipboard copy supported on macOS only")
	}

	args := []string{}
	if prefer != "" {
		args = append(args, "-Prefer", prefer)
	}
	cmd := exec.Command("pbcopy", args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return err
	}
	if _, err := stdin.Write(data); err != nil {
		stdin.Close()
		cmd.Wait()
		return err
	}
	stdin.Close()
	return cmd.Wait()
}

func convertHTMLToRTF(htmlContent string) ([]byte, error) {
	if runtime.GOOS != "darwin" {
		return nil, errors.New("rtf conversion supported on macOS only")
	}

	tempDir, err := os.MkdirTemp("", "wkreport-html")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	htmlPath := tempDir + "/input.html"
	rtfPath := tempDir + "/output.rtf"

	if err := os.WriteFile(htmlPath, []byte(htmlContent), 0600); err != nil {
		return nil, err
	}

	cmd := exec.Command("textutil", "-convert", "rtf", htmlPath, "-output", rtfPath)
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	rtfData, err := os.ReadFile(rtfPath)
	if err != nil {
		return nil, err
	}

	return rtfData, nil
}

func displayFilters(ctx context.Context, client *jira.Client) error {
	filters, err := client.ListFilters(ctx)
	if err != nil {
		return fmt.Errorf("list filters: %w", err)
	}

	if len(filters) == 0 {
		fmt.Println("No filters found.")
		return nil
	}

	fmt.Printf("%-8s %s\n", "ID", "NAME")
	for _, filter := range filters {
		fmt.Printf("%-8d %s\n", filter.ID, filter.Name)
	}

	return nil
}
