#!/usr/bin/env bash
set -euo pipefail
: "${RELEASE_VERSION:?}" "${RELEASE_SHA:?}" "${GITHUB_REPOSITORY:?}" "${RUNNER_TEMP:?}"
mode=${1:-build}
if [[ "$mode" != build && "$mode" != publish ]]; then
  printf 'expected build or publish mode\n' >&2
  exit 2
fi
registries=("ghcr.io/${GITHUB_REPOSITORY,,}")
if [[ ${SIMPLE_RELEASE:-false} != true && ${DOCKERHUB_USERNAME:-skip} != skip ]]; then
  registries+=("${DOCKERHUB_USERNAME}/sub2api")
fi
arches=(amd64 arm64)
if [[ ${SIMPLE_RELEASE:-false} == true ]]; then arches=(amd64); fi
if [[ "$mode" == build ]]; then
for arch in "${arches[@]}"; do
  archive="$RUNNER_TEMP/sub2api-$arch.oci.tar"
  rm -f "$archive.verified"
  args=(--platform "linux/$arch" --file ".release-context/$arch/Dockerfile"
    --label "org.opencontainers.image.version=$RELEASE_VERSION"
    --label "org.opencontainers.image.revision=$RELEASE_SHA"
    --label "org.opencontainers.image.source=https://github.com/$GITHUB_REPOSITORY")
  # One name keeps the OCI archive unambiguous for skopeo consumers.
  args+=(--tag "sub2api-candidate:$arch")
  args+=(--output "type=oci,dest=$archive")
  docker buildx build "${args[@]}" ".release-context/$arch"
done
exit 0
fi
if [[ ${DRY_RUN:-false} == true ]]; then exit 0; fi
# Validate every archive before the first registry write. A marker also binds
# runtime verification to the release identity, so it cannot authorize reuse.
for arch in "${arches[@]}"; do
  archive="$RUNNER_TEMP/sub2api-$arch.oci.tar"
  test -s "$archive"
  test -s "$archive.verified"
  expected=$(printf '%s\t%s\t%s' "$RELEASE_SHA" "$RELEASE_VERSION" "$arch")
  IFS= read -r verified < "$archive.verified"
  test "$verified" = "$expected"
  test "$(wc -l < "$archive.verified")" = 2
  test "$(tail -n 1 "$archive.verified")" = "$(sha256sum "$archive")"
done
for arch in "${arches[@]}"; do
  for registry in "${registries[@]}"; do
    skopeo copy --all --preserve-digests "oci-archive:$RUNNER_TEMP/sub2api-$arch.oci.tar" "docker://$registry:$RELEASE_VERSION-$arch"
  done
done
if [[ ${SIMPLE_RELEASE:-false} == true ]]; then
  docker buildx imagetools create --tag "${registries[0]}:$RELEASE_VERSION" --tag "${registries[0]}:latest" "${registries[0]}:$RELEASE_VERSION-amd64"
else
  major=${RELEASE_VERSION%%.*}
  minor=${RELEASE_VERSION#*.}; minor=${minor%%.*}
  for registry in "${registries[@]}"; do
    docker buildx imagetools create \
      --tag "$registry:$RELEASE_VERSION" --tag "$registry:latest" \
      --tag "$registry:$major.$minor" --tag "$registry:$major" \
      "$registry:$RELEASE_VERSION-amd64" "$registry:$RELEASE_VERSION-arm64"
  done
fi
