//go:build unix

package appdirs

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/user"
	"strconv"
	"strings"
)

// account is the running account's home by its real user ID.
func account() (string, error) {
	return byUID(os.Getuid())
}

// homeInPasswd is the home field of the first line of r, in passwd(5)'s form, whose uid field is
// uid. A line of NIS's, whose name starts with + or -, is skipped, as os/user skips it. That line
// with other than seven fields, a read that fails, and no such line, os/user's UnknownUserIdError,
// are refused.
func homeInPasswd(r io.Reader, uid int) (string, error) {
	want := strconv.Itoa(uid)
	lines := bufio.NewScanner(r)
	for lines.Scan() {
		f := strings.Split(lines.Text(), ":")
		if len(f) < 3 || f[2] != want || strings.HasPrefix(f[0], "+") || strings.HasPrefix(f[0], "-") {
			continue
		}
		if len(f) != 7 {
			return "", fmt.Errorf("the line of uid %d has %d fields, and want 7", uid, len(f))
		}
		return f[5], nil
	}
	if err := lines.Err(); err != nil {
		return "", err
	}
	return "", user.UnknownUserIdError(uid)
}
