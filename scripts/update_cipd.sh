#!/usr/bin/env bash
# Copyright 2023 The Fuchsia Authors. All rights reserved.
# Use of this source code is governed by a BSD-style license that can be
# found in the LICENSE file.

# Script to update the version of the CIPD CLI pinned by Jiri.

set -eu -o pipefail

jiri_dir="$( cd "$( dirname "$( dirname "${BASH_SOURCE[0]}" )" )" >/dev/null && pwd )"

cipd_client_pkg='infra/tools/cipd/${platform}'

# Resolve the latest version of the CIPD CLI. We can't just do `cipd
# selfupdate-roll -version latest` because the "latest" ref might point to a
# different version for different platforms, so instead we resolve the
# git_revision tag of the "latest" version for the current platform and update
# to that tag for all platforms.
versions="$(
    cipd describe "$cipd_client_pkg" -version latest \
    | grep git_revision \
    | tail -5)"  # The last tag in the list is the oldest so most likely to exist for all packages.
versions=($versions)

success=false
for ((i=${#versions[@]}-1; i >= 0; i--)); do
  new_version="${versions[$i]}"
  new_version="$(echo "$new_version" | xargs)" # Trim whitespace
  cipd selfupdate-roll -version-file "$jiri_dir/cipd/cipd_client_version" -version "$new_version" && success=true && break || echo "failed with version ${new_version}"
done
if [ "$success" = false ]; then
  exit 1
fi
