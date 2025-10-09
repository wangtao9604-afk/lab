# x86 构建辅助

本目录提供基于 Docker 的 Linux x86_64 静态构建环境，方便在 Apple Silicon 等非 x86 平台上交叉编译项目。

## 目录结构
- `rockylinux8/`：Rocky Linux 8 + musl toolchain，遵循 Confluent 官方建议，生成与 RHEL 系兼容的全静态可执行文件。
- `ubuntu2404/`：Ubuntu 24.04 + musl-tools，生成针对较新 Ubuntu/Debian 环境的全静态可执行文件。

两个环境都会在容器内执行 `make clean && make build_amd64_static`，产物输出到仓库 `bin/` 目录。

## 使用方法
在项目根目录执行对应脚本即可：

```bash
# Rocky Linux 8 构建流程
./x86build/rockylinux8/build.sh

# Ubuntu 24.04 构建流程
./x86build/ubuntu2404/build.sh
```

脚本行为：
1. 如果本地尚未存在对应构建镜像，会自动以当前目录为上下文运行 `docker build --platform=linux/amd64` 构建镜像。
2. 使用该镜像启动一个临时容器，在容器内挂载项目根目录并执行 `make clean && make build_amd64_static`。

> 默认镜像名称分别为 `qywx-x86-rockylinux8-builder` 与 `qywx-x86-ubuntu2404-builder`，可通过设置环境变量 `IMAGE_NAME` 覆盖。

> 构建脚本依赖仓库根目录的 `Makefile` 中的 `build_amd64_static` 目标，请确保该目标保持最新。
