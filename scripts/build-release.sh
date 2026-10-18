#!/bin/sh
# Local release bundle; builds and records evidence, never publishes an image.
set -eu
cd "$(dirname "$0")/.."
: "${1:?usage: scripts/build-release.sh NEW_OUTPUT_DIRECTORY [linux/amd64|linux/arm64]}"
platform=${2:-linux/amd64}
case "$platform" in
  linux/amd64|linux/arm64) ;;
  *) echo 'supported build targets: linux/amd64, linux/arm64' >&2; exit 1 ;;
esac
if [ -e "$1" ]; then
  echo 'output directory must not already exist' >&2
  exit 1
fi
revision=$(git rev-parse HEAD)
if [ -n "$(git status --porcelain)" ]; then
  echo 'release builds require a clean working tree' >&2
  exit 1
fi
release_tag=$(git describe --tags --exact-match HEAD)
case "$release_tag" in
  v[0-9]*.[0-9]*.[0-9]*) ;;
  *) echo 'release builds require a version tag such as v0.1.0' >&2; exit 1 ;;
esac
release_version=${release_tag#v}
mkdir -p "$1"
output=$(cd "$1" && pwd)
container=''
cleanup() {
  if [ -n "$container" ]; then docker rm "$container" >/dev/null; fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

printf '%s\n' "$revision" > "$output/revision.txt"
printf '%s\n' "$release_version" > "$output/version.txt"
git status --porcelain > "$output/working-tree.txt"
find cmd internal pkg -type f > "$output/source-files.txt"
printf '%s\n' go.mod go.sum LICENSE deployments/Dockerfile .dockerignore >> "$output/source-files.txt"
LC_ALL=C sort -o "$output/source-files.txt" "$output/source-files.txt"
while IFS= read -r file; do
  shasum -a 256 "$file"
done < "$output/source-files.txt" > "$output/source.sha256"

tag="go-modular-release:$(date -u +%Y%m%d%H%M%S)-$$"
docker build --platform "$platform" --provenance=false --tag "$tag" \
  --build-arg "VERSION=$release_version" --build-arg "REVISION=$revision" \
  -f deployments/Dockerfile .
# Resolve the loaded image ID; BuildKit's iidfile may name its config digest.
docker image inspect --format '{{.Id}}' "$tag" > "$output/image.id"
image=$(cat "$output/image.id")
docker image inspect "$image" > "$output/image.json"
container=$(docker create "$image")
docker cp "$container:/licenses" "$output/runtime-notices"
for command in app cli worker scheduler; do
  docker cp "$container:/app/$command" "$output/$command"
  go version -m "$output/$command" > "$output/$command.modules.txt"
done
go list -m -f '{{.Path}} {{.Version}} {{.Sum}}' all > "$output/modules.txt"
# Install only into this bundle, without adding a tool to the application's go.mod.
GOOS="$(go env GOHOSTOS)" GOARCH="$(go env GOHOSTARCH)" CGO_ENABLED=0 \
  GOBIN="$output/tools" go install github.com/google/go-licenses/v2@v2.0.1
export GOOS=linux GOARCH="${platform#linux/}" CGO_ENABLED=0
"$output/tools/go-licenses" report ./cmd/app ./cmd/cli ./cmd/worker ./cmd/scheduler > "$output/licenses.csv"
"$output/tools/go-licenses" save ./cmd/app ./cmd/cli ./cmd/worker ./cmd/scheduler --save_path="$output/notices"
(cd "$output" && shasum -a 256 app cli worker scheduler > binaries.sha256)
echo "Release evidence written to $output (not published)."
