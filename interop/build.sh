#!/usr/bin/env bash
# 编译跨语言互读探针（原生侧）。SDK 目录默认取提测分支的 C++ 构建产物。
set -e
SDK="${SDK:-/data/test/src/tsfile/cpp/target/build}"
HERE="$(cd "$(dirname "$0")" && pwd)"
g++ -std=c++17 -O0 -g \
    -I"$SDK/include" \
    "$HERE/cpp_probe.cpp" \
    -o "$HERE/cpp_probe" \
    -L"$SDK/lib" -ltsfile -Wl,-rpath,"$SDK/lib" -pthread
echo "built: $HERE/cpp_probe (SDK=$SDK)"
