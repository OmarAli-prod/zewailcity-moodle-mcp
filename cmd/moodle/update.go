package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const repo = "OmarAli-prod/zewailcity-moodle-mcp"

var web = &http.Client{Timeout: 2 * time.Minute}

// update replaces moodle and moodle-mcp with the latest release, refreshes the Claude skill,
// and makes sure Claude's "moodle" server points at the updated moodle-mcp.
func update() {
	if runtime.GOOS == "windows" {
		fail("moodle update works on macOS and Linux. On Windows run: go install github.com/" + repo + "/cmd/...@latest")
	}
	var rel struct {
		Tag string `json:"tag_name"`
	}
	check(json.Unmarshal(get("https://api.github.com/repos/"+repo+"/releases/latest"), &rel))
	exe, err := os.Executable()
	check(err)
	exe, _ = filepath.EvalSymlinks(exe)
	dir := filepath.Dir(exe)

	registered := claudeServerPath()
	server := registered
	if server == "" {
		server = filepath.Join(dir, "moodle-mcp")
	}
	if rel.Tag == version {
		fmt.Println(green("✓"), "Already on the latest release,", bold(version))
	} else {
		fmt.Printf("Updating %s → %s\n", dim(version), bold(rel.Tag))
		base := "https://github.com/" + repo + "/releases/download/" + rel.Tag + "/"
		sums := string(get(base + "checksums.txt"))
		targets := map[string][]string{"moodle": {exe}, "moodle-mcp": {filepath.Join(dir, "moodle-mcp")}}
		if server != targets["moodle-mcp"][0] {
			targets["moodle-mcp"] = append(targets["moodle-mcp"], server)
		}
		for name, paths := range targets {
			asset := fmt.Sprintf("%s_%s_%s", name, runtime.GOOS, runtime.GOARCH)
			data := get(base + asset)
			check(verify(data, sums, asset))
			for _, p := range paths {
				check(replace(p, data))
				fmt.Println(green("↓"), p)
			}
		}
	}

	if home, _ := os.UserHomeDir(); dirExists(filepath.Join(home, ".claude")) {
		skill := filepath.Join(home, ".claude", "skills", "zewail-moodle", "SKILL.md")
		data := get("https://raw.githubusercontent.com/" + repo + "/" + rel.Tag + "/skills/zewail-moodle/SKILL.md")
		check(os.MkdirAll(filepath.Dir(skill), 0o755))
		check(os.WriteFile(skill, data, 0o644))
		fmt.Println(green("✓"), "Skill refreshed")
	}

	switch _, err := exec.LookPath("claude"); {
	case err != nil:
		fmt.Println(dim("Claude Code isn't on your PATH, so the server wasn't registered. Point your client at " + server))
	case registered == "" || !fileExists(registered):
		_ = exec.Command("claude", "mcp", "remove", "moodle", "-s", "user").Run()
		out, err := exec.Command("claude", "mcp", "add", "-s", "user", "moodle", "--", server).CombinedOutput()
		if err != nil {
			fail("claude mcp add failed: " + strings.TrimSpace(string(out)))
		}
		fmt.Println(green("✓"), "Registered with Claude Code:", server)
	default:
		fmt.Println(green("✓"), "Claude Code already runs", server)
	}
	if rel.Tag != version {
		fmt.Println(bold("Restart Claude to load the new server."))
	}
}

// verify checks data against the line for asset in a sha256sum-style checksums file.
func verify(data []byte, sums, asset string) error {
	for _, line := range strings.Split(sums, "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == asset {
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != f[0] {
				return fmt.Errorf("checksum mismatch for %s, not installing it", asset)
			}
			return nil
		}
	}
	return fmt.Errorf("no checksum listed for %s", asset)
}

// replace swaps the file at p for data atomically, so a running copy keeps working.
func replace(p string, data []byte) error {
	tmp := p + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// claudeServerPath is the command Claude Code runs for the user-scope "moodle" server, if any.
func claudeServerPath() string {
	home, _ := os.UserHomeDir()
	var cfg struct {
		MCPServers map[string]struct{ Command string } `json:"mcpServers"`
	}
	if b, err := os.ReadFile(filepath.Join(home, ".claude.json")); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	return cfg.MCPServers["moodle"].Command
}

func get(url string) []byte {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "moodle-cli/"+version)
	r, err := web.Do(req)
	check(err)
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	check(err)
	if r.StatusCode != 200 {
		fail(fmt.Sprintf("%s: HTTP %d", url, r.StatusCode))
	}
	return body
}

func fileExists(p string) bool { fi, err := os.Stat(p); return err == nil && !fi.IsDir() }
func dirExists(p string) bool  { fi, err := os.Stat(p); return err == nil && fi.IsDir() }
