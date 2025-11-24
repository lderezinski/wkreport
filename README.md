# wkreport

`wkreport` is a small helper utility for generating weekly status reports from Jira filters. It reads credentials from `cfg/config.yaml` (or the matching environment variables) and prints a concise table of issues that belong to the specified filter.  Output is automatically put into the paste buffer.

## Features

- You can specify Jira filters by name or numeric ID.
- Summaries will show if there is a parent ticket `PARENT-123 / Child Summary`.
- Jira tickets are hyperlinks, parent ticket ids are plain text.
- Sorts issues by parent, status, then key (default/tab/docs) or by status then key (`-slides`) to keep related work grouped.
- Supports multiple output formats for easy sharing:
  - **Default**: fixed-width columns for terminal viewing.
  - **`-tabs`**: tab-separated rows for spreadsheets or quick text processing (copied to the macOS clipboard when run interactively).
  - **`-docs`**: Google Docs–ready table (RTF/HTML copied to the macOS clipboard when run interactively).
  - **`-slides`**: Google Slides–friendly bullets grouped by status with each key linked (copied to the macOS clipboard when run interactively).

## Configuration

Create `cfg/config.yaml` (or set the environment variables shown below):

```yaml
jira:
  url: https://your-domain.atlassian.net/
  email: you@example.com
  token: <jira-api-token>
```

Environment variables can override file values:

- `JIRA_URL`
- `JIRA_EMAIL`
- `JIRA_TOKEN` or `JIRA_API_TOKEN`

## Usage

```bash
go build ./cmd/wkreport
./wkreport -f 18205
```

### Flags

| Flag        | Description                                                                 |
|-------------|-----------------------------------------------------------------------------|
| `-f`        | Jira filter identifier (name or ID). Required.                              |
| `-config`   | Path to the configuration file. Defaults to `cfg/config.yaml`.              |
| `-tabs`     | Output tab-separated rows (summary still truncated to 150 characters). On macOS the rows are copied to the clipboard when run interactively. |
| `-docs`     | Generate a Google Docs–friendly table. On macOS the table is copied to the clipboard; otherwise it is printed to stdout. |
| `-slides`   | Generate status-grouped bullets for Google Slides. On macOS the slide bullets are copied to the clipboard; otherwise an RTF/HTML payload is written to stdout. |
| `-ls`       | List all available filters and exit.                                         |

### Examples

create alias in .zshrc, .bashrc, etc
`alias wkreport='~/opt/wkreport/cmd/wkreport.sh'`

```bash
# Default terminal table
wkreport -f 18205

# Tab-separated rows
wkreport -f 18205 -tabs > report.tsv

# Google Docs table (macOS clipboard)
wkreport -f 18205 -docs

# Slides bullets grouped by status (macOS clipboard)
wkreport -f 18205 -slides

# Clipboard automation examples
wkreport -f 18205 -tabs | pbcopy              # reuse TSV elsewhere
wkreport -f 18205 -docs | pbcopy -Prefer rtf  # preserve table formatting
wkreport -f 18205 -slides | pbcopy -Prefer rtf
```

## Notes on `-docs`

- When run interactively on macOS, the command first tries to copy an RTF table to the clipboard (using `textutil`) and falls back to HTML if necessary. Paste directly into Google Docs after running the command.
- If the command is piped, the table content is written to stdout (RTF when available); pipe the output into `pbcopy -Prefer rtf` or `pbcopy -Prefer html` to preserve formatting.

## Notes on `-tabs`

- Interactive runs on macOS copy the tab-delimited output straight to the clipboard; non-interactive runs print the TSV to stdout.
- Each summary keeps the same truncation (150 characters) used by the default output to avoid giant cells when sharing.

## Notes on `-slides`

- Issues are grouped under headings for each status (`In Progress`, `Blocked`, etc.) and listed as bullet points with the parent-aware summary.
- On macOS the command copies an RTF snapshot of the bullet list to the clipboard (falling back to HTML). Just paste into Slides. In pipelines the generated RTF/HTML is written to stdout so you can feed it to `pbcopy`.

## Development

- Go 1.25 or newer is required (see `go.mod`).
- The executable relies on macOS utilities (`textutil`, `pbcopy`) for the Google Docs export. On other systems, use the tab-separated or default outputs.
