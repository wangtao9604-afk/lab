# syntax=docker/dockerfile:1
FROM rockylinux:8

RUN dnf install -y dnf-plugins-core && \
    (dnf config-manager --set-enabled powertools || true) && \
    (dnf config-manager --set-enabled crb || true) && \
    (command -v crb >/dev/null 2>&1 && crb enable || true) && \
    dnf install -y epel-release && \
    dnf -y copr enable ngompa/musl-libc && \
    dnf groupinstall -y "Development Tools" && \
    dnf install -y wget pkg-config glibc-static zlib-static make golang musl-devel musl-gcc && \
    dnf clean all

ENV GO111MODULE=on

WORKDIR /workspace
