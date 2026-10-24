package moodle

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const instructions = "Tools for the user's Zewail City Moodle (moodle.zewailcity.edu.eg), acting as the user with their own permissions. " +
	"Ids: course_id = course/view.php?id=…, cmid = the id in any mod/<type>/view.php?id=… link, " +
	"assign_id = the assignment instance id returned by moodle_assignments. " +
	"Start with moodle_deadlines for 'what is due', moodle_courses for ids, moodle_course_contents to browse. " +
	"moodle_deadlines only lists unfinished assignments/quizzes; readings and in-class work live on course pages. " +
	"Times are shown in the computer's local time (Cairo for most users). " +
	"moodle_submit_assignment changes the user's real submission: show exactly what will be sent and get a " +
	"clear yes first, and only pass submit_for_grading=true when they say to finalise."

var (
	yes, no = true, false
	read    = &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &yes}
	write   = &mcp.ToolAnnotations{DestructiveHint: &yes, OpenWorldHint: &yes}
	save    = &mcp.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &yes}
)

// clientFn returns the shared client, built on first use so a missing token
// shows up as a tool error instead of a server that won't start.
type clientFn func() (*Client, error)

func lazyClient() clientFn {
	var mu sync.Mutex
	var c *Client
	return func() (*Client, error) {
		mu.Lock()
		defer mu.Unlock()
		if c == nil {
			var err error
			if c, err = New(); err != nil {
				return nil, err
			}
		}
		return c, nil
	}
}

// tool registers one Moodle tool. Results go back as a JSON text block.
func tool[In any](s *mcp.Server, m clientFn, name string, ann *mcp.ToolAnnotations, desc string,
	fn func(*Client, In) (any, error)) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: desc, Annotations: ann},
		func(_ context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			c, err := m()
			if err != nil {
				return nil, nil, err
			}
			out, err := fn(c, in)
			if err != nil {
				return nil, nil, err
			}
			text, err := toJSON(out)
			if err != nil {
				return nil, nil, err
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
		})
}

func toJSON(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	err := enc.Encode(v)
	return buf.String(), err
}

func or[T comparable](v, def T) T {
	var zero T
	if v == zero {
		return def
	}
	return v
}

func orTrue(b *bool) bool { return b == nil || *b }

func NewServer() *mcp.Server { return newServer(lazyClient()) }

func newServer(m clientFn) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "moodle", Version: "0.2.0"}, &mcp.ServerOptions{Instructions: instructions})

	tool(s, m, "moodle_whoami", read,
		"Check the connection: site name, Moodle version, and the logged-in user.",
		func(c *Client, _ struct{}) (any, error) { return c.Whoami() })

	type courses struct {
		Which string `json:"which,omitempty" jsonschema:"inprogress (default), future, past or all"`
	}
	tool(s, m, "moodle_courses", read,
		"List enrolled courses with their ids. which: inprogress (default) | future | past | all.",
		func(c *Client, in courses) (any, error) { return c.Courses(or(in.Which, "inprogress")) })

	type contents struct {
		CourseID            int64 `json:"course_id"`
		IncludeDescriptions *bool `json:"include_descriptions,omitempty" jsonschema:"default true"`
	}
	tool(s, m, "moodle_course_contents", read,
		"Every section of a course with its activities/resources (cmid, type, name, dates, files, links) "+
			"and the section text (where teachers often put weekly readings and 'for next class' notes).",
		func(c *Client, in contents) (any, error) {
			return c.CourseContents(in.CourseID, orTrue(in.IncludeDescriptions))
		})

	type search struct {
		Query     string  `json:"query"`
		CourseIDs []int64 `json:"course_ids,omitempty" jsonschema:"defaults to current courses"`
	}
	tool(s, m, "moodle_search", read,
		"Find activities, files and section text mentioning `query` across current courses (or the given ones).",
		func(c *Client, in search) (any, error) { return c.Search(in.Query, in.CourseIDs) })

	type deadlines struct {
		DaysAhead int   `json:"days_ahead,omitempty" jsonschema:"default 14"`
		DaysBack  int   `json:"days_back,omitempty" jsonschema:"days of overdue items to include, default 0"`
		CourseID  int64 `json:"course_id,omitempty"`
	}
	tool(s, m, "moodle_deadlines", read,
		"Unfinished assignment/quiz deadlines from the Moodle timeline. days_back > 0 also returns overdue items. "+
			"Dates here can lag edits teachers make; confirm critical ones with moodle_assignment.",
		func(c *Client, in deadlines) (any, error) {
			return c.Deadlines(or(in.DaysAhead, 14), in.DaysBack, in.CourseID)
		})

	type assignments struct {
		CourseIDs     []int64 `json:"course_ids,omitempty" jsonschema:"defaults to current courses"`
		IncludeStatus *bool   `json:"include_status,omitempty" jsonschema:"default true"`
		OnlyOpen      bool    `json:"only_open,omitempty"`
	}
	tool(s, m, "moodle_assignments", read,
		"All assignments in current courses (or the given ones), sorted by due date, with "+
			"submission status (new/draft/submitted/nosubmission) and grade when include_status is true.",
		func(c *Client, in assignments) (any, error) {
			return c.Assignments(in.CourseIDs, orTrue(in.IncludeStatus), in.OnlyOpen)
		})

	type assignment struct {
		AssignID int64 `json:"assign_id,omitempty"`
		CMID     int64 `json:"cmid,omitempty"`
	}
	tool(s, m, "moodle_assignment", read,
		"One assignment in full: instructions, attachment files, dates, time remaining, accepted submission "+
			"types, what's already submitted, grade and feedback. Pass assign_id or cmid.",
		func(c *Client, in assignment) (any, error) { return c.Assignment(in.AssignID, in.CMID) })

	type grades struct {
		CourseID int64 `json:"course_id,omitempty"`
	}
	tool(s, m, "moodle_grades", read,
		"Grade items for one course (grade, max, %, weight, feedback). Without course_id: course totals for all courses.",
		func(c *Client, in grades) (any, error) {
			if in.CourseID == 0 {
				return c.CourseTotals()
			}
			return c.Grades(in.CourseID)
		})

	type cmid struct {
		CMID int64 `json:"cmid"`
	}
	tool(s, m, "moodle_read", read,
		"Open any activity/resource by cmid and return its readable content: page text, file list for "+
			"resources/folders, link target for URLs, assignment detail, forum discussions, quiz dates/attempts.",
		func(c *Client, in cmid) (any, error) { return c.ReadModule(in.CMID) })

	type fileText struct {
		FileURL  string `json:"fileurl"`
		Offset   int    `json:"offset,omitempty"`
		MaxChars int    `json:"max_chars,omitempty" jsonschema:"default 40000"`
	}
	tool(s, m, "moodle_file_text", read,
		"Text of a Moodle file (PDF, DOCX, PPTX, XLSX strings, HTML, plain text) from a `fileurl` returned by "+
			"another tool. Long files come back in slices: call again with offset to continue.",
		func(c *Client, in fileText) (any, error) {
			text, err := c.FileText(in.FileURL)
			if err != nil {
				return nil, err
			}
			r := []rune(text)
			start := min(max(in.Offset, 0), len(r))
			end := min(start+or(in.MaxChars, 40000), len(r))
			var next any
			if end < len(r) {
				next = end
			}
			return M{"total_chars": len(r), "offset": start, "next_offset": next, "text": string(r[start:end])}, nil
		})

	type download struct {
		FileURL string `json:"fileurl"`
		DestDir string `json:"dest_dir,omitempty" jsonschema:"default ~/Downloads/Moodle"`
	}
	tool(s, m, "moodle_download", save,
		"Save a Moodle file to disk (default ~/Downloads/Moodle) and return the local path.",
		func(c *Client, in download) (any, error) {
			p, err := c.Download(in.FileURL, or(in.DestDir, "~/Downloads/Moodle"))
			return M{"path": p}, err
		})

	type announcements struct {
		CourseIDs []int64 `json:"course_ids,omitempty" jsonschema:"defaults to current courses"`
		PerCourse int     `json:"per_course,omitempty" jsonschema:"default 5"`
	}
	tool(s, m, "moodle_announcements", read,
		"Latest posts in each course's Announcements forum, newest first.",
		func(c *Client, in announcements) (any, error) {
			return c.Announcements(in.CourseIDs, or(in.PerCourse, 5))
		})

	type discussion struct {
		DiscussionID int64 `json:"discussion_id"`
	}
	tool(s, m, "moodle_discussion", read,
		"All posts in one forum discussion (ids come from moodle_announcements / moodle_read on a forum).",
		func(c *Client, in discussion) (any, error) { return c.Discussion(in.DiscussionID) })

	type notifications struct {
		Limit      int  `json:"limit,omitempty" jsonschema:"default 20"`
		UnreadOnly bool `json:"unread_only,omitempty"`
	}
	tool(s, m, "moodle_notifications", read,
		"The bell-icon notifications: new grades, feedback, forum posts, due-date reminders.",
		func(c *Client, in notifications) (any, error) {
			return c.Notifications(or(in.Limit, 20), in.UnreadOnly)
		})

	type submit struct {
		AssignID         int64    `json:"assign_id"`
		TextHTML         *string  `json:"text_html,omitempty" jsonschema:"online text as HTML"`
		FilePaths        []string `json:"file_paths,omitempty" jsonschema:"local files to attach"`
		SubmitForGrading bool     `json:"submit_for_grading,omitempty"`
	}
	tool(s, m, "moodle_submit_assignment", write,
		"Save the user's submission for an assignment: online text (HTML) and/or local files. This REPLACES the "+
			"current submission's text/files. Leaves it as a draft unless submit_for_grading=true, which finalises it "+
			"(and accepts the submission statement). Only call after the user has approved the exact content.",
		func(c *Client, in submit) (any, error) {
			return c.SaveSubmission(in.AssignID, in.TextHTML, in.FilePaths, in.SubmitForGrading)
		})

	type functions struct {
		Match string `json:"match,omitempty" jsonschema:"substring filter, e.g. calendar"`
	}
	tool(s, m, "moodle_functions", read,
		"Names of the Moodle web-service functions this token can call (filter with `match`, e.g. 'calendar'). "+
			"Use it to find a function for moodle_call when no dedicated tool fits.",
		func(c *Client, in functions) (any, error) { return c.Functions(in.Match) })

	type call struct {
		WSFunction string `json:"wsfunction"`
		Params     M      `json:"params,omitempty"`
	}
	tool(s, m, "moodle_call", write,
		"Escape hatch: call any Moodle web-service function the token allows, e.g. "+
			"wsfunction='mod_quiz_get_quizzes_by_courses', params={'courseids': [123]}. Nested params are fine. "+
			"Treat any function that writes (save/submit/update/delete/send) like a submission: confirm first.",
		func(c *Client, in call) (any, error) { return c.Call(in.WSFunction, in.Params) })

	addResources(s, m)
	addPrompts(s)
	return s
}

// ---------------------------------------------------------------- resources

// resource registers a read-only JSON resource (or template, when uri contains "{").
func resource(s *mcp.Server, m clientFn, uri, name, desc string, fn func(c *Client, uri string) (any, error)) {
	h := func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		c, err := m()
		if err != nil {
			return nil, err
		}
		out, err := fn(c, req.Params.URI)
		if err != nil {
			return nil, err
		}
		text, err := toJSON(out)
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{
			{URI: req.Params.URI, MIMEType: "application/json", Text: text}}}, err
	}
	if strings.Contains(uri, "{") {
		s.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: uri, Name: name, Description: desc, MIMEType: "application/json"}, h)
	} else {
		s.AddResource(&mcp.Resource{URI: uri, Name: name, Description: desc, MIMEType: "application/json"}, h)
	}
}

func addResources(s *mcp.Server, m clientFn) {
	resource(s, m, "moodle://courses", "courses", "Your current Zewail courses with ids, codes and dates.",
		func(c *Client, _ string) (any, error) { return c.Courses("inprogress") })
	resource(s, m, "moodle://deadlines", "deadlines", "Unfinished deadlines for the next 14 days, plus anything overdue from the last 7.",
		func(c *Client, _ string) (any, error) { return c.Deadlines(14, 7, 0) })
	resource(s, m, "moodle://course/{course_id}", "course", "Every section, activity, file and link in one course.",
		func(c *Client, uri string) (any, error) {
			id, err := strconv.ParseInt(strings.TrimPrefix(uri, "moodle://course/"), 10, 64)
			if err != nil {
				return nil, mcp.ResourceNotFoundError(uri)
			}
			return c.CourseContents(id, true)
		})
}

// ---------------------------------------------------------------- prompts

type promptArg struct {
	name, desc string
	required   bool
}

// prompt registers a slash-command style prompt whose text is built from its arguments.
func prompt(s *mcp.Server, name, desc string, args []promptArg, build func(a map[string]string) string) {
	p := &mcp.Prompt{Name: name, Description: desc}
	for _, a := range args {
		p.Arguments = append(p.Arguments, &mcp.PromptArgument{Name: a.name, Description: a.desc, Required: a.required})
	}
	s.AddPrompt(p, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{Description: desc, Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: build(req.Params.Arguments)}}}}, nil
	})
}

func addPrompts(s *mcp.Server) {
	prompt(s, "whats_due", "Plan the coming days around your Moodle deadlines.",
		[]promptArg{{"days", "how many days ahead to look (default 7)", false}},
		func(a map[string]string) string {
			return "Using my Zewail Moodle, call moodle_deadlines with days_ahead=" + or(a["days"], "7") + " and days_back=7. " +
				"List what's due grouped by day, flag anything overdue, and for each item due in the next 48 hours call " +
				"moodle_assignment to check whether I've already submitted and what the instructions ask for. " +
				"End with a short suggested order to tackle them."
		})
	prompt(s, "study_course", "Turn a course's latest material into a summary and practice questions.",
		[]promptArg{{"course", "course name or code, e.g. MATH104 or Linear Algebra", true},
			{"topic", "optional topic or week to focus on", false}},
		func(a map[string]string) string {
			focus := "the most recent week's material"
			if a["topic"] != "" {
				focus = "material about " + a["topic"]
			}
			return "Find my course matching \"" + a["course"] + "\" with moodle_courses, then browse it with moodle_course_contents. " +
				"Pick " + focus + " (lecture slides, PDFs, pages) and read it with moodle_file_text or moodle_read. " +
				"Give me a concise summary of the key ideas, then 5 practice questions with worked answers hidden " +
				"below a 'Solutions' heading. Cite which file each point came from."
		})
	prompt(s, "weekly_digest", "One summary of this week on Moodle: deadlines, announcements, grades, notifications.",
		nil,
		func(map[string]string) string {
			return "Give me a weekly Moodle digest. Use moodle_deadlines (days_ahead=7, days_back=7), " +
				"moodle_announcements, moodle_notifications (unread_only=true) and moodle_grades. " +
				"Sections: Due this week, New announcements (one line each), New grades or feedback, Anything else I should act on. " +
				"Keep it short and skip empty sections."
		})
}
