package write

// throughWith hands the result of the write-through open on Windows to through, which answers the
// result the rename takes, so a test can make a volume refuse write-through, which no test
// machine's NTFS does. through reads it only under testing.Testing().
func throughWith(through func(open error) error) Option {
	return func(o *options) { o.through = through }
}

// asAnother takes this account as another in KeepOwner's look at the old file's owner, so a test
// reaches an old file another account owns, which a plain token cannot make. winapi.Mine reads it
// only under testing.Testing().
func asAnother() Option {
	return func(o *options) { o.elsewhere = true }
}

// noFileIndex makes the write read the volume as one that gives a file no index (a WebDAV drive,
// some SMB servers), which no test machine's volume does, so a test reaches the ErrNoFileIndex
// refusal. indexed reads it only under testing.Testing().
func noFileIndex() Option {
	return func(o *options) { o.noIndex = true }
}

// linkWith answers NoReplace's hard link with answer in place of winapi.Link, which is then not
// made, so a test reaches a volume that keeps access lists and no hard links (some SMB servers),
// which no test machine's NTFS is. link reads it only under testing.Testing().
func linkWith(answer func() error) Option {
	return func(o *options) { o.linkAnswer = answer }
}

// goOpenWith hands seen each folder a call by a path opens by os.OpenRoot, before the open, so a
// test sees whether Go's open, which passes no quality of service, reached a folder. openFolder
// reads it only under testing.Testing().
func goOpenWith(seen func(dir string)) Option {
	return func(o *options) { o.goOpen = seen }
}
