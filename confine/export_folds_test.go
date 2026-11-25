package confine

// DarwinFolding is darwinFolding, Folds' rule on macOS, for the tests of package confine_test,
// which hold it on every system. It is built into the test binary only.
func DarwinFolding(root, typ string, sensitive int, err error) (Folding, error) {
	return darwinFolding(root, typ, sensitive, err)
}

// LinuxFolding is linuxFolding, Folds' rule on Linux, held as DarwinFolding is.
func LinuxFolding(root string, magic, flags uint32, err error) (Folding, error) {
	return linuxFolding(root, magic, flags, err)
}

// WindowsFolding is windowsFolding, Folds' rule on Windows, held as DarwinFolding is.
func WindowsFolding(root, nt, fsName string, flags, cs uint32, csErr error) (Folding, error) {
	return windowsFolding(root, nt, fsName, flags, cs, csErr)
}

// FSCasefoldFL is FS_CASEFOLD_FL, for the tests of package confine_test.
const FSCasefoldFL = fsCasefoldFL
