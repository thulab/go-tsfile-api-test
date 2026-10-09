package tsuite

import (
	"errors"
	"io"
	"testing"

	tsfile "github.com/apache/tsfile/go/tsfile"
)

func TestNewReader(t *testing.T) {
	R(t, "path_正常文件", func(t *testing.T) { // path=正常文件
		r := openReader(t, sampleFile(t))
		wantNil(t, r.Close())
	})

	R(t, "path_不存在文件", func(t *testing.T) { // path=不存在文件
		r, err := tsfile.NewReader(tmpPath(t, "missing.tsfile"))
		if r != nil {
			_ = r.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidPath, tsfile.ErrFileOpen)
	})

	R(t, "path_空字符串", func(t *testing.T) { // path=空字符串
		r, err := tsfile.NewReader("")
		if r != nil {
			_ = r.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})

	R(t, "path_含内嵌_NUL", func(t *testing.T) { // path=含内嵌 NUL
		r, err := tsfile.NewReader("a\x00b.tsfile")
		if r != nil {
			_ = r.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})
}

func TestQueryTable(t *testing.T) {
	R(t, "合法表名", func(t *testing.T) { // 合法表名
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		if rows := collectRows(t, rs); len(rows) != bulkRows(t) {
			t.Fatalf("rows = %d; want %d", len(rows), bulkRows(t))
		}
	})

	R(t, "不存在的表", func(t *testing.T) { // 不存在的表
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("no_such_table", []string{"value"})
		if rs != nil {
			_ = rs.Close()
		}
		wantIs(t, err, tsfile.ErrTableNotExist)
	})

	R(t, "空字符串", func(t *testing.T) { // 空字符串
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("", []string{"value"})
		if rs != nil {
			_ = rs.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})

	R(t, "含内嵌_NUL", func(t *testing.T) { // 含内嵌 NUL
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("met\x00rics", []string{"value"})
		if rs != nil {
			_ = rs.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})
}

func TestQueryColumns(t *testing.T) {
	R(t, "单列合法", func(t *testing.T) { // 单列合法
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		md := rs.Metadata()
		if len(md) != 2 || md[0].Name != "time" || md[1].Name != "value" {
			t.Fatalf("metadata = %+v; want [time value]", md)
		}
	})

	R(t, "不存在的列", func(t *testing.T) { // 不存在的列
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("metrics", []string{"nope"})
		if rs != nil {
			_ = rs.Close()
		}
		wantIs(t, err, tsfile.ErrColumnNotExist)
	})

	R(t, "空数组", func(t *testing.T) { // 空数组
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("metrics", nil)
		if rs != nil {
			_ = rs.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})

	R(t, "数组含空字符串", func(t *testing.T) { // 数组含空字符串
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("metrics", []string{"value", ""})
		if rs != nil {
			_ = rs.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})

	R(t, "数组含内嵌_NUL", func(t *testing.T) { // 数组含内嵌 NUL
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("metrics", []string{"va\x00lue"})
		if rs != nil {
			_ = rs.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})
}

func TestQueryOptions(t *testing.T) {
	R(t, "不传_option_全时段_无限行_行模式", func(t *testing.T) { // 不传 option：全时段、无限行、行模式
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		// 不传 option：全时段 + 无限行 → 读回全部样本行（行数与逐行值都按样本核对）
		wantRowValues(t, collectRows(t, rs), bulkReadOrder(t))
	})

	R(t, "options_含_nil", func(t *testing.T) { // options 含 nil
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("metrics", []string{"value"}, nil)
		if rs != nil {
			_ = rs.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})
}

func TestWithTimeRange(t *testing.T) {
	R(t, "不传时间范围_默认全时段", func(t *testing.T) { // 不传时间范围（默认全时段）
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		wantRowValues(t, collectRows(t, rs), bulkReadOrder(t))
	})

	R(t, "start_end_闭区间", func(t *testing.T) { // start<end：闭区间
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithTimeRange(10, 20))
		defer func() { _ = rs.Close() }()
		rows := collectRows(t, rs)
		if len(rows) != 2 || rows[0].ts != 10 || rows[1].ts != 20 {
			t.Fatalf("rows = %+v; want ts=10,20", rows)
		}
	})

		R(t, "start_end_倒置区间_空结果", func(t *testing.T) { // start>end → 空结果、无错误（产品文档口径：range is empty）
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("metrics", []string{"value"}, tsfile.WithTimeRange(20, 10))
		wantNil(t, err)
		if rs == nil {
			t.Fatalf("start>end 返回 nil 结果集；期望空结果集（无错误）")
		}
		defer func() { _ = rs.Close() }()
		if ok, err := rs.Next(); err != nil || ok {
			t.Fatalf("start>end Next() = %v, %v; want false, nil（空结果）", ok, err)
		}
	})

	R(t, "时间范围_TagFilter_交集", func(t *testing.T) { // 时间范围 × TagFilter（交集）
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"},
			tsfile.WithTimeRange(10, 20), tsfile.WithTagFilter(tsfile.Eq("device", "d1")))
		defer func() { _ = rs.Close() }()
		rows := collectRows(t, rs)
		if len(rows) != 2 {
			t.Fatalf("rows = %d; want 2 (time 10..20 ∩ device=d1)", len(rows))
		}
	})
}

func TestWithOffset(t *testing.T) {
	R(t, "offset_1_跳过首行", func(t *testing.T) { // offset=1 跳过首行
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithOffset(1))
		defer func() { _ = rs.Close() }()
		// 跳过读回顺序的第 1 行（d1 块 ts=10）后返回其余全部行
		wantRowValues(t, collectRows(t, rs), bulkReadOrder(t)[1:])
	})

	R(t, "offset_-1", func(t *testing.T) { // offset=-1
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("metrics", []string{"value"}, tsfile.WithOffset(-1))
		if rs != nil {
			_ = rs.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})
}

func TestWithLimit(t *testing.T) {
	R(t, "limit_1", func(t *testing.T) { // limit=1
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithLimit(1))
		defer func() { _ = rs.Close() }()
		rows := collectRows(t, rs)
		if len(rows) != 1 || rows[0].ts != 10 {
			t.Fatalf("rows = %+v; want single row ts=10", rows)
		}
	})

	R(t, "limit_0_空结果且无错误", func(t *testing.T) { // limit=0 → 空结果且无错误
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithLimit(0))
		defer func() { _ = rs.Close() }()
		if rows := collectRows(t, rs); len(rows) != 0 {
			t.Fatalf("rows = %d; want 0", len(rows))
		}
	})

	R(t, "limit_-1_不限", func(t *testing.T) { // limit=-1 → 不限
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithLimit(-1))
		defer func() { _ = rs.Close() }()
		if rows := collectRows(t, rs); len(rows) != bulkRows(t) {
			t.Fatalf("rows = %d; want %d", len(rows), bulkRows(t))
		}
	})
}

func TestOffsetAndLimit(t *testing.T) {
	R(t, "Offset_与_Limit_同时使用", func(t *testing.T) { // Offset 与 Limit 同时使用
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"},
			tsfile.WithOffset(1), tsfile.WithLimit(2))
		defer func() { _ = rs.Close() }()
		rows := collectRows(t, rs)
		if len(rows) != 2 || rows[0].ts != 20 || rows[1].ts != 50 {
			t.Fatalf("rows = %+v; want ts=20,50（读回按 TAG 分组：d1 块内 ts=10,20,50…）", rows)
		}
	})
}

func TestWithBatchSize(t *testing.T) {
	R(t, "batchSize_0_行模式可用", func(t *testing.T) { // batchSize=0 → 行模式可用
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(0))
		defer func() { _ = rs.Close() }()
		if rows := collectRows(t, rs); len(rows) != bulkRows(t) {
			t.Fatalf("rows = %d; want %d", len(rows), bulkRows(t))
		}
	})

	R(t, "批次循环到_io_EOF", func(t *testing.T) { // 批次循环到 io.EOF
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(2))
		defer func() { _ = rs.Close() }()
		batches, rows := 0, 0
		var sizes []int
		for {
			rec, err := rs.ReadArrowRecordBatch()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("ReadArrowRecordBatch = %v; want nil or io.EOF", err)
			}
			batches++
			n := int(rec.NumRows())
			if n > 2 {
				t.Fatalf("batch rows = %d; want <= batchSize(2)", n)
			}
			sizes = append(sizes, n)
			rows += n
			rec.Release()
		}
		if rows != bulkRows(t) || batches == 0 {
			t.Fatalf("batches=%d rows=%d; want rows=4 with >=1 batch", batches, rows)
		}
		t.Logf("note: batchSize=2 时实际批次切分为 %v（原生按数据块切分，batchSize 为上限）", sizes)
	})
}

func TestTagFilterConstruct(t *testing.T) {
	query := func(t *testing.T, filter tsfile.TagFilter) error {
		t.Helper()
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("metrics", []string{"value"}, tsfile.WithTagFilter(filter))
		if rs != nil {
			_ = rs.Close()
		}
		return err
	}

	R(t, "filter_nil", func(t *testing.T) { // filter=nil
		wantIs(t, query(t, nil), tsfile.ErrInvalidArgument)
	})

	R(t, "列名为空串", func(t *testing.T) { // 列名为空串
		wantIs(t, query(t, tsfile.Eq("", "d1")), tsfile.ErrInvalidArgument)
	})

	R(t, "列名含_NUL", func(t *testing.T) { // 列名含 NUL
		wantIs(t, query(t, tsfile.Eq("dev\x00ice", "d1")), tsfile.ErrInvalidArgument)
	})

	R(t, "值含_NUL", func(t *testing.T) { // 值含 NUL
		wantIs(t, query(t, tsfile.Eq("device", "d\x001")), tsfile.ErrInvalidArgument)
	})

	R(t, "Between_lower_upper", func(t *testing.T) { // Between(lower>upper)
		wantIs(t, query(t, tsfile.Between("device", "d3", "d1")), tsfile.ErrInvalidArgument)
	})
}

func TestTagFilterOperators(t *testing.T) {
	R(t, "Eq_命中", func(t *testing.T) { // Eq 命中
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithTagFilter(tsfile.Eq("device", "d1")))
		defer func() { _ = rs.Close() }()
		wantRowValues(t, collectRows(t, rs), bulkRowsOfDevices(t, "d1"))
	})

	R(t, "Neq", func(t *testing.T) { // Neq
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithTagFilter(tsfile.Neq("device", "d1")))
		defer func() { _ = rs.Close() }()
		wantRowValues(t, collectRows(t, rs), bulkRowsOfDevices(t, "d2", "d3"))
	})
}

func TestTagFilterInvalid(t *testing.T) {
	R(t, "对_FIELD_列构造_Eq", func(t *testing.T) { // 对 FIELD 列构造 Eq
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("metrics", []string{"value"}, tsfile.WithTagFilter(tsfile.Eq("value", "1.0")))
		if rs != nil {
			_ = rs.Close()
		}
		wantErr(t, err)
		t.Logf("note: FIELD 列 TagFilter 实际错误 = %v", err)
	})

	R(t, "不存在的列", func(t *testing.T) { // 不存在的列
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("metrics", []string{"value"}, tsfile.WithTagFilter(tsfile.Eq("nope", "x")))
		if rs != nil {
			_ = rs.Close()
		}
		wantIs(t, err, tsfile.ErrColumnNotExist)
	})
}

func TestGetTableSchema(t *testing.T) {
	R(t, "合法表名", func(t *testing.T) { // 合法表名
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		s, err := r.GetTableSchema("metrics")
		wantNil(t, err)
		if s.Table != "metrics" || len(s.Columns) != 2 {
			t.Fatalf("schema = %+v; want table=metrics with 2 columns", s)
		}
		if s.Columns[0].Name != "device" || s.Columns[0].Category != tsfile.ColumnCategoryTag {
			t.Fatalf("column0 = %+v; want device/TAG", s.Columns[0])
		}
		if s.Columns[1].Name != "value" || s.Columns[1].Category != tsfile.ColumnCategoryField ||
			s.Columns[1].DataType != tsfile.DataTypeDouble {
			t.Fatalf("column1 = %+v; want value/FIELD/DOUBLE", s.Columns[1])
		}
	})

	R(t, "不存在的表", func(t *testing.T) { // 不存在的表
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		_, err := r.GetTableSchema("no_such_table")
		wantIs(t, err, tsfile.ErrTableNotExist)
	})
}

func TestGetAllTableSchemas(t *testing.T) {}
