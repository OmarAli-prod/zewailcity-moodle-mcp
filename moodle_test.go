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
	if len(res.Tools) != 17 { // bump this when you add a tool
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
	want := map[string]bool{"moodle_submit_assignment": true, "moodle_call": true, "moodle_download": true}
	if len(writable) != len(want) || !writable["moodle_submit_assignment"] || !writable["moodle_call"] || !writable["moodle_download"] {
		t.Errorf("writable tools: %v", writable)
	}

	// A tool call without a token comes back as a tool error, not a crash.
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "moodle_whoami"})
	if err != nil || !r.IsError {
		t.Errorf("got %v, %v", r, err)
	}
}
