# zewailcity-moodle-mcp

[![CI](https://github.com/OmarAli-prod/zewailcity-moodle-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/OmarAli-prod/zewailcity-moodle-mcp/actions/workflows/ci.yml)
[![Release](https://github.com/OmarAli-prod/zewailcity-moodle-mcp/actions/workflows/release.yml/badge.svg)](https://github.com/OmarAli-prod/zewailcity-moodle-mcp/actions/workflows/release.yml)
[![Latest release](https://img.shields.io/github/v/release/OmarAli-prod/zewailcity-moodle-mcp)](https://github.com/OmarAli-prod/zewailcity-moodle-mcp/releases/latest)
[![Go version](https://img.shields.io/github/go-mod/go-version/OmarAli-prod/zewailcity-moodle-mcp)](go.mod)
[![License: MIT](https://img.shields.io/github/license/OmarAli-prod/zewailcity-moodle-mcp)](LICENSE)

Connects Claude to your [Zewail City Moodle](https://moodle.zewailcity.edu.eg) account. From the chat you can ask what's due, have Claude open a reading or pull the text out of lecture slides, or check a grade.

It's a small Go program that runs on your own laptop. It signs in with the personal token Moodle gives its phone app and calls the same web-service API the app uses, so Claude sees what you'd see in the app and nothing more. It doesn't scrape pages or need your Office 365 password.

## Things to ask

```text
- What's due in the next two weeks? Include anything overdue.
- What did my professors post in announcements this week?
- Pull the text out of the latest lecture slides and quiz me on it.
- What's my current grade in each course?
- Find every file in my courses that mentions "midterm".
```

## Quick start

### 1. Install

On macOS or Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/OmarAli-prod/zewailcity-moodle-mcp/main/install.sh | sh
```

This downloads the latest release, checks its checksum, and puts two commands in `~/.local/bin`: `moodle` for you and `moodle-mcp` for Claude. Set `BIN_DIR=/somewhere/else` before `sh` to change the folder. If your shell then says `command not found`, add that folder to your `PATH`.

You can also grab the binaries by hand from the [latest release](https://github.com/OmarAli-prod/zewailcity-moodle-mcp/releases/latest). They're plain, static executables named `moodle_linux_amd64`, `moodle-mcp_darwin_arm64` and so on. Download one, `chmod +x` it, and run it.

On Windows, or if you'd rather build it yourself, install [Go](https://go.dev/dl/) 1.26+ and run:

```bash
go install github.com/OmarAli-prod/zewailcity-moodle-mcp/cmd/...@latest   # or: go install ./cmd/... from a clone
```

That puts the commands in `$(go env GOPATH)/bin` instead.

### 2. Sign in

```bash
moodle auth
```

`moodle auth` explains where to find your token and then asks for it in a hidden prompt. The quickest way to get one: while signed in to Moodle, open the app launch link it prints, right-click *"Click here to launch the app"*, copy the link address, and paste it in. [docs/getting-your-token.md](docs/getting-your-token.md) has the full walkthrough.

```bash
moodle whoami    # prints your name if it worked
moodle due       # what's coming up
```

### 3. Connect Claude

#### Claude Code

```bash
claude mcp add -s user moodle -- "$(command -v moodle-mcp)"
claude mcp list | grep moodle    # should say ✔ Connected
```

The full path means Claude finds the server even when it starts without your shell's `PATH`. Start a new Claude Code session and ask *"what's due this week?"*.

#### Claude Desktop

Open Settings > Developer > Edit Config and add the server:

```json
{
  "mcpServers": {
    "moodle": {
      "command": "/Users/YOUR-NAME/.local/bin/moodle-mcp"
    }
  }
}
```

Use the full path that `which moodle-mcp` prints (on Windows, `where moodle-mcp`, with each `\` written as `\\`). Desktop apps don't always inherit your terminal's `PATH`. Quit and reopen Claude Desktop afterwards.

### Updating

```bash
moodle update
```

It downloads the latest release, checks its checksums, and swaps both binaries in place. It refreshes the skill and makes sure Claude Code's `moodle` server points at the new `moodle-mcp`, registering it if needed. Your saved token is untouched. Restart Claude afterwards to load the new server. `moodle version` shows which release you're on.

On Windows, or if you installed with `go install`, run the same `go install` command again instead.

## Tools

| Tool                              | What it does                                                               |
| --------------------------------- | -------------------------------------------------------------------------- |
| `moodle_deadlines`              | Upcoming and overdue assignments and quizzes from your timeline            |
| `moodle_courses`                | Your courses and their ids                                                 |
| `moodle_course_contents`        | Every section, activity, file and link in a course, with the section notes |
| `moodle_search`                 | Find activities, files and section text that mention a word                |
| `moodle_assignments`            | All assignments, sorted by due date, with submission status and grade      |
| `moodle_assignment`             | One assignment in full: instructions, attachments, time left, feedback     |
| `moodle_read`                   | Open any page, file, folder, link, forum or quiz by its id                 |
| `moodle_file_text`              | Text out of a PDF, Word, PowerPoint, Excel, HTML or plain-text file        |
| `moodle_download`               | Save a course file to `~/Downloads/Moodle`                               |
| `moodle_grades`                 | Grade items for one course, or totals for all of them                      |
| `moodle_announcements`          | Latest posts in each course's Announcements forum                          |
| `moodle_discussion`             | Every post in one forum thread                                             |
| `moodle_notifications`          | The bell-icon notifications: grades, feedback, reminders                   |
| `moodle_whats_new`              | What professors uploaded, replaced or posted in the last few days          |
| `moodle_calendar`               | Everything on the calendar, including lecture sessions                     |
| `moodle_sync`                   | Download every course file into `~/Moodle`, fetching only what's new       |
| `moodle_whoami`                 | Check the connection                                                       |
| `moodle_functions`              | List the raw API functions your token is allowed to call                   |
| `moodle_call`                   | Call any of those functions directly (the escape hatch)                    |
| `moodle_submit_assignment` ⚠️ | Save a submission. Stays a draft unless you say to finalise                |

### Prompts

Ready-made requests you can pick from your client's prompt menu (in Claude Code, type `/` and look for the `moodle` entries).

| Prompt | What it does |
| --- | --- |
| `whats_due` | Deadlines grouped by day, with submission status for anything due in 48 hours. Optional `days` |
| `study_course` | Summary and practice questions from a course's latest material. Takes `course`, optional `topic` |
| `weekly_digest` | Deadlines, announcements, new grades and notifications in one summary |

### Resources

Context you can attach to a conversation (in Claude Code, type `@` and pick a `moodle://` entry).

| Resource | Contents |
| --- | --- |
| `moodle://courses` | Your current courses with ids |
| `moodle://deadlines` | The next 14 days of deadlines, plus overdue items |
| `moodle://course/{course_id}` | Every section, activity and file in one course |

### Claude skill

[`skills/zewail-moodle`](skills/zewail-moodle/SKILL.md) teaches Claude how to use these tools well: which tool answers which question, how Zewail course codes look, how to page through long files, and to confirm before submitting anything. Install it for Claude Code:

```bash
mkdir -p ~/.claude/skills && cp -r skills/zewail-moodle ~/.claude/skills/
```

In the Claude apps, zip the `zewail-moodle` folder and upload it under Settings > Capabilities > Skills.

## The `moodle` command

Output is colored in a terminal (set `NO_COLOR=1` to turn that off). Put `--json` before any command for raw output.

```text
moodle due [--days 14] [--back 14]    upcoming + overdue
moodle courses [inprogress|future|past|all]
moodle assignments [COURSE_ID]        with submission status and grade
moodle contents COURSE_ID             sections + activities
moodle read CMID                      open any activity by its id
moodle grades [COURSE_ID]
moodle file FILEURL                   print a file's text
moodle download FILEURL [--to DIR]
moodle news | notifications | search QUERY
moodle new [--days 7]                 what professors uploaded or posted lately
moodle calendar [YYYY-MM]             month grid of deadlines and lectures
moodle sync [DIR]                     download all course files (default ~/Moodle)
moodle functions [FILTER]             what your token can call
moodle call WSFUNCTION '{"json": "params"}'
moodle update                         install the latest release, re-link it to Claude
moodle version | logout
```

```console
$ moodle calendar
           October 2026
  Sun  Mon  Tue  Wed  Thu  Fri  Sat
                        1    2    3
    4    5   6*    7    8    9   10
   11   12  13*   14   15   16  17*
   18   19  20*   21   22  23*   24
 [25]   26  27*  28*   29   30   31

* = something that day, [ ] = today

Tue Oct 27 11:00 AM  SWAPD402  Lecture Attendance (60 min)
Wed Oct 28 12:00 AM  SWAPD402  Project Phase 1 is due

$ moodle sync
↓ MATH104 Linear Algebra/Week 5/5. Math104_WK4_Sec.3.1, 3.2.pptx
↓ MATH104 Linear Algebra/Sheets/sheet 2-F26.pdf
2 downloaded, 45 already up to date, in /Users/you/Moodle
```

A course id is the number in `course/view.php?id=…`, and a cmid is the number in any `mod/<type>/view.php?id=…` link. Claude looks these up on its own, so you only need them for the CLI.

## How it works

Each request is a POST to Moodle's [web-service API](https://docs.moodle.org/dev/Web_service_API_functions) carrying your token and a function name, such as `mod_assign_get_assignments`.

| File                                | Job                                                                                                                                                          |
| ----------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| [`moodle.go`](moodle.go)           | One `Call()` that POSTs to the REST endpoint, plus methods that cut each response down to the fields worth reading                                         |
| [`text.go`](text.go)               | HTML to text, and text out of PDF, DOCX, PPTX and XLSX files                                                                                                 |
| [`server.go`](server.go)           | The MCP tools, built on the [official Go SDK](https://github.com/modelcontextprotocol/go-sdk). Claude reads each tool's description to decide when to call it |
| [`cmd/moodle`](cmd/moodle/main.go) | The CLI, built on the same client                                                                                                                            |

To add a tool: find a function with `moodle functions <word>`, look at its output with `moodle call <fn> '{...}'`, add a method in `moodle.go`, register it in `server.go`, and bump the count in `TestServerTools`. Write the tool description carefully: it's what Claude reads to decide when to call the tool.

```bash
go test ./...      # offline tests, no Moodle needed
go vet ./...
npx @modelcontextprotocol/inspector moodle-mcp    # click through the tools in a browser
```

## Privacy and safety

- Your token sits in `~/.config/moodle-mcp/config.json`, readable only by your user account, and goes nowhere except `moodle.zewailcity.edu.eg`. `moodle logout` deletes it.
- Anything a tool returns becomes part of your conversation with Claude, the same as text you paste in yourself.
- Four tools can change things. `moodle_download` and `moodle_sync` save files to your computer, while `moodle_submit_assignment` and `moodle_call` can write to Moodle. The other sixteen only read.
- `moodle_submit_assignment` saves a draft unless you tell it to finalise, and Claude is told to show you the exact content before sending it.
- Check your course's AI policy before you use this on graded work.

## Troubleshooting

| Problem                                                | Fix                                                                                                 |
| ------------------------------------------------------ | --------------------------------------------------------------------------------------------------- |
| `invalidtoken`, or "the token is missing or expired" | Get a new token and run `moodle auth` again                                                       |
| No *Moodle mobile web service* row on Security keys  | Use the launch-link method, or sign in to the Moodle phone app once                                 |
| `moodle: command not found`                          | Add `~/.local/bin` (or `$(go env GOPATH)/bin` if you used `go install`) to your `PATH`      |
| Claude doesn't list the tools                          | Start a new session after adding the server. In Claude Desktop, use the full path to `moodle-mcp` |
| A file says it can't extract text                      | Use `moodle download` and open the file instead                                                   |

## License

[MIT](LICENSE). Not affiliated with Moodle Pty Ltd or Zewail City.
