//go:build !windows

package secondconsumer_test

// Off Windows a file has the mode bits and the owner the watcher sets and keeps.
const modes = true

// longPathsSetting is "" off Windows: no setting decides how long a path the system takes.
func longPathsSetting() string { return "" }
