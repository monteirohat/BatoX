// Package sysupdate downloads and installs Batocera updates.
//
// Batocera's own batocera-upgrade downloads the update with a single curl and
// deletes what it got when anything fails, so a dropped connection starts a
// 2 GB download over. Here the download resumes where it stopped, retries on
// its own and survives restarts; the file is checked against its MD5, then
// installed by Batocera itself (batocera-upgrade manual), so the delicate
// part stays Batocera's.
//
// The address is worked out exactly as batocera-upgrade does it, so every
// board (Raspberry Pi, x86_64 PCs, other ARM boards) gets its own update.
package sysupdate

import (
	"os"
	"strings"
)

// DefaultServer is batocera-upgrade's update server.
const DefaultServer = "https://updates.batocera.org"

// Channels are the update types batocera-upgrade accepts (updates.type).
var Channels = []string{"stable", "unstable", "butterfly"}

// Source is where a console's updates come from.
type Source struct {
	Board   string `json:"board"`   // as in /boot/boot/batocera.board
	Channel string `json:"channel"` // stable, unstable or butterfly
	Server  string `json:"server"`  // updates.url, or DefaultServer
	Dir     string `json:"url"`     // the folder with boot.tar.xz, its .md5 and batocera.version
}

// NewSource follows batocera-upgrade: the board from batocera.board (rpi4
// downloads rpi464's update), the channel from updates.type (stable when it
// is anything else), and updates.url in place of the default server.
func NewSource(board, channel, server string) Source {
	board = strings.TrimSpace(board)
	dirBoard := board
	if dirBoard == "rpi4" {
		dirBoard = "rpi464" // batocera-upgrade: "temporarly" download on rpi464 for rpi4
	}
	channel = strings.TrimSpace(channel)
	if channel != "stable" && channel != "unstable" && channel != "butterfly" {
		channel = "stable"
	}
	server = strings.TrimSpace(server)
	if server == "" {
		server = DefaultServer
	}
	return Source{
		Board:   board,
		Channel: channel,
		Server:  server,
		Dir:     strings.TrimRight(server, "/") + "/" + dirBoard + "/" + channel + "/last",
	}
}

func (s Source) FileURL() string    { return s.Dir + "/boot.tar.xz" }
func (s Source) MD5URL() string     { return s.Dir + "/boot.tar.xz.md5" }
func (s Source) VersionURL() string { return s.Dir + "/batocera.version" }

// readTrim reads a one-line file such as batocera.board or batocera.version.
func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
