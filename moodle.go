// Package moodle is a thin client for Zewail City's Moodle web-service API.
//
// It talks to the REST endpoint (webservice/rest/server.php) with a user token,
// the same kind the official Moodle app uses. Every helper returns plain
// maps/slices already trimmed down to the fields worth reading.
package moodle

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const SiteURL = "https://moodle.zewailcity.edu.eg"

// M is one trimmed record, or one raw JSON object from Moodle.
type M = map[string]any

type Error struct{ Msg, Code string }

func (e *Error) Error() string { return e.Msg }

// ---------------------------------------------------------------- config

func ConfigPath() string {
	if p := os.Getenv("MOODLE_MCP_CONFIG"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "moodle-mcp", "config.json")
}

// LoadToken reads MOODLE_TOKEN, falling back to the config file.
func LoadToken() string {
	if t := os.Getenv("MOODLE_TOKEN"); t != "" {
		return t
	}
	var cfg struct{ Token string }
	if b, err := os.ReadFile(ConfigPath()); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	return cfg.Token
}

func SaveToken(token string) error {
	p := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(M{"url": SiteURL, "token": token}, "", "  ")
	return os.WriteFile(p, b, 0o600)
}

func ClearToken() bool { return os.Remove(ConfigPath()) == nil }

var (
	launchToken = regexp.MustCompile(`token=([A-Za-z0-9+/=_-]+)`)
	bareToken   = regexp.MustCompile(`^[A-Za-z0-9]{20,64}$`)
)

// ParseToken accepts a bare token or the moodlemobile://token=... link the app launch page produces.
func ParseToken(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if m := launchToken.FindStringSubmatch(raw); m != nil {
		decoded, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(m[1], "="))
		parts := strings.Split(string(decoded), ":::")
		if err != nil || len(parts) < 2 {
			return "", errors.New("that link didn't contain a token")
		}
		return parts[1], nil
	}
	if !bareToken.MatchString(raw) {
		return "", errors.New("that doesn't look like a Moodle token (expected ~32 letters/digits)")
	}
	return raw, nil
}

// ---------------------------------------------------------------- client

type Client struct {
	URL, Token string
	HTTP       *http.Client

	mu sync.Mutex
	me M
}

func New() (*Client, error) {
	token := LoadToken()
	if token == "" {
		return nil, &Error{"No Moodle token configured. Run `moodle auth` in a terminal to add one.", "notoken"}
	}
	return NewWithToken(token), nil
}

func NewWithToken(token string) *Client {
	return &Client{URL: SiteURL, Token: token, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

func (c *Client) do(req *http.Request) ([]byte, string, error) {
	req.Header.Set("User-Agent", "moodle-mcp/0.2 (MoodleMobile)")
	r, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err == nil && r.StatusCode >= 400 {
		err = fmt.Errorf("%s: HTTP %d", req.URL.Path, r.StatusCode)
	}
	return body, r.Header.Get("Content-Type"), err
}

// Call runs one web-service function. It is the only network path for API calls.
func (c *Client) Call(wsfunction string, params M) (any, error) {
	form := url.Values{}
	flatten("", params, form)
	form.Set("wstoken", c.Token)
	form.Set("wsfunction", wsfunction)
	form.Set("moodlewsrestformat", "json")
	req, _ := http.NewRequest("POST", c.URL+"/webservice/rest/server.php", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	body, _, err := c.do(req)
	if err != nil {
		return nil, err
	}
	var out any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, fmt.Errorf("%s: unreadable response: %w", wsfunction, err)
		}
	}
	if e, ok := out.(M); ok && e["exception"] != nil {
		code, msg := str(e["errorcode"]), str(e["message"])
		if (code == "invalidtoken" || code == "accessexception") && strings.Contains(strings.ToLower(msg), "token") {
			msg += " — the token is missing or expired; run `moodle auth` again."
		}
		return nil, &Error{wsfunction + ": " + msg, code}
	}
	return out, nil
}

// flatten turns nested params into Moodle's courseids[0]=1&options[0][name]=x form.
func flatten(prefix string, v any, out url.Values) {
	key := func(k string) string {
		if prefix == "" {
			return k
		}
		return prefix + "[" + k + "]"
	}
	switch v := v.(type) {
	case nil:
	case M:
		for k, x := range v {
			flatten(key(k), x, out)
		}
	case []any:
		for i, x := range v {
			flatten(key(strconv.Itoa(i)), x, out)
		}
	case []M:
		for i, x := range v {
			flatten(key(strconv.Itoa(i)), x, out)
		}
	case []int64:
		for i, x := range v {
			out.Set(key(strconv.Itoa(i)), strconv.FormatInt(x, 10))
		}
	case bool:
		out.Set(prefix, map[bool]string{true: "1", false: "0"}[v])
	case float64: // JSON numbers; %v would print timestamps as 1.7e+09
		out.Set(prefix, strconv.FormatFloat(v, 'f', -1, 64))
	default:
		out.Set(prefix, fmt.Sprint(v))
	}
}

func (c *Client) Me() (M, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.me == nil {
		res, err := c.Call("core_webservice_get_site_info", nil)
		if err != nil {
			return nil, err
		}
		c.me = obj(res)
	}
	return c.me, nil
}

func (c *Client) userID() (int64, error) {
	me, err := c.Me()
	return num(me["userid"]), err
}

func (c *Client) Whoami() (M, error) {
	m, err := c.Me()
	if err != nil {
		return nil, err
	}
	return M{"site": m["sitename"], "url": m["siteurl"], "release": m["release"], "userid": m["userid"],
		"username": m["username"], "fullname": m["fullname"], "functions_available": len(list(m["functions"]))}, nil
}

// Functions lists every web-service function this token may call, optionally filtered by a substring.
func (c *Client) Functions(match string) ([]string, error) {
	m, err := c.Me()
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, f := range list(m["functions"]) {
		if n := str(f["name"]); strings.Contains(strings.ToLower(n), strings.ToLower(match)) {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names, nil
}

// ---------------------------------------------------------------- courses

// Courses: which is inprogress | future | past | all.
func (c *Client) Courses(which string) ([]M, error) {
	var raw []M
	if which == "all" {
		uid, err := c.userID()
		if err != nil {
			return nil, err
		}
		res, err := c.Call("core_enrol_get_users_courses", M{"userid": uid})
		if err != nil {
			return nil, err
		}
		raw = list(res)
	} else {
		res, err := c.Call("core_course_get_enrolled_courses_by_timeline_classification",
			M{"classification": which, "limit": 0})
		if err != nil {
			return nil, err
		}
		raw = list(obj(res)["courses"])
	}
	out := []M{}
	for _, x := range raw {
		id, short := num(x["id"]), u(x["shortname"])
		// Zewail full names end in "-<shortname>", e.g. "Linear Algebra -MATH104-FALL2026-LCTR02".
		full := strings.TrimSpace(strings.TrimSuffix(u(x["fullname"]), "-"+short))
		out = append(out, M{"id": id, "shortname": short, "fullname": full,
			"start": FmtTime(num(x["startdate"])), "end": FmtTime(num(x["enddate"])), "progress": x["progress"],
			"url": fmt.Sprintf("%s/course/view.php?id=%d", c.URL, id)})
	}
	return out, nil
}

func (c *Client) courseIDs(ids []int64) ([]int64, error) {
	if len(ids) > 0 {
		return ids, nil
	}
	courses, err := c.Courses("inprogress")
	for _, x := range courses {
		ids = append(ids, x["id"].(int64))
	}
	return ids, err
}

func (c *Client) CourseContents(courseID int64, descriptions bool) ([]M, error) {
	res, err := c.Call("core_course_get_contents", M{"courseid": courseID})
	if err != nil {
		return nil, err
	}
	out := []M{}
	for _, s := range list(res) {
		mods := []M{}
		for _, m := range list(s["modules"]) {
			if m["uservisible"] == false {
				continue
			}
			mod := M{"cmid": num(m["id"]), "type": m["modname"], "name": u(m["name"]), "instance": m["instance"], "url": m["url"]}
			var dates []string
			for _, d := range list(m["dates"]) {
				if ts := num(d["timestamp"]); ts != 0 {
					dates = append(dates, str(d["label"])+" "+FmtTime(ts))
				}
			}
			if dates != nil {
				mod["dates"] = dates
			}
			if descriptions && str(m["description"]) != "" {
				mod["description"] = cut(HTMLToText(str(m["description"])), 1500)
			}
			var files []M
			for _, f := range list(m["contents"]) {
				switch str(f["type"]) {
				case "file":
					files = append(files, M{"filename": f["filename"], "fileurl": f["fileurl"], "size": f["filesize"],
						"modified": FmtTime(num(f["timemodified"]))})
				case "url":
					if m["modname"] == "url" && mod["external_url"] == nil {
						mod["external_url"] = f["fileurl"]
					}
				}
			}
			if files != nil {
				mod["files"] = files
			}
			if comp := obj(m["completiondata"]); comp != nil {
				mod["completed"] = num(comp["state"]) != 0
			}
			mods = append(mods, mod)
		}
		sec := M{"section": s["section"], "name": u(s["name"]), "modules": mods}
		if descriptions && str(s["summary"]) != "" {
			sec["summary"] = cut(HTMLToText(str(s["summary"])), 3000)
		}
		if len(mods) > 0 || sec["summary"] != nil {
			out = append(out, sec)
		}
	}
	return out, nil
}

// Search does a case-insensitive match over module names, descriptions, section text and file names.
func (c *Client) Search(query string, courseIDs []int64) ([]M, error) {
	q := strings.ToLower(query)
	ids, err := c.courseIDs(courseIDs)
	if err != nil {
		return nil, err
	}
	hits := []M{}
	for _, cid := range ids {
		secs, err := c.CourseContents(cid, true)
		if err != nil {
			return nil, err
		}
		for _, sec := range secs {
			summary, name := str(sec["summary"]), str(sec["name"])
			if strings.Contains(strings.ToLower(summary+"\n"+name), q) {
				hits = append(hits, M{"course": cid, "section": name, "match": "section text",
					"excerpt": excerpt(summary+"\n"+name, q)})
			}
			for _, m := range sec["modules"].([]M) {
				blob := str(m["name"]) + " " + str(m["description"])
				files, _ := m["files"].([]M)
				for _, f := range files {
					blob += " " + str(f["filename"])
				}
				if strings.Contains(strings.ToLower(blob), q) {
					hits = append(hits, M{"course": cid, "section": name, "cmid": m["cmid"], "type": m["type"],
						"name": m["name"], "url": m["url"], "excerpt": excerpt(blob, q)})
				}
			}
		}
	}
	return hits, nil
}

// ---------------------------------------------------------------- deadlines

func (c *Client) Deadlines(daysAhead, daysBack int, courseID int64) ([]M, error) {
	now := time.Now().Unix()
	p := M{"timesortfrom": now - int64(daysBack)*86400, "timesortto": now + int64(daysAhead)*86400, "limitnum": 50}
	fn := "core_calendar_get_action_events_by_timesort"
	if courseID != 0 {
		fn, p["courseid"] = "core_calendar_get_action_events_by_course", courseID
	} else {
		p["limittononsuspendedevents"] = true
	}
	res, err := c.Call(fn, p)
	if err != nil {
		return nil, err
	}
	out := []M{}
	for _, e := range list(obj(res)["events"]) {
		course, due := obj(e["course"]), num(e["timesort"])
		name := e["activityname"]
		if str(name) == "" {
			name = e["name"]
		}
		out = append(out, M{"name": u(name), "course": u(course["shortname"]), "course_id": course["id"],
			"type": e["modulename"], "cmid": cmidOf(str(e["url"])), "due": FmtTime(due), "due_ts": due,
			"overdue": due < now || e["overdue"] == true, "action": obj(e["action"])["name"], "url": e["url"]})
	}
	return out, nil
}

// ---------------------------------------------------------------- assignments

func (c *Client) Assignments(courseIDs []int64, withStatus, onlyOpen bool) ([]M, error) {
	ids, err := c.courseIDs(courseIDs)
	if err != nil {
		return nil, err
	}
	res, err := c.Call("mod_assign_get_assignments", M{"courseids": ids})
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	out := []M{}
	for _, crs := range list(obj(res)["courses"]) {
		for _, a := range list(crs["assignments"]) {
			due, cutoff := num(a["duedate"]), num(a["cutoffdate"])
			if onlyOpen && cutoff != 0 && cutoff < now {
				continue
			}
			item := M{"assign_id": num(a["id"]), "cmid": num(a["cmid"]), "course": u(crs["shortname"]),
				"course_id": crs["id"], "name": u(a["name"]), "due": FmtTime(due), "due_ts": due,
				"cutoff": FmtTime(cutoff), "url": fmt.Sprintf("%s/mod/assign/view.php?id=%d", c.URL, num(a["cmid"]))}
			if withStatus {
				if st, err := c.assignStatusBrief(num(a["id"])); err != nil {
					item["status_error"] = err.Error()
				} else {
					for k, v := range st {
						item[k] = v
					}
				}
			}
			out = append(out, item)
		}
	}
	sortKey := func(m M) int64 {
		if d := m["due_ts"].(int64); d != 0 {
			return d
		}
		return 1 << 40
	}
	sort.SliceStable(out, func(i, j int) bool { return sortKey(out[i]) < sortKey(out[j]) })
	return out, nil
}

func (c *Client) assignStatusBrief(assignID int64) (M, error) {
	res, err := c.Call("mod_assign_get_submission_status", M{"assignid": assignID})
	if err != nil {
		return nil, err
	}
	st := obj(res)
	last := obj(st["lastattempt"])
	sub := obj(last["submission"])
	if sub == nil {
		sub = obj(last["teamsubmission"])
	}
	status := str(sub["status"])
	if status == "" {
		status = "nosubmission"
	}
	brief := M{"submission": status, "grading": last["gradingstatus"]}
	if g := str(obj(st["feedback"])["gradefordisplay"]); g != "" {
		brief["grade"] = HTMLToText(g)
	}
	return brief, nil
}

// Assignment returns full detail for one assignment: instructions, attachments, dates, submission, feedback.
func (c *Client) Assignment(assignID, cmid int64) (M, error) {
	var courseIDs []int64
	if assignID == 0 {
		if cmid == 0 {
			return nil, &Error{Msg: "Give assign_id or cmid."}
		}
		res, err := c.Call("core_course_get_course_module", M{"cmid": cmid})
		if err != nil {
			return nil, err
		}
		cm := obj(obj(res)["cm"])
		assignID, courseIDs = num(cm["instance"]), []int64{num(cm["course"])}
	}
	res, err := c.Call("mod_assign_get_assignments", M{"courseids": courseIDs})
	if err != nil {
		return nil, err
	}
	var a M
	for _, crs := range list(obj(res)["courses"]) {
		for _, x := range list(crs["assignments"]) {
			if num(x["id"]) == assignID {
				a = x
			}
		}
	}
	if a == nil {
		return nil, &Error{Msg: fmt.Sprintf("Assignment %d not found in your courses.", assignID)}
	}
	sres, err := c.Call("mod_assign_get_submission_status", M{"assignid": assignID})
	if err != nil {
		return nil, err
	}
	st := obj(sres)
	last := obj(st["lastattempt"])
	sub := obj(last["submission"])
	subFiles, subText := []M{}, ""
	for _, p := range list(sub["plugins"]) {
		for _, fa := range list(p["fileareas"]) {
			for _, f := range list(fa["files"]) {
				subFiles = append(subFiles, M{"filename": f["filename"], "fileurl": f["fileurl"]})
			}
		}
		for _, ef := range list(p["editorfields"]) {
			if t := str(ef["text"]); t != "" {
				subText = HTMLToText(t)
			}
		}
	}
	types := []string{}
	for _, cfg := range list(a["configs"]) {
		if cfg["subtype"] == "assignsubmission" && cfg["name"] == "enabled" && cfg["value"] == "1" && cfg["plugin"] != "comments" {
			types = append(types, str(cfg["plugin"]))
		}
	}
	fb := obj(st["feedback"])
	var feedback []string
	for _, p := range list(fb["plugins"]) {
		for _, ef := range list(p["editorfields"]) {
			if t := str(ef["text"]); t != "" {
				feedback = append(feedback, HTMLToText(t))
			}
		}
	}
	status := str(sub["status"])
	if status == "" {
		status = "nosubmission"
	}
	due := num(a["duedate"])
	remaining := ""
	if due != 0 {
		remaining = timeLeft(due)
	}
	attachments := []M{}
	for _, f := range list(a["introattachments"]) {
		attachments = append(attachments, M{"filename": f["filename"], "fileurl": f["fileurl"]})
	}
	return M{
		"assign_id": num(a["id"]), "cmid": num(a["cmid"]), "name": u(a["name"]),
		"url":   fmt.Sprintf("%s/mod/assign/view.php?id=%d", c.URL, num(a["cmid"])),
		"opens": FmtTime(num(a["allowsubmissionsfromdate"])), "due": FmtTime(due),
		"cutoff": FmtTime(num(a["cutoffdate"])), "grading_due": FmtTime(num(a["gradingduedate"])),
		"submission_types":       types,
		"requires_submit_button": num(a["submissiondrafts"]) != 0,
		"requires_statement":     num(a["requiresubmissionstatement"]) != 0,
		"instructions":           HTMLToText(str(a["intro"])),
		"attachments":            attachments,
		"submission_status":      status,
		"last_modified":          FmtTime(num(sub["timemodified"])),
		"grading_status":         last["gradingstatus"],
		"can_edit":               last["canedit"], "can_submit": last["cansubmit"],
		"submitted_files": subFiles, "submitted_text": subText,
		"grade":          HTMLToText(str(fb["gradefordisplay"])),
		"feedback":       strings.Join(feedback, "\n\n"),
		"time_remaining": remaining,
	}, nil
}

// ---------------------------------------------------------------- grades

func (c *Client) Grades(courseID int64) ([]M, error) {
	uid, err := c.userID()
	if err != nil {
		return nil, err
	}
	res, err := c.Call("gradereport_user_get_grade_items", M{"courseid": courseID, "userid": uid})
	if err != nil {
		return nil, err
	}
	items := []M{}
	for _, ug := range list(obj(res)["usergrades"]) {
		for _, g := range list(ug["gradeitems"]) {
			name := str(g["itemname"])
			if name == "" {
				name = str(g["itemtype"])
				if name == "course" {
					name = "Course total"
				}
			}
			typ := g["itemmodule"]
			if typ == nil {
				typ = g["itemtype"]
			}
			items = append(items, M{"item": name, "type": typ, "cmid": g["cmid"], "grade": g["gradeformatted"],
				"max": g["grademax"], "percent": g["percentageformatted"], "weight": g["weightformatted"],
				"feedback": HTMLToText(str(g["feedback"]))})
		}
	}
	return items, nil
}

func (c *Client) CourseTotals() ([]M, error) {
	uid, err := c.userID()
	if err != nil {
		return nil, err
	}
	res, err := c.Call("gradereport_overview_get_course_grades", M{"userid": uid})
	if err != nil {
		return nil, err
	}
	courses, err := c.Courses("all")
	if err != nil {
		return nil, err
	}
	names := map[int64]any{}
	for _, x := range courses {
		names[x["id"].(int64)] = x["shortname"]
	}
	out := []M{}
	for _, g := range list(obj(res)["grades"]) {
		out = append(out, M{"course_id": num(g["courseid"]), "course": names[num(g["courseid"])], "grade": g["grade"]})
	}
	return out, nil
}

// ---------------------------------------------------------------- modules

// ReadModule opens any course activity/resource by its cmid and returns what's readable in it.
func (c *Client) ReadModule(cmid int64) (M, error) {
	res, err := c.Call("core_course_get_course_module", M{"cmid": cmid})
	if err != nil {
		return nil, err
	}
	cm := obj(obj(res)["cm"])
	kind, inst, course := str(cm["modname"]), num(cm["instance"]), num(cm["course"])
	out := M{"cmid": cmid, "type": kind, "name": u(cm["name"]), "course_id": course,
		"url": fmt.Sprintf("%s/mod/%s/view.php?id=%d", c.URL, kind, cmid)}
	merge := func(m M, err error) (M, error) {
		for k, v := range m {
			out[k] = v
		}
		return out, err
	}
	byCourse := func(key string) (M, error) {
		res, err := c.Call(fmt.Sprintf("mod_%s_get_%s_by_courses", kind, key), M{"courseids": []int64{course}})
		for _, x := range list(obj(res)[key]) {
			if num(x["id"]) == inst || num(x["coursemodule"]) == cmid {
				return x, err
			}
		}
		return M{}, err
	}

	switch kind {
	case "assign":
		return merge(c.Assignment(inst, 0))
	case "page":
		p, err := byCourse("pages")
		return merge(M{"intro": HTMLToText(str(p["intro"])), "content": HTMLToText(str(p["content"])),
			"files": files(p["contentfiles"])}, err)
	case "resource":
		r, err := byCourse("resources")
		return merge(M{"intro": HTMLToText(str(r["intro"])), "files": files(r["contentfiles"])}, err)
	case "url":
		x, err := byCourse("urls")
		return merge(M{"external_url": x["externalurl"], "intro": HTMLToText(str(x["intro"]))}, err)
	case "label":
		x, err := byCourse("labels")
		return merge(M{"content": HTMLToText(str(x["intro"]))}, err)
	case "folder":
		f, err := byCourse("folders")
		if err != nil {
			return nil, err
		}
		res, err := c.Call("core_course_get_contents", M{"courseid": course,
			"options": []M{{"name": "cmid", "value": cmid}}})
		fl := []M{}
		for _, s := range list(res) {
			for _, m := range list(s["modules"]) {
				if num(m["id"]) != cmid {
					continue
				}
				for _, x := range list(m["contents"]) {
					if x["type"] == "file" {
						fl = append(fl, M{"filename": x["filename"], "path": x["filepath"], "fileurl": x["fileurl"]})
					}
				}
			}
		}
		return merge(M{"intro": HTMLToText(str(f["intro"])), "files": fl}, err)
	case "forum":
		d, err := c.ForumDiscussions(inst, 15)
		return merge(M{"discussions": d}, err)
	case "quiz":
		q, err := byCourse("quizzes")
		if err != nil {
			return nil, err
		}
		ares, err := c.Call("mod_quiz_get_user_attempts", M{"quizid": inst, "status": "all"})
		if err != nil {
			return nil, err
		}
		attempts := []M{}
		for _, a := range list(obj(ares)["attempts"]) {
			attempts = append(attempts, M{"state": a["state"], "finished": FmtTime(num(a["timefinish"])), "grade": a["sumgrades"]})
		}
		var best any
		if b, err := c.Call("mod_quiz_get_user_best_grade", M{"quizid": inst}); err == nil {
			best = obj(b)["grade"]
		}
		var allowed any = "unlimited"
		if n := num(q["attempts"]); n != 0 {
			allowed = n
		}
		return merge(M{"intro": HTMLToText(str(q["intro"])), "opens": FmtTime(num(q["timeopen"])),
			"closes": FmtTime(num(q["timeclose"])), "time_limit_min": num(q["timelimit"]) / 60,
			"attempts_allowed": allowed, "grade_max": q["grade"], "your_attempts": attempts, "best_grade": best}, nil)
	}
	// Fallback: whatever core_course_get_contents knows about it.
	secs, err := c.CourseContents(course, true)
	var entry M
	for _, s := range secs {
		for _, m := range s["modules"].([]M) {
			if m["cmid"] == cmid {
				entry = m
			}
		}
	}
	return merge(M{"note": fmt.Sprintf("No dedicated reader for '%s'; showing the course listing entry.", kind), "entry": entry}, err)
}

// ---------------------------------------------------------------- forums

func (c *Client) ForumDiscussions(forumID int64, limit int) ([]M, error) {
	res, err := c.Call("mod_forum_get_forum_discussions",
		M{"forumid": forumID, "sortorder": -1, "page": 0, "perpage": limit})
	if err != nil {
		return nil, err
	}
	out := []M{}
	for _, d := range list(obj(res)["discussions"]) {
		subject := d["subject"]
		if str(subject) == "" {
			subject = d["name"]
		}
		out = append(out, M{"discussion_id": num(d["discussion"]), "subject": u(subject), "author": d["userfullname"],
			"posted": FmtTime(num(d["created"])), "posted_ts": num(d["created"]), "replies": d["numreplies"],
			"message": cut(HTMLToText(str(d["message"])), 2500), "attachments": files(d["attachments"])})
	}
	return out, nil
}

func (c *Client) Discussion(discussionID int64) ([]M, error) {
	res, err := c.Call("mod_forum_get_discussion_posts",
		M{"discussionid": discussionID, "sortby": "created", "sortdirection": "ASC"})
	if err != nil {
		return nil, err
	}
	out := []M{}
	for _, p := range list(obj(res)["posts"]) {
		out = append(out, M{"author": obj(p["author"])["fullname"], "posted": FmtTime(num(p["timecreated"])),
			"subject": u(p["subject"]), "message": HTMLToText(str(p["message"])), "attachments": files(p["attachments"])})
	}
	return out, nil
}

func (c *Client) Announcements(courseIDs []int64, perCourse int) ([]M, error) {
	ids, err := c.courseIDs(courseIDs)
	if err != nil {
		return nil, err
	}
	forums, err := c.Call("mod_forum_get_forums_by_courses", M{"courseids": ids})
	if err != nil {
		return nil, err
	}
	out := []M{}
	for _, f := range list(forums) {
		if f["type"] != "news" {
			continue
		}
		ds, err := c.ForumDiscussions(num(f["id"]), perCourse)
		if err != nil {
			return nil, err
		}
		for _, d := range ds {
			d["course_id"] = num(f["course"])
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i]["posted_ts"].(int64) > out[j]["posted_ts"].(int64) })
	return out, nil
}

func (c *Client) Notifications(limit int, unreadOnly bool) ([]M, error) {
	uid, err := c.userID()
	if err != nil {
		return nil, err
	}
	res, err := c.Call("message_popup_get_popup_notifications",
		M{"useridto": uid, "newestfirst": 1, "limit": limit, "offset": 0})
	if err != nil {
		return nil, err
	}
	out := []M{}
	for _, n := range list(obj(res)["notifications"]) {
		if unreadOnly && n["read"] == true {
			continue
		}
		text := str(n["smallmessage"])
		if text == "" {
			text = str(n["fullmessage"])
		}
		out = append(out, M{"subject": u(n["subject"]), "read": n["read"], "when": FmtTime(num(n["timecreated"])),
			"text": cut(HTMLToText(text), 600), "url": n["contexturl"]})
	}
	return out, nil
}

// ---------------------------------------------------------------- files

func (c *Client) fileURL(fileurl string) string {
	if !strings.Contains(fileurl, "/webservice/pluginfile.php") {
		fileurl = strings.Replace(fileurl, "/pluginfile.php", "/webservice/pluginfile.php", 1)
	}
	sep := "?"
	if strings.Contains(fileurl, "?") {
		sep = "&"
	}
	return fileurl + sep + "token=" + url.QueryEscape(c.Token)
}

// FetchFile downloads a Moodle file URL and returns its bytes, content type and file name.
func (c *Client) FetchFile(fileurl string) ([]byte, string, string, error) {
	req, err := http.NewRequest("GET", c.fileURL(fileurl), nil)
	if err != nil {
		return nil, "", "", err
	}
	data, ctype, err := c.do(req)
	if err != nil {
		return nil, "", "", err
	}
	if strings.Contains(ctype, "application/json") && bytes.HasPrefix(data, []byte("{")) {
		var e struct{ Error, Message, Errorcode string }
		_ = json.Unmarshal(data, &e)
		msg := e.Error + e.Message
		if msg == "" {
			msg = "File download failed"
		}
		return nil, "", "", &Error{msg, e.Errorcode}
	}
	name := "download"
	if p, err := url.Parse(fileurl); err == nil && path.Base(p.Path) != "/" {
		name = path.Base(p.Path)
	}
	return data, ctype, name, nil
}

func (c *Client) Download(fileurl, dest string) (string, error) {
	data, _, name, err := c.FetchFile(fileurl)
	if err != nil {
		return "", err
	}
	dir := expandHome(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, filepath.Base(name))
	return p, os.WriteFile(p, data, 0o644)
}

func (c *Client) FileText(fileurl string) (string, error) {
	data, ctype, name, err := c.FetchFile(fileurl)
	if err != nil {
		return "", err
	}
	return ExtractText(data, ctype, name)
}

// ---------------------------------------------------------------- writes

func (c *Client) uploadDraft(file string, itemID int64) (int64, error) {
	fh, err := os.Open(expandHome(file))
	if err != nil {
		return 0, err
	}
	defer fh.Close()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("token", c.Token)
	_ = w.WriteField("filearea", "draft")
	_ = w.WriteField("itemid", strconv.FormatInt(itemID, 10))
	part, _ := w.CreateFormFile("file_1", filepath.Base(file))
	if _, err := io.Copy(part, fh); err != nil {
		return 0, err
	}
	w.Close()
	req, _ := http.NewRequest("POST", c.URL+"/webservice/upload.php", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	data, _, err := c.do(req)
	if err != nil {
		return 0, err
	}
	var res any
	if err := json.Unmarshal(data, &res); err != nil {
		return 0, fmt.Errorf("upload: unreadable response: %w", err)
	}
	if e := obj(res); e != nil {
		msg := str(e["error"]) + str(e["message"])
		return 0, &Error{"upload: " + msg, str(e["errorcode"])}
	}
	files := list(res)
	if len(files) == 0 {
		return 0, &Error{Msg: "upload: Moodle returned no file"}
	}
	return num(files[0]["itemid"]), nil
}

// SaveSubmission replaces the user's submission text and/or files, and finalises it if asked.
func (c *Client) SaveSubmission(assignID int64, textHTML *string, filePaths []string, submitForGrading bool) (M, error) {
	plugindata := M{}
	if textHTML != nil {
		plugindata["onlinetext_editor"] = M{"text": *textHTML, "format": 1, "itemid": 0}
	}
	if len(filePaths) > 0 {
		var itemID int64
		for _, fp := range filePaths {
			var err error
			if itemID, err = c.uploadDraft(fp, itemID); err != nil {
				return nil, err
			}
		}
		plugindata["files_filemanager"] = itemID
	}
	if len(plugindata) == 0 {
		return nil, &Error{Msg: "Nothing to submit: give text and/or files."}
	}
	w, err := c.Call("mod_assign_save_submission", M{"assignmentid": assignID, "plugindata": plugindata})
	if err != nil {
		return nil, err
	}
	warnings := append([]any{}, asSlice(w)...)
	result := M{"saved": true}
	if submitForGrading {
		w2, err := c.Call("mod_assign_submit_for_grading", M{"assignmentid": assignID, "acceptsubmissionstatement": 1})
		if err != nil {
			return nil, err
		}
		result["submitted_for_grading"] = true
		warnings = append(warnings, asSlice(w2)...)
	}
	result["warnings"] = warnings
	result["status_now"], err = c.assignStatusBrief(assignID)
	return result, err
}

// ---------------------------------------------------------------- JSON helpers

func str(v any) string { s, _ := v.(string); return s }
func num(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	}
	return 0
}
func obj(v any) M         { m, _ := v.(M); return m }
func asSlice(v any) []any { a, _ := v.([]any); return a }

// list keeps the objects in a JSON array.
func list(v any) []M {
	out := []M{}
	for _, x := range asSlice(v) {
		if m, ok := x.(M); ok {
			out = append(out, m)
		}
	}
	return out
}

func files(v any) []M {
	out := []M{}
	for _, f := range list(v) {
		out = append(out, M{"filename": f["filename"], "fileurl": f["fileurl"], "size": f["filesize"]})
	}
	return out
}

var cmidRe = regexp.MustCompile(`[?&]id=(\d+)`)

func cmidOf(link string) any {
	if m := cmidRe.FindStringSubmatch(link); m != nil {
		n, _ := strconv.ParseInt(m[1], 10, 64)
		return n
	}
	return nil
}

func timeLeft(due int64) string {
	delta := due - time.Now().Unix()
	sign := ""
	if delta < 0 {
		sign, delta = "overdue by ", -delta
	}
	d, h, m := delta/86400, delta%86400/3600, delta%3600/60
	if d > 0 {
		return fmt.Sprintf("%s%dd %dh", sign, d, h)
	}
	return fmt.Sprintf("%s%dh %dm", sign, h, m)
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[1:])
	}
	return p
}
