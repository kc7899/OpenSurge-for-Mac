# OpenSurge Ubuntu third-party notices

This Linux package retains the OpenSurge upstream and fork authorship and
GPL-3.0-only license. Exact OpenSurge source revision is installed alongside
this file in `source-commit`; source is available at
https://github.com/kc7899/OpenSurge-for-Mac .

The bundled Mihomo is 1.19.33-opensurge.1, GPL-3.0-only, built from upstream
commit `3f27ef39f8c96bbfbc8b0b9bb10f5c9ef60149d8` with the patches and overlays
under `patches/mihomo` in the exact OpenSurge source revision. The complete
build recipe is `scripts/build-opensurge-mihomo.sh` with
`OPENSURGE_MIHOMO_OS=linux OPENSURGE_MIHOMO_ARCH=amd64`.

Upstream source:
https://github.com/MetaCubeX/mihomo/tree/3f27ef39f8c96bbfbc8b0b9bb10f5c9ef60149d8

Pinned source archive SHA256:
`ecda5f8c8a8a1d60bc088c2640c85d92c8f002c0686234a26bdf671735afe216`.

The Go control programs use yaml.v3 (MIT/Apache-2.0) and metacubex/bbolt (MIT).
The embedded Web console uses React, React DOM and scheduler (MIT). License
texts from `third_party/licenses` are installed in `licenses/`; the retained
upstream `THIRD_PARTY_NOTICES.md` provides dependency versions and links.

The Ubuntu package does **not** bundle the macOS dnsmasq binary or Wails
desktop host described in that upstream notice. Linux dnsmasq is supplied by
Ubuntu's `dnsmasq-base` dependency; its exact version and corresponding source
and copyright are provided by Ubuntu's package metadata and
`/usr/share/doc/dnsmasq-base/`.
