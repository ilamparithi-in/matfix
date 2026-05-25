#!/bin/sh
set -e

# Docker creates bind-mount directories on the host as root, and named volumes
# may also be root-owned if they predate this image.  Fix ownership of the two
# writable mount points before dropping privileges.
chown matfix:matfix /data /run/matfix

exec setpriv --reuid=matfix --regid=matfix --init-groups "$@"
