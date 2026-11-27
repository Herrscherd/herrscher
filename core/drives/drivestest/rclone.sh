#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_RCLONE_LOG"
env | grep '^RCLONE_' >> "$FAKE_RCLONE_LOG"
cmd=""
target=""
count=-1
stat=0
while [ $# -gt 0 ]; do
	case "$1" in
	--config | --max-depth | --size) shift ;;
	--count)
		shift
		count=$1
		;;
	--stat) stat=1 ;;
	--ask-password=false | --no-mimetype | --) ;;
	*)
		if [ -z "$cmd" ]; then
			cmd=$1
		elif [ "$cmd" = config ] && [ -z "$sub" ]; then
			sub=$1
		else
			target=$1
		fi
		;;
	esac
	shift
done
path="$FAKE_RCLONE_ROOT/$(printf '%s' "$target" | sed 's/:/\//')"
case "$cmd" in
version) echo "rclone v1.75.1-fake" ;;
lsjson)
	if [ "$stat" = 1 ]; then
		if [ -d "$path" ]; then
			printf '{"Path":"","Name":"","Size":-1,"IsDir":true}\n'
		elif [ -f "$path" ]; then
			printf '{"Path":"%s","Name":"%s","Size":%s,"IsDir":false}\n' "$(basename "$path")" "$(basename "$path")" "$(wc -c <"$path" | tr -d ' ')"
		else
			echo "2026/01/01 00:00:00 NOTICE: Failed to lsjson: directory not found" >&2
			exit 3
		fi
		exit 0
	fi
	if [ ! -d "$path" ]; then
		echo "2026/01/01 00:00:00 ERROR : error listing: directory not found" >&2
		exit 3
	fi
	printf '['
	sep=''
	for entry in "$path"/*; do
		[ -e "$entry" ] || continue
		name=$(basename "$entry")
		if [ -d "$entry" ]; then
			printf '%s{"Path":"%s","Name":"%s","Size":-1,"IsDir":true}' "$sep" "$name" "$name"
		else
			printf '%s{"Path":"%s","Name":"%s","Size":%s,"IsDir":false}' "$sep" "$name" "$name" "$(wc -c <"$entry" | tr -d ' ')"
		fi
		sep=','
	done
	printf ']\n'
	;;
cat)
	if [ -n "$FAKE_RCLONE_HOLD" ]; then
		: >"$FAKE_RCLONE_HOLD.started"
		while [ ! -e "$FAKE_RCLONE_HOLD" ]; do sleep 0.05; done
	fi
	if [ ! -f "$path" ]; then
		echo "2026/01/01 00:00:00 ERROR : object not found" >&2
		exit 4
	fi
	if [ "$count" -ge 0 ]; then
		head -c "$count" "$path"
	else
		cat "$path"
	fi
	printf 'end cat\n' >>"$FAKE_RCLONE_LOG"
	;;
rcat)
	mkdir -p "$(dirname "$path")"
	cat >"$path"
	;;
config) printf 'end config %s\n' "$sub" >>"$FAKE_RCLONE_LOG" ;;
*)
	echo "fake rclone: unknown command $cmd" >&2
	exit 1
	;;
esac
