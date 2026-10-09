package tsuite

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	tsfile "github.com/apache/tsfile/go/tsfile"
)

// 跨语言互读（RN-145 批次 4）：Go 与 Python / 原生 C++ 探针对同一份样本文件相互读写。
//
// 依赖（74 上已就绪）：
//   - Python：/data/test/src/tsfile/python/venv/bin/python（tsfile wheel 来自提测分支 rc/2.5.0）
//   - 原生探针：interop/cpp_probe（先执行 `bash interop/build.sh` 编译，链接 cpp/target/build/lib/libtsfile.so）
// 可用环境变量覆盖：GO_TSFILE_INTEROP_DIR、GO_TSFILE_INTEROP_PY。

// interopRowsText 生成跨语言读回的期望文本（格式 ts|device|value，空值输出 NULL），
// 行序 = 产品实测读回顺序（按 TAG 分块、块内 ts 升序），与 py_sample.py / cpp_probe.cpp 一致。
func interopRowsText(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, row := range bulkReadOrder(t) {
		if row.isNull {
			fmt.Fprintf(&b, "%d|%s|NULL\n", row.ts, row.device)
		} else {
			fmt.Fprintf(&b, "%d|%s|%.1f\n", row.ts, row.device, row.value)
		}
	}
	return b.String()
}

// firstLineDiff 返回两组文本行的首个差异（避免 1200 行日志刷屏）。
func firstLineDiff(got, want []string) string {
	for i := 0; i < len(got) || i < len(want); i++ {
		g, w := "<缺失>", "<缺失>"
		if i < len(got) {
			g = got[i]
		}
		if i < len(want) {
			w = want[i]
		}
		if g != w {
			return fmt.Sprintf("第 %d 行 got=%q want=%q", i+1, g, w)
		}
	}
	return "无（行数相同且逐行一致）"
}

func interopDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("GO_TSFILE_INTEROP_DIR")
	if dir == "" {
		dir = filepath.Join("..", "interop")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("interop 目录不可用: %s (%v)", dir, err)
	}
	return dir
}

func interopPython(t *testing.T) string {
	t.Helper()
	py := os.Getenv("GO_TSFILE_INTEROP_PY")
	if py == "" {
		py = "/data/test/src/tsfile/python/venv/bin/python"
	}
	if _, err := os.Stat(py); err != nil {
		t.Fatalf("Python 解释器不可用: %s（设置 GO_TSFILE_INTEROP_PY 覆盖）", py)
	}
	return py
}

func interopProbe(t *testing.T) string {
	t.Helper()
	probe := filepath.Join(interopDir(t), "cpp_probe")
	if _, err := os.Stat(probe); err != nil {
		t.Fatalf("原生探针未编译: %s（先执行 `bash interop/build.sh`）", probe)
	}
	return probe
}

func runInterop(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	if err != nil {
		t.Fatalf("%v 执行失败: %v\n输出:\n%s", args, err, out)
	}
	return string(out)
}

func wantInteropRows(t *testing.T, out string) {
	t.Helper()
	want := interopRowsText(t)
	if out != want {
		gotLines := strings.Split(out, "\n")
		wantLines := strings.Split(want, "\n")
		t.Fatalf("跨语言读回结果不一致：got %d 行 / want %d 行；差异：%s",
			len(gotLines)-1, len(wantLines)-1, firstLineDiff(gotLines, wantLines))
	}
}

// wantSampleRows 校验读回结果与扩展样本一致（ts/value/null；TAG 列见 wantSampleRowsWithTag）。
func wantSampleRows(t *testing.T, got []rowValue) {
	t.Helper()
	want := bulkSample(t)
	if len(got) != len(want) {
		t.Fatalf("rows = %d; want %d", len(got), len(want))
	}
	byTS := map[int64]rowValue{}
	for _, g := range got {
		byTS[g.ts] = g
	}
	for _, w := range want {
		g, ok := byTS[w.ts]
		if !ok {
			t.Fatalf("缺少 ts=%d 的行", w.ts)
		}
		if g.null != w.isNull || (!w.isNull && g.value != w.value) {
			t.Fatalf("ts=%d = %+v; want value=%v null=%v", w.ts, g, w.value, w.isNull)
		}
	}
}

// Go 写 → Python 读
func TestInteropGoWritePythonRead(t *testing.T) {
	R(t, "Go_写_Python_读", func(t *testing.T) {
		path := tmpPath(t, "go_written.tsfile")
		writeSample(t, path)
		out := runInterop(t, interopPython(t), filepath.Join(interopDir(t), "py_sample.py"), "read", path)
		wantInteropRows(t, out)
	})
}

// Python 写 → Go 读
func TestInteropPythonWriteGoRead(t *testing.T) {
	R(t, "Python_写_Go_读", func(t *testing.T) {
		path := tmpPath(t, "py_written.tsfile")
		runInterop(t, interopPython(t), filepath.Join(interopDir(t), "py_sample.py"), "write", path)
		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		s, err := r.GetTableSchema("metrics")
		wantNil(t, err)
		if s.Table != "metrics" || len(s.Columns) != 2 || s.Columns[0].Category != tsfile.ColumnCategoryTag {
			t.Fatalf("schema = %+v; want metrics(device TAG + value FIELD)", s)
		}
		wantSampleRowsWithTag(t, collectRowsWithTag(t, r))
	})
}

// Go 写 → 原生 C++ 读
func TestInteropGoWriteCppRead(t *testing.T) {
	R(t, "Go_写_Cpp_读", func(t *testing.T) {
		path := tmpPath(t, "go_written_cpp.tsfile")
		writeSample(t, path)
		out := runInterop(t, interopProbe(t), "read", path)
		wantInteropRows(t, out)
	})
}

// 原生 C++ 写 → Go 读
func TestInteropCppWriteGoRead(t *testing.T) {
	R(t, "Cpp_写_Go_读", func(t *testing.T) {
		path := tmpPath(t, "cpp_written.tsfile")
		runInterop(t, interopProbe(t), "write", path)
		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		wantSampleRowsWithTag(t, collectRowsWithTag(t, r))
	})
}

// 跨语言 × 批次模式：Python 写的文件用 Go 批次模式读，Arrow 批大小与空值位置一致
func TestInteropBatchMode(t *testing.T) {
	R(t, "跨语言_x_批次模式", func(t *testing.T) {
		path := tmpPath(t, "py_written_batch.tsfile")
		runInterop(t, interopPython(t), filepath.Join(interopDir(t), "py_sample.py"), "write", path)
		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(2))
		defer func() { _ = rs.Close() }()

		total, batches := 0, 0
		var nullAt []int64
		for {
			rec, err := rs.ReadArrowRecordBatch()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				t.Fatalf("ReadArrowRecordBatch: %v", err)
			}
			batches++
			n := int(rec.NumRows())
			if n > 2 {
				t.Fatalf("第 %d 批行数 = %d; 超过 batchSize=2", batches, n)
			}
			ts := rec.Column(0).(*array.Int64)
			val := rec.Column(1).(*array.Float64)
			for i := 0; i < n; i++ {
				if val.IsNull(i) {
					nullAt = append(nullAt, ts.Value(i))
				}
			}
			total += n
			rec.Release()
		}
		wantNullRows := bulkNullRows(t)
		wantNullAt := make([]int64, 0, len(wantNullRows))
		for _, w := range wantNullRows {
			wantNullAt = append(wantNullAt, w.ts)
		}
		if total != bulkRows(t) || batches < 2 {
			t.Fatalf("跨语言批次模式: 总行数=%d 批次数=%d; want %d 行、>=2 批", total, batches, bulkRows(t))
		}
		if len(nullAt) != len(wantNullAt) {
			t.Fatalf("空值个数 = %d; want %d（期望位置 %v）", len(nullAt), len(wantNullAt), wantNullAt)
		}
		for i := range wantNullAt {
			if nullAt[i] != wantNullAt[i] {
				t.Fatalf("第 %d 个空值 ts = %d; want %d", i+1, nullAt[i], wantNullAt[i])
			}
		}
	})
}

// 跨语言 × Tag 过滤与分页：Python 写的文件用 Go 做 TagFilter + Offset/Limit
func TestInteropTagFilterPaging(t *testing.T) {
	R(t, "跨语言_x_Tag_过滤与分页", func(t *testing.T) {
		path := tmpPath(t, "py_written_tag.tsfile")
		runInterop(t, interopPython(t), filepath.Join(interopDir(t), "py_sample.py"), "write", path)
		r := openReader(t, path)
		defer func() { _ = r.Close() }()

		limited := mustQuery(t, r, "metrics", []string{"value"},
			tsfile.WithTagFilter(tsfile.Eq("device", "d1")), tsfile.WithOffset(0), tsfile.WithLimit(1))
		rows := collectRows(t, limited)
		_ = limited.Close()
		d1Rows := bulkRowsOfDevices(t, "d1")
		wantRowValues(t, rows, d1Rows[:1])

		all := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithTagFilter(tsfile.Eq("device", "d1")))
		rows = collectRows(t, all)
		_ = all.Close()
		wantRowValues(t, rows, d1Rows)
	})
}

// 多表文件：原生 C++ 生成两张表的文件，Go 返回全部表结构并可分别查询
func TestInteropMultiTableSchemas(t *testing.T) {
	R(t, "多表文件_返回全部表结构", func(t *testing.T) {
		path := tmpPath(t, "cpp_multi_table.tsfile")
		runInterop(t, interopProbe(t), "multi", path)
		r := openReader(t, path)
		defer func() { _ = r.Close() }()

		schemas, err := r.GetAllTableSchemas()
		wantNil(t, err)
		if len(schemas) != 2 {
			t.Fatalf("表数量 = %d (%+v); want 2", len(schemas), schemas)
		}
		byName := map[string]tsfile.TableSchema{}
		for _, s := range schemas {
			byName[s.Table] = s
		}
		for _, name := range []string{"metrics", "metrics2"} {
			s, ok := byName[name]
			if !ok {
				t.Fatalf("缺少表 %s；实际表 = %+v", name, schemas)
			}
			if len(s.Columns) != 2 || s.Columns[0].Name != "device" ||
				s.Columns[0].Category != tsfile.ColumnCategoryTag ||
				s.Columns[1].Name != "value" || s.Columns[1].Category != tsfile.ColumnCategoryField {
				t.Fatalf("表 %s 结构 = %+v; want device TAG + value FIELD", name, s.Columns)
			}
		}
		rs := mustQuery(t, r, "metrics2", []string{"device", "value"})
		rows := collectRowsFromResultWithTag(t, rs)
		_ = rs.Close()
		// metrics2 写的是"形状相同、TAG 前缀换成 e"的样本 → 与 metrics 数据可区分
		wantRowsWithTagOrdered(t, rows, sampleReadOrder(bulkSampleAlt(t)))
	})
}
