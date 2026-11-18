#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# Immutable public snapshot; never use an unversioned latest asset in a build.
REVISION=61beccc959c75a100d3997b63d6491e5f8ce5eb1
OUT="${1:?usage: fetch-ubuntu-geodata.sh OUTPUT_DIRECTORY}"
mkdir -p "$OUT"
cd "$OUT"
for entry in geoip.dat:GeoIP.dat geosite.dat:GeoSite.dat geoip.metadb:geoip.metadb GeoLite2-ASN.mmdb:ASN.mmdb; do
 source_name="${entry%%:*}"
 target_name="${entry#*:}"
 expected="$(awk -v name="$target_name" '$2 == name {print $1}' "$ROOT/packaging/linux/geodata.sha256")"
 if ! { [[ -f "$target_name" ]] && printf '%s  %s\n' "$expected" "$target_name" | shasum -a 256 --check --status; }; then
  curl -fLsS --retry 3 --connect-timeout 20 --max-time 180 "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/$REVISION/$source_name" -o "$target_name.part"
  printf '%s  %s\n' "$expected" "$target_name.part" | shasum -a 256 --check --status
  mv "$target_name.part" "$target_name"
 fi
done
shasum -a 256 --check "$ROOT/packaging/linux/geodata.sha256"
