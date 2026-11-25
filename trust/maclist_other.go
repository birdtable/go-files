//go:build unix && !linux && !darwin

package trust

import "io/fs"

// listOf marks every name unread on a Unix whose access lists are not read here, FreeBSD's and
// NetBSD's among them: a list there may let another account write a name its mode keeps to its
// owner, so judgeMac passes no name on its mode alone.
func listOf(n node, _ string, _ fs.FileInfo) (node, error) {
	n.macUnread = "this system's access lists are not read"
	return n, nil
}
