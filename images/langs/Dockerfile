# syntax=docker/dockerfile:1.7
# One Dockerfile for every language image. A language is just a feature that
# runs at build time, so adding a language = adding features/<name>/install.sh
# and a catalog entry. build.sh passes:
#   BASE_IMAGE      the workspace-base reference
#   LANG_FEATURE    e.g. rust
#   EXTRA_FEATURES  optional comma list to bake in (cmake,gdb) → tag suffix -cmake-gdb
ARG BASE_IMAGE=workspace-base:dev
FROM ${BASE_IMAGE}
ARG LANG_FEATURE
ARG EXTRA_FEATURES=""
RUN install-feature "${LANG_FEATURE}" \
 && for f in ${EXTRA_FEATURES//,/ }; do install-feature "$f"; done \
 && rm -rf /var/lib/apt/lists/* /var/cache/apt/archives/*.deb /tmp/*
ENV WORKSPACE_IMAGE=${LANG_FEATURE}
