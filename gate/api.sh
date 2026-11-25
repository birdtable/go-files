# The API gate, the same shape as the API gates of go-proc, go-socket, go-run and go-console.
# task api sources it, so go-task's own
# interpreter runs it on every system:
#
#   . ./gate/api.sh
#
# apidiff (tools/apidiff.mod) writes the module's export data once per system, darwin, linux and
# windows, all from this host with GOOS set and cgo off. Three things are then held, and each that
# fails says so; the gate is red if any fails:
#
#   1. The systems alike. apidiff's full report from darwin to linux and from darwin to windows,
#      each line led by the system and a tab, equals api/systems.txt less its # lines. The full
#      report, not -incompatible: a name added on one system is "compatible".
#   2. The breaks since the last tag. The base is the newest v* tag that is an ancestor of the head
#      and does not point at it: `git describe --tags --abbrev=0 --match 'v*'`, less each v* tag on
#      the head. Its tree is written out of this repository by git checkout-index, through an index
#      of its own. apidiff -incompatible from the base's export data to the head's, per system, the
#      three joined and sorted with LC_ALL=C sort -u, equals api/breaks/<base>.txt byte for byte, or
#      nothing where that file is not there. So a break with no line is red, and so is a line for a
#      break the tree does not make. The gate runs in the release tier alone, so the run at a new tag
#      is the one that must see its breaks: there the base is the tag before, and the tag's breaks are
#      held to api/breaks/<previous tag>.txt, the file every head since that tag was held to.
#   3. The marks. A name api/marks.txt does not list is experimental. Each line it holds is
#      <package><tab><name><tab><mark>, the mark a word that is not experimental, and go doc must find
#      the name in the package.
#
# go-task runs this with errexit, so a command that fails outside an if or a || ends it.
# apidiff exits 0 on a break, so its output decides, and it exits 1 on a tree that does not build:
# every apidiff that fails is red by itself, never an empty report. A clone with no tag passes 2 and
# says so; a shallow one is red, as its last tag may be one it never fetched.
#
# On Windows go-task's interpreter cannot append an external program's output to a file (>>
# answers "Bad file descriptor"), and its own tar takes no -C. So each program writes a file of its own with >, the files are joined
# after, and no tar is used.
unset IFS
api_bin=$(CGO_ENABLED=0 go tool -modfile=tools/apidiff.mod -n apidiff) || exit 1
api_d="${TMPDIR:-${TEMP:-/tmp}}/go-files-api-$$"
mkdir "$api_d" "$api_d/old" || exit 1
trap 'rm -rf "$api_d"' EXIT
: >"$api_d/none"
api_red=0

for goos in darwin linux windows; do
	GOOS=$goos CGO_ENABLED=0 "$api_bin" -m -w "$api_d/$goos.exp" . 2>"$api_d/err" || {
		cat "$api_d/err"
		echo "api: apidiff cannot write the module's export data for $goos, so nothing is held"
		exit 1
	}
done

# 1. The systems alike.
for goos in linux windows; do
	"$api_bin" -m "$api_d/darwin.exp" "$api_d/$goos.exp" 2>/dev/null >"$api_d/one" || {
		echo "api: apidiff cannot compare darwin with $goos"
		exit 1
	}
	awk -v g="$goos" '{ print g "\t" $0 }' "$api_d/one" >"$api_d/systems-$goos" || exit 1
done
cat "$api_d/systems-linux" "$api_d/systems-windows" >"$api_d/systems" || exit 1
awk '!/^#/' api/systems.txt >"$api_d/systems.want" || exit 1
if git diff --no-index --exit-code "$api_d/systems.want" "$api_d/systems"; then
	echo "api: the three systems' surfaces differ only as api/systems.txt says"
else
	echo "api: the systems' surfaces are not what api/systems.txt says (- the file, + apidiff from darwin to each system)"
	api_red=1
fi

# 2. The breaks since the last tag, the tags on the head left out.
api_here=$(git tag --points-at HEAD --list 'v*' | awk '{ print "--exclude=" $0 }') || exit 1
if [ "$(git rev-parse --is-shallow-repository)" != false ]; then
	echo "api: this clone is shallow, so its last tag is not known; fetch the whole history"
	api_red=1
elif ! api_base=$(git describe --tags --abbrev=0 --match 'v*' $api_here 2>/dev/null); then
	echo "api: no tag before this head yet, so no break is held; the systems and the marks are"
else
	GIT_INDEX_FILE="$api_d/index" git read-tree "$api_base" || exit 1
	GIT_INDEX_FILE="$api_d/index" git checkout-index -a --prefix="$api_d/old/" || exit 1
	for goos in darwin linux windows; do
		(cd "$api_d/old" && GOOS=$goos CGO_ENABLED=0 GOWORK=off "$api_bin" -m -w "$api_d/old-$goos.exp" .) 2>"$api_d/err" || {
			cat "$api_d/err"
			echo "api: apidiff cannot write $api_base's export data for $goos"
			exit 1
		}
		"$api_bin" -m -incompatible "$api_d/old-$goos.exp" "$api_d/$goos.exp" 2>/dev/null >"$api_d/breaks-$goos" || {
			echo "api: apidiff cannot compare $api_base with the tree for $goos"
			exit 1
		}
	done
	LC_ALL=C sort -u "$api_d/breaks-darwin" "$api_d/breaks-linux" "$api_d/breaks-windows" >"$api_d/breaks.got" || exit 1
	api_want="api/breaks/$api_base.txt"
	[ -f "$api_want" ] || api_want="$api_d/none"
	if git diff --no-index --exit-code "$api_want" "$api_d/breaks.got"; then
		echo "api: the breaks since $api_base are the $(wc -l <"$api_d/breaks.got" | tr -d ' ') lines of api/breaks/$api_base.txt"
	else
		echo "api: the breaks since $api_base are not api/breaks/$api_base.txt (- the file, + apidiff); a commit that removes or changes an exported name adds apidiff's line there"
		api_red=1
	fi
fi

# 3. The marks.
api_n=0
while IFS= read -r api_line; do
	case "$api_line" in '' | '#'*) continue ;; esac
	api_n=$((api_n + 1))
	IFS='	' read -r api_pkg api_name api_mark api_more <<EOF
$api_line
EOF
	if [ -z "$api_mark" ] || [ -n "$api_more" ] || [ "$api_mark" = experimental ]; then
		echo "api: api/marks.txt line '$api_line' is not <package>, <name> and a mark other than experimental, tab-separated"
		api_red=1
	elif ! CGO_ENABLED=0 go doc "./$api_pkg" "$api_name" >/dev/null 2>&1; then
		echo "api: api/marks.txt marks $api_pkg $api_name, which go doc does not find"
		api_red=1
	fi
done <api/marks.txt
echo "api: api/marks.txt marks $api_n names other than experimental"

exit $api_red
