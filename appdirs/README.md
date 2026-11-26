# appdirs

`appdirs` names the folders a Go program keeps its files in, on Linux, FreeBSD, macOS and
Windows, says how much room a folder has and what kind of volume holds it, and makes the one
folder that is hard to make safely: the program's own temp folder. It imports the standard
library, `golang.org/x/sys` and this module's `trust`, which judges that folder.

## Install

```sh
go get github.com/birdtable/go-files/appdirs@latest
```

The full API is on [pkg.go.dev](https://pkg.go.dev/github.com/birdtable/go-files/appdirs). Runnable examples are in [example_test.go](example_test.go).

## How it works

Built on the XDG Base Directory layout, macOS's `Library` and Windows' `LOCALAPPDATA`, as
`github.com/adrg/xdg` reads them. `os.UserConfigDir` agrees on the XDG layout alone. On Windows it
is the roaming `%AppData%`, which `appdirs` never uses. It adds the system, the home folder and
the environment handed in, so a test on one system checks another's, a `Run` and a `Data` folder,
`Temp`, `Free` and `Volume`. The program configures its name, the system, the home folder, the environment
reader, the `Layout` on macOS, and what `Temp`'s `trust.Who` values carry: its apart folders and mounts.

`For` is a pure function of what it is handed: the system, the program's name, the home folder
and a reader of the environment. It reads no environment and no disk, so a program run on Linux
tests its Windows folders, and a program that reads its environment once hands the same reader to
everything. It makes no folder: the program makes the one it writes to, with the mode it wants.
`Temp` alone makes one, and reads the disk to.


## What it holds

| call | what it does |
|---|---|
| `For(goos, app, home, env, layout)` | The five folders of the program `app` on the system `goos`, for the account whose home folder is `home`, with `env` reading its environment. A nil `env` reads every variable as unset. It gives `Folders` and an error. |
| `Here(app, layout)` | `For` on the system it runs on, for the account that runs it: the system's layout it is built for, Windows', macOS's or the XDG one, which the `here_*.go` files give by their build constraints, `os.UserHomeDir` and `os.Getenv`, each read once, at the call, so a program keeps all three out of its own code. A system `For` names no folders for, ios, js, plan9, wasip1 and hurd among them, is `ErrSystem`. A home the system will not say is `ErrHome`, with its cause. |
| `HereWith(app, layout, env)` | `Here` with the caller's environment: `For` on the system it is built for, as `Here` takes it, with the home and every other variable read from `env`. The home is `USERPROFILE` on Windows and `HOME` on every Unix system, as `os.UserHomeDir` reads it; a nil `env` reads every variable as unset, and an empty home, or one `For` refuses, is `ErrHome`. It never reads the process's own home or environment, and answers exactly as `For` does, so a program that may not name the system can give each of a parallel test's programs a home and XDG variables of its own. |
| `HomeVar` | The variable `HereWith` reads the home from on the system it is built for: `USERPROFILE` on Windows, `HOME` on every Unix system, and `""` where `HereWith` answers `ErrSystem`. A test builds its program's environment from `map[string]string{appdirs.HomeVar: dir}`, so it names no system. |
| `Sources(layout)` | For each of the five folders, the variable `For` reads it from on the system it is built for, in `layout`, or `""` where the home alone moves it: the five `XDG_*` variables in the XDG layout, `LOCALAPPDATA` for all five on Windows, and none on macOS in `Native`. The home's variable is `HomeVar`, and no field repeats it. A layout neither `XDG` nor `Native` is `ErrLayout`, and a system `For` names no folders for `ErrSystem`. |
| `System(goos, app, env)` | The machine-wide folder of the program `app`, the settings every account shares, which an administrator writes: below. Pure, as `For` is. |
| `Folders` | `Config`, `State`, `Cache`, `Run` and `Data`, each an absolute path for the system `For` was given, but `Run` where `For` gives `ErrRun`, which is `""`. |
| `Layout`: `XDG`, `Native` | The layout `For` gives on macOS, which every call names, as there is no default: `XDG`, as on Linux, or `Native`, the system's own folders under `~/Library`. It changes nothing on another system. |
| `Temp(app, who...)` | The running account's own temp folder for the program `app`, made `0700` when it is not there (on Windows with an access list only this account and SYSTEM reach, none of the temp folder's inherited), judged by `trust` as this account's with no link on its way, and kept apart from each folder of `who`'s `Apart`, below. |
| `TempOverflow(app, who...)` | `Temp` for a test in a Linux user namespace that maps this account and not root: both walks set `trust.Who.Overflow`, below. For tests alone. |
| `Free(path)` | The bytes free to this account on the file system that holds `path`: what a write by this process may fill. |
| `Volume(path)` | What kind of volume holds `path`, links followed, as a `VolumeInfo`, so a program that keeps a database or a lock file there can refuse one whose locks and fsync it may not trust: below. Experimental. |
| `VolumeInfo` | `Type`, the system's own name for the file system (`apfs`, `ext4`, `fuse.sshfs`, `NTFS`), which names and decides nothing, and `Kind`. `Local()` is true for `Local` alone. Experimental. |
| `VolumeKind`: `Local`, `Network`, `FUSE`, `SyncPlaceholder`, `Unknown` | A disk of this machine, removable ones among them, with no network, FUSE daemon or sync provider behind it; a share served over the network; a file system served through FUSE; a folder a sync provider keeps and may evict; or a volume `Volume` cannot place. The zero value is `Unknown`, so an unset `VolumeInfo` never reads as local. Experimental. |
| `AccountHome()` | The home of the account that runs the process, from the system's account records and never the environment: below. Experimental. |
| `ErrLayout`, `ErrHome`, `ErrApp`, `ErrSystem` | `For`'s four refusals, below. |
| `ErrNoAccountHome` | `AccountHome`'s refusal, where the system cannot say the home or says one empty, relative or not clean. Experimental. |
| `ErrRun` | `For`'s answer where the XDG layout has no `$XDG_RUNTIME_DIR`: the other four folders, and `Run` `""`. |


## The folders

`<app>` is the program's name, `~` the home folder `For` is handed, and `$X` the variable `X` as
`env` reads it.

| folder | Linux, FreeBSD, macOS in `XDG` | macOS in `Native` | Windows |
|---|---|---|---|
| `Config` | `$XDG_CONFIG_HOME/<app>`, or `~/.config/<app>` | `~/Library/Application Support/<app>` | `%LOCALAPPDATA%\<app>` |
| `State` | `$XDG_STATE_HOME/<app>`, or `~/.local/state/<app>` | `~/Library/Application Support/<app>` | `%LOCALAPPDATA%\<app>\State` |
| `Cache` | `$XDG_CACHE_HOME/<app>`, or `~/.cache/<app>` | `~/Library/Caches/<app>` | `%LOCALAPPDATA%\<app>\Cache` |
| `Run` | `$XDG_RUNTIME_DIR/<app>`, or `""` with `ErrRun` | `~/Library/Application Support/<app>` | `%LOCALAPPDATA%\<app>\Run` |
| `Data` | `$XDG_DATA_HOME/<app>`, or `~/.local/share/<app>` | `~/Library/Application Support/<app>` | `%LOCALAPPDATA%\<app>` |

- **A variable that is not an absolute path is read as unset.** A relative value in a checkout's
  environment never puts a folder under the working folder.
- **Windows** takes `%LOCALAPPDATA%`, or `~\AppData\Local` without it, and never the roaming
  `%APPDATA%`, which a roaming profile copies to every machine the account signs in to. It reads
  no XDG variable. Its paths are joined with a backslash on whatever system `For` runs.
- **macOS in `Native`** reads no variable; both folders are built from the home given.
- **`Run` is never `State`.** Without `$XDG_RUNTIME_DIR`, as in a session no login manager
  started, `For` gives the other four folders and `ErrRun`, and `Run` is `""`: a program that
  keeps a socket or a process file checks the error before it uses `Run`, and one that keeps
  none goes on past `ErrRun`. `State` outlives the session, so a socket there outlives the
  process that made it.
- **Cache is apart, as the paths are written.** Where a person's variables make `Cache` the
  folder `Config` or `Data` is, or one that holds it, `Cache` is its own `Cache` folder, and that
  again, until it is neither, so a program that clears its cache does not delete its settings.
  The paths are compared as written: `For` reads no disk, by design, so the same folder written
  in another case on a volume that ignores case, or reached through a link, is not seen.
- **Linux and the other Unix systems** (OpenBSD, NetBSD, DragonFly, illumos, Solaris, AIX,
  Android) take the XDG layout. A system with no layout here, plan9 or js among them, is
  `ErrSystem`.


## The machine-wide folder

| system | `System` |
|---|---|
| Linux, FreeBSD and the other Unix systems | `/etc/xdg/<app>` |
| macOS | `/Library/Application Support/<app>` |
| Windows | `%PROGRAMDATA%\<app>`, or `C:\ProgramData\<app>` where it is not an absolute path on a drive |

- **`XDG_CONFIG_DIRS` is never read.** `/etc/xdg` is its default, and a variable any account
  sets does not move the folder every account trusts. The table is `github.com/adrg/xdg`'s, read
  as a reference.
- **Any account may make a folder in Windows' `ProgramData`.** A folder there the program's
  installer did not make may be another account's: a program judges it with `trust` before it
  reads settings from it.
- **A share in `PROGRAMDATA` is never taken.** `\\host\share` gives `C:\ProgramData\<app>`:
  opening a share offers its host this account's credentials before `trust` judges anything.
  Windows' variable is read at all, where `XDG_CONFIG_DIRS` is not, because `ProgramData` may
  be moved, and a folder on a drive it names is judged with `trust` as one in `ProgramData` must
  be anyway.
- **Nothing is made and nothing on the disk is read.** `System` refuses a name as `For` does,
  with `ErrApp`, and a system with no folder here with `ErrSystem`.

### Each of `System`'s guards and its test

| guard | test |
|---|---|
| `XDG_CONFIG_DIRS` is never read | `TestSystemGivesEachSystemsFolder` |
| a `PROGRAMDATA` that is not absolute, a device path or a climb is `C:\ProgramData` | `TestSystemGivesEachSystemsFolder` |
| a share in `PROGRAMDATA` is `C:\ProgramData` | `TestSystemGivesEachSystemsFolder` |
| a bad name is `ErrApp`, and an unknown system `ErrSystem` | `TestSystemRefusesWhatForRefuses` |

## The account's home

`AccountHome` is the home of the account that runs the process, from the system's account records,
for a program that must not take the home from the environment it is handed: `Here` reads
`os.UserHomeDir`, which is `HOME` or `USERPROFILE`, and any caller sets those.

| system | `AccountHome` |
|---|---|
| Linux, FreeBSD, macOS and the other Unix systems, built with cgo, and macOS without it | the home of the real user ID, `os.Getuid`, by `os/user`'s `LookupId`, which asks the C library's account database (macOS's through libSystem) |
| Linux, FreeBSD and the other Unix systems but macOS, built without cgo or with `osusergo`, and android | the home field of the line of `/etc/passwd` whose uid is `os.Getuid`, read by `AccountHome` itself; `os/user` is not called |
| macOS built with `osusergo` | `ErrNoAccountHome`: its accounts are not in `/etc/passwd` |
| Windows | the profile folder of the process's token, by `os/user`'s `Current` |
| any other system | `ErrNoAccountHome`: `os/user` reads the home there from the environment |

- **No variable is read.** Where `os/user` takes its account from the system, the build of its
  `cgo_lookup_unix.go`, `AccountHome` asks it. Where it falls back to `HOME` when `/etc/passwd`
  does not hold the account, the build of its `lookup_stubs.go`, `AccountHome` calls none of it:
  `LookupId` asks `Current` first, so the fallback reaches it too. `account_system.go` and
  `account_passwd.go` carry those two files' build constraints, at go.mod's Go.
- **Where the system cannot say, it is `ErrNoAccountHome`, with the cause.** A uid the records do
  not hold, as an account only in LDAP or sssd is to a build that reads `/etc/passwd`, a line of
  that uid that is not seven fields, a read that fails, and a home the records give empty,
  relative or not clean are refused. The answer is absolute, clean and never empty.
- **Nothing is made and the folder is not opened.** A program judges it with `trust` before it
  trusts what is in it.

### Each of `AccountHome`'s guards and its test

| guard | test |
|---|---|
| the home's variables, set to another folder or unset, never move the answer | `TestAccountHomeIsTheAccountsWhateverTheEnvironmentSays` |
| a program built with `osusergo` and without cgo, run with `HOME` set to a folder of the test's, never answers it, and answers `/etc/passwd`'s home, macOS's refusal or the token's profile | `TestAProgramBuiltWithOsusergoAndWithoutCgoNeverAnswersHOME` |
| a failed lookup, and an empty, relative or unclean home, are `ErrNoAccountHome` with the cause | `TestAccountHomeRefusesALookupThatFailsAndAnEmptyRelativeOrUncleanHome` |
| `/etc/passwd`'s first line of the uid answers, NIS's lines skipped; no line, a line not of seven fields and a read that fails are `ErrNoAccountHome` | `TestOnUnixPasswdIsReadForTheUIDsOwnHomeAlone` |
| on Unix, a uid no account has is `ErrNoAccountHome` | `TestOnUnixAnUnknownUIDIsErrNoAccountHome` |

## What is refused

| refusal | what it is |
|---|---|
| `ErrLayout` | A `Layout` that is neither `XDG` nor `Native`, the zero `Layout` among them, on every system, so a program that chose none finds out on the system it is tested on. |
| `ErrHome` | A home folder that is empty or not absolute for the system: `/home/u` on Windows, `C:\Users\u` on Linux, `C:Users`, a device path (`\\?\`, `\\.\`), a share with no host or share name, or a path with a `.` or `..` element on Windows. |
| `ErrApp` | A program name that is not one plain folder name on every system: one holding `..`, or one `confine.Portable` refuses: empty, `.`, holding `/`, `\`, `:`, a control character, or one of `<>"\|?*`, ending in a dot or a space, which Windows drops, a Windows device (`CON`, `PRN`, `AUX`, `NUL`, `CONIN$`, `CONOUT$`, `COM0` to `COM9`, `LPT0` to `LPT9`), with or without an extension, which Windows opens in place of a folder, an 8.3 short name (`APP~1`), `.git`, a format character, a default-ignorable character such as a variation selector, or a character whose case folding is several. |
| `ErrSystem` | A system `For` names no folders for. |
| `ErrNoAccessList` | `Temp`'s folder to make on a Windows volume that keeps no access lists (FAT, exFAT), where it would be every account's, and where this account cannot open by name what it makes with that list (a restricted token or an AppContainer the list does not name, a share that knows this account by another SID): nothing is made. It matches `errors.ErrUnsupported`. This is tried first on a throwaway folder beside it, `.probe-` and random letters, made and removed, and only where the name is not there already: a process killed in that instant leaves it, empty, and it is the caller's to remove, and so does a removal of it that fails, with an error that names it and is `ErrNoAccessList` only where it could not be opened by name either. Where the cause is a denial, it matches `fs.ErrPermission` too, so test `ErrNoAccessList` first. |

The layout is checked first, then the name, then the home, so a call with all three wrong is
`ErrLayout`, and one with the name and home wrong is `ErrApp`.

### Each of `For`'s guards and its test

| guard | test |
|---|---|
| no default layout: a zero or unknown `Layout` is `ErrLayout` on every system, before the name and home | `TestALayoutNeitherXDGNorNativeIsRefusedWithErrLayout` |
| `Run` is `""` with `ErrRun` without `$XDG_RUNTIME_DIR`, and never `State` | `TestForGivesEachSystemsFolders`, `TestEachVariableMovesItsOwnFolderAlone`, `TestEveryFolderIsAbsoluteHoldsTheNameAndCacheIsApart` |
| a bad home is `ErrHome` | `TestABadHomeIsRefusedWithErrHome` |
| a bad name is `ErrApp` | `TestABadAppIsRefusedWithErrApp` |
| an unknown system is `ErrSystem` | `TestAnUnknownSystemIsRefusedWithErrSystem` |
| a variable not absolute is read as unset | `TestEveryFolderIsAbsoluteHoldsTheNameAndCacheIsApart` |


## Temp

`Temp(app, who...)` is `<temp>/<app>-<uid>` on Unix, `<uid>` the effective one, and
`<temp>\<app>` on Windows, where `%TEMP%` is the account's own already. `<temp>` is
`os.TempDir()` read through its links, as what it names, so macOS's `/var/folders/...` is
`/private/var/folders/...`, once `trust` has judged each of them: `trust.Path` walks
`<temp>/<app>-<uid>` with links followed, so each link and junction on the way passes `trust`'s
own rule (its owner, its access list, the hold right in a sticky folder) before it is read, and
the walk ends below `<temp>`, since a sticky folder as the last name is refused. The name not
there yet is that walk's pass. From `<temp>` down, every name is judged by `trust.Path` as a
folder, with `Who{NoLinks: true}` and the `Apart`, `FileSystems` and `Mounts` of each of `who`, joined; `apart` below is the joined `Apart`:

1. The walk runs before anything is made. It judges each folder above the temp folder as a
   folder above the last name, so root's sticky `/tmp` passes, and ends at the temp folder,
   which is not there yet: that is its pass. Any other refusal is answered, and nothing is made.
   A name not there above the temp folder, or at a folder of `apart` removed since it was read,
   is such a refusal: only the temp folder's own name not there is the pass.
2. `os.Mkdir` makes it `0700`, and makes no name that is there by now.
3. The walk runs again, over the whole path. A folder of the way swapped for a link between
   the check and the make, or a temp folder another account made first, is judged here, so
   what is answered is what was judged.
4. The temp folder may not hold a folder of `apart`. Each one's way, as written and as its
   links resolve, is read by file ID (device and inode, or volume serial and file index), with
   no judgement, and one that is the temp folder is refused. A Windows folder whose 64-bit index
   reads zero (a WebDAV or SMB drive) or all-ones (ReFS, with no 64-bit form of its 128-bit ID)
   gives `os.SameFile` nothing to tell it apart by, so it is refused with `ErrNoFileIndex`, the one
   value `write.ErrNoFileIndex` is, matching `errors.ErrUnsupported`. On Windows the links and junctions
   resolve as the system's final path of the folder: `filepath.EvalSymlinks` keeps a junction,
   which `os` reads as no link since Go 1.23. A folder of `apart` that is a bind mount, or on
   Windows a mounted volume folder, of a folder inside the temp folder is on no such way, and is
   not seen: only root, or an administrator, makes one.

Each of `apart` must be an absolute, clean path to a folder that is there; each is read before
anything is made. An empty entry is not absolute, and is refused: a caller drops empty entries
itself. `Temp` is built where `trust` is, on Unix and Windows: plan9, js and wasip1 have no `Temp`.

**A temp folder on a disk mounted `nosuid` is refused until it is named.** On Linux `trust`
refuses, with `trust.ErrMount`, a name on a disk mounted `nosuid`, as an account's stick is, and
a CIS `/tmp`, `/var/tmp` or `/home` partition is too, as is every disk in a systemd unit with
`ProtectSystem=strict` or `DynamicUser=`, and every podman volume and bwrap or Flatpak bind.
A program whose temp folder is on one names that disk's folder in `Who.Mounts`, as
`Temp("app", trust.Who{Mounts: []string{"/var/tmp"}})`, and a type `trust` does not judge in
`Who.FileSystems`; both walks take them. A tmpfs `/tmp` needs neither.

**The answer is a path judged once, and not kept.** `Temp` holds no folder open. Right in a
sticky folder the judgement lasts only while the folder is there: a cleaner that ages `/tmp`
removes an idle folder, and another account may then make the name. Call `Temp` again before each
use, and never keep the string across a wait.

**A sub-folder of the temp folder is two calls, with no join.** Open `Temp`'s answer with
`os.OpenRoot`, and make the sub-folder with `confine.Mkdir(root, sub, 0o700)`, which makes each
missing folder by handle, following no link, and answers the sub-folder's own `*os.Root`: nothing
is made through a name another account could swap between a join and a mkdir. On Windows the
folder made has an access list only this account and SYSTEM reach. `appdirs` adds
no function for it:

```go
func cacheIn(app string) (*os.Root, error) {
	dir, err := appdirs.Temp(app)
	if err != nil {
		return nil, err
	}
	top, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer top.Close()
	return confine.Mkdir(top, "cache", 0o700) // on Windows the owner-only list, as Temp's
}
```

**`Who.Overflow` is refused where it does not apply, never ignored.** Off Linux, and on Linux
outside a test binary, a walk with `Who.Overflow` set refuses with `trust.ErrOverflow`, matching
`errors.ErrUnsupported` off Linux, as `TempOverflow` does: a no-op would let a refusal pass
silently, and there is no overflow account off Linux, so a test that sets it would pass a
namespace check it never made. A caller sets it only in a Linux test, as the field's doc says.

**`TempOverflow` is `Temp` for a test in a user namespace.** In a Linux user namespace that maps
this account and not root, `/` and root's `/tmp` show as the kernel's overflow account, so `Temp`
refuses there, naming it (`/ is owned by uid:65534`). `TempOverflow(app, who...)` is `Temp` with
`trust.Who.Overflow` set in both its walks, the one through `<temp>`'s links and the one from
`<temp>` down, so `trust` trusts that account as root, and judges every other rule as `Temp`'s
walks do. It is for tests alone: outside a test binary in such a namespace it
refuses with an error matching `trust.ErrOverflow`, as `trust` does, and off Linux one matching
`errors.ErrUnsupported` too, and nothing is made. `Temp` never sets `Who.Overflow`.

**A temp folder already there must be private, and keeps its mode.** `Temp` makes the folder
`0700`, and judges the folder as `trust.PrivateFolder`, both before the make and after it. One this
account made earlier that its group or others may read is refused with `trust.ErrOthersRead`, and
the error names its mode. The judgement covers the mode, a Linux access list's mask,
a macOS access list and a Windows access list, whose inherit-only entries, which the files made
in it start with, are judged as its own, all but CREATOR OWNER's. `Temp` changes no
mode: the refusal's `Fix` says
what to clear.

**Root's temp folder right in root's sticky folder is root's own.** When this account is root,
as in a container, and `<temp>` is `/tmp` with `TMPDIR` unset, `trust` judges root's folder there
as this account's own. For every account, the folder there passes only on `/tmp`'s
own device: a file system another account mounts on the folder reports any owner.

| refusal | when |
|---|---|
| `ErrApp` | the program's name is not one plain folder name, as for `For` |
| `ErrNoAccessList` | on Windows, the temp folder is not there and its volume keeps no access lists (FAT, exFAT), or this account cannot open by name what it makes there with that list: nothing is made |
| `trust.ErrNotOwners` | another account owns a folder or a link of the way, or `<temp>`'s way passes a link right in root's sticky folder, whoever owns it, or, when this account is not root, a folder of root's there, as libpam-tmpdir's `/tmp/user/<uid>` |
| `trust.ErrNotOwners` | the temp folder right in root's sticky folder is on another device than that folder, as a FUSE file system another account mounts there |
| `trust.ErrOthersWrite` | a folder of the way can be written by its group or by others, but root's sticky folder above the temp folder |
| `trust.ErrOthersRead` | the temp folder, made earlier, can be read by its group or by others, by its mode or an access list: the error names the mode |
| `trust.ErrNotPlain` | a link, or on Windows a junction, is below `<temp>` or at the temp folder's own name, before or after the make |
| `trust.ErrInside` | the temp folder is a folder of `apart`, lies below one, or holds one |
| `trust.ErrMountEntry` | a folder of `who`'s `Mounts` is not a mount's root at a name only root can move, or names nothing, which is never read as the temp folder not yet made: before anything is made |
| `fs.ErrNotExist` | a folder of `apart` is not there, or a folder above the temp folder is not there: before anything is made |

### Each guard and its test

| guard | test |
|---|---|
| the walk before the make: a refusal makes nothing | `TestOnUnixAFolderOfTheWayItsGroupOrOthersCanWriteIsRefusedAndNothingIsMade`, `TestATempFolderThatIsOrLiesBelowAnApartFolderIsRefusedAndNotMade` |
| no link on the way or at the name (`Who.NoLinks`) | `TestALinkOnTheWayOrAtTheNameIsRefusedAndNothingIsMadeThroughIt` |
| the walk after the make, over the whole path | `TestAFolderOfTheWaySwappedForALinkBetweenTheCheckAndTheMakeIsRefused` |
| the temp folder holds no folder of `apart`, through its links | `TestATempFolderThatHoldsAnApartFolderIsRefusedHoweverTheFolderIsSpelled` |
| each of `apart` read before anything is made | `TestAnApartFolderThatIsNotThereIsAnErrorBeforeAnythingIsMade` |
| made `0700` | `TestOnUnixTheTempFolderIsMadeForThisAccountAlone` |
| a temp folder already there is private (`trust.PrivateFolder`), and its mode is kept | `TestOnUnixATempFolderAlreadyThereItsGroupOrOthersCanReadIsRefusedAndKeptAsItIs`; the access lists are `trust`'s, in its `PrivateFolder` rows |
| each link of `<temp>`'s way judged by `trust` before it is read | `TestOnUnixATempFolderThroughALinkTrustRefusesIsRefusedAndNothingIsMade`, `TestATempFolderThroughALinkTrustPassesIsAnsweredAsWhatTheLinkNames` |
| a temp folder right in root's sticky folder is this account's own, root's too, and `0700` | `TestOnUnixATempFolderRightInRootsStickyFolderIsThisAccountsOwn`, run as root in a container; the device rule is `trust`'s, in `TestRootsOwnFolderRightInRootsStickyFolderPassesForRootAloneAndOnItsDevice` |
| only the temp folder's own name not there is the first walk's pass | `TestANameNotThereAboveTheTempFolderIsAnErrorAndNothingIsMade` |
| on Linux, a temp folder on a mount `trust` does not judge, or on a mount `nosuid`, refused with `trust.ErrMount` until `who`'s `FileSystems` and `Mounts` name it, both walks passing then | `TestOnLinuxATempFolderOnAMountNosuidIsRefusedUntilTempNamesIt` |
| a folder of `who`'s `Mounts` that names nothing refused with `trust.ErrMountEntry`, never `fs.ErrNotExist`, so not taken for the temp folder not yet made | `TestOnUnixAWhoMountsEntryThatNamesNothingRefusesTheTempBase` |
| the `Apart` of each `who` given, joined | `TestTempJoinsTheApartFoldersOfEachWhoItIsGiven` |
| `TempOverflow` refuses with `trust.ErrOverflow` off Linux (matching `errors.ErrUnsupported` too) and on Linux where the uid_map maps root, and makes nothing | `TestTempOverflowRefusesOffLinuxAndWhereTheUIDMapMapsRoot` |
| in a namespace that maps this account and not root, `TempOverflow` is a private folder `0700`, answered again, and `Temp` refuses the overflow account by name | `TestInAUserNamespaceTempOverflowIsAPrivateFolderAndTempRefusesTheOverflowAccount` (Linux, in `unshare --user --map-current-user`) |

Another account's folder on the way cannot be made by a test that is not root, which every rule
trusts; that refusal is `trust`'s owner rule, proven in `trust`'s own tests. One already there is
refused by the first walk, as the others-write case proves; one another account makes between the
check and the make is refused by the second walk, the walk the link-swap case proves.

The two Unix tests that need root's sticky folder (`TestOnUnixATempFolderThroughALinkTrustRefusesIsRefusedAndNothingIsMade`
and `TestOnUnixATempFolderRightInRootsStickyFolderIsThisAccountsOwn`) fail, and do not skip, where `/tmp` is not root's sticky folder, as in a user
namespace where `/` reads as another account's: a runner that cannot prove those guards shows red.

## Free, per system

| system | the call | what it counts |
|---|---|---|
| Linux, macOS, FreeBSD | `statfs` | blocks free to an account that is not root, times the block size. FreeBSD's count is signed and below zero once root has used its reserve: that is 0. |
| Windows | `GetDiskFreeSpaceEx` | bytes free to the caller, under its quota. |
| the others | none | an `*fs.PathError` matching `errors.ErrUnsupported`. |

A path that is not there is an `*fs.PathError` matching `fs.ErrNotExist`.

## Volume, per system

| system | what it asks | what is `Local` | what is not |
|---|---|---|---|
| macOS | `statfs`'s `f_fstypename` and `MNT_LOCAL`, then `listxattr` of the path and each folder above it, and the path's `SF_DATALESS` flag | `apfs`, `hfs`, `msdos` and `exfat` with `MNT_LOCAL` set and no File Provider mark | a type that holds `fuse` is `FUSE`, whatever `MNT_LOCAL` says, as macFUSE may set it; `nfs`, `smbfs`, `afpfs`, `webdav`, `cifs` and `ftp` are `Network`, FUSE-T's mounts among them, as it serves through NFS or SMB; a path in a File Provider domain, which the system's `com.apple.file-provider-domain-id` or a `com.apple.fileprovider.` attribute on it or a folder above it shows, or an evicted file, is `SyncPlaceholder`; a path below a `Library/CloudStorage` or `Library/Mobile Documents` folder with no mark, one whose folders' attributes cannot be listed, and any other type, `devfs` among them, are `Unknown` |
| Linux | `statfs`'s `f_type`, and `/proc/self/mountinfo`'s fstype for `Type` | ext2 to ext4, XFS, btrfs, f2fs, tmpfs, ZFS, FAT, exFAT, bcachefs and overlay, whose upper layer cannot be NFS | `FUSE` by its magic number, `fuseblk` and every `fuse.<subtype>` among them; NFS, SMB, CIFS, AFS, Ceph, Coda and 9p are `Network`; any other, procfs, sysfs and NTFS's kernel driver among them, is `Unknown`. The magic number alone decides: `Type` names it. Linux keeps no sync placeholders |
| Windows | `GetFinalPathNameByHandle` (`VOLUME_NAME_NT`), `GetVolumeInformationByHandleW`'s file system name and `GetDriveType` of the volume; then, on a volume not served over the network, `GetFileInformationByHandleEx` (`FileAttributeTagInfo`) of the final path and each folder above it up to the volume's root, each opened with `FILE_FLAG_OPEN_REPARSE_POINT`, and the sync roots registered under `HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Explorer\SyncRootManager`, each root's `UserSyncRoots` | NTFS, ReFS, FAT, FAT32 and exFAT on a numbered local disk volume, fixed or removable, under no sync root | a remote drive or a `\Device\Mup` path is `Network`, a share of this machine's own disk among them; a file system whose name holds FUSE is `FUSE`; a path below an `IO_REPARSE_TAG_CLOUD` tag or one of its variants, or below a registered sync root, matched by whole names with no case, OneDrive among them, is `SyncPlaceholder`; a path whose tags or the registered roots cannot be read, another device, a WinFsp or Dokan drive that names itself NTFS among them, another drive type and another file system are `Unknown`. The Cloud Files API's own `CfGetSyncRootInfoByPath` is not asked: go-files reaches no DLL's call by name |
| the others, FreeBSD among them | none | none | an `*fs.PathError` matching `errors.ErrUnsupported` |

A path that is not there is an `*fs.PathError` matching `fs.ErrNotExist`, and any error comes with
the zero `VolumeInfo`, which is `Unknown`. An error and `Unknown` are separate answers, and neither
reads as local. The answer is the volume's when asked: a mount over the folder, or a sync provider
set up later, may answer otherwise.

Windows' Cloud Files detection is not yet proved on a real OneDrive sync root: no runner it is
tested on has one. Its reparse tag half may never see a tag, as the Cloud Files filter can disguise
placeholders from a process that has not opted in to seeing them, and its registry half matches a
root as the registry spells it, so a root registered through a junction or another spelling of its
path does not match. Until the run of
[go-files#19](https://github.com/birdtable/go-files/issues/19) proves it, a caller that must not run
on a sync folder should take `SyncPlaceholder` on Windows as best effort.



## The alternatives looked at

Each was read on 15 Nov 2026.

| what | version | what it does | why it falls short |
|---|---|---|---|
| `os.UserConfigDir`, `os.UserCacheDir` | Go 1.27.4 | The parent folder of a program's settings or cache on this system. | Each reads the process's own environment, so a program cannot test another system's folders or hand in a reader. On Windows `UserConfigDir` is the roaming `%AppData%`. On macOS it is `~/Library/Application Support`, with no XDG layout. There is no state, runtime or data folder. |
| `github.com/adrg/xdg` | v0.5.3 | The XDG folders on each system, in package variables. | It reads the environment once, at `init`, into variables shared by the whole program, and `Reload` reads it again. On macOS its defaults are `~/Library`, with no `~/.config`. On Windows it honours the XDG variables, and looks up the known folders. A dependency, with testify in its graph. |

**The verdict.** Written here, over `path`, `strings` and two system calls.
