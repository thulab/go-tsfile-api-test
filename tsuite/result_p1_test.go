package tsuite

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	tsfile "github.com/apache/tsfile/go/tsfile"
)

func TestResultSetNextExtra(t *testing.T) {
	R(t, "关闭后调用", func(t *testing.T) { // 关闭后调用
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		wantNil(t, rs.Close())
		_, err := rs.Next()
		wantIs(t, err, tsfile.ErrClosed)
	})
}

func TestResultSetMetadataExtra(t *testing.T) {
	R(t, "列顺序与查询列一致", func(t *testing.T) { // 列顺序与查询列一致
		r := openReader(t, wideSampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "wide", []string{"c_string", "c_bool", "c_blob"})
		defer func() { _ = rs.Close() }()
		md := rs.Metadata()
		got := []string{}
		for _, m := range md {
			got = append(got, m.Name)
		}
		want := []string{"time", "c_string", "c_bool", "c_blob"}
		if len(got) != len(want) {
			t.Fatalf("metadata = %v; want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("metadata = %v; want %v", got, want)
			}
		}
	})

	R(t, "时间戳列_第_1_列", func(t *testing.T) { // 时间戳列（第 1 列）
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		md := rs.Metadata()
		if len(md) == 0 || md[0].Name != "time" {
			t.Fatalf("第 1 列 = %+v; want time", md)
		}
		if !mustNext(t, rs) {
			t.Fatalf("Next = false")
		}
		ts, err := rs.Int64(1)
		wantNil(t, err)
		if ts != 10 {
			t.Fatalf("Int64(1) = %d; want 10（第 1 列为时间戳）", ts)
		}
	})
}

func TestResultSetIsNullExtra(t *testing.T) {
	positioned := func(t *testing.T) (*tsfile.Reader, *tsfile.ResultSet) {
		t.Helper()
		r := openReader(t, sampleFile(t))
		rs := mustQuery(t, r, "metrics", []string{"value"})
		if !mustNext(t, rs) {
			t.Fatalf("Next = false")
		}
		return r, rs
	}

	R(t, "列_1_时间戳", func(t *testing.T) { // 列 1（时间戳）
		r, rs := positioned(t)
		defer func() { _ = rs.Close(); _ = r.Close() }()
		null, err := rs.IsNull(1)
		wantNil(t, err)
		if null {
			t.Fatalf("IsNull(1) = true; want false（时间戳列不为空）")
		}
	})

	R(t, "列_n_末列", func(t *testing.T) { // 列 n（末列）
		r, rs := positioned(t)
		defer func() { _ = rs.Close(); _ = r.Close() }()
		null, err := rs.IsNull(2)
		wantNil(t, err)
		if null {
			t.Fatalf("IsNull(2) = true; want false（首行 value=1）")
		}
	})

	R(t, "列_-1", func(t *testing.T) { // 列 -1
		r, rs := positioned(t)
		defer func() { _ = rs.Close(); _ = r.Close() }()
		_, err := rs.IsNull(-1)
		wantIs(t, err, tsfile.ErrOutOfRange)
	})

	R(t, "关闭后调用", func(t *testing.T) { // 关闭后调用
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		wantNil(t, rs.Close())
		_, err := rs.IsNull(2)
		wantIs(t, err, tsfile.ErrClosed)
	})
	R(t, "批次模式结果集上调用_IsNull", func(t *testing.T) {
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(2))
		defer func() { _ = rs.Close() }()
		_, err := rs.IsNull(2)
		wantIs(t, err, tsfile.ErrWrongResultMode)
	})

}

func TestResultSetGetterTypes(t *testing.T) {
	// wide 表列序：device(1) c_bool(2) c_int32(3) c_int64(4) c_float(5) c_double(6)
	//            c_text(7) c_string(8) c_blob(9) c_date(10) c_ts(11)；结果列 = time + 所查列
	rows := func(t *testing.T, cols ...string) *tsfile.ResultSet {
		t.Helper()
		r := openReader(t, wideSampleFile(t))
		t.Cleanup(func() { _ = r.Close() })
		rs := mustQuery(t, r, "wide", cols)
		t.Cleanup(func() { _ = rs.Close() })
		if !mustNext(t, rs) {
			t.Fatalf("Next = false")
		}
		return rs
	}

	R(t, "Bool", func(t *testing.T) { // Bool
		rs := rows(t, "c_bool")
		v, err := rs.Bool(2)
		wantNil(t, err)
		if !v {
			t.Fatalf("Bool(2) = false; want true")
		}
	})

	R(t, "Int32_含_DATE_列", func(t *testing.T) { // Int32（含 DATE 列）
		rs := rows(t, "c_int32", "c_date")
		v, err := rs.Int32(2)
		wantNil(t, err)
		if v != 7 {
			t.Fatalf("Int32(2) = %d; want 7", v)
		}
		d, err := rs.Int32(3)
		wantNil(t, err)
		if d != 20260922 {
			t.Fatalf("Int32(3) DATE = %d; want 20260922", d)
		}
	})

	R(t, "Int64_含_TIMESTAMP_列", func(t *testing.T) { // Int64（含 TIMESTAMP 列）
		rs := rows(t, "c_int64", "c_ts")
		v, err := rs.Int64(2)
		wantNil(t, err)
		if v != 8 {
			t.Fatalf("Int64(2) = %d; want 8", v)
		}
		tsv, err := rs.Int64(3)
		wantNil(t, err)
		if tsv != 1690000000000 {
			t.Fatalf("Int64(3) TIMESTAMP = %d; want 1690000000000", tsv)
		}
	})

	R(t, "Float32", func(t *testing.T) { // Float32
		rs := rows(t, "c_float")
		v, err := rs.Float32(2)
		wantNil(t, err)
		if v != 1.5 {
			t.Fatalf("Float32(2) = %v; want 1.5", v)
		}
	})

	R(t, "Float64", func(t *testing.T) { // Float64
		rs := rows(t, "c_double")
		v, err := rs.Float64(2)
		wantNil(t, err)
		if v != 2.5 {
			t.Fatalf("Float64(2) = %v; want 2.5", v)
		}
	})

	R(t, "String_含空串", func(t *testing.T) { // String（含空串）
		rs := rows(t, "c_text", "c_string")
		txt, err := rs.String(2)
		wantNil(t, err)
		if txt != "text" {
			t.Fatalf("String(2) = %q; want text", txt)
		}
		empty, err := rs.String(3)
		wantNil(t, err)
		if empty != "" {
			t.Fatalf("String(3) = %q; want empty", empty)
		}
	})

	R(t, "Bytes_含内嵌_NUL", func(t *testing.T) { // Bytes（含内嵌 NUL）
		rs := rows(t, "c_blob")
		b, err := rs.Bytes(2)
		wantNil(t, err)
		want := []byte{0, 1, 2}
		if len(b) != len(want) {
			t.Fatalf("Bytes(2) = %v; want %v", b, want)
		}
		for i := range want {
			if b[i] != want[i] {
				t.Fatalf("Bytes(2) = %v; want %v", b, want)
			}
		}
	})
}

// nullKindsFile 生成 2 行文件：第 1 行是 false/""/空字节串，第 2 行 3 列全 NULL。
func nullKindsFile(t *testing.T) string {
	t.Helper()
	path := tmpPath(t, "nullkinds.tsfile")
	schema := tsfile.TableSchema{Table: "kinds", Columns: []tsfile.ColumnSchema{
		{Name: "device", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag},
		{Name: "b", DataType: tsfile.DataTypeBoolean, Category: tsfile.ColumnCategoryField},
		{Name: "s", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryField},
		{Name: "bl", DataType: tsfile.DataTypeBlob, Category: tsfile.ColumnCategoryField},
	}}
	w, err := tsfile.NewWriter(path, schema)
	wantNil(t, err)
	tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
		{Name: "device", DataType: tsfile.DataTypeString},
		{Name: "b", DataType: tsfile.DataTypeBoolean},
		{Name: "s", DataType: tsfile.DataTypeString},
		{Name: "bl", DataType: tsfile.DataTypeBlob},
	}, 2)
	wantNil(t, err)
	defer func() { _ = tab.Close() }()
	wantNil(t, tab.AddTimestamp(0, 10))
	wantNil(t, tab.SetString(0, 0, "d1"))
	wantNil(t, tab.SetBool(0, 1, false))
	wantNil(t, tab.SetString(0, 2, ""))
	wantNil(t, tab.SetBytes(0, 3, []byte{}))
	wantNil(t, tab.AddTimestamp(1, 20))
	wantNil(t, tab.SetString(1, 0, "d1")) // 第 2 行 3 个 FIELD 均不设置 → NULL
	wantNil(t, w.WriteTableTablet(tab))
	wantNil(t, w.Close())
	return path
}

func TestNullSemanticsExtra(t *testing.T) {
	R(t, "NULL_与_false", func(t *testing.T) { // NULL 与 false
		r := openReader(t, nullKindsFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "kinds", []string{"b"})
		defer func() { _ = rs.Close() }()
		if !mustNext(t, rs) {
			t.Fatalf("row1 missing")
		}
		null, err := rs.IsNull(2)
		wantNil(t, err)
		if null {
			t.Fatalf("false 被识别为 NULL")
		}
		v, err := rs.Bool(2)
		wantNil(t, err)
		if v {
			t.Fatalf("Bool(2) = true; want false")
		}
		if !mustNext(t, rs) {
			t.Fatalf("row2 missing")
		}
		null, err = rs.IsNull(2)
		wantNil(t, err)
		if !null {
			t.Fatalf("NULL 行 IsNull = false; want true")
		}
	})

	R(t, "NULL_与空串", func(t *testing.T) { // NULL 与空串
		r := openReader(t, nullKindsFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "kinds", []string{"s"})
		defer func() { _ = rs.Close() }()
		if !mustNext(t, rs) {
			t.Fatalf("row1 missing")
		}
		if v, err := rs.String(2); err != nil || v != "" {
			t.Fatalf("String(2) = (%q,%v); want (\"\",nil)", v, err)
		}
		if !mustNext(t, rs) {
			t.Fatalf("row2 missing")
		}
		if _, err := rs.String(2); !errors.Is(err, tsfile.ErrNullValue) {
			t.Fatalf("NULL String error = %v; want ErrNullValue", err)
		}
	})

	R(t, "NULL_与空字节串", func(t *testing.T) { // NULL 与空字节串
		r := openReader(t, nullKindsFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "kinds", []string{"bl"})
		defer func() { _ = rs.Close() }()
		if !mustNext(t, rs) {
			t.Fatalf("row1 missing")
		}
		b, err := rs.Bytes(2)
		wantNil(t, err)
		if len(b) != 0 {
			t.Fatalf("空字节串长度 = %d; want 0", len(b))
		}
		if !mustNext(t, rs) {
			t.Fatalf("row2 missing")
		}
		if _, err := rs.Bytes(2); !errors.Is(err, tsfile.ErrNullValue) {
			t.Fatalf("NULL Bytes error = %v; want ErrNullValue", err)
		}
	})
}

func TestConsumeModeExtra(t *testing.T) {
	R(t, "行模式结果集调用_ReadArrowBatch", func(t *testing.T) { // 行模式结果集调用 ReadArrowBatch
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		if _, err := rs.ReadArrowBatch(); !errors.Is(err, tsfile.ErrWrongResultMode) {
			t.Fatalf("ReadArrowBatch on row-mode = %v; want ErrWrongResultMode", err)
		}
	})

	R(t, "批次模式结果集调用_IsNull", func(t *testing.T) { // 批次模式结果集调用 IsNull
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(2))
		defer func() { _ = rs.Close() }()
		if _, err := rs.IsNull(2); !errors.Is(err, tsfile.ErrWrongResultMode) {
			t.Fatalf("IsNull on batch-mode = %v; want ErrWrongResultMode", err)
		}
	})

	R(t, "ReadArrowRecordBatch_返回对象内容", func(t *testing.T) { // ReadArrowRecordBatch 返回对象内容
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(2))
		defer func() { _ = rs.Close() }()
		rec, err := rs.ReadArrowRecordBatch()
		wantNil(t, err)
		defer rec.Release()
		fields := rec.Schema().Fields()
		if len(fields) != 2 || fields[0].Name != "time" || fields[1].Name != "value" {
			t.Fatalf("batch schema = %v; want [time value]", rec.Schema())
		}
		ts := rec.Column(0).(*array.Int64)
		if ts.Value(0) != 10 {
			t.Fatalf("batch 首行 time = %d; want 10", ts.Value(0))
		}
	})

	R(t, "ReadArrowBatch_返回对象", func(t *testing.T) { // ReadArrowBatch 返回对象
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(2))
		defer func() { _ = rs.Close() }()
		tbl, err := rs.ReadArrowBatch()
		wantNil(t, err)
		defer tbl.Release()
		if tbl.NumRows() == 0 {
			t.Fatalf("ReadArrowBatch 返回 0 行")
		}
		names := []string{}
		for _, f := range tbl.Schema().Fields() {
			names = append(names, f.Name)
		}
		if len(names) != 2 || names[0] != "time" || names[1] != "value" {
			t.Fatalf("table schema = %v; want [time value]", names)
		}
	})

	R(t, "结果集_Close_后读批次", func(t *testing.T) { // 结果集 Close 后读批次
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(2))
		wantNil(t, rs.Close())
		if _, err := rs.ReadArrowRecordBatch(); !errors.Is(err, tsfile.ErrClosed) {
			t.Fatalf("closed batch read error = %v; want ErrClosed", err)
		}
	})
	R(t, "跨批次分页_Offset_Limit_BatchSize", func(t *testing.T) {
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"},
			tsfile.WithOffset(1), tsfile.WithLimit(7), tsfile.WithBatchSize(2))
		defer func() { _ = rs.Close() }()
		batches, total := 0, 0
		var gotTS []int64
		for {
			rec, err := rs.ReadArrowRecordBatch()
			if err != nil {
				break
			}
			n := int(rec.NumRows())
			if n > 2 {
				t.Fatalf("单批行数 %d 超过 batchSize=2", n)
			}
			tsIdx := -1
			for i, f := range rec.Schema().Fields() {
				if f.Name == "time" {
					tsIdx = i
				}
			}
			if tsIdx < 0 {
				t.Fatalf("批 schema = %v; want 含 time 列", rec.Schema())
			}
			tsCol := rec.Column(tsIdx).(*array.Int64)
			for i := 0; i < n; i++ {
				gotTS = append(gotTS, tsCol.Value(i))
			}
			batches++
			total += n
			rec.Release()
		}
		// offset=1、limit=7 → 返回读回顺序的第 2~8 行，跨 4 批（2/2/2/1）
		want := bulkReadOrder(t)[1:min(8, bulkRows(t))]
		if total != len(want) || batches < 2 {
			t.Fatalf("实测 batches=%d total=%d; 期望 total=%d 且 batches>=2", batches, total, len(want))
		}
		if len(gotTS) != len(want) {
			t.Fatalf("时间戳个数 = %d; want %d", len(gotTS), len(want))
		}
		for i := range want {
			if gotTS[i] != want[i].ts {
				t.Fatalf("第 %d 行 ts = %d; want %d（实际 %v）", i+1, gotTS[i], want[i].ts, gotTS)
			}
		}
		t.Logf("跨批次分页实测：batches=%d total=%d", batches, total)
	})

}

func TestQueryCombosExtra(t *testing.T) {
	rowsOf := func(t *testing.T, opts ...tsfile.QueryOption) []rowValue {
		t.Helper()
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, opts...)
		defer func() { _ = rs.Close() }()
		return collectRows(t, rs)
	}

	R(t, "TimeRange_Offset", func(t *testing.T) { // TimeRange × Offset
		rows := rowsOf(t, tsfile.WithTimeRange(10, 30), tsfile.WithOffset(1))
		if len(rows) != 2 || rows[0].ts != 20 || rows[1].ts != 30 {
			t.Fatalf("rows = %+v; want ts=20,30", rows)
		}
	})

	R(t, "TimeRange_Limit", func(t *testing.T) { // TimeRange × Limit
		rows := rowsOf(t, tsfile.WithTimeRange(10, 30), tsfile.WithLimit(2))
		if len(rows) != 2 || rows[1].ts != 20 {
			t.Fatalf("rows = %+v; want ts=10,20", rows)
		}
	})

	R(t, "TagFilter_Offset", func(t *testing.T) { // TagFilter × Offset
		rows := rowsOf(t, tsfile.WithTagFilter(tsfile.Eq("device", "d1")), tsfile.WithOffset(1))
		if len(rows) != bulkRows(t)/2-1 || rows[0].ts != 20 || rows[1].ts != 50 {
			t.Fatalf("rows = %d 行（首两行 %v）; want %d 行（d1 过滤后跳过首行）", len(rows), rows[:2], bulkRows(t)/2-1)
		}
	})

	R(t, "TagFilter_Limit", func(t *testing.T) { // TagFilter × Limit
		rows := rowsOf(t, tsfile.WithTagFilter(tsfile.Eq("device", "d1")), tsfile.WithLimit(1))
		if len(rows) != 1 || rows[0].ts != 10 {
			t.Fatalf("rows = %+v; want ts=10", rows)
		}
	})

	R(t, "Offset_Limit", func(t *testing.T) { // Offset × Limit
		rows := rowsOf(t, tsfile.WithOffset(1), tsfile.WithLimit(2))
		if len(rows) != 2 || rows[0].ts != 20 || rows[1].ts != 50 {
			t.Fatalf("rows = %+v; want ts=20,50（读回按 TAG 分组）", rows)
		}
	})

	R(t, "TimeRange_TagFilter_Offset", func(t *testing.T) { // TimeRange × TagFilter × Offset
		rows := rowsOf(t, tsfile.WithTimeRange(10, 40), tsfile.WithTagFilter(tsfile.Eq("device", "d1")), tsfile.WithOffset(1))
		if len(rows) != 1 || rows[0].ts != 20 {
			t.Fatalf("rows = %+v; want ts=20", rows)
		}
	})

	R(t, "TimeRange_TagFilter_Limit", func(t *testing.T) { // TimeRange × TagFilter × Limit
		rows := rowsOf(t, tsfile.WithTimeRange(10, 40), tsfile.WithTagFilter(tsfile.Eq("device", "d1")), tsfile.WithLimit(1))
		if len(rows) != 1 || rows[0].ts != 10 {
			t.Fatalf("rows = %+v; want ts=10", rows)
		}
	})

	R(t, "TimeRange_Offset_Limit", func(t *testing.T) { // TimeRange × Offset × Limit
		rows := rowsOf(t, tsfile.WithTimeRange(10, 40), tsfile.WithOffset(1), tsfile.WithLimit(2))
		if len(rows) != 2 || rows[0].ts != 20 || rows[1].ts != 30 {
			t.Fatalf("rows = %+v; want ts=20,30", rows)
		}
	})

	R(t, "TagFilter_Offset_Limit", func(t *testing.T) { // TagFilter × Offset × Limit
		rows := rowsOf(t, tsfile.WithTagFilter(tsfile.Neq("device", "d1")), tsfile.WithOffset(1), tsfile.WithLimit(1))
		if len(rows) != 1 || rows[0].ts != 70 {
			t.Fatalf("rows = %+v; want ts=70（d2 块 ts=30,70…，跳过首行）", rows)
		}
	})

	R(t, "全条件组合到空结果", func(t *testing.T) { // 全条件组合到空结果
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"},
			tsfile.WithTimeRange(10, 20),
			tsfile.WithTagFilter(tsfile.Eq("device", "d3")),
			tsfile.WithOffset(0), tsfile.WithLimit(5), tsfile.WithBatchSize(2))
		defer func() { _ = rs.Close() }()
		total := 0
		for {
			rec, err := rs.ReadArrowRecordBatch()
			if errors.Is(err, io.EOF) {
				break
			}
			wantNil(t, err)
			total += int(rec.NumRows())
			rec.Release()
		}
		if total != 0 {
			t.Fatalf("rows = %d; want 0（时间 10..20 ∩ d3 为空）", total)
		}
	})
}

func TestCloseSemanticsExtra(t *testing.T) {
	R(t, "ResultSet_重复_Close", func(t *testing.T) { // ResultSet 重复 Close
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		wantNil(t, rs.Close())
		wantNil(t, rs.Close())
	})

	R(t, "Reader_重复_Close", func(t *testing.T) { // Reader 重复 Close
		r := openReader(t, sampleFile(t))
		wantNil(t, r.Close())
		wantNil(t, r.Close())
	})

	R(t, "Tablet_重复_Close", func(t *testing.T) { // Tablet 重复 Close
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{{Name: "v", DataType: tsfile.DataTypeDouble}}, 1)
		wantNil(t, err)
		wantNil(t, tab.Close())
		wantNil(t, tab.Close())
	})

	R(t, "Writer_重复_Close", func(t *testing.T) { // Writer 重复 Close
		path := tmpPath(t, "case307.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		writeBulkRows(t, w)
		wantNil(t, w.Close())
		wantNil(t, w.Close())
		wantBulkRowsReadBack(t, path)
	})

	R(t, "Reader_Close_后已交付_Arrow_对象仍可释放", func(t *testing.T) { // Reader.Close 后已交付 Arrow 对象仍可释放
		r := openReader(t, sampleFile(t))
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(2))
		rec, err := rs.ReadArrowRecordBatch()
		wantNil(t, err)
		before := int(rec.NumRows())
		wantNil(t, r.Close())
		after := int(rec.NumRows())
		if before != after {
			t.Fatalf("Reader.Close 后对象行数变化: %d → %d", before, after)
		}
		rec.Release() // 调用方仍可释放
		_ = rs.Close()
	})
}

func TestErrorModelExtra(t *testing.T) {
	R(t, "ErrTableNotExist", func(t *testing.T) { // ErrTableNotExist
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("nope", []string{"value"})
		if rs != nil {
			_ = rs.Close()
		}
		if !errors.Is(err, tsfile.ErrTableNotExist) {
			t.Fatalf("err = %v; want ErrTableNotExist", err)
		}
	})

	R(t, "ErrColumnNotExist", func(t *testing.T) { // ErrColumnNotExist
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("metrics", []string{"nope"})
		if rs != nil {
			_ = rs.Close()
		}
		if !errors.Is(err, tsfile.ErrColumnNotExist) {
			t.Fatalf("err = %v; want ErrColumnNotExist", err)
		}
	})

	R(t, "ErrInvalidSchema", func(t *testing.T) { // ErrInvalidSchema
		bad := metricsSchema()
		bad.Columns = nil
		w, err := tsfile.NewWriter(tmpPath(t, "case316.tsfile"), bad)
		if w != nil {
			_ = w.Close()
		}
		if !errors.Is(err, tsfile.ErrInvalidSchema) {
			t.Fatalf("err = %v; want ErrInvalidSchema", err)
		}
	})

	R(t, "ErrOverflow", func(t *testing.T) { // ErrOverflow
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{{Name: "v", DataType: tsfile.DataTypeDouble}}, 1<<30)
		if tab != nil {
			_ = tab.Close()
		}
		if !errors.Is(err, tsfile.ErrOverflow) {
			t.Fatalf("err = %v; want ErrOverflow", err)
		}
	})

	R(t, "ErrTypeMismatch", func(t *testing.T) { // ErrTypeMismatch
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "v", DataType: tsfile.DataTypeDouble},
			{Name: "b", DataType: tsfile.DataTypeBoolean},
		}, 1)
		wantNil(t, err)
		defer func() { _ = tab.Close() }()
		if err := tab.SetBool(0, 0, true); !errors.Is(err, tsfile.ErrTypeMismatch) {
			t.Fatalf("err = %v; want ErrTypeMismatch", err)
		}
	})

	R(t, "ErrInvalidPath", func(t *testing.T) { // ErrInvalidPath
		r, err := tsfile.NewReader(tmpPath(t, "no-such-file.tsfile"))
		if r != nil {
			_ = r.Close()
		}
		if !errors.Is(err, tsfile.ErrInvalidPath) && !errors.Is(err, tsfile.ErrFileOpen) {
			t.Fatalf("err = %v; want ErrInvalidPath 或 ErrFileOpen", err)
		}
	})
}

func TestConcurrentReaders(t *testing.T) {
	R(t, "多_goroutine_并发读_-race_无告警且结果正确", func(t *testing.T) { // 多 goroutine 并发读（-race 无告警且结果正确）
		path := sampleFile(t)
		wantByTS := map[int64]rowValue{}
		for _, w := range bulkSample(t) {
			wantByTS[w.ts] = rowValue{ts: w.ts, value: w.value, null: w.isNull}
		}
		const workers = 8
		var wg sync.WaitGroup
		errs := make(chan error, workers)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, err := tsfile.NewReader(path)
				if err != nil {
					errs <- err
					return
				}
				defer func() { _ = r.Close() }()
				rs, err := r.Query("metrics", []string{"value"})
				if err != nil {
					errs <- err
					return
				}
				defer func() { _ = rs.Close() }()
				n := 0
				for {
					ok, err := rs.Next()
					if err != nil {
						errs <- err
						return
					}
					if !ok {
						break
					}
					ts, err := rs.Int64(1)
					if err != nil {
						errs <- err
						return
					}
					null, err := rs.IsNull(2)
					if err != nil {
						errs <- err
						return
					}
					want, ok := wantByTS[ts]
					if !ok {
						errs <- fmt.Errorf("并发读：出现非样本时间戳 ts=%d", ts)
						return
					}
					if null != want.null {
						errs <- fmt.Errorf("并发读：ts=%d null=%v; want %v", ts, null, want.null)
						return
					}
					if !want.null {
						v, err := rs.Float64(2)
						if err != nil {
							errs <- err
							return
						}
						if v != want.value {
							errs <- fmt.Errorf("并发读：ts=%d value=%v; want %v", ts, v, want.value)
							return
						}
					}
					n++
				}
				if n != bulkRows(t) {
					errs <- fmt.Errorf("并发读行数不为 %d", bulkRows(t))
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("并发读失败: %v", err)
		}
	})
}

var _ = io.EOF
