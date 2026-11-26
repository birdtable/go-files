# The budget step each tier runs. A tier's cmds source it with its tier and step set in literal words
# of Taskfile.yml, so go-task's own interpreter runs it on every system and no sh need be on the path:
#
#   tier=pre-push; step=cpu; . ./gate/budget.sh
#   tier=pre-commit; step=start; . ./gate/budget.sh
#
# pre-push is judged by its CPU time, step cpu: it reads the 1-minute load with hatch and the start,
# then has `hatch budget --cpu` (tools/hatch.mod) run the tier's members, `task pre-push-members`,
# in a tree of its own and hold the CPU time that tree used, user plus system, to the tier's
# CPU limit in gate/budgets.yaml. CPU time is the same on a quiet machine and a loaded one, so the
# load excuses nothing and none is needed. The tier's wall budget is only a hang guard there: hatch
# kills a tier still running at 10 times it, and that fails as hung.
#
# pre-commit and release are judged by their wall time, steps start and end. start reads the
# 1-minute load with hatch, then writes the tier's start in seconds and that load to
# .task/budget/<tier>.<pid>. The pid is go-task's own ($$ in its interpreter): every command of one
# run has it, and no run beside it does, so two runs of one tier in one tree each time themselves.
# end reads that file, removes it, and has `hatch budget` hold the elapsed seconds to the tier's
# budget. A pre-commit whose load at its start was above 15 passes past its budget, saying so; a
# release past its budget fails at any load.
#
# hatch holds no policy, so this file holds go-files': which tier takes which steps, and which row
# applies on this kind of host. No tier skips, and none is tried again.
#
# Nothing here is taken from a Task var or the caller's environment: a start or load kept in a var
# would be whatever the caller's environment said, as `LOAD=16 git push`.
# The tier and step are set by the Taskfile's words, and every other name is set here
# before it is read. GITHUB_ACTIONS is read, as the one name from the environment that decides
# anything: it marks a hosted runner, whose job's timeout holds it, so release's budget off a hosted
# runner is not applied there.
unset IFS
case "$tier" in
pre-commit | pre-push | release) ;;
*)
	echo "budget: the tier is '$tier', and wants pre-commit, pre-push or release" >&2
	exit 1
	;;
esac
# Each tier takes its own steps and no other, so pre-push is never judged by wall time alone.
case "$tier:$step" in
pre-push:cpu | pre-commit:start | pre-commit:end | release:start | release:end) ;;
*)
	echo "budget: $tier has no step '$step': pre-push takes cpu, and pre-commit and release take start and end" >&2
	exit 1
	;;
esac
f=".task/budget/$tier.$$"
start=""
load=""
rest=""
case "$step" in
start)
	# hatch reads the load the same way on each system; where there is none (Windows) it fails, and
	# no load is written. The load is read before the start, so hatch's first build is not timed.
	load=$(go tool -modfile=tools/hatch.mod hatch budget --load --window 1 2>/dev/null) || load=""
	mkdir -p .task/budget || exit 1
	printf '%s %s\n' "$(date +%s)" "$load" >"$f" || exit 1
	exit 0
	;;
cpu)
	# As start does, but kept in this one command, which also runs the members: no file holds it.
	load=$(go tool -modfile=tools/hatch.mod hatch budget --load --window 1 2>/dev/null) || load=""
	start=$(date +%s)
	;;
end)
	if [ ! -f "$f" ]; then
		echo "budget: $f is missing, so $tier has no start to time from" >&2
		exit 1
	fi
	read -r start load rest <"$f"
	rm -f "$f"
	;;
esac
# Each field is checked before it is used: the start as whole seconds with no leading 0, which $(( ))
# and hatch's --since would read as octal, and the load as digits with one dot inside, or nothing,
# so no spelling a number parser reads (1e9, 0x20) passes for a load.
case "$start" in
'' | 0* | *[!0-9]*)
	echo "budget: $f gives the start as '$start', and wants whole seconds" >&2
	exit 1
	;;
esac
case "$load" in
*[!0-9.]* | .* | *. | *.*.*)
	echo "budget: $f gives the load as '$load', and wants a load such as 3.21, or none" >&2
	exit 1
	;;
esac
if [ -n "$rest" ]; then
	echo "budget: $f has '$rest' after the start and the load, and wants nothing more" >&2
	exit 1
fi
elapsed=$(($(date +%s) - start))
if [ "$elapsed" -lt 0 ]; then
	echo "budget: $tier started at $start, after now, so its time would be ${elapsed}s" >&2
	exit 1
fi
# The row: the tier's own on the kind of host the file names; release's own on Linux or
# Windows amd64 off a hosted runner; and none elsewhere, where the time is printed.
host=$(uname -sm 2>/dev/null | tr ' ' '-')
row=""
if [ "$host" = "$(awk -F': *' '$1 == "host" { print $2 }' gate/budgets.yaml)" ]; then
	row=$tier
elif [ "$tier" = release ] && [ -z "${GITHUB_ACTIONS:-}" ]; then
	case "$host" in
	Linux-x86_64 | MINGW64_NT-*-x86_64 | MSYS_NT-*-x86_64) row=release-session ;;
	esac
fi
# The members' run of task has --color=false: Task colours what it writes wherever CI is set, and
# a colour code it leaves open would start the line hatch writes after it.
if [ "$step" = cpu ] && [ -z "$row" ]; then
	go tool -modfile=tools/task.mod task --color=false "$tier-members" || exit 1
fi
if [ -z "$row" ]; then
	at=""
	if [ -n "$load" ]; then at=" at load $load"; fi
	echo "$tier took $(($(date +%s) - start))s$at; no budget for this kind of host ($host), so none applies"
	exit 0
fi
set -- --budgets gate/budgets.yaml --row '^(?P<id>[a-z-]+)-budget: (?P<limit>[0-9]+)$' --tier "$row" --since "$start" --window 1 --noun "$tier"
if [ -n "$load" ]; then set -- "$@" --load-before "$load"; fi
if [ "$tier" = pre-commit ]; then set -- "$@" --limit 15 --loaded-when before --excused "passed at the load it started at"; fi
set -- "$@" --advice "a tier over its budget is a speed bug to find; gate/budgets.yaml has the budgets"
if [ "$step" = cpu ]; then
	go tool -modfile=tools/hatch.mod hatch budget "$@" --cpu --cpu-row '^(?P<id>[a-z-]+)-cpu-limit: (?P<limit>[0-9]+)$' -- go tool -modfile=tools/task.mod task --color=false "$tier-members"
else
	go tool -modfile=tools/hatch.mod hatch budget "$@"
fi
