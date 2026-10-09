package tsuite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	tsfile "github.com/apache/tsfile/go/tsfile"
)

// R 运行一个以 RN-145 用例编号命名的子测试。
func R(t *testing.T, id string, fn func(t *testing.T)) {
	t.Helper()
	t.Run(id, fn)
}

func wantNil(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func wantIs(t *testing.T, err error, targets ...error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error, got nil (want any of %v)", targets)
	}
	for _, target := range targets {
		if errors.Is(err, target) {
			return
		}
	}
	t.Fatalf("error = %v; want any of %v", err, targets)
}

func wantErr(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a non-nil error")
	}
}

func tmpPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

// metricsSchema 是标准样本表结构：device(STRING/TAG) + value(DOUBLE/FIELD)。
func metricsSchema() tsfile.TableSchema {
	return tsfile.TableSchema{
		Table: "metrics",
		Columns: []tsfile.ColumnSchema{
			{Name: "device", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag},
			{Name: "value", DataType: tsfile.DataTypeDouble, Category: tsfile.ColumnCategoryField},
		},
	}
}

// 扩展样本（行数 = caseRows，默认 1200）：ts = 10*(i+1)；device = d1,d1,d2,d3 循环；
// value = i+1，i%4==2 时为 NULL。前 4 行与旧 4 行样本完全一致
// （10/d1/1.0、20/d1/2.0、30/d2/NULL、40/d3/4.0），便于既有语义断言推算复核。
type sampleRow struct {
	ts     int64
	device string
	value  float64
	isNull bool
}

func bulkRows(t *testing.T) int { return caseRows(t) }

func bulkDevice(i int) string {
	switch i % 4 {
	case 0, 1:
		return "d1"
	case 2:
		return "d2"
	default:
		return "d3"
	}
}

func bulkSample(t *testing.T) []sampleRow {
	t.Helper()
	n := caseRows(t)
	rows := make([]sampleRow, n)
	for i := 0; i < n; i++ {
		rows[i].ts = int64(10 * (i + 1))
		rows[i].device = bulkDevice(i)
		if i%4 == 2 {
			rows[i].isNull = true
		} else {
			rows[i].value = float64(i + 1)
		}
	}
	return rows
}

// sampleReadOrder 把样本按产品实测的读回顺序排列：先按 TAG 值分组（字典序 d1→d2→d3），
// 组内按 ts 升序。实测依据：读回结果按 TAG 分块返回，不是全局时间戳序
// （如 Eq(d1) 返回 10,20,50,60…，Not(d1) 返回 30,70,110…），
// 因此 offset / limit / 批次 / 并发等"顺序相关"断言必须按此顺序推算。
func sampleReadOrder(rows []sampleRow) []sampleRow {
	seen := map[string]bool{}
	devices := []string{}
	for _, r := range rows {
		if !seen[r.device] {
			seen[r.device] = true
			devices = append(devices, r.device)
		}
	}
	sort.Strings(devices)
	out := make([]sampleRow, 0, len(rows))
	for _, d := range devices {
		for _, r := range rows {
			if r.device == d {
				out = append(out, r)
			}
		}
	}
	return out
}

// bulkReadOrder 是标准样本的读回顺序（d1 块 → d2 块 → d3 块，块内 ts 升序）。
func bulkReadOrder(t *testing.T) []sampleRow {
	t.Helper()
	return sampleReadOrder(bulkSample(t))
}

// bulkRowsOfDevices 返回 TAG 值命中给定集合的样本行（仍按读回顺序）。
func bulkRowsOfDevices(t *testing.T, devices ...string) []sampleRow {
	t.Helper()
	hit := map[string]bool{}
	for _, d := range devices {
		hit[d] = true
	}
	out := []sampleRow{}
	for _, r := range bulkReadOrder(t) {
		if hit[r.device] {
			out = append(out, r)
		}
	}
	return out
}

// bulkNullRows 返回 value 为 NULL 的样本行（按读回顺序）。
func bulkNullRows(t *testing.T) []sampleRow {
	t.Helper()
	out := []sampleRow{}
	for _, r := range bulkReadOrder(t) {
		if r.isNull {
			out = append(out, r)
		}
	}
	return out
}

// bulkSampleAlt 与 bulkSample 形状完全一致，但 TAG 值前缀换成 e（e1/e2/e3）。
// 供多表用例区分"两张表的数据"，避免两张表数据相同导致张冠李戴无法暴露。
func bulkSampleAlt(t *testing.T) []sampleRow {
	t.Helper()
	rows := bulkSample(t)
	for i := range rows {
		rows[i].device = "e" + rows[i].device[1:]
	}
	return rows
}

// wantRowValues 按读回顺序逐行比对 (ts, value, null)；顺序敏感。
func wantRowValues(t *testing.T, got []rowValue, want []sampleRow) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("rows = %d; want %d", len(got), len(want))
	}
	for i := range want {
		g := got[i]
		if g.ts != want[i].ts || g.null != want[i].isNull || (!want[i].isNull && g.value != want[i].value) {
			t.Fatalf("rows[%d] = %+v; want ts=%d value=%v null=%v",
				i, g, want[i].ts, want[i].value, want[i].isNull)
		}
	}
}

// wantRowsWithTagOrdered 按读回顺序逐行比对含 TAG 列的读回结果；顺序敏感。
func wantRowsWithTagOrdered(t *testing.T, got []rowWithTag, want []sampleRow) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("rows = %d; want %d", len(got), len(want))
	}
	for i := range want {
		g := got[i]
		if g.ts != want[i].ts || g.device != want[i].device || g.null != want[i].isNull ||
			(!want[i].isNull && g.value != want[i].value) {
			t.Fatalf("rows[%d] = %+v; want ts=%d device=%s value=%v null=%v",
				i, g, want[i].ts, want[i].device, want[i].value, want[i].isNull)
		}
	}
}
func writeSample(t *testing.T, path string) {
	t.Helper()
	rows := bulkSample(t)
	writer, err := tsfile.NewWriter(path, metricsSchema())
	wantNil(t, err)
	const batch = 500
	for from := 0; from < len(rows); from += batch {
		n := min(batch, len(rows)-from)
		tablet, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: "value", DataType: tsfile.DataTypeDouble},
		}, n)
		wantNil(t, err)
		for r := 0; r < n; r++ {
			row := rows[from+r]
			wantNil(t, tablet.AddTimestamp(r, row.ts))
			wantNil(t, tablet.SetString(r, 0, row.device))
			if !row.isNull {
				wantNil(t, tablet.SetFloat64(r, 1, row.value))
			}
		}
		wantNil(t, writer.WriteTableTablet(tablet))
		_ = tablet.Close()
	}
	wantNil(t, writer.Close())
}

// wantBulkRowsReadBack 读回 writeBulkRows 写入的批量样本（含 TAG 列），
// 用于把「写入类」用例从"只断言无错误"升级为"数据真落盘且可读回"。
// wantBulkRowsReadBack 读回批量样本并与期望逐行比对（含 TAG 列，按 ts 建表、顺序无关）。
func wantBulkRowsReadBack(t *testing.T, path string) {
	t.Helper()
	want := bulkSample(t)
	r := openReader(t, path)
	defer func() { _ = r.Close() }()
	got := collectRowsWithTag(t, r)
	if len(got) != len(want) {
		t.Fatalf("read back rows = %d; want %d", len(got), len(want))
	}
	byTS := map[int64]rowWithTag{}
	for _, g := range got {
		byTS[g.ts] = g
	}
	for _, w := range want {
		g, ok := byTS[w.ts]
		if !ok {
			t.Fatalf("缺少 ts=%d 的行", w.ts)
		}
		if g.device != w.device || g.null != w.isNull || (!w.isNull && g.value != w.value) {
			t.Fatalf("ts=%d = %+v; want device=%s value=%v null=%v", w.ts, g, w.device, w.value, w.isNull)
		}
	}
}

// rowWithTag 是含 TAG 列的整行读取结果。
type rowWithTag struct {
	ts     int64
	device string
	value  float64
	null   bool
}

// collectRowsWithTag 查询 device+value 两列并逐行读取（校验 TAG 值可正确读回）。
func collectRowsWithTag(t *testing.T, r *tsfile.Reader, opts ...tsfile.QueryOption) []rowWithTag {
	t.Helper()
	rs := mustQuery(t, r, "metrics", []string{"device", "value"}, opts...)
	defer func() { _ = rs.Close() }()
	return collectRowsFromResultWithTag(t, rs)
}

// collectRowsFromResultWithTag 逐行读取已构造的结果集（含 TAG 列；列序 time/device/value）。
func collectRowsFromResultWithTag(t *testing.T, rs *tsfile.ResultSet) []rowWithTag {
	t.Helper()
	var out []rowWithTag
	for {
		ok, err := rs.Next()
		wantNil(t, err)
		if !ok {
			return out
		}
		ts, err := rs.Int64(1)
		wantNil(t, err)
		device, err := rs.String(2)
		wantNil(t, err)
		null, err := rs.IsNull(3)
		wantNil(t, err)
		row := rowWithTag{ts: ts, device: device, null: null}
		if !null {
			v, err := rs.Float64(3)
			wantNil(t, err)
			row.value = v
		}
		out = append(out, row)
	}
}

// wantSampleRowsWithTag 校验含 TAG 列的读回结果与标准样本一致。
// wantSampleRowsWithTag 校验含 TAG 列的读回结果与扩展样本一致（按 ts 建表，顺序无关）。
func wantSampleRowsWithTag(t *testing.T, got []rowWithTag) {
	t.Helper()
	want := bulkSample(t)
	if len(got) != len(want) {
		t.Fatalf("rows = %d; want %d", len(got), len(want))
	}
	byTS := map[int64]rowWithTag{}
	for _, g := range got {
		byTS[g.ts] = g
	}
	for _, w := range want {
		g, ok := byTS[w.ts]
		if !ok {
			t.Fatalf("缺少 ts=%d 的行", w.ts)
		}
		if g.device != w.device || g.null != w.isNull || (!w.isNull && g.value != w.value) {
			t.Fatalf("ts=%d = %+v; want device=%s value=%v null=%v", w.ts, g, w.device, w.value, w.isNull)
		}
	}
}

// caseRows 返回"空值 / 数据类型 / 各种列"类用例的数据行数：
// 默认 1200 行（用户 2026-09-23 要求不要用太少的数据），可用环境变量 CASE_ROWS 覆盖。
func caseRows(t *testing.T) int {
	t.Helper()
	if v := os.Getenv("CASE_ROWS"); v != "" {
		n := 0
		if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
			t.Fatalf("CASE_ROWS=%q 不是正整数", v)
		}
		return n
	}
	return 1200
}

// sampleFile 生成标准样本文件并返回路径。
func sampleFile(t *testing.T) string {
	t.Helper()
	path := tmpPath(t, "sample.tsfile")
	writeSample(t, path)
	return path
}

func openReader(t *testing.T, path string) *tsfile.Reader {
	t.Helper()
	r, err := tsfile.NewReader(path)
	wantNil(t, err)
	return r
}

// mustQuery 执行一次查询并返回结果集。
func mustQuery(t *testing.T, r *tsfile.Reader, table string, columns []string, opts ...tsfile.QueryOption) *tsfile.ResultSet {
	t.Helper()
	rs, err := r.Query(table, columns, opts...)
	wantNil(t, err)
	return rs
}

// collectRows 以行模式读取全部行，返回 (时间戳, 值, 是否空值) 序列。
func collectRows(t *testing.T, rs *tsfile.ResultSet) []rowValue {
	t.Helper()
	var out []rowValue
	for {
		ok, err := rs.Next()
		wantNil(t, err)
		if !ok {
			return out
		}
		ts, err := rs.Int64(1)
		wantNil(t, err)
		null, err := rs.IsNull(2)
		wantNil(t, err)
		rv := rowValue{ts: ts, null: null}
		if !null {
			v, err := rs.Float64(2)
			wantNil(t, err)
			rv.value = v
		}
		out = append(out, rv)
	}
}

type rowValue struct {
	ts    int64
	value float64
	null  bool
}

// writeBulkRows 向给定 Writer 写入扩展样本（行数 = caseRows）。
// writeBulkRows 向给定 Writer 写入扩展样本（行数 = caseRows）。
func writeBulkRows(t *testing.T, w *tsfile.Writer) {
	t.Helper()
	rows := bulkSample(t)
	const batch = 500
	for from := 0; from < len(rows); from += batch {
		n := min(batch, len(rows)-from)
		tablet, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: "value", DataType: tsfile.DataTypeDouble},
		}, n)
		wantNil(t, err)
		for r := 0; r < n; r++ {
			row := rows[from+r]
			wantNil(t, tablet.AddTimestamp(r, row.ts))
			wantNil(t, tablet.SetString(r, 0, row.device))
			if !row.isNull {
				wantNil(t, tablet.SetFloat64(r, 1, row.value))
			}
		}
		wantNil(t, w.WriteTableTablet(tablet))
		_ = tablet.Close()
	}
}

// wideColumns 是覆盖 10 类公开 FIELD 类型 + 1 个 TAG 的列定义（getter/字段类型用例共用）。
func wideColumns() []tsfile.ColumnSchema {
	return []tsfile.ColumnSchema{
		{Name: "device", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag},
		{Name: "c_bool", DataType: tsfile.DataTypeBoolean, Category: tsfile.ColumnCategoryField},
		{Name: "c_int32", DataType: tsfile.DataTypeInt32, Category: tsfile.ColumnCategoryField},
		{Name: "c_int64", DataType: tsfile.DataTypeInt64, Category: tsfile.ColumnCategoryField},
		{Name: "c_float", DataType: tsfile.DataTypeFloat, Category: tsfile.ColumnCategoryField},
		{Name: "c_double", DataType: tsfile.DataTypeDouble, Category: tsfile.ColumnCategoryField},
		{Name: "c_text", DataType: tsfile.DataTypeText, Category: tsfile.ColumnCategoryField},
		{Name: "c_string", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryField},
		{Name: "c_blob", DataType: tsfile.DataTypeBlob, Category: tsfile.ColumnCategoryField},
		{Name: "c_date", DataType: tsfile.DataTypeDate, Category: tsfile.ColumnCategoryField},
		{Name: "c_ts", DataType: tsfile.DataTypeTimestamp, Category: tsfile.ColumnCategoryField},
	}
}

// wideSampleFile 生成含 1 行、覆盖 10 类 FIELD 的样本文件（列序与 wideColumns 一致）。
func wideSampleFile(t *testing.T) string {
	t.Helper()
	path := tmpPath(t, "wide.tsfile")
	w, err := tsfile.NewWriter(path, tsfile.TableSchema{Table: "wide", Columns: wideColumns()})
	wantNil(t, err)
	tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
		{Name: "device", DataType: tsfile.DataTypeString},
		{Name: "c_bool", DataType: tsfile.DataTypeBoolean},
		{Name: "c_int32", DataType: tsfile.DataTypeInt32},
		{Name: "c_int64", DataType: tsfile.DataTypeInt64},
		{Name: "c_float", DataType: tsfile.DataTypeFloat},
		{Name: "c_double", DataType: tsfile.DataTypeDouble},
		{Name: "c_text", DataType: tsfile.DataTypeText},
		{Name: "c_string", DataType: tsfile.DataTypeString},
		{Name: "c_blob", DataType: tsfile.DataTypeBlob},
		{Name: "c_date", DataType: tsfile.DataTypeDate},
		{Name: "c_ts", DataType: tsfile.DataTypeTimestamp},
	}, 1)
	wantNil(t, err)
	defer func() { _ = tab.Close() }()
	wantNil(t, tab.AddTimestamp(0, 10))
	wantNil(t, tab.SetString(0, 0, "d1"))
	wantNil(t, tab.SetBool(0, 1, true))
	wantNil(t, tab.SetInt32(0, 2, 7))
	wantNil(t, tab.SetInt64(0, 3, 8))
	wantNil(t, tab.SetFloat32(0, 4, 1.5))
	wantNil(t, tab.SetFloat64(0, 5, 2.5))
	wantNil(t, tab.SetString(0, 6, "text"))
	wantNil(t, tab.SetString(0, 7, ""))
	wantNil(t, tab.SetBytes(0, 8, []byte{0, 1, 2}))
	wantNil(t, tab.SetInt32(0, 9, 20260922))
	wantNil(t, tab.SetInt64(0, 10, 1690000000000))
	wantNil(t, w.WriteTableTablet(tab))
	wantNil(t, w.Close())
	return path
}

// newTableFromRecord 用 Arrow Record 构造 Arrow Table（用于 WriteArrowBatch 的 Table 分支）。
func newTableFromRecord(t *testing.T, rec arrow.Record) arrow.Table {
	t.Helper()
	return array.NewTableFromRecords(rec.Schema(), []arrow.Record{rec})
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// tailText 截取字符串尾部 n 个字符（日志取证用）。
func tailText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
