package service

import (
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

var ssPidRE = regexp.MustCompile(`pid=(\d+)`)

// Default TCP port for Slidev browser preview (see PPT agent prompt).
const slidevPreviewTCPPort = 6008

// killListenersOnTCPPort stops processes listening on the given port (best-effort).
// Uses fuser when available, then lsof + SIGKILL for anything still bound.
func killListenersOnTCPPort(port int) {
	if port <= 0 || port > 65535 {
		return
	}
	ps := strconv.Itoa(port)
	tcp := ps + "/tcp"
	c := exec.Command("fuser", "-k", tcp)
	c.Stdout, c.Stderr = io.Discard, io.Discard
	_ = c.Run()

	out, err := exec.Command("lsof", "-t", "-i", ":"+ps).Output()
	if err == nil && len(out) > 0 {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			pid, err := strconv.Atoi(line)
			if err != nil || pid <= 1 {
				continue
			}
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		return
	}

	// Minimal containers often lack `lsof`; `ss` from iproute2 is usually present.
	ssOut, err := exec.Command("ss", "-ltnp", "sport = :"+ps).Output()
	if err != nil || len(ssOut) == 0 {
		return
	}
	for _, m := range ssPidRE.FindAllStringSubmatch(string(ssOut), -1) {
		if len(m) < 2 {
			continue
		}
		pid, err := strconv.Atoi(m[1])
		if err != nil || pid <= 1 {
			continue
		}
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// ReleaseSlidevPreviewPort frees the default Slidev preview port (e.g. after Stop or server exit).
func (s *PPTService) ReleaseSlidevPreviewPort() {
	killListenersOnTCPPort(slidevPreviewTCPPort)
	if s != nil && s.state != nil {
		s.state.BroadcastPPTLog("[slidev] released TCP port " + strconv.Itoa(slidevPreviewTCPPort))
	}
}
