# syntax=docker/dockerfile:1
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      build-essential \
      ca-certificates \
      git \
      golang-go \
      musl-tools \
      pkg-config \
      wget \
      zlib1g-dev && \
    rm -rf /var/lib/apt/lists/*

ENV GO111MODULE=on

WORKDIR /workspace
