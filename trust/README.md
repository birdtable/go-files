# trust

`trust` checks a path before a program runs or reads what it names: a program it starts, a key it
signs with, a file that decides what runs, a socket it talks to. `Path` walks the path from the
top of its volume and answers it with no link left in it, or refuses it with what was seen and the
change that fixes it, as data. `ReadOwned` reads a whole file through one descriptor checked to be
plain, owned and small. `Held` judges a file or folder already open, on its descriptor, as `Path`
judges the last name. `Fix.Commands` gives a fix as the argv of each command that makes it.
`Interpreters` gives the programs a file's `#!` line would have the system start. For tests,
`trusttest` lets another account in, keeps this account out, gives a name to a group, checks a
mode and that a name is private to this account, and finds a file the system owns that this
account may not write.

## Install

```sh
go get github.com/birdtable/go-files/trust@latest
```

The full API is on [pkg.go.dev](https://pkg.go.dev/github.com/birdtable/go-files/trust).

## Path

`Path(path, last, who)` takes an absolute, clean path, `/usr/bin/git` or `C:\Program Files\Git\bin`,
and reads each name from the top down without following it: `lstat(2)` on Unix, and on Windows a
handle opened on the name itself (`FILE_FLAG_OPEN_REPARSE_POINT`), from which its type, owner and
access list are read. Every name before the last must be a folder or a link, and the last of the
kind `last`: `File`, `Folder`, `Key`, `Program`, `Socket` or `PrivateFolder`. A link is judged before it is read;
once it passes, the walk starts again from the top on its target and the names after it, up to
forty links. No path is cleaned before its links are read: on Unix a `..` in a link's target, or after
a link, goes up from where the link leads, as the system goes, and is read and judged there, and on
Windows the walk takes a target by its spelling, against the link's folder, as the system does. A
name that is not there, or whose facts cannot be read, is an error, never a pass.

`Who` says whom the walk trusts. Root, and on Windows Administrators, SYSTEM and TrustedInstaller,
is always trusted, and so is the account that runs the program, unless `RootOnly`. `RootOnly`
trusts root alone: no group, no sticky folder and no link. `NoLinks` refuses a link anywhere on
the path. `Group`, when it answers true for a name's group, lets that group write the name, as a
package manager's folder its group shares; nil lets no group write. Windows has no group to ask.

`Overflow` is for a test alone, and no program may set it. In a Linux user namespace
that maps this account and not root, as `unshare --user --map-current-user` makes, every account
the namespace does not map shows as the kernel's overflow account, the uid in
`/proc/sys/kernel/overflowuid`: root's `/` among them, so no walk could pass there. `Overflow`
trusts that account as root is trusted, and nothing else: the mode bits, the links, the sticky
folder and `Group` are judged as ever, and the overflow group is not trusted. Since the overflow
account there is every account the namespace does not map, a walk with `Overflow` set trusts a
name of any of them as root's, so the field is only as good as the test's own plants being this
account's. `Path`, `Held` and `ReadOwned` refuse it with `ErrOverflow`, reading nothing: off Linux, test binary or not,
where the refusal matches `errors.ErrUnsupported` too; outside a test binary
(`testing.Testing()`); and where `/proc/self/uid_map` cannot be read, maps root, maps none, or
maps the overflow id itself, which would make it an account the namespace maps as well. It is
never ignored. `ReadOwned` reads it from its `mounts`, set where any of them sets it, for the walk above each `Who.Mounts` entry and the owners it trusts as root.

On Linux, `Path`, `ReadOwned`, `Held` and `Account` refuse a program that runs as the overflow account
where `/proc/self/uid_map` is not the one line `0 0 4294967295`, which maps every id as itself:
in a user namespace that does not map it, the account is any the namespace leaves out. They
refuse one where the overflow account cannot be read too, with `ErrOverflow`, before any name is
read. Where the map is that line, in the first namespace and in a container with no
user namespace of its own, the overflow id is `nobody` (65534 on most systems), and a program run
as `nobody` is served as at v0.1.0; in a namespace that maps part of the ids, as a rootless
container's, it is refused. `Account` then answers the account read beside the refusal, and
`ReadOwned` refuses every owner. The unreadable case fails closed: a Linux system where `/proc`
is not mounted refuses every call.

`Apart` is folders the path may not be, nor lie below: a program keeps its temp folder out of a work
tree this way, where a commit could put a file its tools read. Each must be absolute and clean,
and is read once, before the walk, through its links as what it names: `stat(2)` on Unix, and on
Windows a handle opened through its reparse points. One that is not there, or cannot be read, is
an error, never a pass. Each name on the way is then held to them by its file ID, read on the
walk's own read of the name, its device and inode on Unix and its volume's serial number and file
index on Windows, and one that is the same file as one of them is refused with `ErrInside`,
naming both paths. Since the walk reaches every name of the path, a link's target among them, a
path below the folder is refused at the folder, whatever spelling reached it: through a link or a
junction, or in another case on a volume that ignores case, as macOS's and NTFS do by
default. No path is compared. Where the IDs two volumes give agree, as a copied disk's serial number or a
ReFS file ID cut to 64 bits can, a name is refused that is not the folder: the error is a refusal,
never a pass. A file whose 64-bit index itself reads zero (a WebDAV or SMB drive, whose files keep
no file ID) or all-ones (ReFS, for a file whose 128-bit ID has no 64-bit form) gives `os.SameFile`
nothing to tell it apart by, so the walk refuses it with `ErrNoFileIndex`, the one value
`write.ErrNoFileIndex` is, matching `errors.ErrUnsupported`, rather than trust the compare.

Every refusal is an `*Unsafe` whose `Path` is the name it is about, as the walk reached it: the
path asked for, or a folder or link on its way. It is `""` only for a setting of `Who` (a
`Who.Apart` entry that is not absolute and clean, or `Who.Overflow`), which names no path. A
caller that puts its own subject first compares `Path` with it, and never reads the words. Its
`Observed` says what was seen and its `Fix` is the change that makes the next check pass, as data for the caller to show or make in its own words: the
name (`Path`), the mode bits to take away or add on Unix (`Clear`, `Set`), the account to give it
to (`Owner`, in `Account`'s form), whose entries come off its list (`Revoke`: a SID on Windows,
and on macOS a `uid:<n>`, `gid:<n>` or `guid:<GUID>`, whose deny entries stay), and on Windows a
null list to reset (`Reset`). Nothing in `trust` quotes or joins a command line, so no path is
quoted for a shell: `Fix.Commands` gives the fix as argv (below). The zero `Fix` is none, when only another setting can (a name
of the wrong kind, or one right in `/tmp`, which only moving fixes). It matches the error it was refused
with, so a program says which it found: `ErrNotPlain`, `ErrNotOwners`, `ErrOthersWrite`,
`ErrOthersRead`, `ErrTooLong`, `ErrOverflow` or `ErrInside`. A path that is not absolute and clean, one through more than
forty links, a program its owner cannot run, and `ReadOwned`'s refusal of its own `limit` or
`owner` match none of them.

### On Linux, macOS and the other Unix systems: the mode bits

A name must be owned by root or, unless `RootOnly`, this account. Neither its group nor others may
write it, but for two cases. A group `Who.Group` accepts may write it. A folder of root's with the
sticky bit, as `/tmp`, passes above the last name: another account may add a name there and may
not rename or remove one. A program is one its owner may execute, and a key one its group and
others can neither read nor write, and so is a `PrivateFolder`, a folder.

A name right in such a folder is held further. Another account can add a hard link there to any
file or link it can see, which then passes as that name, so a file, key, program, socket or link
right in it is refused whoever owns it, and a folder of root's there is refused too. A folder of
this account's in `/tmp` passes, and what is in it is judged as anywhere; a link of this
account's right in `/tmp` does not, as a hard link to it would pass as it.

A folder right in `/tmp` passes only on `/tmp`'s own device, whoever owns it. An account may mount
a FUSE file system on a folder it owns, and that file system reports any owner and mode it likes
for the folder. The device is the kernel's, so a folder another account serves never passes as
this account's. A folder mounted there from another file system, a bind mount of
another volume or a btrfs subvolume among them, is refused for the same reason. The device rule
holds right in a sticky folder alone; a mount further down, or below a folder that is not sticky,
is judged by its mounter, as the paragraph below on a volume an account not trusted mounted says.

When this account is root, root's own folder right in `/tmp` passes as this account's own, as
tmux's `/tmp/tmux-<uid>` and OpenSSH's StrictModes judge it: another account can make no folder of
root's, and the sticky bit keeps it from renaming or removing one. For every other
account a folder of root's there is still refused, since root makes such folders for others, as
libpam-tmpdir's `/tmp/user/<uid>`.

A name on a volume an account the walk does not trust mounted is refused, wherever it is on the
way, with `ErrNotOwners`, naming the folder it is mounted on and its mounter, and the zero `Fix`.
Its FUSE server, or the image it attached, reports any owner and mode it likes, so one mounted with `allow_other` on a folder below folders that pass would pass on root's owner, and
a device compare cannot tell it from root's own mounts. Root and, unless `RootOnly`, this account
are trusted; `ReadOwned` trusts root and the owner it is asked for. Where the mounter is read:

| system | the mounter |
|---|---|
| Linux | a FUSE file system's, by `fstatfs`, from its `user_id=` in `/proc/self/mountinfo`, found by device; the name is opened `O_PATH` and held to the file `lstat` read, so the mount judged is the one its facts came from; a FUSE mount mountinfo does not name, or of any type but virtiofs with no `user_id=`, is refused. `user_id=` is numbered in the mounter's user namespace, so it is read only where this process's user namespace and its mount namespace's owner, by `NS_GET_USERNS`, are the same file, by device and inode, and the initial one: in a container with a user namespace of its own, or after joining a mount namespace another user namespace owns, every FUSE name is refused as unnamed, and a container with none reads as the host. A joined mount namespace's owner may mount over `/proc`, or bind or link another file of a procfs there, so what is read there is held to procfs. Mountinfo is opened by `openat2` from `/proc`, itself held to a procfs's root by `fstatfs` and inode 1, crossing no mount and following no magic link, so it is this process's procfs's own and no file bound, mounted or linked there; the `uid_map` and overflow account of `Who.Overflow` are read only from a file `fstatfs` shows on procfs; and each is read up to 4 MiB. A mountinfo not so held names no mounter, so the name is refused, and gives no mount's type either: where a seccomp filter refuses `openat2` with `ENOSYS`, every name on a type held to its mount's type is refused with `ErrMount`, as below Linux 5.8; where one refuses it with `EPERM`, as older container runtimes' default profiles did, every such name, a container's own overlayfs root among them, is an error and goes unjudged. Left open: another file of procfs bound or linked over `uid_map` or the overflow account is read as it. The two namespaces come from a pidfd of this process by Linux 6.11's `PIDFD_GET_USER_NAMESPACE` and `PIDFD_GET_MNT_NAMESPACE`, which read nothing under `/proc`; an older kernel opens `/proc/self/ns`. Each is held to nsfs by `fstatfs` and to its type by `NS_GET_NSTYPE` before `NS_GET_USERNS` is asked of it, so no other file is taken for one, though on an older kernel a real namespace file of another process may be bound there; virtiofs, which only root mounts, and every file system not FUSE pass, as only root mounts them outside a user namespace |
| macOS | every volume's, `statfs`'s `f_owner`, read with its ownership flag |
| FreeBSD, NetBSD | not judged: only root mounts unless `vfs.usermount` is set |
| Windows | not judged: a volume is the system's, not an account's |

On Linux the owners of a mount are judged only where the kernel keeps them, by the type `fstatfs`
reads on the name held: ext2, ext3 and ext4, xfs, btrfs, f2fs and ZFS, which a disk backs; tmpfs,
ramfs and overlayfs, which only root mounts outside a user namespace; and FUSE and NFS, by the rules
above. A name on any other type, as CIFS, 9p, vfat or exfat, squashfs (and so a
snap) or erofs, is refused with `ErrMount`, which matches `ErrNotOwners`, naming the type's
magic, at the first name the walk meets on it, the folder it is mounted on; `Who.FileSystems` adds
a type by its `f_type`, as erofs's `0xe0f5e1e2`. The type alone does not prove a disk: a 9P2000.L
client answers `fstatfs` with its server's type, whatever it is, so a libvirt, Vagrant or Lima
share of a host's ext4, tmpfs or overlayfs reads as that type in the guest. So on Linux each type
is held to its mount. ext2/3/4, xfs and f2fs pass on a device whose major is not 0, the held
descriptor's, as every 9p mount's is an anonymous device of major 0, without their mount's type
read. Every other type, tmpfs, ramfs, overlayfs, FUSE and NFS included, and those three
on major 0, is held to the type mountinfo gives the name's mount, found by its mount number, by
`statx`'s `STATX_MNT_ID`, as a btrfs subvolume's device is not its mount's: btrfs and ZFS must
read as themselves there, and a mount mountinfo calls 9p is judged as 9p, whatever its server
answers, a share of a host's ext4 or xfs included, so it is refused unless `Who.FileSystems`
names 9p's own type, `0x01021997`; a disk's type on major 0 on any other mount is refused. A
mount whose type is not read is refused with `ErrMount`, and nothing more is read of it, as its
NFSv4 list or FUSE mounter. trust needs Linux 5.8 or later, and below it fails closed: with no
`STATX_MNT_ID` there, every name on btrfs, ZFS, tmpfs, ramfs, overlayfs, FUSE, NFS or a type
`Who.FileSystems` adds is so refused, `$XDG_RUNTIME_DIR` and `/dev/shm` among them, and a 9p share
of a host's disk, even where the app names 9p, so only paths on ext4, xfs and f2fs keep trust
there; and so is each where this process is not in `/proc`'s pid namespace, as after `nsenter --mount`
into a container but not its pid namespace, where `/proc/self` names nothing, so the mountinfo there is taken as not
this process's own and is not read, and where a seccomp filter refuses `openat2` with `ENOSYS`. `/proc/self/fdinfo`'s number is not read instead, as an owner of a namespace this process
joined may bind its own over it. A type of 0, as a server may answer, is refused on Linux;
it passes only where no type is read, off Linux. A ZFS name carrying `system.nfs4_acl_xdr`, the
NFSv4 list TrueNAS SCALE enforces, is refused with `ErrMount` too, as trust does not read it; only
the read on the file held may show it absent, by `ENODATA` or `EOPNOTSUPP`. A ZFS socket,
fifo or device, as a program's socket under a ZFS home, is not reopened to read it, and so is an
error, never judged; `$XDG_RUNTIME_DIR` is a tmpfs, and its sockets are judged. So is a ZFS name
this account may not read, as a folder of mode `--x` on the way, since its reopen fails: on a ZFS
root a walk through one by any account but root is an error. Left open on
ZFS: a snapshot under `.zfs/snapshot` keeps each file's old contents under its old mode and owner,
and a dataset whose root `zfs allow` or `zfs zone` delegates is its delegate's to shape, as a disk
the account mounts would be. A disk mounted `nosuid` is refused so too, as
fusermount, udisks and fstab's `user` option mount an account's stick or image, unless its folder
is one of `Who.Mounts`, held by device: a `/home` hardened `nosuid`, as CIS sets it, is refused
until the program names it. A FUSE mount root's helper made `nosuid`, as udisks mounts an
account's stick with ntfs-3g or exfat-fuse, is refused so too, as ntfs-3g takes every owner from
a map on the volume itself; a FUSE mount this account made is left to the mounter rule. Where a
sandbox mounts every file system `nosuid`, every walk is refused at `/` until `Who.Mounts` names
each disk it crosses: in systemd, a unit with `ProtectSystem=strict` or `DynamicUser=`
(`NoNewPrivileges=` with `PrivateTmp=` leaves `/` setuid); a podman volume; and every bwrap or
Flatpak bind. A Docker bind is not `nosuid`. Such a sandbox cannot tell its own disks from a stick
udisks mounts and that propagates into it with its own `nosuid`, so the rule stays fail-closed
there. A loop or udisks mount without `nosuid`, of another account's image
among them, passes on the owners its image holds: only root mounts one so, and an account that
can forge owners there can already plant a program setuid-root. That reason does not hold where
the mount is `noexec`, which takes away that setuid program: a disk mounted `noexec` without
`nosuid`, as usbmount's default options mount an ext4 stick, is refused as one mounted `nosuid` is,
until `Who.Mounts` names its folder, and so is a FUSE mount root's helper made so. No CIS or STIG
profile pairs `noexec` with anything but `nosuid`, systemd's `tmp.mount` is a tmpfs, and udisks
always adds `nosuid`, so none of them meets this rule. These do, and carry its cost: a systemd
unit's `NoExecPaths=`, which remounts each path `noexec` alone, so in a unit with `NoExecPaths=/`
a walk is refused at `/` until `Who.Mounts` names each disk it crosses; a disk mounted in fstab
with `noexec` alone; and a ZFS dataset with `exec=off`. The rule reads `noexec` whether or not the
mount is `nodev`, the stricter reading where an account that can plant a device node on a mount
without `nodev` may still fall short of root.
`Path`, `Held` and `ReadOwned` judge each `Who.Mounts` entry at every call, before the path, and
refuse the call with `ErrMountEntry`, the entry as `Unsafe.Path`, unless it is absolute and clean,
every name above it is one only root may change, as a walk with `RootOnly` judges its parent,
taking as its `Who.Mounts` the call's entries above it, judged first, shallowest first in whatever
order they are given, so no entry vouches for itself or for one above it, it is no link, and it is the root of a mount: on Linux by its mount, statx's `STATX_ATTR_MOUNT_ROOT`, so
a disk bound over a folder of the same disk counts, and a kernel before 5.8, which has none, is
refused; on macOS and every other Unix by a device other than its parent's. So an entry at a name
another account controls, as a link in `/tmp`, or a folder whose disk is not mounted, which names
the disk beneath it, is refused. The check holds the entry's name, one only root can move, and
that a mount is there; it does not hold on whose ask root mounted it, nor whose disk is there. A
mount point a root helper fills for whoever plugs a disk in, as usbmount's `/media/usb0` or
udisks' shared `/media/<label>`, passes, and lends its pass to that disk, whose owners and modes
its maker wrote: name one only where every account that can fill it is trusted with what the pass
lets through. The entry's owner is not judged: an account may own the folder
root mounted for it, as `/run/user/<uid>`. An entry that names nothing, or whose folder above
names nothing, is refused with `ErrMountEntry` too, never matching `fs.ErrNotExist`, which a
caller, as `appdirs`' temp folder, reads as a name not yet made; one that cannot be read is that
error, as it is. The `nosuid` and `noexec` rows run with
no root, in the user and mount namespace `unshare --user --map-root-user --mount` makes, where a
folder on a disk is bound over itself and made `nosuid`, or `noexec` and `nodev` without it, which
the kernel reports by `fstatfs` as it does udisks' and usbmount's mounts; the `noexec` rows want a
disk not mounted `nosuid`, which the namespace cannot lift; a FUSE mount root's helper made cannot be made there, and its row is the unit's.
Left open: an
overlayfs whose lower
layer is a FUSE mount, in a namespace another account owns, passes on what that server reports.

**Linux** keeps a POSIX access list's mask in the group bits. A group-writable name with an access
list is refused even when `Who.Group` accepts its group, since the mask lets every account and
group the list names write it; the fix takes the group's write bit away, which makes the mask
read-only. A default access
list is only what a new name starts with, and the walk judges that name when it reaches it.

**On an NFS mount, Linux** shows the server's NFSv4 list as `system.nfs4_acl`, which may grant a
named account what the mode does not show, as a ZFS or ONTAP server keeps it. The
name is held open with `O_PATH`, and its list is read on the file held, never by a path, which a
joined mount namespace's owner may mount over. Neither `fgetxattr` nor Linux 6.13's
`getxattrat` takes an `O_PATH` descriptor, so the name is reopened to be read through
`/proc/self/fd`, and the descriptor opened is held to the one held by its mount number, device
and inode before its list is asked. Another file there, or any error before the list is asked, is
an error that matches no errno, so it is never read as no list, and the name goes unjudged.
So on an NFS mount a name this account may not read, as a folder of mode `--x`, and a device, fifo
or socket, are errors, unjudged; each name read costs the server an OPEN; and a device an owner of
the mount namespace binds over the path is opened, non-blocking, before it is refused. The list is judged by the macOS rule below, in NFSv4's own rights: `OWNER@` is the owner, `GROUP@` the group, and a named principal, written
`nfs4:<who>` or `nfs4group:<who>`, is never trusted, since the server maps names as it likes, so
an entry naming this account or root is refused too. A `PrivateFolder`, a key and `ReadOwned`'s
private file are refused READ_DATA or EXECUTE, as a server reads a file to EXECUTE alone (RFC
8881, 6.2.1.3.1). An allow entry of `GROUP@` or `EVERYONE@` whose write is within WRITE_DATA,
APPEND_DATA and DELETE_CHILD is judged as the mode's w bit of its class, which knfsd derives it from:
it passes where the mode shows that bit and the mode passed, as root's sticky folder above the last
name or a group `Who.Group` lets in, and is refused where it does not. An audit or
alarm entry grants nothing; an entry of another type, or a
list that does not parse, is refused as a list not read. `Fix.Revoke` is the principal, whose
entries but its deny entries come off. A server that keeps no list has none, and a link's own
list is not read. A mount that cannot show the list, as NFSv3, or an NFSv4 mount whose server
does not offer the attribute, both of which answer `EOPNOTSUPP`, is still judged by it at the
server, so there a key, a `PrivateFolder` and `ReadOwned`'s private file are refused, matching
`ErrOthersRead` and `errors.ErrUnsupported`, and other kinds pass on the mode: a program keeps its
keys on a local disk, or on a mount that shows its list.

**macOS** keeps an access list beside the mode, set with `chmod +a`, and `Path` reads it with
`getattrlist`, never following a link, after the mode bits pass. An allow entry
that is not inherit-only and gives an account or group the walk does not trust a right that
changes the name is refused: writing or appending to it, adding to or removing from a folder,
deleting it, writing its attributes, extended attributes or list, taking it, or the generic all
and write rights. A key or a `PrivateFolder` is refused an entry that lets another read it too, and a `PrivateFolder` one that lets another search it, as its mode refuses `0701` and `0710`. Only root and, unless
`RootOnly`, this account are trusted, and only by the GUID the kernel gives an account by its id;
an account named by its own GUID from the directory, and every group, `Who.Group`'s included,
are not: `chmod +a "user:<me> allow write"` makes an entry that names this account by its
directory GUID, and that entry is refused, so a person who granted their own account an entry
takes it off. A deny
entry passes, so a home folder, whose list denies everyone its deletion, passes. An inherit-only
entry passes on the folder that holds it, but a name made in that folder inherits it as an
entry that is not inherit-only, and that name is refused. A `PrivateFolder` is refused an inherit-only
entry as one that is not, since nothing judges the files a program makes in it.
An entry of a kind neither allow nor deny is refused, and so is a list that cannot be read,
matching `errors.ErrUnsupported` too. An answer for a file other than the one the name's `lstat`
read, as when the name's owner replaces it by rename, as `write` does, between the two reads, is
read again from the `lstat`, up to eight tries; a name replaced at each of them is refused with
`ErrReplaced`, which matches no refusal of a file's facts, since none was judged, and a later call
may pass. The fix is `Fix.Revoke`, the entry's `uid:<n>`, `gid:<n>` or `guid:<GUID>`: every entry of
it but its deny entries comes off the list of `Fix.Path` itself, a link's own for a link, and the
rest stay, the deny entries that guard a home folder among them.
`golang.org/x/sys` has no call that reads the list, so the walk makes one raw system call
until it gains one.

A volume mounted to ignore ownership (`MNT_IGNORE_OWNERSHIP`, "Ignore ownership on this volume" in
the Finder), as an external disk usually is, shows every account as the owner of every name on it,
with the owner's rights, so a `0600` key there is anyone's to rewrite. `Path` reads each name's
mount with `statfs`, a link's by the folder that holds it, and refuses every name on such a volume,
from the folder it is mounted on down, with `ErrOwnersIgnored`, which matches `ErrNotOwners` and
names that folder; `ReadOwned` reads its file's on the descriptor. Its `Fix` is the zero one: only
the volume's setting, or a copy onto a volume that keeps ownership, fixes it.

**FreeBSD, NetBSD and every other Unix system**: an access list there, where the system keeps
one, is not read, so every name is refused once its mode passes, matching `ErrOthersWrite` and
`errors.ErrUnsupported`.

### On Windows: six rules

Windows gives a name an owner and an access list, and no mode bits. The rules were drawn from the
lists of `C:\`, a profile and `C:\Program Files\Git\mingw64\bin` on a Windows PC; the rules pass
the last two and refuse `C:\` as the last name.

1. The owner must be Administrators, SYSTEM, TrustedInstaller or, unless `RootOnly`, this account.
   The fix gives the name to Administrators.
2. No allow entry may give a right that changes the name to an account outside those: write or add
   a file, append or add a folder, write its extended attributes or attributes, delete a child,
   delete, write its list, take it, or the generic or maximum rights. A key or a `PrivateFolder` is refused when such an
   account may read it. The fix revokes that account's allow entries, once the ones the name
   inherits are copied onto it as its own.
3. An inherit-only entry gives the name itself nothing and is not judged; the walk judges the child
   it reaches. A `PrivateFolder` is the exception: nothing judges the files a program makes in it, so
   its inherit-only entries are judged as its own, and only CREATOR OWNER's (`S-1-3-0`) passes, as
   on a new name it becomes its maker's entry; CREATOR GROUP's and every other is judged as any.
4. A null list, which lets anyone do anything, is refused. The fix resets the list to the entries
   the name inherits.
5. A reparse point is a link, a junction or a symbolic link alike, judged and followed as one, but
   for a Unix socket's. It is judged at its own name before it is followed, by rules 1 to 4 as a
   folder there is: whoever may change a reparse point may point it elsewhere. The right to add a
   folder alone, which passes on a folder above the last name, passes on no link.
6. What cannot be read, the handle, the security descriptor, its owner or an entry, is an error,
   never a pass, and so is a SID that cannot be written as a string, the owner's or this account's
   own, read from its token. An entry of a type that is neither allow nor deny is refused.

A deny entry gives nothing. One allow entry passes that rule 2 would refuse: an entry that gives
only the right to add a folder (`FILE_ADD_SUBDIRECTORY`, 0x4), on a folder above the last name.
That account may add a name there and may not rename or remove one, as on a sticky folder, since
that needs a right on the child or the right to delete a child here. It is what `C:\` gives every
signed-in account, so without it every path on the system drive is refused. It holds under
`RootOnly` too, for the same reason, and a folder such an account adds is that account's, which
rule 1 refuses.

An entry of an AppContainer's SID is not judged at all. That is an app package's SID,
`S-1-15-2-...` (ALL APPLICATION PACKAGES, `S-1-15-2-1`, is one), or a capability's,
`S-1-15-3-...`. The walk knows one by the SID's parts, never by its string: identifier authority
15, then a first sub-authority of 2 or 3, then at least one more. Windows writes such entries into
every account's AppData, so without this rule a program's config in `%APPDATA%` is refused. Only
the token of a sandboxed app holds such a SID, and that app runs as an account. Windows gives the
app a right only when the list also gives it to that account or its groups, and those entries are
judged as ever. So the rule holds under `RootOnly` and in `ReadOwned` too, and for reading as for
writing: a key, or a private file `ReadOwned` reads, that an AppContainer may read passes, as one it
may write does. Every other account
is judged as before: Users, Everyone, Authenticated Users, any other account's SID, and a SID that
only looks like an AppContainer's.

On Windows each entry's SID is read from the entry's own bytes, within the entry's size and the
list's: revision 1, at most 15 sub-authorities, all of them inside the entry. An entry that runs
past its list, or whose SID does not fit in it, is an error, never a pass.

A path is absolute when it names its volume: `C:\x` and `\\?\C:\x` are, and `C:x`, relative to the
drive's current folder, is not.

### What the Windows rules do not hold

- **A trusted folder moved in under a new name.** An account with the right to add a folder on
  `C:\` and the right to delete a child in a folder where a folder of a trusted owner sits can move
  that folder into `C:\` under a new name, and the walk passes it there, still its owner's, with its
  own list. On Unix, root's sticky folder holds a name right in it further; Windows has no twin of
  that hold. OpenSSH's StrictModes for Windows does not guard this either.
- **A remote volume.** A path on a share or a mapped drive is judged by that server's accounts, on
  the descriptor as the server hands it, and opening the name signs this account in to the server
  before anything is judged. Remote volumes are not refused: that would refuse profile folders
  redirected to a share, a common enterprise setup, and the common practice judges the descriptor
  as served.

## ReadOwned

`ReadOwned(path, owner, private, limit)` opens the file so it never waits (a pipe at the name is
refused at once) and never through a link at the name, and checks it on the descriptor opened,
never on the name, so a file swapped in at the name after the open is not the one read. The file
must be a regular file `owner` owns, `uid:1000` on Unix and a SID such as `S-1-5-18` on Windows; no
other account but root's may write it, nor read it when `private`; and it may hold at most `limit`
bytes. A file over the limit, or one that grows past it while it is read, is refused whole, never
cut short. A limit of 0 or less, and an owner in neither form, are refused before anything is
opened. On Linux a file on a mount `Path` does not judge, by its type or a disk mounted `nosuid`, or `noexec` without it, is refused with `ErrMount`, unless one of the `Who` values given after `limit` names the type or the mount, of which only `FileSystems`, `Mounts` and `Overflow` are read, `Overflow` refused with `ErrOverflow`, reading nothing, outside its gates. On macOS a file on a volume that ignores ownership is refused with `ErrOwnersIgnored`, read
on the descriptor, and the file's access list is judged as `Path` judges a key's, and is read by the
name, held to the descriptor by its device and inode: when a file is swapped in at the name after
the open, the name is opened again and the new descriptor checked whole, as a second call would,
up to eight tries, and a name swapped at each of them is refused with `ErrReplaced`, never read. `ReadOwned` does not walk the folders above the file; a
program that needs them checked calls `Path` on the folder first.
`Account()` is this account in that form, the one the walk trusts, so a program names itself as `owner`.
`OwnerOf(f)` is the owner of an open file or folder in that form, read on its descriptor and never
by a name: `root.Open(".")` and then `OwnerOf` reads a root's owner, and a folder swapped in at
the name after the open is not the one read. It judges nothing; `Path` and `Held` judge.

## Held

`Held(f, last, who)` judges `f`, a file or folder already open, as `Path` judges the last name of a
path of the kind `last` for `who`, and walks no folder above it. Its type, owner, mode bits and
access lists, and the volume it is on, are read on its descriptor, never by a name, so a file
swapped in at the name after the open is not the one judged: on Unix by `fstat`, `fstatfs` and, on
Linux, `fgetxattr` for an access list's mask, and on Windows by `GetSecurityInfo` on the handle,
which gives the owner and the access list there, where Go's mode of a folder is always 0777. Every
rule `Path` keeps for the last name holds, with the same refusals, whose `Path` is `f.Name()`, but
with no `Fix`: a fix acts by a name, and the name may lead elsewhere than the file judged, through a
link at it or another file renamed over it, so a fix for the name is `Path`'s to give, which reads
the name it judges. `Who.Apart` is judged by the held file's own file ID, so a
folder of `Who.Apart` itself is refused, but a file below one passes `Held`: the caller's `Path`,
which walks the folders above it, is what refuses that (`ErrInside`). `Who.Overflow`, `Who.Group`,
`Who.FileSystems` and `Who.Mounts` are judged as by `Path`; `Who.NoLinks` has nothing to refuse, as a file held
is never a link.

It does not judge how `f` was reached. Since no folder above it is read, it does not hold a file to
root's sticky folder as `Path` holds a name right in one, and a file opened through a name another
account could change may be any file a trusted account owns: a program that holds what it judges
walks the folder with `Path` first and opens the name through it, as `root.Open` does. On macOS,
whose access list is read by the name, the list is read by `f.Name()` and held to the descriptor by
its device and inode: a name that names another file, or none, at each of eight tries, as one moved,
replaced or opened by a name relative to another folder, is refused with `ErrReplaced`, never passed
and never `fs.ErrNotExist`, as the file is held though its name leads nowhere. Reading the list on
the descriptor itself takes `fgetattrlist`, a raw call beyond the one the package allows itself on macOS. A
closed file, or one whose facts cannot be read, is an error, never a pass.

## MkdirPrivate

`MkdirPrivate(dir, who)` answers `dir`, an absolute, clean path, as an `*os.Root` of its own: a
folder only this account reaches, judged by `Path` for `who` before anything is made and again once
it is there.

1. **The nearest part.** The longest part of `dir` that is there is found by `os.Lstat` of each
   name as written, so a link is a name that is there.
2. **The judge above.** `Path` walks that part and the first name below it that is not there, so
   every folder above that name, each link on the way among them, is judged as `Path` judges a
   folder above the last name, root's sticky folder included, and nothing is made through a name
   it refuses then. A link is followed only where `Path` trusts it, so only by an account the
   caller trusts, and none is followed with `Who.NoLinks`: refusing every link would refuse
   macOS's `/var` and `/tmp`. A walk cut short by anything but a name not there is answered as it
   refused.
   So is one that ends in another folder than the nearest part: a link that leads to a name not
   there, or a link changed after the walk. Nothing is made where the link points.
3. **The make.** Each missing folder is made below the folder the walk reached, which holds no
   link, by `confine.MkdirPrivate`: private as it is made, 0700 exactly on Unix whatever the umask,
   and the owner-only list on Windows, with no link followed.
4. **The judge of dir.** `dir` is judged by `Path` as a `PrivateFolder`. The folder answered must
   be the file at the path `Path` answered (`os.SameFile`), and is then judged on its own handle by
   `Held`.

A folder above changed after the judge above, as another account may in a sticky folder, is
refused by the judge of `dir`, and the folders this call made below it are left there, 0700 and
empty.

A folder that was there keeps its mode. One that is not private is refused as `Path` refuses it,
with its `Fix`, and is never changed: the fix is the person's to run.

Another make of `dir` by this account at the same time is not refused. Three things send this call
back to step 1, which finds the folder there and judges it as one that was there:

- a folder another make made between the look and the judge;
- a name it put in a folder this call made (`confine.ErrNotEmpty`);
- a folder made but not there to a second look, as Windows answers for a moment.

A try is counted, never timed. After 8 tries cut short so, the call is refused with
`ErrReplaced`. Any other refusal of `confine`, as `confine.ErrOutside` or `confine.ErrChanged`, is
answered at once, naming the folder below which the make was made.

`who` is both walks'. `RootOnly` trusts no folder this account makes, so it is refused before
anything is read, matching `ErrNotOwners`.

## Commands

`Fix.Commands()` gives a `Fix` as the commands that make it, in order, each a `Step` whose `Argv`
is the words a program is started with: `chmod` with chmod's symbolic words (`go-w`) and `chown -h
root` on Unix, `icacls <path> /reset /L`, `icacls <path> /inheritance:d /L` then `icacls <path>
/remove:g *<SID> /L`, and `icacls <path> /setowner *S-1-5-32-544 /L` on Windows. An owner is
any account in `Account`'s form, so a caller's own `Fix` with `Account()`'s answer as its `Owner`
gives a name to this account: `chown -h <N> <path>` for `uid:N` (`root` for `uid:0`), and
`icacls <path> /setowner *<SID> /L` for a SID. That step needs root on Unix, and on
Windows an elevated prompt unless the name's list grants this account the right to take
ownership; `chown` reads a number as a user name first where an account is so named. Each step but
the mode's acts on the name itself, never on what a link there names: the walk refuses a link by
its own owner and list (rule 5), so its fix is for the link, and `/L` and `-h` keep the step on
it; `takeown` has no such switch, so `icacls` sets the owner. `chmod` follows a link, as Linux
keeps no mode of a link's own: `trust` gives no link a fix of mode bits, since the walk judges a
link by its owner and lists alone, so only a caller's own `Fix` of a link with a mode reaches
what the link names. The owner step comes last, so each step before it is made
by the owner the name has. `Admin` says the step needs root or an elevated
prompt, which trust names in no word of its own: there is no `sudo` in an argv. Nothing in `trust`
quotes or joins a step, so a path is one word however it is spelled, and a program that shows a
step as a line to paste quotes each word for the shell it names, since cmd and PowerShell quote one
Windows argv differently. What the fix holds decides the system its commands are for, not the
system this runs on, so the same `Fix` gives the same steps everywhere.

It gives no steps and `ok` false when no command here makes the whole fix: the zero `Fix`; a path
that is not absolute in the form of the system the fix is for (a drive or a share on Windows, `/`
on Unix), so no path is read as a switch, or that holds a NUL; mode bits beyond the nine; an owner
not in `Account`'s form (a name, a uid with a leading zero or more than ten digits, a SID with
words after it); a revoke from a macOS or an NFSv4 list, whose entries take
more than a name to remove; a SID that is not one; and a fix that mixes Windows's changes with
Unix's. The caller then says the fix in its own words.

## Interpreters

`Interpreters(path)` gives the programs a file's `#!` line would have the system start, in order:
the interpreter the line names, then, while that one is itself a script, the interpreter it names,
the last being the program the system runs. A file with no `#!` line gives none, and so does every
file on Windows, which starts no interpreter a file names. It reads each line by the system's own
rule, proven by starting scripts there (Linux 7.0, macOS 27):

| system | the line |
|---|---|
| Linux | the first 256 bytes; a newline before any NUL in them ends the line, and with none the interpreter must end in them; up to 5 scripts in one chain |
| macOS | a newline or a `#` within the first 512 bytes ends the line; a script's interpreter must not be a script |
| both | spaces and tabs before the interpreter dropped; it ends at a space, a tab or a NUL; its arguments not read |
| FreeBSD, NetBSD | not proven: a file with a `#!` line is an error matching `errors.ErrUnsupported` |
| Windows | no line read: none |

The chain ends at `/usr/bin/env` for `#!/usr/bin/env python3`: the program env then runs is found
on the `PATH` of the run, which the caller holds and walks. Nothing is judged: the caller walks
each program with `Path`, as a `Program`. It is an error, and no chain, where the system starts
none of it, matching the errno the system gives: a line that names no interpreter or is cut before
it ends (`ENOEXEC`); an interpreter that is not absolute, which the system looks for in the folder
a run is in (`ENOENT`); a name that is not a regular file (`EACCES`), opened without waiting so a
pipe is never read; a sixth script in a chain on Linux (`ELOOP`) and a script's interpreter that is
a script on macOS (`ENOEXEC`). A program its runner cannot read is an error too, though the system
may start it.

## trusttest

`trust/trusttest` is for the tests of a program that judges its paths with `trust`: each helper
says one intent, which is mode bits on Unix and an access list on Windows, and puts back what it
changed when the test ends. A test imports it itself; no other module forwards its
names.

| helper | Unix | Windows |
|---|---|---|
| `Loosen(t, path, who)`: lets its group or others read or write it | adds the one mode bit, as `os.Chmod` | adds an allow entry for Users (S-1-5-32-545) or Everyone (S-1-1-0), the rights to read or to write a file; a null list is left as it is |
| `Deny(t, path)`: keeps this account out | clears the nine permission bits | sets an empty list that takes nothing from the folder above; the owner may still change it |
| `Perm(t, path, want)`: the mode this system keeps for `want`, and, where `want` lets group and others nothing, private to this account | `want` | `0o444` without the owner write bit, `0o666` with it, a folder adding `0o111` |

Each follows a link at `path`. Private is `Held`'s judge of a `Key`, or a `PrivateFolder`, on
the name opened to read: so `Perm` holds a private `want` to let its owner read. `Held` is given
no `Who`, so on Linux a private `want` fails, whatever the mode, on a disk `Path` refuses until
`Who.Mounts` names it, one mounted `nosuid`, or `noexec` without `nosuid`: `Perm` has no `Who` to
name the mount with, and a test that needs a private name keeps it on a disk without those flags,
as a GitHub runner's work folder and `/tmp` are, or on a tmpfs, which neither flag touches. `Deny` skips the
test, saying so, where this account still opens the name, as root does on Unix; `Loosen` and
`Deny` skip it on a Windows volume that keeps no access lists, as FAT. Any other failure fails
the test, and so does a `Who` that is none or a `want` with bits beyond the nine. `Perm` and
`Deny` open the name with no wait for a pipe's other end (`O_NONBLOCK` on Unix), so a pipe at
`path` fails or skips the test, and never hangs it until its timeout.

`GiveGroup(t, path, gid)` gives `path` to the group `gid` until the test ends, as `os.Chown(path,
-1, gid)` does, following a link, so a test plants a name of this account's given to a group, as
the admin group `AdminGroup` answers, with no `os.Chown` of its own. At the test's
end it puts back the group, and the set-user-ID and set-group-ID bits a change of group clears,
as macOS and Linux clear them on either change. It skips the test, saying why, before any
change where this account is not root and the name's own group is neither its effective group nor
one of `os.Getgroups()`, so it could not give the name back, as on macOS with a name below
`/private/tmp`, which takes the group `wheel`; where this account may not give the name to `gid` (`EPERM`: an account but root that is not in the group
or does not own the name), where a Linux user namespace maps no such gid (`EINVAL`), where the
disk keeps another group than `gid`, as one that keeps no groups does, and on Windows, which keeps
no group id on a name. A `gid` of `math.MaxUint32`, which `chown` reads as no change, fails the
test, as does any other failure and a put back refused after that check, as something changed the
name meanwhile, but for a name the test removed.

`SystemFile(t)` answers a file the system owns, which this account neither owns nor may open to
write, and its owner as `OwnerOf` reads it and `ReadOwned` takes it, so a test of a program that
must refuse, or leave alone, another account's file has one with no root. It is
found, never named: the first candidate that is a regular file, named with no link on the way,
owned by an account other than this one, and whose open to write is refused this account. The
candidates are `ntdll.dll` in the folder `GetSystemDirectory` answers on Windows, never one the
environment names (TrustedInstaller owns it, and Administrators may not write it);
`/private/etc/hosts` and `/usr/bin/true` on macOS; `/etc/passwd` and `/bin/sh` on Linux and
another Unix. The owner is the one read: in a Linux user namespace that does not map root, as
go-testkit's `contain` makes, root's file is owned by the overflow account, `uid:65534` by
default, not `uid:0`, and that holds for a test that is uid 0 in the namespace too. Every
candidate is tried first; where none passes, the test is skipped when this account is root or
SYSTEM (`S-1-5-18`), so the run shows the proof not made, and failed otherwise, each naming this
account and every candidate with why. Being in Administrators is not being SYSTEM.

`Bind(t, how)` gives a test on Linux 5.12 or later, with no root, a mount for `trust` to judge:
`Nosuid`, as udisks mounts an account's stick, or `NoexecWithoutNosuid`, `noexec` and `nodev`, as
usbmount mounts one. It binds a folder of the test's temp folder named `trusttest-bind` over the
first of `/mnt` and `/srv` that is a folder real root made, whatever it holds, and that no `Bind`
holds, with the flag set before the mount is placed and each flag the kernel locks on the disk's
own mount kept, by `open_tree`, `mount_setattr` and `move_mount`, and unmounts it when the test
ends. It mounts only in a user namespace other than the first, where `/proc/self/uid_map` is not
the map of every id to itself, so the mount is in a mount namespace that user namespace owns, never
the system's, and hides what the entry holds from that namespace alone; as `unshare --user
--map-current-user --mount --keep-caps` makes, never where root is mapped. It returns `entry` and
`dir`, a folder of this account's on the mount; real root's names show there as the overflow
account, so a test walks with `trust.Who{Overflow: true, Mounts: []string{entry}}`, and `ReadOwned`
reads with that `Who` too. Parallel tests may each call it, in one test binary or in several that
share one namespace, as `go test ./...` in one `unshare` runs them: an entry one holds is no
other's until its test ends. Binds of several processes are judged by `/proc/self/mountinfo`, with
no lock between them and no wait: an entry where a `Bind`'s mount stands, its root a folder named
`trusttest-bind`, is held, and of two `Bind`s that mount over one entry at once, the earliest keeps
it and the later undoes its own mount and tries the next. A `Bind`'s mount left by a process that
ended without its unmount holds its entry while the namespace lasts. With more `Bind`s at once
than entries, the extra ones skip, so a caller that wants none to skip runs them one at a time,
as one that mixes `Nosuid` and `NoexecWithoutNosuid` across processes does too: of two racing over
one entry, the one that keeps it may judge the other's mount, with the other flag and an empty
`dir`, until that one is undone. It skips, saying why, off Linux, in the first user namespace (root's on the host among it), where it
may not mount (go-proc's `testenv.Contain` among them, which takes the capability away, a
kernel before 5.12, and a seccomp profile older than the kernel that refuses `open_tree`,
`mount_setattr` or `move_mount`, the skip naming the call refused, `open_tree` before 5.2 and
`mount_setattr` from 5.2 to 5.11, and the errno, `ENOSYS` or `EPERM`), where no folder qualifies or is free, and where the temp folder's disk is one
whose flags `trust` does not judge, as a tmpfs `/tmp`, or, for `NoexecWithoutNosuid`, one mounted
`nosuid`.

On Windows each put back writes the descriptor read before the change, its control bits too,
by `SetKernelObjectSecurity`, as `SetSecurityInfo` would mark an inheriting list auto-inherited;
it opens the name by `NtCreateFile` with `READ_CONTROL` and `WRITE_DAC` alone, the rights its owner
keeps under any list, since `CreateFile` asks `SYNCHRONIZE` too, which the list `Deny` sets refuses.

## AdminGroup

`AdminGroup()` is the system's admin group, the group whose members may administer it: on macOS
the group `admin` names, gid 80 on a Mac as it ships, read from the system's own directory by
`os/user.LookupGroup` each time it is asked, with cgo or without it. Every other system answers
none, `ok` false, and looks nothing up: Linux's `sudo` and `wheel` are a site's choice, and Windows'
Administrators is a SID, which the walk already trusts. A lookup that fails, or answers a gid that is
not a decimal 32-bit number, is an error with `ok` false, never a guess.

It trusts nothing itself. A program that lets the admin group write the folders it reads, as a
package manager's an administrator installed, says so in its own `Who`, by `Group`:
`func(g uint32) bool { return ok && g == gid }`. `Path` then judges that group as any other a `Group`
lets in: by the mode bits alone, and not in a macOS access list, which refuses every group.

## Guards

| refusal | test |
|---|---|
| a path that is not absolute and clean, before any name is read | `TestWhatTheWalkCannotReadIsAnErrorNeverAPass`, `TestADriveLetterPathIsAbsoluteAndADriveRelativeOneIsNot` |
| a path that is, or lies below, a folder of `Who.Apart`, by the file ID of a name on the way, matching `ErrInside`: the folder itself, below it, through a link or junction, in another case, the folder named through a link; a folder on another volume with the same file index passes | `TestAPathThatIsOrLiesBelowAnApartFolderIsRefusedByItsFileID`, `TestAPathInAnApartFolderOnThisDiskIsRefusedHoweverItIsSpelled` |
| a folder of `Who.Apart` that is not absolute and clean, before any name is read; one that cannot be read, as an error; each read once | `TestAnApartFolderThatIsNotAbsoluteOrCannotBeReadIsAnErrorNeverAPass`, `TestAPathThatIsOrLiesBelowAnApartFolderIsRefusedByItsFileID` |
| a name that is not there, or whose facts cannot be read, as an error | `TestWhatTheWalkCannotReadIsAnErrorNeverAPass`, `TestRule6ANameWhoseListCannotBeReadIsAnErrorNeverAPass` |
| a name of the wrong kind: a folder, a pipe, a device, a file where a folder or socket is asked | `TestEachRuleOfTheModeBitsRefusesItsNameWithItsFix`, `TestEachWindowsRuleRefusesItsNameWithItsFix` |
| a name another account owns; with `RootOnly`, one of this account's | `TestEachRuleOfTheModeBitsRefusesItsNameWithItsFix`, `TestRootAloneRefusesANameOfThisAccountsOnThisDisk`, `TestRule1AFileOfThisAccountsIsRefusedWhenRootAloneIsTrusted` |
| a name its group or others can write, each kind on a real disk, and passing once its fix is run | `TestEachRuleOfTheModeBitsRefusesItsNameWithItsFix`, `TestEachKindOnThisDiskIsRefusedByItsModeAndPassesOnceItsFixIsRun` |
| a name its group can write, unless `Who.Group` accepts the group; always with `RootOnly` | `TestEachRuleOfTheModeBitsRefusesItsNameWithItsFix`, `TestAFolderItsGroupCanWritePassesOnlyWhenGroupLetsItsGroupIn` |
| `AdminGroup`: a lookup that fails, and a gid that is not a decimal 32-bit number (empty, signed, hex, spaced, past 32 bits), as an error with `ok` false and gid 0, never a guess; on macOS, a group the system does not name `admin`; off macOS, any group at all; on a real disk, a folder of the admin group that group may write, refused where `Who` does not let the group in | `TestTheAdminGroupIsTheLookupsGidAsADecimal32BitNumberOrAnError`, `TestAdminGroupOnMacOSIsTheGroupTheSystemNamesAdmin`, `TestAdminGroupIsNoneOffMacOS`, `TestAFolderTheAdminGroupCanWritePassesOnlyWhereWhoLetsTheAdminGroupIn` |
| a folder others can write, but root's sticky folder above the last name and not with `RootOnly` | `TestAFolderOthersCanWritePassesOnlyAsRootsStickyFolderAboveTheLastName`, `TestAWorldWritableFolderPassesOnlyWithTheStickyBitAndOnlyAboveTheLastName` |
| a file, key, program, socket or link right in root's sticky folder, whoever owns it; a folder of root's there | `TestANameRightInRootsStickyFolderIsHeldFurther`, `TestALinkRightInRootsStickyFolderIsRefusedWhoeverOwnsIt` |
| root's own folder right in root's sticky folder, for root alone | `TestRootsOwnFolderRightInRootsStickyFolderPassesForRootAloneAndOnItsDevice` |
| a folder right in root's sticky folder on another device than it, whoever owns it, with no Fix the next check refuses | `TestANameRightInRootsStickyFolderIsHeldFurther`, `TestRootsOwnFolderRightInRootsStickyFolderPassesForRootAloneAndOnItsDevice`, on a Linux tmpfs `TestOnARealDiskAFolderMountedRightInRootsStickyFolderIsRefusedAndOneBesideItPasses` |
| a name on a volume an account not trusted mounted, anywhere on the way: a FUSE mount by uid 1000 under root's `/tmp/user`, walked as root; one whose mounter is not named; this account's for `RootOnly`; a link on it before it is read; `ReadOwned`'s file on another's mount; Linux's mounter read only in the initial user namespace, which a host's process reads itself in; Linux's mounter read from mountinfo, each line split on the one space the kernel writes between fields and on no other, so a FUSE mount whose source, subtype or mount point holds `\r`, `\v`, `\f`, U+0085 or U+00A0 names no other mounter, no virtiofs and no other type, and `ReadOwned` refuses a file on it; and macOS's by `statfs` on a real image | `TestANameOnAVolumeAnAccountNotTrustedMountedIsRefused`, `TestTheMounterOfAFUSEFileSystemIsReadFromMountinfo`, `TestAMountinfoLineIsSplitOnTheKernelsSpaceAlone`, `TestAFUSEMounterIsReadOnlyInTheInitialUserNamespace`, `TestThisProcessReadsItsUserNamespacesAsTheInitialOneOnAHost`, `TestTheMounterOfAVolumeIsReadByStatfs` |
| Linux: a name on a mount whose owners are not judged, with `ErrMount`, matching `ErrNotOwners`: a type outside the list, unless `Who.FileSystems` names it; a disk mounted `nosuid`, and a type the app adds so mounted, unless `Who.Mounts` names its folder by device, a relative or unread folder refused as an entry, ZFS among the disks; the same for a disk, ZFS or a type the app adds mounted `noexec` without `nosuid`, one mounted both refused as `nosuid`, and a tmpfs so mounted passing; a ZFS name carrying `system.nfs4_acl_xdr`, by `Path` and `ReadOwned`, even where `Who.FileSystems` names ZFS, and one whose list read is not `ENODATA` or `EOPNOTSUPP` from the file held unjudged, and natively, through fsOf's ZFS branch, a socket held, not reopened to read it, and a file of the test's disk carrying none; ext4 and xfs on a device of major 0, its mount unread even where the app names 9p, or another type's, and ext4 on a disk's device passing, its mount unread; ext4 and xfs on major 0 whose mount mountinfo calls 9p, passing where the app names 9p's type, and natively that mount's type read there and not on a disk's device; an NFS, ZFS, tmpfs or FUSE name whose mount's type is not read, as where `/proc` is another pid namespace's, read no further, and openat2's `ENOENT` there matching no name-not-there; a mount mountinfo calls 9p under ext4, ZFS, tmpfs, overlayfs, NFS, FUSE or a type the app adds, a tmpfs there passing where the app names 9p's type, unless `nosuid`; btrfs on another type's mount; tmpfs, btrfs, NFS, ZFS, FUSE or a type the app adds on a mount whose type is not read, as before Linux 5.8; and a 0 `fstatfs` answers; tmpfs, btrfs and a type the app adds on their own mount, and 9p's own type named, passing; a FUSE mount root made `nosuid`, or `noexec` without it, unless `Who.Mounts` names its folder, one made with setuid or by this account passing; a tmpfs mounted `nosuid`, FUSE by its own rule, and no type read pass; `ReadOwned`'s file of a type outside the list, and on a disk mounted `nosuid`, or `noexec` without it, each passing when a `Who` given names it; natively, a procfs name refused at `/proc`, and by `ReadOwned` but for two `Who` values naming its type and `/proc`, and the type, `nosuid` and `noexec` read by `fstatfs` on the name held, procfs's `noexec` read as set, and the type mountinfo gives its mount, procfs's and the test folder's agreeing | `TestANameOnAMountWhoseOwnersAreNotJudgedIsRefused`, `TestAMountsTypeAndNosuidAreReadByFstatfsOnTheNameHeld`, `TestAMountsTypeIsReadFromMountinfoByItsNumber`, `TestAZFSNameIsJudgedByWhetherItsHeldFileCarriesAnNFSv4List`, `TestAMountsTypeIsReadOnMajor0AndAMountNotReadIsReadNoFurther` |
| Linux, with no root, in the user and mount namespace `unshare --user --map-root-user --mount` makes: a folder on a disk bound over itself judged as before, then bound `nosuid` refused with `ErrMount`, as udisks, fuse2fs run by the account and fstab's `user` mount a stick; passing where `Who.Mounts` names it, refused where it names `/proc`, another device, or the folder by a name relative to the working folder; and bound again with setuid, as fstab's `user,suid` mounts it, passing, or refused as `noexec` where the disk was; and likewise a folder on a disk not mounted `nosuid` bound `noexec` and `nodev` without `nosuid`, as usbmount mounts a stick, refused with `ErrMount`, passing where `Who.Mounts` names it, refused where it names `/proc`, and bound again with exec passing | `TestOnLinuxADiskBoundNosuidInAUserNamespaceIsRefusedUntilWhoMountsNamesIt`, `TestOnLinuxADiskBoundNoexecWithoutNosuidInAUserNamespaceIsRefusedUntilWhoMountsNamesIt` |
| a folder of `Who.Mounts` that is not a mount's root at a name only root can move, with `ErrMountEntry`, matching `ErrMount`, the entry as `Unsafe.Path`, before the path is read, by `Path`, `Held` and `ReadOwned` at every call: not absolute, not clean, below a name an account controls (refused as a `RootOnly` walk of its parent refuses it), a link, a folder that is no mount's root, and one whose mount cannot be told, as on Linux before 5.8; a bad entry after a good one; an entry that names nothing, whose folder above names nothing, or gone before its mount is read, `ErrMountEntry`, never `fs.ErrNotExist`, and one that cannot be read the error that reads it, never a pass nor `ErrMountEntry`; nested entries on a disk mounted `nosuid`, both named, in either order, passing, and the deeper alone, on its ancestor's device, refused, as no entry vouches for itself or for one above it; an account's own mount root under root's names, as `/run/user/<uid>`, passing, its owner not judged; on Linux a mount's root told by `STATX_ATTR_MOUNT_ROOT` and not device, a link to one and a folder below one not, and, in `unshare --user --map-root-user --mount`, a folder bound over itself, on its folder's device, one, and its unbound twin not; a host that tells no mount's root, as Windows', judging no entry; on this disk `/dev` passing and the test's own folder and a relative name refused | `TestEachFolderOfWhoMountsIsJudgedBeforeThePathAndRefusedWithErrMountEntry`, `TestPathHeldAndReadOwnedEachJudgeWhoMountsOnThisDisk` (Unix), `TestOnLinuxAMountsRootIsToldByItsMount` (Linux) |
| on Linux, a name on a FUSE mount where mountinfo is read from a file not held to this process's procfs's own (one not on procfs, or another procfs file bound, mounted or linked there), past 4 MiB, or before Linux 5.6; a `uid_map` or overflow account read from a file not on procfs, or past 4 MiB; a namespace file not on nsfs, or of another type; the pidfd namespace calls answering other namespaces than `/proc/self/ns`, or none on Linux 6.11 or later; a failed pidfd call closing a descriptor it did not open | `TestAMountinfoNotOnProcfsNamesNoMounter`, `TestWhatIsReadUnderProcIsHeldToProcfs`, `TestAFileOfProcfsMountedOrLinkedOverTheOneAskedForIsNotRead`, `TestTheNamespacesAreHeldToNsfsAndTheirTypeAndThePidfdCallsAgreeWithProc`, `TestAFailedPidfdCallClosesNoDescriptorItDidNotOpen` |
| a program its owner cannot execute; a key or a `PrivateFolder` its group or others can read or write, the error naming the mode | `TestEachRuleOfTheModeBitsRefusesItsNameWithItsFix`, `TestEachKindOnThisDiskIsRefusedByItsModeAndPassesOnceItsFixIsRun` |
| on Linux, a group-writable name whose group bits are an access list's mask | `TestEachRuleOfTheModeBitsRefusesItsNameWithItsFix`, `TestAFileWhoseAccessListMaskGrantsWriteIsRefusedOnLinux` |
| on Linux, an NFSv4 list allowing another account, a named one or `EVERYONE@`, a right that changes the name, or read or EXECUTE when private; any named principal; an entry of a type not read, or a list that does not parse; on its descriptor for `ReadOwned`; a write `GROUP@` or `EVERYONE@` gives that the mode does not show, or past WRITE_DATA, APPEND_DATA and DELETE_CHILD where it does; a private kind on a mount that cannot show its list; a list read where `/proc/self/fd` is mounted over, or names no file, which is no errno, or of a name not a file or a folder | `TestEachRuleOfAnNFSv4ListRefusesItsNameWithItsFix`, `TestAnNFSv4ListIsReadAsItsEntries`, `TestReadOwnedJudgesAnNFSv4ListOnItsDescriptor`, `TestAnNFSv4ListDerivedFromTheModeIsJudgedAsTheMode`, `TestAListIsReadOnTheFileHeldNotOnAPathMountedOver` |
| `Held`: a file or folder held open, judged on its descriptor by each rule `Path` keeps for the last name: the wrong kind, another account's, this account's with `RootOnly`, one its group or others may write, a group-writable one whose bits are a Linux access list's mask (read by `fgetxattr`, and taken as one where it cannot be told), a key or `PrivateFolder` others may read, a program its owner cannot run, one on a volume another mounted or that ignores owners, a Windows list that lets Everyone write it or is null, a folder of `Who.Apart` itself by its file ID (a file below one passes, and the caller's `Path` refuses it), `Who.Overflow` where `Path` refuses it; each refusal with no `Fix`, and the name's from `Path` making it pass; the file held judged, with no `Fix`, and not the one renamed over its name, nor the one a link at its last name leads to, nor none where the name was moved away or is relative to another folder, or on macOS each of these refused with `ErrReplaced` after eight tries, never `fs.ErrNotExist`; a list mask that cannot be read, a closed file, no file and a host that judges nothing, as an error | `TestHeldJudgesTheFileItHoldsByEachRulePathKeepsForTheLastName`, `TestWhatHeldCannotReadIsAnErrorNeverAPass`, `TestHeldRefusesAnApartFolderAndANameReplacedAtEachTry`, `TestHeldOnThisDiskJudgesTheFolderAndFileItHolds`, `TestHeldJudgesTheFileHeldNotTheOneNowAtItsName`, `TestAHeldFileWhoseAccessListMaskGrantsWriteIsRefused`, `TestHeldOnWindowsJudgesTheListOfTheFolderAndFileItHolds` |
| `Fix.Commands`: no steps, `ok` false, for the zero `Fix`, no path, nothing to change, a path not absolute for the fix's system (relative, drive-relative, one starting `\`) or holding a NUL, the sticky bit or any bit beyond the nine, an owner not in `Account`'s form (a name, a gid, a uid with a leading zero, no number, a sign or more than ten digits, a SID with words after it or in lower case), a macOS or NFSv4 revoke, a SID with words after it, and Windows's and Unix's changes mixed; each word of a step one argv word, a path with a space, quote, dollar, percent, backquote, backslash or leading dash never split, read as a switch or expanded; no string of trust's source quoting or joining a step, and none naming `sudo`, nor a program outside `commands.go`; natively, each step run as its argv makes the name pass and leaves a name it did not name refused, on Unix by `chmod` and on Windows by `icacls` on a direct grant, an inherited one and a null list, and `icacls /setowner` from an elevated prompt, and where not elevated each owner step marked `Admin` and not run, the name held refused for its owner alone; a fix for a link, a junction and where this account may make one a symbolic link, its own list null or letting Everyone write, and its owner, made on the link by `/L` on each `icacls` step and leaving the folder it names its owner and list as they were; the owner step last, after every step its present owner makes; any account's owner step, a uid's as `chown -h <N>` and a SID's as `icacls /setowner *<SID> /L`, marked `Admin`, and natively the step for this account run as its argv on a name it owns, a link on Unix among them, leaving it this account's | `TestAFixIsTheStepsThatMakeItAsArgv`, `TestNothingInTrustQuotesOrJoinsACommandLine`, `TestEachStepOfAFixRunAsItsArgvMakesTheNamePass`, `TestEachStepOfAFixRunAsItsArgvOnWindowsMakesTheNamePass`, `TestEachStepOfAFixForALinkOnWindowsChangesTheLinkAndNotWhatItNames`, `TestTheStepThatGivesANameToThisAccountRunsAsItsArgv`, `TestTheStepThatGivesANameToThisAccountRunsOnWindows` |
| `Interpreters`: a `#!` line read past where the system ends it (a `#` on macOS, a NUL, a newline after a NUL on Linux), or a line the system cuts read whole (Linux's 256 bytes, its interpreter ending at the 256th byte passing; macOS's 512); a line naming no interpreter; an interpreter not absolute, even one the folder a run is in holds; a name not a regular file, a folder or a pipe, the pipe never read, and refused by its type before it is opened, so no device's driver runs its open; a sixth script in a chain on Linux and a script's interpreter that is a script on macOS, each refusal matching the system's errno; a `#!` file on FreeBSD and NetBSD, matching `errors.ErrUnsupported`; none on Windows; natively on Linux and macOS, each answer held to the system starting the script by `execve` | `TestEachSystemReadsAShebangLineByItsOwnRule`, `TestAChainOfScriptsIsWalkedToTheProgramTheSystemStarts`, `TestInterpretersAgreesWithTheSystemStartingEachScript`, `TestInterpretersOpensNoNameThatIsNotARegularFile`, `TestInterpretersOnWindowsIsNoneForAScript` |
| a link of another account's, judged before it is read | `TestALinkIsJudgedBeforeItIsReadAndRefusedWhereNoneIsFollowed` |
| a link at the end or in the middle with `NoLinks`, and any link with `RootOnly` | `TestALinkIsJudgedBeforeItIsReadAndRefusedWhereNoneIsFollowed`, `TestALinkOnThisDiskIsFollowedOrRefusedAsAsked`, `TestRule5AJunctionIsALinkFollowedOrRefusedAsAsked` |
| Windows rule 5: a link whose own list is null, lets another account change it, gives only the right to add a folder, or holds an entry neither allow nor deny, before it is read | `TestAWindowsLinkAnotherAccountMayChangeIsRefusedBeforeItIsFollowed`, `TestRule5AJunctionAnotherAccountMayChangeIsRefusedBeforeItIsFollowed` |
| an owner `""`, as this account's or root's; a host that cannot read this account's SID, for the walk, `ReadOwned` and `Account` | `TestAnOwnerOrAnAccountOfNoNameIsNeverTrusted`, `TestAccountIsTheOwnerTheWalkTrustsAsThisAccount` |
| more than forty links, or a loop | `TestALinkIsFollowedFromTheTopAndAPathWithNoneIsAnswered` |
| Windows rule 2: an allow entry giving another account a right that changes the name, each such right alone; a key or a `PrivateFolder` another may read | `TestEachWindowsRuleRefusesItsNameWithItsFix`, `TestEachWriteRightAloneIsRefused`, `TestRule2AFileEveryoneMayWriteIsRefusedAndPassesWithTheGrantRemoved` |
| Windows: the right to add a folder, on the last name or with any other right | `TestAddingAFolderAlonePassesOnAFolderAboveTheLastName`, `TestThePartCFoldersGiveTheirRecordedVerdictsOnTheirFacts`, `TestThePartCFoldersGiveTheirRecordedVerdictsOnThisSystem` |
| Windows: a folder added on the way by the account that may add one, by rule 1 | `TestAFolderAddedOnTheWayByTheAccountThatMayAddOneIsRefusedByTheOwnerRule` |
| Windows: any account but an AppContainer's beside its entry; a SID of another authority or first sub-authority, or with none after it | `TestAnAppContainerSIDIsKnownByItsAuthorityAndFirstSubAuthority`, `TestAFolderAnotherAccountMayWriteIsRefusedBesideAnAppContainer`, `TestAnAppContainerMayWriteANameAndNoOtherAccountMay`, `TestReadOwnedJudgesAWindowsListOnItsDescriptor` |
| Windows, the skip: a folder, file or key an AppContainer may write, and `%APPDATA%`, pass; so does `ReadOwned`'s file | `TestAFolderAnAppContainerMayWriteIsTrusted`, `TestAnAppContainerMayWriteANameAndNoOtherAccountMay`, `TestThePartCFoldersGiveTheirRecordedVerdictsOnThisSystem`, `TestReadOwnedJudgesAWindowsListOnItsDescriptor`, `TestReadOwnedOnWindowsReadsOnlyWhatItsOwnerAloneMayChange` |
| Windows: an entry that runs past its list or cuts its SID short, as an error; a SID not whole in its bytes, of another revision or over 15 sub-authorities | `TestAnAccessEntryIsReadOnlyWithinItsOwnSizeAndItsList`, `TestASIDNotWholeInItsBytesIsNotRead`, `TestASIDIsReadFromItsBytesAsWindowsWritesIt`, `TestASIDsAuthorityIsHighByteFirstAndItsSubAuthoritiesLowByteFirst`, `TestASIDsAuthorityOf2To32OrMoreIsWrittenInHex` |
| Windows rule 3: an entry that is not inherit-only, where the same entry inherit-only passes; for a `PrivateFolder`, an inherit-only entry giving another read or write, CREATOR GROUP's too, and on a real folder the file made in it, while CREATOR OWNER's passes | `TestRule3AnInheritOnlyEntryGivesTheFolderItselfNothing`, `TestEachWindowsRuleRefusesItsNameWithItsFix`, `TestAPrivateFolderIsRefusedAnEntryItGivesTheFilesMadeInIt` |
| Windows rule 4: a null list | `TestEachWindowsRuleRefusesItsNameWithItsFix`, `TestRule4ANullAccessListIsRefusedAndPassesOnceItIsReset` |
| Windows rule 6: an entry neither allow nor deny | `TestEachWindowsRuleRefusesItsNameWithItsFix` |
| ReadOwned: a link, a folder, a socket or a pipe at the name, at once | `TestReadOwnedRefusesEachFileItMustNotRead`, `TestRule5AJunctionIsALinkFollowedOrRefusedAsAsked`, `TestReadOwnedOnWindowsReadsOnlyWhatItsOwnerAloneMayChange` |
| ReadOwned: another owner; a file another can write, or read when private | `TestReadOwnedRefusesEachFileItMustNotRead`, `TestReadOwnedJudgesAWindowsListOnItsDescriptor`, `TestReadOwnedOnWindowsReadsOnlyWhatItsOwnerAloneMayChange` |
| ReadOwned: no limit, or an owner in neither form | `TestReadOwnedRefusesEachFileItMustNotRead` |
| ReadOwned: the smallest file over the limit, and one that grows past it while it is read | `TestReadOwnedRefusesTheSmallestFileOverItsLimit`, `TestReadOwnedRefusesAFileThatGrowsPastItsLimitWhileItIsRead`, `TestReadOwnedOnWindowsReadsOnlyWhatItsOwnerAloneMayChange` |
| ReadOwned: a file planted at the name after the open, either way round; on macOS, the name opened again and the planted file judged | `TestReadOwnedReadsOnlyTheFileItCheckedWhenOneIsPlantedAfterTheOpen` |
| a name replaced by another file at each of eight tries, for the walk and `ReadOwned`, with `ErrReplaced` and no refusal of its facts; on macOS, a name `Held` holds that leads to another file or none, with `ErrReplaced`, never `fs.ErrNotExist`; on macOS, a file its owner keeps replacing by rename passes each of 2,000 reads by each | `TestANameReplacedWhileTheWalkReadsItIsReadAgainUpToItsTries`, `TestAFileReplacedAfterReadOwnedOpensItIsOpenedAgainUpToItsTries`, `TestAMacAnswerForAnotherFileIsReadAgainNotJudged`, `TestAFileItsOwnerKeepsReplacingByRenamePassesEachRead`, `TestHeldJudgesTheFileHeldNotTheOneNowAtItsName` |
| macOS: an allow entry, not inherit-only, giving an account or group the walk does not trust a right that changes the name, each such right alone; a key or a `PrivateFolder` another may read; a `PrivateFolder` another may search, by SEARCH or GENERIC_EXECUTE; a `PrivateFolder` whose inherit-only entry lets another read, or search, what is made in it; an account by its own GUID; any group, one `Who.Group` accepts included; this account's entry with `RootOnly` | `TestEachRuleOfAMacListRefusesItsNameWithItsFix`, `TestEachRightThatChangesANameIsRefusedInAMacList`, `TestAMacListOnThisDiskIsRefusedAndPassesOnceItsFixIsRun` |
| macOS: an entry neither allow nor deny; a list that cannot be read, matching `errors.ErrUnsupported` | `TestEachRuleOfAMacListRefusesItsNameWithItsFix`, `TestAMacAnswerThatCannotBeReadIsRefused` |
| macOS: a name on a volume mounted to ignore ownership, from the folder it is mounted on down, a link on it before it is read, and `ReadOwned`'s file on it by its descriptor, with `ErrOwnersIgnored`, matching `ErrNotOwners`, whoever it shows as owner; a 0600 key on a disk image attached with owners off, for `Path` and `ReadOwned` | `TestANameOnAVolumeThatIgnoresOwnershipIsRefused`, `TestAKeyOnADiskImageMountedWithOwnersOffIsRefused` |
| the module's lint: in `maclist_darwin.go`, a raw system call besides its one, the one made without `FSOPT_NOFOLLOW`, of another number or through `syscall`, a raw call of `syscall`, or `syscall` or `unix` imported by another name (lint excepts that call's line alone) | `TestTheMacListFileMakesOnlyItsOneRawCall` |
| macOS: an answer cut short, holding an attribute not asked or lacking the device or inode, a list outside it, of another magic, or of more or fewer entries than its bytes | `TestAMacAnswerThatCannotBeReadIsRefused`, `TestTheMacAnswerIsReadAsTheKernelLaysItOut`, `TestEachGUIDOfAMacListIsNamedAsItsAccountOrGroup` |
| macOS: a link whose own list lets another change it, before it is read | `TestALinkWhoseMacListLetsAnotherChangeItIsRefusedBeforeItIsRead`, `TestALinkWhoseMacListLetsAnotherChangeItIsRefusedOnThisDisk` |
| macOS, ReadOwned: a file whose list lets another write it, or read it when private | `TestReadOwnedJudgesAMacListOnItsDescriptor`, `TestReadOwnedRefusesAFileWhoseMacListLetsAnotherInOnThisDisk` |
| FreeBSD, NetBSD and every other Unix: every name, once its mode passes, matching `errors.ErrUnsupported` | `TestEachRuleOfAMacListRefusesItsNameWithItsFix`, `TestReadOwnedJudgesAMacListOnItsDescriptor` |
| `Who.Overflow` on Linux outside a test binary, before any name is read | `TestAProgramThatIsNotATestIsRefusedTheOverflowAccount`, `TestAUserNamespaceTrustsTheOverflowAccountOnlyWhereItMapsThisAccountAndNotRoot` |
| `Who.Overflow` off Linux, test binary or not, matching `errors.ErrUnsupported` too, for `Path`, `Held` and `ReadOwned` (set in any of its `mounts`) | `TestOverflowIsTakenOnlyInATestInANamespaceThatMapsNoRoot`, `TestAProgramThatIsNotATestIsRefusedTheOverflowAccount`, `TestWhatHeldCannotReadIsAnErrorNeverAPass` |
| `Who.Overflow` where `/proc/self/uid_map` cannot be read, maps root (a line from id 0), maps the overflow id (a line whose range holds it) or maps none | `TestNoRootTakesAUIDMapOfLinesThatMapNoRoot`, `TestOverflowIsTakenOnlyInATestInANamespaceThatMapsNoRoot`, `TestAUserNamespaceTrustsTheOverflowAccountOnlyWhereItMapsThisAccountAndNotRoot` |
| with `Who.Overflow`, the overflow account trusted as root and nothing more: another account's name, a name its group can write, a file right in its sticky folder | `TestAUserNamespaceTrustsTheOverflowAccountOnlyWhereItMapsThisAccountAndNotRoot` |
| on Linux, a program that runs as the overflow account where `/proc/self/uid_map` is not the whole range, or cannot be read, or where the overflow account cannot be read, for `Path`, `ReadOwned`, `Held` and `Account`; `nobody` served where the map is the whole range | `TestOverflowOfRefusesTheOverflowAccountOnlyWhereTheUIDMapIsNotTheWholeRange`, `TestAUserNamespaceTrustsTheOverflowAccountOnlyWhereItMapsThisAccountAndNotRoot` |
| each refusal names the name it is about in `Unsafe.Path`, as the walk reached it: the last name, a folder or link on the way, the name on the way that is a `Who.Apart` folder (never that folder's spelling), and the file `ReadOwned` was asked for; `""` only for a setting of `Who` | `TestEachRefusalNamesTheNameItJudgedInUnsafePath`, `TestARefusalOfAnApartFolderNamesTheNameOnTheWayNotTheSpellingOfWhoApart`, `TestOnThisDiskUnsafePathIsTheFileOrTheFolderOnTheWayThatWasRefused`, and every refusal `wantRefused` checks |
| `trusttest`: a test failed, never passed, by a `Who` that is none, a `Perm` want with bits beyond the nine or, private, without the owner read bit, a mode other than the one this system keeps, a name `Held` does not hold private (by its mode, and on macOS its access list, and on Linux, in a user namespace, a private file on a disk bound `nosuid`, or `noexec` and `nodev` without `nosuid`, saying the mount), and a change that cannot be made or put back; skipped, saying so, only where `Deny` leaves this account able to open the name (root on Unix) or a Windows volume keeps no access lists, and failed, the change put back, where `Deny`'s open fails for another reason than the denial, as a name gone, or on Linux no descriptor left; each change put back when the test ends, on Windows to the very list and control flags it found, its owner and group as they were, and a put back refused fails the test but for a name the test removed | `TestAChangeThatCannotBePutBackFailsTheTest`, `TestLoosenLetsWhoInSoTrustRefusesTheNameUntilTheTestEnds`, `TestDenyKeepsThisAccountOutUntilTheTestEnds`, `TestDenyFailsWhenItsOpenFailsForAReasonOtherThanTheDenial` (Linux), `TestDenyFailsAnOpenThatFailsForAReasonOtherThanTheDenial`, `TestPermFailsAModeOtherThanTheOneKeptAndANameNotPrivate`, `TestPermFailsANameAMacListLetsOthersReadThoughItsModeIsPrivate`, `TestEachChangeOnWindowsPutsBackTheAccessListItFound`, `TestPermFailsAPrivateNameOnADiskTrustRefusesUntilWhoMountsNamesIt` (Linux) |
| `trusttest.GiveGroup`: the test skipped, saying why, the group unchanged and no cleanup asked for, before any `chown` where the name's own group is not one of this account's and it is not root (a name below `/private/tmp` on macOS, group 0), where `chown` is refused with `EPERM` (a real group this account is not in, as an account but root) or `EINVAL`, where the disk keeps another group than `gid`, and on Windows; failed, `chown` never called, by a `gid` of `math.MaxUint32`, and failed by any other `chown` error (`ENOENT`); a file, a file set-user-ID and set-group-ID, a folder and a link to a file given to a second group of this account's, and at the test's end given back their group and mode, the special bits a change of group clears among them; a put back refused fails the test, but for a name the test removed | `TestGiveGroupGivesTheNameToTheGroupUntilTheTestEnds`, `TestGiveGroupSkipsWhereTheGroupCannotBeGivenAndFailsOtherwise`, `TestGiveGroupFailsAPutBackRefusedButNotOfANameRemoved` (Unix), `TestGiveGroupSkipsOnWindows` (Windows) |
| `trusttest.SystemFile`: a candidate refused, with why, where a link is on the way to it (the name a link, or a folder above it), it cannot be opened, it is not a regular file, this account owns it, or this account's open of it to write is not refused, opened or failing for another reason; the test failed, naming this account and each candidate with why, where none passes, and skipped, saying nothing is proved, only where this account is root or SYSTEM: Administrators and any other account fail; a candidate that passes taken whoever runs the test, root and SYSTEM included; in `unshare --user` with `--map-current-user` and with `--map-root-user`, `/etc/passwd` answered owned by the overflow account, never `uid:0`; on Windows, `ntdll.dll` of `GetSystemDirectory`'s folder with `SystemRoot` and `windir` set elsewhere | `TestSystemFileTakesTheFirstCandidateThatPassesWhoeverRunsTheTest`, `TestSystemFileFailsNamingEachCandidateAndWhyItDidNotPass`, `TestSystemFileFailsACandidateWithALinkOnTheWay` (Unix), `TestSystemFileSkipsOnlyForTheSystemsOwnAccountWhereNoCandidatePasses`, `TestSystemFileAnswersAFileOfAnotherAccountsThisAccountMayNotWrite`, `TestSystemFileInAUserNamespaceAnswersTheOverflowAccountAsTheOwner` (Linux), `TestSystemFileOnWindowsTakesItsFolderFromTheSystemNotTheEnvironment` (Windows) |
| `trusttest.Bind`: a test failed by a `Bound` that is none; skipped, saying why, off Linux, in a namespace where it may not mount (this process's own, the first user namespace by its `uid_map`, one that maps root, as `unshare --map-root-user` makes, and `unshare` without `--keep-caps`, the mount refused, and a kernel before 5.12, the skip naming the call refused, `open_tree` before 5.2 and `mount_setattr` from 5.2 to 5.11, which comes before the `statx` that gives no mount id before 5.8, and the errno, `ENOSYS` or `EPERM`, as a seccomp profile's refusal does), where no folder qualifies as the entry (each one's reason given, trust's refusal of a folder an account made and a name that is none), over an entry another `Bind` of this process holds, over one mountinfo shows a `Bind`'s mount at, another process's in the namespace or one whose process ended (a second test binary's `Bind` holding `/mnt`; mountinfo's lines split on the kernel's one space and no other, so a temp folder named with `\r` or U+00A0 shifts no root or mount point, and a root with an escaped space read; a root ending `//deleted` held, the root's last element matched exactly, so `x-trusttest-bind`, `trusttest-bind-2` and `trusttest-bind/sub` are not held, a mount point with a space, a tab, a newline and a backslash read through its octal escapes, and a marked mount below the entry or at a name the entry is a prefix of not held), and over one whose mount, made at once with another process's, stood on that one's and was undone (two processes' `Binds` over one entry past a barrier, exactly one holding it, and in one process a `Bind` blind to the first's checks, the first's mount at the entry unchanged and passing `trust`, and unmounted at its end), where the temp folder is on a tmpfs, whose flags trust does not judge, and, for `NoexecWithoutNosuid`, on a disk mounted `nosuid`; in `unshare --user --map-current-user --mount --keep-caps`, each `Bound`'s flags shown by `fstatfs` on `/mnt` or `/srv`, the entry passing as a folder of `Who.Mounts`, a key in its folder refused with `ErrMount` without `Mounts` and passing with it, by `Path` and by `ReadOwned` (its `Who.Overflow` in the second of its `mounts`), all with `Who.Overflow`, a `nodev` the kernel locks from a namespace above kept, and the entry on another mount, unmounted, at the test's end; a `Bind` whose mount went off with the one beneath it, at that one's end, going on to the next entry, no unmount error at either end, neither entry on its `Bind`'s mount after (a mount not found read as held, an unmount through the handle off already counted an error, and one unmount through the handle, which reaches the topmost mount on it, each turn it red), and an unmount that stops, with an error, after one unmount for each mount mountinfo shows and one more where the mount stands still; its runs in a namespace make their temp folders on a disk, `TMPDIR` a tmpfs or not. A `statx` of its copy that fails or gives no mount id fails the test. Its own check that the mount it made is refused without `Mounts` and passes with it fails the test, and no row reaches it: `judgeBound` holds the same of every mount it gives | `TestBindFailsABoundItDoesNotKnow`, `TestBindSkipsOffLinux` (not Linux), `TestBindMountsEachBoundOverAnEntryInTheTestsOwnNamespace`, `TestABindsMountIsReadFromMountinfoSplitOnTheKernelsSpaceAlone`, `TestASkipOfARefusedMountNamesItsErrno`, `TestABindsMarkIsItsRootsLastElementAtTheEntryItself`, `TestBindsUnmountStopsOnAMountThatStandsStill`, `TestBindFailsAStatxThatGivesNoMountID` (Linux) |
| `MkdirPrivate`: a path that is not absolute and clean, and `RootOnly`, before any name is read, with nothing made | `TestMkdirPrivateRefusesRootOnlyAndAPathThatIsNotAbsoluteAndClean` |
| `MkdirPrivate`: a folder that was there and is not private, refused with `Path`'s refusal and its `Fix`, and left as it was | `TestMkdirPrivateRefusesAFolderThatWasThereAndIsNotPrivateAndLeavesItAsItWas` |
| `MkdirPrivate`: a folder above that others can write, a link with `NoLinks`, a link to a name not there, and a link to the name it would make, each with nothing made below or where it points; a link this account owns, followed when `Who` allows | `TestMkdirPrivateMakesNothingBelowAFolderAboveItRefuses` |
| `MkdirPrivate`: a walk of the folders above cut short by anything but a name not there, answered as it refused with no make tried, on Linux and macOS | `TestAWalkCutShortByAnythingButANameNotThereIsAnsweredAsItRefused` |
| `MkdirPrivate`: a link at the nearest part retargeted after the walk, refused with the walk's refusal and nothing made where it led before, on Linux and macOS | `TestALinkAtTheNearestPartRetargetedAfterTheWalkMakesNothingWhereItLedBefore` |
| `MkdirPrivate`: each folder made private and the last answered as a root, and the private folder that was there answered again | `TestMkdirPrivateMakesEachMissingFolderPrivateAndAnswersTheLastAsARoot` |
| `MkdirPrivate`: another make cutting a try short, judged again up to 8 tries and then refused with `ErrReplaced`, with `dir` not made | `TestAFolderAnotherMakeMadeWhileItWasLookedForIsJudgedAgainForEightTriesAtMost` |
| `MkdirPrivate`: a folder gone before the make, or filled by another make (`confine.ErrNotEmpty`), tried again; `confine.ErrOutside`, `confine.ErrChanged` and a denial answered at once | `TestAMakeAnotherMakeCutShortIsTriedAgainAndAnyOtherRefusalIsAnswered` |
| `MkdirPrivate`: makers of one folder at once, each answered that folder | `TestMakersOfOneFolderAtOnceEachAnswerIt` |
| `MkdirPrivate`: another folder renamed in at `dir` after the make, refused with `ErrReplaced` and the made one not answered, on Linux and macOS | `TestAFolderSwappedInAtDirAfterTheMakeIsRefusedAndTheMadeOneNotAnswered` |
| `MkdirPrivate`: `dir` opened to others after its path was judged, refused by `Held` on the folder answered, with no `Fix` | `TestAFolderOpenedToOthersAfterItsJudgeIsRefusedOnItsHandle` |
| `MkdirPrivate`: `dir` not there to the judge after the make, made again, on Linux and macOS | `TestADirTakenAwayAfterTheMakeIsMadeAgain` |

The rules are written once, as judges of the facts a host hands them, so `rules_test.go`,
`acl_test.go` and `maclist_test.go` hold every rule, Windows' and macOS's included, on every
system. `trust_unix_test.go`, `aclmask_linux_test.go`, `maclist_darwin_test.go` and
`trust_windows_test.go` read the same facts from real names: the macOS tests set each list with
`setattrlist`, where a person runs `chmod +a`, and the
Windows tests set each list with `SetNamedSecurityInfo`, or a junction's own with `SetSecurityInfo` on
its handle, and make each junction with
`FSCTL_SET_REPARSE_POINT`, where a person runs `icacls` and `mklink /J`, since the module's lint
keeps `os/exec` out of this package. They run on an ordinary account, never as root or an
administrator.

## What it imports

The standard library, `os/user` on macOS alone for `AdminGroup`, `confine`, whose `MkdirPrivate` makes the
folders `trust.MkdirPrivate` judges, and `golang.org/x/sys` to read
a Linux access list's presence
(`unix.Lgetxattr`), a macOS access list and a Windows owner and access list
(`windows.GetSecurityInfo`). The layer rows of the root's [.golangci.yml](../.golangci.yml) say
so, and `task lint` holds them. The macOS list is read by `unix.Syscall6(unix.SYS_GETATTRLIST, ...)`
in `maclist_darwin.go`, the module's one raw system call: the lint rule refusing a
raw call excepts that one file by name, and the call and its exception go when `golang.org/x/sys`
gains `Getattrlist`.

## The alternatives looked at

Read on 15 Nov 2026.

| looked at | version | what it does | why it falls short |
|---|---|---|---|
| `os.Root`, `os.OpenInRoot` | Go 1.27 | opens a name inside a folder, never past it by `..` or a link | confines a path to a folder; it judges no owner, no mode and no folder above the root. |
| `os.Lstat`, `syscall.Stat_t` | Go 1.27 | a name's type, mode, owner and group | the read the walk makes on Unix, not the walk. |
| `os/user.LookupGroup` | Go 1.27 | a group's gid by its name, on macOS through the system's library with cgo off | the read `AdminGroup` makes on macOS, not which group administers which system. |
| `golang.org/x/sys/windows` | v0.50.0 | `CreateFile`, `GetSecurityInfo`, `GetAce`, `GetFileInformationByHandleEx` | the reads the walk makes on Windows, not the walk. |
| `os.Root.MkdirAll` | Go 1.27 | makes each missing folder of a name inside a root | judges no owner and no folder above the root, and on Windows sets no owner-only list. |
| pathrs-lite `MkdirAll` | v0.7.0 | makes each missing folder of a path with no link escaping its root | path-based, Linux alone, and judges no owner. |
| git's `safe.directory`, OpenSSH's `safe_path` | C, inside each program | every folder from the top down is root's or the user's and writable by no one else | not libraries; each is a function inside its own program. The walk is the same rule, written once. |

Verdict: written here, against the standard library and `golang.org/x/sys`.
