# go-tsfile-api-test

----

TsFile **Go 接口**（RN-145）的自动化用例载体。

# 环境

- Go >= 1.22.12（`/usr/local/go/bin/go`；本机 `GOPROXY=https://mirrors.aliyun.com/goproxy,https://goproxy.cn,direct`、`GOSUMDB=sum.golang.google.cn`、`GOTOOLCHAIN=local`）
- Maven >= 3.9.9（实测 3.9.16）+ JDK >= 17（实测 Temurin 17.0.20）：编译产品原生库时使用
- GCC >= 11（实测 13.3.0）、GNU Make >= 4.3、CMake >= 3.22（实测 3.28.3）
- Go 第三方依赖（`github.com/apache/arrow-go/v18 v18.0.0` 等）由 `go.mod` / `go.sum` 锁定，经 GOPROXY 拉取

# 结构

```bash
# 项目结构
|—— build                    # 存放编译产物的目录（go test -c 生成 tsuite.test）
|—— reports                  # 存放运行产物的目录（all.jsonl / all.xml / case-*.xml）
|—— tsuite                   # 存放测试用例的目录（Go 测试包）
|   |—— doc.go               # 包说明与用例编写约定
|   |—— harness.go           # 公共夹具：metrics 表（device STRING/TAG + value DOUBLE/FIELD）、断言助手
|   |—— writer_test.go       # 写入接口用例（NewWriter / WriteTableTablet / WriteArrowBatch / AddProperty / Flush / Close）
|   |—— tablet_test.go       # Tablet 接口用例（构造、AddTimestamp、值边界、Close）
|   |—— reader_test.go       # 读取与查询接口用例（NewReader / Query / GetTableSchema 等）
|   |—— result_test.go       # ResultSet 接口用例（Next / IsNull / Metadata / getter / 消费模式）
|   |—— {writer,tablet,reader,result}_p1_test.go   # 各接口的 P1 场景用例（边界、异常、组合）
|—— go.mod                   # 模块定义 gitlab-eco.timecho.com/test/go-tsfile-api-test（replace 指向被测源码树）
|—— go.sum                   # 依赖校验和
|—— start.sh                 # 编译、运行、用例清单的入口脚本
|—— xml_from_gojson.py       # go test -json → JUnit XML（写入 case_id 属性）
|—— map_lookup.py            # PLM CASE_ID ↔ 测试节点 映射查询辅助脚本
|—— .plm-case-catalog.json   # 用例清单（自然测试节点），list-cases 的数据源
|—— .plm-case-id-map.json    # PLM CASE_ID → 测试节点 映射（未导入 PLM 时为空，键由 PLM 发号回填）
|—— .plm-case-info.json      # 用例元信息（名称、接口、自动化类型 ts-go）
|—— .plm-cn-names.json       # 用例中文名
|—— README.md                # 说明文档
```

# 依赖

步骤一：编译源码生成头文件和库文件

```bash
cd /data/test/src/tsfile          # 被测源码，提测分支 rc/2.5.0（本轮 e6d09dd7，2026-09-11）
git log -1 --format='%H %cd %s'   # 记录提测 commit
mvn clean install -P with-cpp -DskipTests
```

- 头文件目录：编译后位于 `/data/test/src/tsfile/cpp/target/build/include`
- 库文件目录：编译后位于 `/data/test/src/tsfile/cpp/target/build/lib`，本轮为 `libtsfile.so -> libtsfile.so.2.4.1.dev`（4224544 B，2026-09-15 15:28 构建）
- 第三方运行时（antlr4 runtime 等）已编入 `libtsfile.so`，本项目无需单独安装
- **换提测分支 / commit 后必须重编，禁止沿用历史产物**；只改产品源码而不重编时，测试链接的仍是旧 `.so`（cgo 只在编译期解析链接）

步骤二：本项目如何引用头文件和库文件

本项目**不把 `include` / `lib` 复制到项目目录**，而是通过模块替换 + cgo 指令原地引用被测源码树的构建产物：

- `go.mod`：`replace github.com/apache/tsfile/go => /data/test/src/tsfile/go`（直接用被测源码，不走网络、不锁版本）
- 被测源码 `go/tsfile/cgo_bridge.go` 自带 cgo 指令（本项目无需修改）：
  - `#cgo CFLAGS: -I${SRCDIR}/../../cpp/target/build/include`
  - `#cgo !windows LDFLAGS: -L${SRCDIR}/../../cpp/target/build/lib -ltsfile -Wl,-rpath,${SRCDIR}/../../cpp/target/build/lib`
  - `#cgo windows LDFLAGS: -L${SRCDIR}/../../cpp/target/build/lib -ltsfile`（MinGW 下需自备 `libtsfile.dll` 并置于 `PATH`）
- `${SRCDIR}` = Go 包所在目录（`/data/test/src/tsfile/go/tsfile`），相对路径因此解析到 `cpp/target/build`
- 结论：本项目目录可任意移动，但**不能移动 / 删除 `/data/test/src/tsfile`**；Linux 侧 rpath 已指向构建目录，**无需设置 `LD_LIBRARY_PATH`**

步骤三：确认链接到了正确的原生库（自检）

```bash
ldd build/tsuite.test | grep tsfile    # 应指向 cpp/target/build/lib/libtsfile.so.<当前版本>
```

# 测试

## 自动化测试——go test（`start.sh`）

```bash
sh start.sh prepare             # go build + go test -c → build/tsuite.test
sh start.sh all                 # 全量用例 → reports/all.xml + all.jsonl
sh start.sh page writer         # 按页面运行：writer | tablet | reader | result
sh start.sh case <CASE_ID>      # 单条用例（需 PLM 映射已回填）→ reports/case-<CASE_ID>.xml
sh start.sh cases <ID1,ID2>     # 批量用例（同上）
sh start.sh list-cases --json   # 用例清单（自然测试节点；已导入 PLM 时附带 caseId）
```

用例约定：**一个场景 = 一个子测试**，子测试名是自然场景名（如 `path_空字符串`、`SetBool_row_maxRows`）；
PLM CASE_ID 不出现在测试代码里，只出现在 `.plm-case-id-map.json`；未导入 PLM 时映射为空，此时 `case` / `cases` 不可用，
可直接执行 `go test -run 'TestNewWriter/path_空字符串' ./tsuite` 运行单个子测试。

当前用例规模：**284 个子测试**（P0 106 + P1 178），覆盖 NewWriter / NewTablet / Tablet / Writer / Reader / Query / ResultSet / TagFilter / Close 语义 / 错误模型 / NULL 语义 / 消费模式 / 并发读。

## 覆盖率测试——Go 原生 cover（暂未接入 CI）

```bash
go test -cover ./tsuite
go test -coverprofile=coverage.out ./tsuite && go tool cover -html=coverage.out
```

## 性能测试——暂未实现（RN-145 本轮不纳入性能验证）
