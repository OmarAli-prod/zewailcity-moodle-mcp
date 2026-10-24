package main

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestVerify(t *testing.T) {
	data := []byte("binary")
	sum := sha256.Sum256(data)
	sums := hex.EncodeToString(sum[:]) + "  moodle_linux_amd64\n" + "deadbeef  moodle-mcp_linux_amd64\n"
	if err := verify(data, sums, "moodle_linux_amd64"); err != nil {
		t.Errorf("good checksum rejected: %v", err)
	}
	if err := verify(data, sums, "moodle-mcp_linux_amd64"); err == nil {
		t.Error("bad checksum accepted")
	}
	if err := verify(data, sums, "moodle_darwin_arm64"); err == nil {
		t.Error("missing checksum accepted")
	}
}

func TestTidyKeepsNamesAndDropsBoilerplate(t *testing.T) {
	got := tidy("Feedback is new in the course Linear Algebra -MATH104-FALL2026-LCTR02.Change your notification preferences")
	if got != "Feedback is new in the course Linear Algebra -MATH104-FALL2026-LCTR02" {
		t.Errorf("got %q", got)
	}
}
