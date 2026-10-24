package moodle

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"
	"golang.org/x/net/html"
)

var blockTags = map[string]bool{"p": true, "div": true, "br": true, "tr": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "table": true, "ul": true, "ol": true, "section": true}

var (
	spaces    = regexp.MustCompile(`[ \t]+`)
	lineEdges = regexp.MustCompile(` *\n *`)
	blankRuns = regexp.MustCompile(`\n{3,}`)
)

// HTMLToText flattens Moodle HTML into readable text, keeping list items, table cells and link targets.
func HTMLToText(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	z := html.NewTokenizer(strings.NewReader(s))
	skip, href := 0, ""
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		name, _ := z.TagName()
		tag := string(name)
		switch tt {
		case html.TextToken:
			if skip == 0 {
				b.Write(z.Text())
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			switch {
			case tag == "script" || tag == "style":
				skip++
			case tag == "li":
				b.WriteString("\n- ")
			case blockTags[tag]:
				b.WriteString("\n")
			case tag == "td" || tag == "th":
				b.WriteString(" | ")
			case tag == "a":
				for {
					k, v, more := z.TagAttr()
					if string(k) == "href" && strings.HasPrefix(string(v), "http") {
						href = string(v)
					}
					if !more {
						break
					}
				}
			}
		case html.EndTagToken:
			switch {
			case tag == "script" || tag == "style":
				skip--
			case tag == "a" && href != "":
				b.WriteString(" <" + href + ">")
				href = ""
			case blockTags[tag]:
				b.WriteString("\n")
			}
		}
	}
	text := strings.ReplaceAll(b.String(), " ", " ")
	text = spaces.ReplaceAllString(text, " ")
	text = lineEdges.ReplaceAllString(text, "\n")
	return strings.TrimSpace(blankRuns.ReplaceAllString(text, "\n\n"))
}

// FmtTime renders a Unix timestamp like "Thu Mar 5 2026 9:07 AM" in local time, or "" for 0.
func FmtTime(ts int64) string {
	if ts == 0 {
		return ""
	}
	return time.Unix(ts, 0).Format("Mon Jan 2 2006 3:04 PM")
}

func u(v any) string { return html.UnescapeString(str(v)) }

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func excerpt(text, q string) string {
	const width = 160
	r := []rune(text)
	i := strings.Index(strings.ToLower(text), q)
	if i < 0 {
		return cut(text, width)
	}
	start := max(0, len([]rune(text[:i]))-width/2)
	prefix := ""
	if start > 0 {
		prefix = "…"
	}
	end := min(len(r), start+width)
	return prefix + strings.ReplaceAll(string(r[start:end]), "\n", " ") + "…"
}

var (
	xmlBreaks = regexp.MustCompile(`</w:p>|</a:p>|</si>`)
	xmlTags   = regexp.MustCompile(`<[^>]+>`)
	slideName = regexp.MustCompile(`^ppt/slides/slide(\d+)\.xml$`)
	textExts  = []string{".txt", ".md", ".c", ".h", ".cpp", ".py", ".java", ".go", ".m", ".csv", ".json", ".s", ".tex"}
)

// ExtractText pulls readable text out of PDF, DOCX, PPTX, XLSX (shared strings), HTML and plain-text files.
func ExtractText(data []byte, ctype, name string) (string, error) {
	low := strings.ToLower(name)
	switch {
	case strings.HasSuffix(low, ".pdf") || strings.Contains(ctype, "pdf"):
		return pdfText(data)
	case strings.HasSuffix(low, ".docx") || strings.HasSuffix(low, ".pptx") || strings.HasSuffix(low, ".xlsx"):
		return officeText(data, low)
	case strings.Contains(ctype, "html") || strings.HasSuffix(low, ".html") || strings.HasSuffix(low, ".htm"):
		return HTMLToText(string(data)), nil
	case strings.HasPrefix(ctype, "text/"):
		return string(data), nil
	}
	for _, ext := range textExts {
		if strings.HasSuffix(low, ext) {
			return string(data), nil
		}
	}
	return "", &Error{Msg: fmt.Sprintf("Can't extract text from %s (%s); use download instead.", name, ctype)}
}

func pdfText(data []byte) (out string, err error) {
	defer func() { // the PDF parser panics on some malformed files
		if r := recover(); r != nil {
			err = &Error{Msg: fmt.Sprintf("Couldn't read this PDF (%v); use download instead.", r)}
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", &Error{Msg: "Couldn't read this PDF: " + err.Error()}
	}
	pages := make([]string, 0, r.NumPage())
	for i := 1; i <= r.NumPage(); i++ {
		var txt string
		if p := r.Page(i); !p.V.IsNull() {
			txt, _ = p.GetPlainText(nil)
		}
		pages = append(pages, fmt.Sprintf("--- page %d ---\n%s", i, strings.TrimSpace(txt)))
	}
	return strings.Join(pages, "\n\n"), nil
}

func officeText(data []byte, low string) (string, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", &Error{Msg: "Not a valid Office file: " + err.Error()}
	}
	byName := map[string]*zip.File{}
	for _, f := range z.File {
		byName[f.Name] = f
	}
	var parts []string
	pptx := strings.HasSuffix(low, ".pptx")
	switch {
	case strings.HasSuffix(low, ".docx"):
		parts = []string{"word/document.xml"}
	case pptx:
		for n := range byName {
			if slideName.MatchString(n) {
				parts = append(parts, n)
			}
		}
		slideNum := func(n string) int { i, _ := strconv.Atoi(slideName.FindStringSubmatch(n)[1]); return i }
		sort.Slice(parts, func(i, j int) bool { return slideNum(parts[i]) < slideNum(parts[j]) })
	default:
		parts = []string{"xl/sharedStrings.xml"}
	}
	chunks := []string{}
	for i, p := range parts {
		f := byName[p]
		if f == nil {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		raw, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return "", err
		}
		txt := html.UnescapeString(xmlTags.ReplaceAllString(xmlBreaks.ReplaceAllString(string(raw), "\n"), ""))
		if pptx {
			txt = fmt.Sprintf("--- slide %d ---\n%s", i+1, strings.TrimSpace(txt))
		}
		chunks = append(chunks, strings.TrimSpace(txt))
	}
	return strings.Join(chunks, "\n\n"), nil
}
