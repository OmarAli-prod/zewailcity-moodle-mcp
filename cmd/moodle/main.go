// Command moodle talks to Zewail City Moodle from the terminal, using the same client as moodle-mcp.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	moodle "github.com/OmarAli-prod/zewailcity-moodle-mcp"
	"golang.org/x/term"
)

const authHelp = `Zewail City Moodle signs you in through Office 365, so there's no password to give this tool.
Instead it uses the same per-user token the official Moodle app gets. Two ways to grab it:

  A) While signed in to Moodle in your browser, open:
     %[1]s/admin/tool/mobile/launch.php?service=moodle_mobile_app&passport=1&urlscheme=moodlemobile&confirmed=1
     Right-click "Click here to launch the app" → Copy Link Address, and paste that whole link.

  B) Moodle → your avatar → Preferences → Security keys (%[1]s/user/managetoken.php)
     Click Reset on the "Moodle mobile web service" row and copy the key it shows once.
     If that row isn't there, sign in to the Moodle phone app once and it will appear.

The token is stored in %[2]s (readable only by you).
`

const usage = `usage: moodle [--json] <command> [args]

  auth                                save your Moodle token
  version                             which build this is
  update                              install the latest release and re-link it to Claude
  logout                              delete the saved token
  whoami                              check the connection
  due [--days 14] [--back 14]         upcoming + overdue deadlines
  courses [inprogress|future|past|all]
  assignments [COURSE_ID]             with submission status and grade
  contents COURSE_ID                  sections + activities
  read CMID                           open any activity by its id
  grades [COURSE_ID]
  file FILEURL                        print a file's text
  download FILEURL [--to DIR]
  news | notifications | search QUERY
  new [--days 7]                      what professors uploaded or posted lately
  calendar [YYYY-MM]                  month grid of deadlines and lectures
  sync [DIR]                          download all course files (default ~/Moodle)
  functions [FILTER]                 what your token can call
  call WSFUNCTION '{"json": "params"}'
`

// version is set by the release build (-ldflags "-X main.version=v0.2.1").
var version = "dev"

func main() {
	asJSON := flag.Bool("json", false, "raw JSON output")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version":
		fmt.Println("moodle", version)
		return
	case "update":
		update()
		return
	case "auth":
		auth()
		return
	case "logout":
		if moodle.ClearToken() {
			fmt.Println("✓ Removed", moodle.ConfigPath())
		} else {
			fmt.Println("No saved token to remove.")
		}
		return
	}
	c, err := moodle.New()
	check(err)
	if err := run(c, cmd, rest, *asJSON); err != nil {
		check(err)
	}
}

func run(c *moodle.Client, cmd string, rest []string, asJSON bool) error {
	arg := func(i int) string {
		if i < len(rest) {
			return rest[i]
		}
		return ""
	}
	id := func(i int) int64 {
		if arg(i) == "" {
			return 0
		}
		n, err := strconv.ParseInt(arg(i), 10, 64)
		check(err)
		return n
	}
	need := func(n int) {
		if len(rest) < n {
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
	}
	if !asJSON {
		loadCourses(c)
	}
	var out any
	var err error
	switch cmd {
	case "due":
		fs := flag.NewFlagSet("due", flag.ExitOnError)
		days, back := fs.Int("days", 14, "days ahead"), fs.Int("back", 14, "days of overdue items to include")
		_ = fs.Parse(rest)
		items, err := c.Deadlines(*days, *back, 0)
		if err != nil || asJSON {
			return show(items, err)
		}
		printDue(items, *days)
		return nil
	case "courses":
		items, err := c.Courses(or(arg(0), "inprogress"))
		if err != nil || asJSON {
			return show(items, err)
		}
		printCourses(items)
		return nil
	case "assignments":
		var ids []int64
		if n := id(0); n != 0 {
			ids = []int64{n}
		}
		items, err := c.Assignments(ids, true, false)
		if err != nil || asJSON {
			return show(items, err)
		}
		printAssignments(items)
		return nil
	case "whoami":
		who, err := c.Whoami()
		if err != nil || asJSON {
			return show(who, err)
		}
		printWhoami(who)
		return nil
	case "functions":
		var names []string
		if names, err = c.Functions(arg(0)); err == nil {
			fmt.Println(strings.Join(names, "\n"))
		}
		return err
	case "contents":
		need(1)
		secs, err := c.CourseContents(id(0), true)
		if err != nil || asJSON {
			return show(secs, err)
		}
		printContents(secs)
		return nil
	case "read":
		need(1)
		out, err = c.ReadModule(id(0))
	case "grades":
		if n := id(0); n != 0 {
			items, err := c.Grades(n)
			if err != nil || asJSON {
				return show(items, err)
			}
			printGrades(items)
			return nil
		}
		items, err := c.CourseTotals()
		if err != nil || asJSON {
			return show(items, err)
		}
		printTotals(items)
		return nil
	case "file":
		need(1)
		var text string
		if text, err = c.FileText(arg(0)); err == nil {
			fmt.Println(text)
		}
		return err
	case "download":
		need(1)
		fs := flag.NewFlagSet("download", flag.ExitOnError)
		to := fs.String("to", "~/Downloads/Moodle", "destination folder")
		_ = fs.Parse(rest[1:])
		var p string
		if p, err = c.Download(arg(0), *to); err == nil {
			fmt.Println(green("↓"), p)
		}
		return err
	case "new":
		fs := flag.NewFlagSet("new", flag.ExitOnError)
		days := fs.Int("days", 7, "how far back to look")
		_ = fs.Parse(rest)
		items, err := c.Updates(nil, time.Now().AddDate(0, 0, -*days).Unix())
		if err != nil || asJSON {
			return show(items, err)
		}
		printNew(items, *days)
		return nil
	case "calendar":
		month := time.Now()
		if arg(0) != "" {
			month, err = time.ParseInLocation("2006-01", arg(0), time.Local)
			check(err)
		}
		first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.Local)
		events, err := c.Calendar(first.Unix(), first.AddDate(0, 1, 0).Unix()-1)
		if err != nil || asJSON {
			return show(events, err)
		}
		printMonth(first, events)
		return nil
	case "sync":
		res, err := c.Sync(or(arg(0), "~/Moodle"), nil)
		if err != nil || asJSON {
			return show(res, err)
		}
		printSync(res)
		return nil
	case "news":
		items, err := c.Announcements(nil, 5)
		if err != nil || asJSON {
			return show(items, err)
		}
		printNews(items)
		return nil
	case "notifications":
		items, err := c.Notifications(20, false)
		if err != nil || asJSON {
			return show(items, err)
		}
		printNotifications(items)
		return nil
	case "search":
		need(1)
		items, err := c.Search(strings.Join(rest, " "), nil)
		if err != nil || asJSON {
			return show(items, err)
		}
		printSearch(items)
		return nil
	case "call":
		need(1)
		params := moodle.M{}
		if arg(1) != "" {
			check(json.Unmarshal([]byte(arg(1)), &params))
		}
		out, err = c.Call(arg(0), params)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	return show(out, err)
}

func auth() {
	fmt.Printf(authHelp, moodle.SiteURL, moodle.ConfigPath())
	fmt.Print("Paste token or launch link (hidden): ")
	var raw string
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		check(err)
		raw = string(b)
	} else {
		raw, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	token, err := moodle.ParseToken(raw)
	check(err)
	who, err := moodle.NewWithToken(token).Whoami()
	if err != nil {
		fail("Moodle rejected that token: " + err.Error())
	}
	check(moodle.SaveToken(token))
	fmt.Printf("✓ Signed in as %v (%v). %v web-service functions available.\n",
		who["fullname"], who["username"], who["functions_available"])
}

func show(v any, err error) error {
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, red("✗"), msg)
	os.Exit(1)
}
