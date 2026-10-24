#!/bin/sh
# NetworkManager keeps the interface connected until this hook returns.
# Only a logind sleep request triggers TV standby; ordinary disconnects do not.
set -eu
[ "${2-}" = pre-down ] || exit 0
preparing=$(/usr/bin/busctl --system --timeout=1 get-property org.freedesktop.login1 /org/freedesktop/login1 org.freedesktop.login1.Manager PreparingForSleep 2>/dev/null) || exit 0
[ "$preparing" = 'b true' ] || exit 0
socket=@SOCKET@
[ -S "$socket" ] || exit 0
# Drop privileges before accessing a user-owned socket. The hook never runs a
# binary or script from the user's home as root.
/usr/bin/setpriv --reuid=@UID@ --regid=@GID@ --clear-groups --no-new-privs \
    /usr/bin/curl --noproxy '*' --silent --show-error --fail --max-time 3 \
    --unix-socket "$socket" --request POST http://local/network/pre-down >/dev/null || :
