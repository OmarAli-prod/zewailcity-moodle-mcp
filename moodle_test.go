package moodle

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// isolate keeps a test away from the real ~/.config/moodle-mcp and MOODLE_TOKEN.
func isolate(t *testing.T) string {
	p := filepath.Join(t.TempDir(), "moodle-mcp", "config.json")
	t.Setenv("MOODLE_MCP_CONFIG", p)
	t.Setenv("MOODLE_TOKEN", "")
	return p
}

// fake builds a client whose HTTP calls go to handler.
func fake(t *testing.T, handler http.HandlerFunc) *Client {
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewWithToken(strings.Repeat("t", 32))
	c.URL = srv.URL
	return c
}

func jsonReply(v any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(v) }
}

// ---------------------------------------------------------------- tokens + config

func TestParseToken(t *testing.T) {
	tok := strings.Repeat("a1b2", 8)
	if got, _ := ParseToken("  " + tok + "\n"); got != tok {
		t.Errorf("bare token: got %q", got)
	}
	link := "moodlemobile://token=" + base64.StdEncoding.EncodeToString([]byte("sitehash:::"+tok))
	if got, _ := ParseToken(link); got != tok {
		t.Errorf("launch link: got %q", got)
	}
	if _, err := ParseToken("not a token"); err == nil {
		t.Error("junk accepted")
	}
}

func TestSaveLoadClearToken(t *testing.T) {
	p := isolate(t)
	if err := SaveToken(strings.Repeat("x", 32)); err != nil {
		t.Fatal(err)
	}
	if LoadToken() != strings.Repeat("x", 32) {
		t.Error("token not loaded back")
	}
	if fi, _ := os.Stat(p); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("config mode %v, want 0600", fi.Mode().Perm())
	}
	t.Setenv("MOODLE_TOKEN", "fromenv")
	if LoadToken() != "fromenv" {
		t.Error("MOODLE_TOKEN should win over the file")
	}
	if !ClearToken() || ClearToken() {
		t.Error("ClearToken should report true then false")
	}
}

func TestMissingTokenSaysHowToFix(t *testing.T) {
	isolate(t)
	_, err := New()
	var e *Error
	if !errors.As(err, &e) || e.Code != "notoken" || !strings.Contains(e.Msg, "moodle auth") {
		t.Errorf("got %v", err)
	}
}

// ---------------------------------------------------------------- formatting

func TestFlatten(t *testing.T) {
	out := url.Values{}
	flatten("", M{"courseids": []int64{5, 7}, "options": []any{M{"name": "cmid", "value": 9.0}},
		"flag": true, "ts": 1.7e9, "skip": nil}, out)
	want := url.Values{"courseids[0]": {"5"}, "courseids[1]": {"7"}, "options[0][name]": {"cmid"},
		"options[0][value]": {"9"}, "flag": {"1"}, "ts": {"1700000000"}}
	if out.Encode() != want.Encode() {
		t.Errorf("got %v", out)
	}
}

func TestHTMLToText(t *testing.T) {
	text := HTMLToText("<p>Read <b>chapter 3</b> &amp; notes</p><ul><li>one</li><li>two</li></ul>" +
		"<script>track()</script><a href='https://example.com/notes'>notes</a>")
	for _, want := range []string{"Read chapter 3 & notes", "- one", "- two", "<https://example.com/notes>"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
	if strings.Contains(text, "track()") {
		t.Error("script leaked")
	}

	// Word-pasted tables wrap every cell in <p>; each row should still be one line.
	table := HTMLToText("<table><tr><td><p>Quizzes</p></td><td><p>10%</p></td></tr>" +
		"<tr><td><p>Final</p></td><td><p>40%</p></td></tr></table>")
	if table != "| Quizzes | 10%\n| Final | 40%" {
		t.Errorf("table: %q", table)
	}
	if li := HTMLToText("<ul>\n<li>\n<p>Review labs</p>\n</li>\n<li><p>Practice</p></li></ul>"); li != "- Review labs\n- Practice" {
		t.Errorf("list: %q", li)
	}
}

func TestFmtTime(t *testing.T) {
	for when, want := range map[time.Time]string{
		time.Date(2026, 3, 5, 9, 7, 0, 0, time.Local):   "Thu Mar 5 2026 9:07 AM",
		time.Date(2026, 3, 5, 0, 30, 0, 0, time.Local):  "Thu Mar 5 2026 12:30 AM",
		time.Date(2026, 3, 5, 23, 59, 0, 0, time.Local): "Thu Mar 5 2026 11:59 PM",
	} {
		if got := FmtTime(when.Unix()); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if FmtTime(0) != "" {
		t.Error("zero should be empty")
	}
}

// ---------------------------------------------------------------- file text

func zipOf(files map[string]string) []byte {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, data := range files {
		w, _ := z.Create(name)
		_, _ = w.Write([]byte(data))
	}
	z.Close()
	return buf.Bytes()
}

func TestExtractText(t *testing.T) {
	docx := zipOf(map[string]string{"word/document.xml": "<w:p><w:t>Hello</w:t></w:p><w:p><w:t>World</w:t></w:p>"})
	if got, _ := ExtractText(docx, "", "notes.docx"); strings.Join(strings.Fields(got), " ") != "Hello World" {
		t.Errorf("docx: %q", got)
	}

	deck := zipOf(map[string]string{"ppt/slides/slide10.xml": "<a:p>S10</a:p>", "ppt/slides/slide2.xml": "<a:p>S2</a:p>",
		"ppt/slides/slide1.xml": "<a:p>S1</a:p>"})
	got, _ := ExtractText(deck, "", "lecture.pptx")
	if !(strings.Index(got, "S1") < strings.Index(got, "S2") && strings.Index(got, "S2") < strings.Index(got, "S10")) {
		t.Errorf("pptx slide order: %q", got)
	}

	if got, _ := ExtractText([]byte("print('hi')"), "", "lab.py"); got != "print('hi')" {
		t.Errorf("plain: %q", got)
	}
	if got, _ := ExtractText([]byte("<h1>Week 3</h1><p>Read pp. 10-20</p>"), "text/html", "page.html"); got != "Week 3\n\nRead pp. 10-20" {
		t.Errorf("html: %q", got)
	}
	if _, err := ExtractText([]byte{0, 1}, "application/octet-stream", "video.mp4"); err == nil {
		t.Error("unknown type should fail")
	}
	if _, err := ExtractText([]byte("%PDF-garbage"), "application/pdf", "broken.pdf"); err == nil {
		t.Error("broken pdf should fail, not panic")
	}
}

// ---------------------------------------------------------------- HTTP

func TestCallPostsTokenFunctionAndParams(t *testing.T) {
	var path string
	var form url.Values
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		path, form = r.URL.Path, r.PostForm
		_, _ = w.Write([]byte(`{"ok": true}`))
	})
	out, err := c.Call("mod_assign_get_assignments", M{"courseids": []int64{5, 7}})
	if err != nil || obj(out)["ok"] != true {
		t.Fatalf("got %v, %v", out, err)
	}
	if path != "/webservice/rest/server.php" || form.Get("wsfunction") != "mod_assign_get_assignments" ||
		form.Get("moodlewsrestformat") != "json" || form.Get("courseids[1]") != "7" {
		t.Errorf("path %s form %v", path, form)
	}
}

func TestExpiredTokenTellsYouToReauth(t *testing.T) {
	c := fake(t, jsonReply(M{"exception": "moodle_exception", "errorcode": "invalidtoken",
		"message": "Invalid token - token not found"}))
	_, err := c.Call("core_webservice_get_site_info", nil)
	var e *Error
	if !errors.As(err, &e) || e.Code != "invalidtoken" || !strings.Contains(e.Msg, "moodle auth") {
		t.Errorf("got %v", err)
	}
}

func TestFunctionsListsAndFilters(t *testing.T) {
	c := fake(t, jsonReply(M{"userid": 1, "functions": []M{{"name": "mod_assign_get_assignments"},
		{"name": "core_calendar_get_calendar_upcoming_view"}}}))
	all, _ := c.Functions("")
	cal, _ := c.Functions("CALENDAR")
	if len(all) != 2 || all[0] != "core_calendar_get_calendar_upcoming_view" || len(cal) != 1 {
		t.Errorf("all %v cal %v", all, cal)
	}
}

func TestDeadlinesAreTrimmed(t *testing.T) {
	c := fake(t, jsonReply(M{"events": []M{{"activityname": "Lab 5 &amp; report",
		"course": M{"id": 3, "shortname": "CSAI 201"}, "modulename": "assign", "timesort": 4102444800,
		"url": "https://moodle.zewailcity.edu.eg/mod/assign/view.php?id=4821", "action": M{"name": "Add submission"}}}}))
	ds, err := c.Deadlines(7, 0, 0)
	if err != nil || len(ds) != 1 {
		t.Fatalf("got %v, %v", ds, err)
	}
	d := ds[0]
	if d["name"] != "Lab 5 & report" || d["course"] != "CSAI 201" || d["cmid"] != int64(4821) || d["overdue"] != false {
		t.Errorf("got %v", d)
	}
}

func TestFileURLGetsWebservicePathAndToken(t *testing.T) {
	c := NewWithToken(strings.Repeat("t", 32))
	got := c.fileURL(SiteURL + "/pluginfile.php/1/mod_resource/content/0/week3.pdf")
	if !strings.Contains(got, "/webservice/pluginfile.php/") || !strings.HasSuffix(got, "?token="+strings.Repeat("t", 32)) {
		t.Errorf("got %s", got)
	}
}

// ---------------------------------------------------------------- MCP server

func TestServerTools(t *testing.T) {
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	srv := newServer(func() (*Client, error) { return nil, errors.New("offline") })
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != 20 { // bump this when you add a tool
		t.Errorf("%d tools registered", len(res.Tools))
	}
	writable := map[string]bool{}
	for _, tl := range res.Tools {
		if strings.TrimSpace(tl.Description) == "" {
			t.Errorf("%s has no description", tl.Name)
		}
		if tl.Annotations == nil || !tl.Annotations.ReadOnlyHint {
			writable[tl.Name] = true
		}
	}
	want := map[string]bool{"moodle_submit_assignment": true, "moodle_call": true, "moodle_download": true, "moodle_sync": true}
	if len(writable) != len(want) || !writable["moodle_submit_assignment"] || !writable["moodle_call"] || !writable["moodle_download"] || !writable["moodle_sync"] {
		t.Errorf("writable tools: %v", writable)
	}

	// A tool call without a token comes back as a tool error, not a crash.
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "moodle_whoami"})
	if err != nil || !r.IsError {
		t.Errorf("got %v, %v", r, err)
	}

	prompts, err := cs.ListPrompts(ctx, nil)
	if err != nil || len(prompts.Prompts) != 3 {
		t.Errorf("prompts: %v, %v", prompts, err)
	}
	p, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: "study_course", Arguments: map[string]string{"course": "MATH104"}})
	if err != nil || !strings.Contains(p.Messages[0].Content.(*mcp.TextContent).Text, `"MATH104"`) {
		t.Errorf("study_course: %v, %v", p, err)
	}
	resources, err := cs.ListResources(ctx, nil)
	templates, err2 := cs.ListResourceTemplates(ctx, nil)
	if err != nil || err2 != nil || len(resources.Resources) != 2 || len(templates.ResourceTemplates) != 1 {
		t.Errorf("resources: %v %v, %v %v", resources, templates, err, err2)
	}
}

func TestSaveSubmissionUploadsThenSaves(t *testing.T) {
	var calls []string
	var saved url.Values
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/webservice/upload.php":
			_ = r.ParseMultipartForm(1 << 20)
			f, h, err := r.FormFile("file_1")
			if err != nil || h.Filename != "report.pdf" || r.FormValue("filearea") != "draft" {
				t.Errorf("bad upload: %v %v", h, err)
			} else {
				f.Close()
			}
			calls = append(calls, "upload")
			_, _ = w.Write([]byte(`[{"itemid": 777}]`))
		default:
			_ = r.ParseForm()
			fn := r.PostForm.Get("wsfunction")
			calls = append(calls, fn)
			switch fn {
			case "mod_assign_save_submission":
				saved = r.PostForm
				_, _ = w.Write([]byte(`[]`))
			case "mod_assign_submit_for_grading":
				_, _ = w.Write([]byte(`[]`))
			default:
				_, _ = w.Write([]byte(`{"lastattempt": {"submission": {"status": "submitted"}}}`))
			}
		}
	})
	file := filepath.Join(t.TempDir(), "report.pdf")
	_ = os.WriteFile(file, []byte("%PDF"), 0o644)
	text := "<p>hi</p>"
	res, err := c.SaveSubmission(42, &text, []string{file}, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "upload,mod_assign_save_submission,mod_assign_submit_for_grading,mod_assign_get_submission_status" {
		t.Errorf("calls: %v", calls)
	}
	if saved.Get("plugindata[files_filemanager]") != "777" || saved.Get("plugindata[onlinetext_editor][text]") != text ||
		saved.Get("assignmentid") != "42" {
		t.Errorf("saved form: %v", saved)
	}
	if obj(res["status_now"])["submission"] != "submitted" || res["submitted_for_grading"] != true {
		t.Errorf("result: %v", res)
	}
}

// courseSite fakes one course with a single lecture file, for the what's-new and sync tests.
func courseSite(t *testing.T, fileHits *int) *Client {
	var c *Client
	c = fake(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "pluginfile.php") {
			*fileHits++
			_, _ = w.Write([]byte("slides"))
			return
		}
		_ = r.ParseForm()
		switch r.PostForm.Get("wsfunction") {
		case "core_course_get_enrolled_courses_by_timeline_classification":
			jsonReply(M{"courses": []M{{"id": 7, "shortname": "MATH104-FALL2026-LCTR02", "fullname": "Linear Algebra -MATH104-FALL2026-LCTR02"}}})(w, r)
		case "core_course_get_contents":
			jsonReply([]M{{"section": 1, "name": "Week 1", "modules": []M{
				{"id": 11, "modname": "resource", "name": "Lecture 1", "url": "u", "contents": []M{{"type": "file",
					"filename": "Lec 1: intro.pdf", "filesize": 6, "timemodified": 1792000000,
					"fileurl": c.URL + "/webservice/pluginfile.php/1/mod_resource/content/0/lec1.pdf"}}},
				{"id": 12, "modname": "label", "name": "Note"}}}})(w, r)
		case "core_course_get_updates_since":
			jsonReply(M{"instances": []M{
				{"contextlevel": "module", "id": 11, "updates": []M{{"name": "contentfiles", "timeupdated": 1792000000}}},
				{"contextlevel": "module", "id": 12, "updates": []M{{"name": "configuration"}}}}})(w, r)
		default:
			t.Errorf("unexpected call %s", r.PostForm.Get("wsfunction"))
		}
	})
	return c
}

func TestUpdatesSkipsSettingsOnlyChanges(t *testing.T) {
	var hits int
	ups, err := courseSite(t, &hits).Updates(nil, 0)
	if err != nil || len(ups) != 1 {
		t.Fatalf("got %v, %v", ups, err)
	}
	if ups[0]["name"] != "Lecture 1" || ups[0]["section"] != "Week 1" || ups[0]["files"] == nil {
		t.Errorf("got %v", ups[0])
	}
}

func TestSyncDownloadsOnlyWhatChanged(t *testing.T) {
	var hits int
	c, dir := courseSite(t, &hits), t.TempDir()
	res, err := c.Sync(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("MATH104 Linear Algebra", "Week 1", "Lec 1_ intro.pdf")
	if got := res["downloaded"].([]string); len(got) != 1 || got[0] != want {
		t.Errorf("downloaded %v, want %s", got, want)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, want)); string(b) != "slides" {
		t.Errorf("file content %q", b)
	}
	res, _ = c.Sync(dir, nil)
	if hits != 1 || res["up_to_date"] != 1 || len(res["downloaded"].([]string)) != 0 {
		t.Errorf("second run re-downloaded: hits=%d %v", hits, res)
	}
}
