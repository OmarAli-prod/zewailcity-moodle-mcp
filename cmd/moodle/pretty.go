package main

import (
	"fmt"
	"hash/fnv"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	moodle "github.com/OmarAli-prod/zewailcity-moodle-mcp"
	"golang.org/x/term"
)

// Colors only when printing to a terminal, and never when NO_COLOR is set.
var colorOn = term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("NO_COLOR") == ""

func paint(code, s string) string {
	if !colorOn || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func bold(s string) string   { return paint("1", s) }
func dim(s string) string    { return paint("2", s) }
func red(s string) string    { return paint("31", s) }
func green(s string) string  { return paint("32", s) }
func yellow(s string) string { return paint("33", s) }
func cyan(s string) string   { return paint("36", s) }

// Course labels: "MATH104 Linear Algebra", filled once per run by loadCourses.
var (
	courseLabels = map[string]string{} // shortname -> label
	courseShort  = map[int64]any{}     // course id -> shortname, for output that only has an id
	labelWidth   = 9
)

func loadCourses(c *moodle.Client) {
	courses, _ := c.Courses("all")
	for _, x := range courses {
		short := str(x["shortname"])
		code, _, _ := strings.Cut(short, "-")
		label := cut(code+" "+str(x["fullname"]), 60)
		courseLabels[short], courseShort[i64(x["id"])] = label, short
		labelWidth = max(labelWidth, len([]rune(label)))
	}
}

// courseTag prints a course's code and full name padded to a column, in a color fixed per course.
func courseTag(shortname any) string {
	short := fmt.Sprint(shortname)
	code, _, _ := strings.Cut(short, "-")
	label := courseLabels[short]
	if label == "" {
		label = code
	}
	h := fnv.New32a()
	h.Write([]byte(code))
	palette := []string{"36", "35", "34", "32", "33", "96", "95", "94"}
	return paint("1;"+palette[h.Sum32()%uint32(len(palette))], fmt.Sprintf("%-*s", labelWidth, label))
}

func ago(ts int64) string {
	d := time.Since(time.Unix(ts, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return time.Unix(ts, 0).Format("Jan 2 2006")
}

// until renders the time left before ts, colored by urgency: red when overdue, yellow inside 48 hours.
func until(ts int64) string {
	d := time.Until(time.Unix(ts, 0))
	span := func(d time.Duration) string {
		days, hours := int(d.Hours())/24, int(d.Hours())%24
		if days > 0 {
			return fmt.Sprintf("%dd %dh", days, hours)
		}
		return fmt.Sprintf("%dh %dm", hours, int(d.Minutes())%60)
	}
	switch {
	case d < 0:
		return red(fmt.Sprintf("%-11s", "overdue "+span(-d)))
	case d < 48*time.Hour:
		return yellow(fmt.Sprintf("%-11s", "in "+span(d)))
	}
	return green(fmt.Sprintf("%-11s", "in "+span(d)))
}

func width() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 40 {
		return min(w, 110)
	}
	return 100
}

// wrap word-wraps text to the terminal, indents every line, and keeps at most maxLines (0 = all).
func wrap(text string, indent, maxLines int) string {
	pad, limit := strings.Repeat(" ", indent), width()-indent
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		line := ""
		for _, w := range strings.Fields(para) {
			if line != "" && len([]rune(line))+1+len([]rune(w)) > limit {
				lines, line = append(lines, line), ""
			}
			if line != "" {
				line += " "
			}
			line += w
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = append(lines[:maxLines], "…")
	}
	return pad + strings.Join(lines, "\n"+pad)
}

func str(v any) string { s, _ := v.(string); return s }
func i64(v any) int64  { n, _ := v.(int64); return n }

// ---------------------------------------------------------------- printers

func printWhoami(m moodle.M) {
	fmt.Printf("%s Signed in as %s %s\n", green("✓"), bold(str(m["fullname"])), dim("("+str(m["username"])+")"))
	fmt.Printf("  %s · Moodle %s · %v web-service functions\n", m["site"], strings.Fields(str(m["release"]) + " ")[0], m["functions_available"])
}

func printCourses(items []moodle.M) {
	for _, x := range items {
		fmt.Printf("%s %s %s\n", courseTag(x["shortname"]), x["fullname"], dim(fmt.Sprintf("(id %v)", x["id"])))
	}
}

func printDue(items []moodle.M, days int) {
	if len(items) == 0 {
		fmt.Printf("%s Nothing due in the next %d days.\n", green("✓"), days)
		return
	}
	for _, e := range items {
		fmt.Printf("%s %s %s  %s\n", until(i64(e["due_ts"])), courseTag(e["course"]), bold(str(e["name"])), dim(str(e["due"])))
		fmt.Printf("%*s%s\n", 13+labelWidth, "", dim(str(e["url"])))
	}
}

func printAssignments(items []moodle.M) {
	for _, a := range items {
		icon := red("✗")
		switch a["submission"] {
		case "submitted":
			icon = green("✓")
		case "draft":
			icon = yellow("✎")
		}
		when := dim("no due date")
		if ts := i64(a["due_ts"]); ts != 0 {
			when = until(ts)
			if a["submission"] == "submitted" {
				when = dim(fmt.Sprintf("%-11s", "done"))
			}
		}
		grade := ""
		if g := str(a["grade"]); g != "" {
			grade = "  " + cyan("grade "+g)
		}
		fmt.Printf("%s %s %s %s%s\n", icon, when, courseTag(a["course"]), a["name"], grade)
	}
	fmt.Println(dim("\n✓ submitted  ✎ draft  ✗ not submitted"))
}

func printNews(items []moodle.M) {
	if len(items) == 0 {
		fmt.Println("No announcements.")
		return
	}
	for _, d := range items {
		fmt.Printf("%s %s\n", courseTag(courseShort[i64(d["course_id"])]), bold(str(d["subject"])))
		fmt.Printf("  %s\n", dim(fmt.Sprintf("%v · %s", d["author"], ago(i64(d["posted_ts"])))))
		fmt.Println(wrap(str(d["message"]), 2, 4))
		fmt.Println()
	}
}

var notifTrailer = regexp.MustCompile(`\.?\s*Change your notification preferences\.?\s*$`)

// tidy drops the "Change your notification preferences" boilerplate Moodle appends to notifications.
func tidy(s string) string { return strings.TrimSpace(notifTrailer.ReplaceAllString(s, "")) }

func printNotifications(items []moodle.M) {
	if len(items) == 0 {
		fmt.Println("No notifications.")
		return
	}
	for _, n := range items {
		dot := yellow("●")
		if n["read"] == true {
			dot = dim("○")
		}
		subject := tidy(str(n["subject"]))
		fmt.Printf("%s %s %s\n", dot, dim(fmt.Sprintf("%-8s", ago(i64(n["when_ts"])))), bold(subject))
		if t := tidy(str(n["text"])); t != "" && t != subject {
			fmt.Println(dim(wrap(t, 11, 2)))
		}
	}
	fmt.Println(dim("\n● unread  ○ read"))
}

func bar(percent string) string {
	p, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(percent), "%")), 64)
	if err != nil {
		return dim(strings.Repeat("·", 10))
	}
	n := min(max(int(p/10+0.5), 0), 10)
	paintBar := green
	if p < 50 {
		paintBar = red
	} else if p < 75 {
		paintBar = yellow
	}
	return paintBar(strings.Repeat("█", n)) + dim(strings.Repeat("░", 10-n))
}

func printGrades(items []moodle.M) {
	for _, g := range items {
		name := str(g["item"])
		if name == "" || g["type"] == "category" {
			continue // unnamed category subtotals
		}
		grade := fmt.Sprintf("%v / %v", g["grade"], g["max"])
		if g["grade"] == "-" {
			grade = "-"
		}
		line := fmt.Sprintf("%-34s %s %10s", cut(name, 34), bar(str(g["percent"])), grade)
		if g["type"] == "course" {
			line = bold(line)
		}
		fmt.Println(line)
		if f := str(g["feedback"]); f != "" {
			fmt.Println(dim(wrap(f, 4, 3)))
		}
	}
}

func printTotals(items []moodle.M) {
	for _, g := range items {
		fmt.Printf("%s %s\n", courseTag(g["course"]), g["grade"])
	}
}

var changeWords = map[string]string{"contentfiles": "new files", "discussions": "new posts", "submissions": "submissions",
	"intro": "description edited", "introfiles": "new attachments", "entries": "new entries"}

func printNew(items []moodle.M, days int) {
	if len(items) == 0 {
		fmt.Printf("Nothing new in the last %d days.\n", days)
		return
	}
	last := ""
	for _, x := range items {
		if c := str(x["course"]); c != last {
			if last != "" {
				fmt.Println()
			}
			fmt.Println(strings.TrimRight(courseTag(c), " "))
			last = c
		}
		var what []string
		for _, ch := range x["changed"].([]string) {
			what = append(what, or(changeWords[ch], ch))
		}
		fmt.Printf("  %s %s %s\n", dim(str(x["section"])+" ›"), bold(str(x["name"])), cyan(strings.Join(what, ", ")))
		files, _ := x["files"].([]moodle.M)
		for _, f := range files {
			fmt.Printf("      %s\n", dim("· "+str(f["filename"])))
		}
	}
}

func printSearch(items []moodle.M) {
	if len(items) == 0 {
		fmt.Println("No matches.")
		return
	}
	for _, h := range items {
		name := str(h["name"])
		if name == "" {
			name = "(section text)"
		}
		fmt.Printf("%s %s %s\n", courseTag(courseShort[i64(h["course"])]), dim(str(h["section"])+" ›"), bold(name))
		fmt.Println(dim(wrap(str(h["excerpt"]), 2, 2)))
	}
}

func printContents(secs []moodle.M) {
	for _, s := range secs {
		fmt.Println(bold(or(str(s["name"]), "General")))
		for _, m := range s["modules"].([]moodle.M) {
			fmt.Printf("  %s %s %s\n", cyan(fmt.Sprintf("%-10v", m["type"])), m["name"], dim(fmt.Sprintf("(cmid %v)", m["cmid"])))
			files, _ := m["files"].([]moodle.M)
			for _, f := range files {
				fmt.Printf("             %s\n", dim("· "+str(f["filename"])))
			}
		}
		fmt.Println()
	}
}

func printSync(res moodle.M) {
	for _, p := range res["downloaded"].([]string) {
		fmt.Println(green("↓"), p)
	}
	for _, f := range res["failed"].([]moodle.M) {
		fmt.Println(red("✗"), f["file"], dim("- "+str(f["error"])))
	}
	fmt.Printf("%s %d downloaded, %v already up to date, in %v\n", green("✓"), len(res["downloaded"].([]string)), res["up_to_date"], res["folder"])
}

// printMonth draws a Sunday-first month grid (the Zewail week), marks days with events,
// highlights today, and lists the events underneath.
func printMonth(first time.Time, events []moodle.M) {
	byDay := map[int][]moodle.M{}
	for _, e := range events {
		d := time.Unix(i64(e["start_ts"]), 0)
		byDay[d.Day()] = append(byDay[d.Day()], e)
	}
	title := first.Format("January 2006")
	fmt.Printf("%*s\n", (35+len(title))/2, bold(title))
	fmt.Println(dim("  Sun  Mon  Tue  Wed  Thu  Fri  Sat"))
	fmt.Print(strings.Repeat("     ", int(first.Weekday())))
	today, days := time.Now(), first.AddDate(0, 1, -1).Day()
	for d := 1; d <= days; d++ {
		cell := strconv.Itoa(d)
		if len(byDay[d]) > 0 {
			cell += "*"
		}
		isToday := first.Year() == today.Year() && first.Month() == today.Month() && d == today.Day()
		if isToday {
			cell = "[" + cell + "]"
		}
		cell = fmt.Sprintf("%5s", cell)
		switch {
		case isToday:
			cell = paint("1;7", cell)
		case len(byDay[d]) > 0:
			cell = yellow(cell)
		}
		fmt.Print(cell)
		if (int(first.Weekday())+d)%7 == 0 || d == days {
			fmt.Println()
		}
	}
	if len(events) == 0 {
		fmt.Println("\nNothing on the calendar this month.")
		return
	}
	fmt.Println(dim("\n* = something that day, [ ] = today"))
	fmt.Println()
	for _, e := range events {
		start := time.Unix(i64(e["start_ts"]), 0)
		length := ""
		if m := i64(e["duration_min"]); m > 0 {
			length = dim(fmt.Sprintf(" (%d min)", m))
		}
		name := str(e["name"])
		if e["type"] == "due" {
			name = bold(name)
		}
		fmt.Printf("%s %s  %s %s%s\n", dim(fmt.Sprintf("%-10s", start.Format("Mon Jan 2"))),
			fmt.Sprintf("%8s", start.Format("3:04 PM")), courseTag(e["course"]), name, length)
	}
}

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
