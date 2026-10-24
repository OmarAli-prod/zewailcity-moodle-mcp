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
		if len(items) == 0 {
			fmt.Printf("Nothing on the Moodle timeline for the next %d days.\n", *days)
		}
		for _, e := range items {
			mark := ""
			if e["overdue"] == true {
				mark = "OVERDUE "
			}
			fmt.Printf("%s%-26s %-20s %v\n%27s%v\n", mark, e["due"], e["course"], e["name"], "", e["url"])
		}
		return nil
	case "courses":
		items, err := c.Courses(or(arg(0), "inprogress"))
		if err != nil || asJSON {
			return show(items, err)
		}
		for _, x := range items {
			fmt.Printf("%6v  %-26v %v\n", x["id"], x["shortname"], x["fullname"])
		}
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
		for _, a := range items {
			grade := ""
			if g, _ := a["grade"].(string); g != "" {
				grade = "  grade " + g
			}
			fmt.Printf("%-26s %-20v [%v]%s  %v  (assign %v)\n", or(a["due"].(string), "no due date"), a["course"],
				a["submission"], grade, a["name"], a["assign_id"])
		}
		return nil
	case "whoami":
		out, err = c.Whoami()
	case "functions":
		var names []string
		if names, err = c.Functions(arg(0)); err == nil {
			fmt.Println(strings.Join(names, "\n"))
		}
		return err
	case "contents":
		need(1)
		out, err = c.CourseContents(id(0), true)
	case "read":
		need(1)
		out, err = c.ReadModule(id(0))
	case "grades":
		if n := id(0); n != 0 {
			out, err = c.Grades(n)
		} else {
			out, err = c.CourseTotals()
		}
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
			fmt.Println(p)
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
		if len(items) == 0 {
			fmt.Printf("Nothing new in the last %d days.\n", *days)
		}
		for _, x := range items {
			fmt.Printf("%-26v %v › %v  [%s]\n", x["course"], x["section"], x["name"], strings.Join(x["changed"].([]string), ", "))
			files, _ := x["files"].([]moodle.M)
			for _, f := range files {
				fmt.Printf("%27s%v\n", "", f["filename"])
			}
		}
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
		for _, p := range res["downloaded"].([]string) {
			fmt.Println("↓", p)
		}
		for _, f := range res["failed"].([]moodle.M) {
			fmt.Println("✗", f["file"], "-", f["error"])
		}
		fmt.Printf("%d downloaded, %v already up to date, in %v\n", len(res["downloaded"].([]string)), res["up_to_date"], res["folder"])
		return nil
	case "news":
		out, err = c.Announcements(nil, 5)
	case "notifications":
		out, err = c.Notifications(20, false)
	case "search":
		need(1)
		out, err = c.Search(strings.Join(rest, " "), nil)
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

// printMonth draws a Sunday-first month grid (the Zewail week), marks days with events,
// brackets today, and lists the events underneath.
func printMonth(first time.Time, events []moodle.M) {
	byDay := map[int][]moodle.M{}
	for _, e := range events {
		d := time.Unix(e["start_ts"].(int64), 0)
		byDay[d.Day()] = append(byDay[d.Day()], e)
	}
	title := first.Format("January 2006")
	fmt.Printf("%*s\n", (35+len(title))/2, title)
	fmt.Println("  Sun  Mon  Tue  Wed  Thu  Fri  Sat")
	fmt.Print(strings.Repeat("     ", int(first.Weekday())))
	today, days := time.Now(), first.AddDate(0, 1, -1).Day()
	for d := 1; d <= days; d++ {
		cell := strconv.Itoa(d)
		if len(byDay[d]) > 0 {
			cell += "*"
		}
		if first.Year() == today.Year() && first.Month() == today.Month() && d == today.Day() {
			cell = "[" + cell + "]"
		}
		fmt.Printf("%5s", cell)
		if (int(first.Weekday())+d)%7 == 0 || d == days {
			fmt.Println()
		}
	}
	if len(events) == 0 {
		fmt.Println("\nNothing on the calendar this month.")
		return
	}
	fmt.Println("\n* = something that day, [ ] = today")
	fmt.Println()
	for _, e := range events {
		start := time.Unix(e["start_ts"].(int64), 0)
		length := ""
		if m := e["duration_min"].(int64); m > 0 {
			length = fmt.Sprintf(" (%d min)", m)
		}
		code, _, _ := strings.Cut(fmt.Sprint(e["course"]), "-")
		fmt.Printf("%-10s %8s  %-9s %v%s\n", start.Format("Mon Jan 2"), start.Format("3:04 PM"), code, e["name"], length)
	}
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
	fmt.Fprintln(os.Stderr, "✗", msg)
	os.Exit(1)
}
