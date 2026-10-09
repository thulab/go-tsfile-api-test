package tsuite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	tsfile "github.com/apache/tsfile/go/tsfile"
)

func TestNewWriterPathForms(t *testing.T) {
	R(t, "path_含中文路径", func(t *testing.T) { // path=含中文路径
		path := tmpPath(t, "中文目录/指标.tsfile")
		wantNil(t, os.MkdirAll(filepath.Dir(path), 0o755))
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		writeBulkRows(t, w)
		wantNil(t, w.Close())
		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		if _, err := r.GetTableSchema("metrics"); err != nil {
			t.Fatalf("中文路径文件不可读: %v", err)
		}
	})

	R(t, "path_含空格路径", func(t *testing.T) { // path=含空格路径
		path := tmpPath(t, "with space/metrics file.tsfile")
		wantNil(t, os.MkdirAll(filepath.Dir(path), 0o755))
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		writeBulkRows(t, w)
		wantNil(t, w.Close())
		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		if _, err := r.GetTableSchema("metrics"); err != nil {
			t.Fatalf("含空格路径文件不可读: %v", err)
		}
	})

	R(t, "path_目录路径", func(t *testing.T) { // path=目录路径
		dir := t.TempDir()
		w, err := tsfile.NewWriter(dir, metricsSchema())
		if w != nil {
			_ = w.Close()
		}
		wantErr(t, err)
		t.Logf("note: 目录路径实际错误 = %v", err)
	})
}

func TestNewWriterSchemaForms(t *testing.T) {
	R(t, "schema_Table_大小写混合", func(t *testing.T) { // schema.Table=大小写混合
		path := tmpPath(t, "case11.tsfile")
		s := metricsSchema()
		s.Table = "MeTrIcS"
		w, err := tsfile.NewWriter(path, s)
		wantNil(t, err)
		writeBulkRows(t, w)
		wantNil(t, w.Close())
		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		if _, err := r.GetTableSchema("metrics"); err != nil {
			t.Fatalf("混合大小写建表后按小写读取失败: %v", err)
		}
	})

	R(t, "schema_Columns_单列_FIELD", func(t *testing.T) { // schema.Columns=单列 FIELD
		s := tsfile.TableSchema{Table: "single", Columns: []tsfile.ColumnSchema{
			{Name: "value", DataType: tsfile.DataTypeDouble, Category: tsfile.ColumnCategoryField},
		}}
		path := tmpPath(t, "case14.tsfile")
		w, err := tsfile.NewWriter(path, s)
		wantNil(t, err)
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{{Name: "value", DataType: tsfile.DataTypeDouble}}, 1)
		wantNil(t, err)
		defer func() { _ = tab.Close() }()
		wantNil(t, tab.AddTimestamp(0, 10))
		wantNil(t, tab.SetFloat64(0, 0, 1.0))
		wantNil(t, w.WriteTableTablet(tab))
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "single", []string{"value"})
		defer func() { _ = rs.Close() }()
		rows := collectRows(t, rs)
		if len(rows) != 1 || rows[0].ts != 10 || rows[0].null || rows[0].value != 1.0 {
			t.Fatalf("单列 FIELD 表读回 = %+v; want 1 行 ts=10 value=1.0", rows)
		}
	})

	R(t, "DataType_越界_99", func(t *testing.T) { // DataType=越界(99)
		s := metricsSchema()
		s.Columns = append(s.Columns, tsfile.ColumnSchema{Name: "bad", DataType: tsfile.DataType(99), Category: tsfile.ColumnCategoryField})
		w, err := tsfile.NewWriter(tmpPath(t, "case23.tsfile"), s)
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidSchema)
	})

	R(t, "DataType_负数_-1", func(t *testing.T) { // DataType=负数(-1)
		s := metricsSchema()
		s.Columns = append(s.Columns, tsfile.ColumnSchema{Name: "bad", DataType: tsfile.DataType(-1), Category: tsfile.ColumnCategoryField})
		w, err := tsfile.NewWriter(tmpPath(t, "case24.tsfile"), s)
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidSchema)
	})

	R(t, "Category_非法_2", func(t *testing.T) { // Category=非法(2)
		s := metricsSchema()
		s.Columns = append(s.Columns, tsfile.ColumnSchema{Name: "bad", DataType: tsfile.DataTypeDouble, Category: tsfile.ColumnCategory(2)})
		w, err := tsfile.NewWriter(tmpPath(t, "case25.tsfile"), s)
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidSchema)
	})

	R(t, "Category_-1", func(t *testing.T) { // Category=-1
		s := metricsSchema()
		s.Columns = append(s.Columns, tsfile.ColumnSchema{Name: "bad", DataType: tsfile.DataTypeDouble, Category: tsfile.ColumnCategory(-1)})
		w, err := tsfile.NewWriter(tmpPath(t, "case26.tsfile"), s)
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidSchema)
	})
}

func TestNewWriterOptionsAndPaths(t *testing.T) {
	R(t, "WithMemoryThreshold_1", func(t *testing.T) { // WithMemoryThreshold(1)
		path := tmpPath(t, "case29.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema(), tsfile.WithMemoryThreshold(1))
		wantNil(t, err)
		writeBulkRows(t, w)
		wantNil(t, w.Close())
		wantBulkRowsReadBack(t, path)
	})

	R(t, "WithMemoryThreshold_128MiB", func(t *testing.T) { // WithMemoryThreshold(128MiB)
		path := tmpPath(t, "case30.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema(), tsfile.WithMemoryThreshold(128<<20))
		wantNil(t, err)
		writeBulkRows(t, w)
		wantNil(t, w.Close())
		wantBulkRowsReadBack(t, path)
	})

	R(t, "同名文件已存在_截断覆盖", func(t *testing.T) { // 同名文件已存在 → 截断覆盖
		path := tmpPath(t, "case36.tsfile")
		w1, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		writeBulkRows(t, w1)
		wantNil(t, w1.Close())

		w2, err := tsfile.NewWriter(path, metricsSchema()) // 同名再写
		wantNil(t, err)
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: "value", DataType: tsfile.DataTypeDouble},
		}, 2)
		wantNil(t, err)
		defer func() { _ = tab.Close() }()
		for row, ts := range []int64{100, 200} {
			wantNil(t, tab.AddTimestamp(row, ts))
			wantNil(t, tab.SetString(row, 0, "d9"))
			wantNil(t, tab.SetFloat64(row, 1, float64(ts)))
		}
		wantNil(t, w2.WriteTableTablet(tab))
		wantNil(t, w2.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rows := collectRowsWithTag(t, r)
		if len(rows) != 2 || rows[0].ts != 100 || rows[0].device != "d9" || rows[0].value != 100 ||
			rows[1].ts != 200 || rows[1].device != "d9" || rows[1].value != 200 {
			t.Fatalf("rows = %+v; 期望同名文件被截断只剩新写入的 2 行(100/d9,200/d9)", rows)
		}
	})

	R(t, "非法_schema_失败后再构造合法_Writer", func(t *testing.T) { // 非法 schema 失败后再构造合法 Writer
		bad := metricsSchema()
		bad.Columns[0].Category = tsfile.ColumnCategory(2)
		w, err := tsfile.NewWriter(tmpPath(t, "case37-bad.tsfile"), bad)
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidSchema)

		good, err := tsfile.NewWriter(tmpPath(t, "case37-good.tsfile"), metricsSchema())
		wantNil(t, err)
		writeBulkRows(t, good)
		wantNil(t, good.Close())
	})
}

func TestWriteTableTabletMismatch(t *testing.T) {
	newWriter := func(t *testing.T, name string) *tsfile.Writer {
		t.Helper()
		w, err := tsfile.NewWriter(tmpPath(t, name), metricsSchema())
		wantNil(t, err)
		return w
	}

	R(t, "Tablet_列数少于表结构", func(t *testing.T) { // Tablet 列数少于表结构
		w := newWriter(t, "case123.tsfile")
		defer func() { _ = w.Close() }()
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{{Name: "device", DataType: tsfile.DataTypeString}}, 1)
		wantNil(t, err)
		defer func() { _ = tab.Close() }()
		wantNil(t, tab.AddTimestamp(0, 10))
		wantNil(t, tab.SetString(0, 0, "d1"))
		wantIs(t, w.WriteTableTablet(tab), tsfile.ErrInvalidSchema)
	})

	R(t, "Tablet_列名与表结构不同", func(t *testing.T) { // Tablet 列名与表结构不同
		w := newWriter(t, "case124.tsfile")
		defer func() { _ = w.Close() }()
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: "other", DataType: tsfile.DataTypeDouble},
		}, 1)
		wantNil(t, err)
		defer func() { _ = tab.Close() }()
		wantNil(t, tab.AddTimestamp(0, 10))
		wantNil(t, tab.SetString(0, 0, "d1"))
		wantNil(t, tab.SetFloat64(0, 1, 1.0))
		wantIs(t, w.WriteTableTablet(tab), tsfile.ErrColumnNotExist)
	})

	R(t, "Tablet_列序与表结构不同_待实测钉死", func(t *testing.T) { // Tablet 列序与表结构不同（待实测钉死）
		w := newWriter(t, "case125.tsfile")
		defer func() { _ = w.Close() }()
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "value", DataType: tsfile.DataTypeDouble},
			{Name: "device", DataType: tsfile.DataTypeString},
		}, 1)
		wantNil(t, err)
		defer func() { _ = tab.Close() }()
		wantNil(t, tab.AddTimestamp(0, 10))
		wantNil(t, tab.SetString(0, 1, "d1"))
		wantNil(t, tab.SetFloat64(0, 0, 1.0))
		err = w.WriteTableTablet(tab)
		t.Logf("note: 列序不同实测 err=%v（nil=按列名匹配成功；非 nil=报错），待研发确认口径", err)
	})

	R(t, "Tablet_列类型与表结构不同", func(t *testing.T) { // Tablet 列类型与表结构不同
		w := newWriter(t, "case126.tsfile")
		defer func() { _ = w.Close() }()
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: "value", DataType: tsfile.DataTypeInt64},
		}, 1)
		wantNil(t, err)
		defer func() { _ = tab.Close() }()
		wantNil(t, tab.AddTimestamp(0, 10))
		wantNil(t, tab.SetString(0, 0, "d1"))
		wantNil(t, tab.SetInt64(0, 1, 1))
		wantIs(t, w.WriteTableTablet(tab), tsfile.ErrTypeMismatch)
	})

	R(t, "tablet_nil", func(t *testing.T) { // tablet=nil
		w := newWriter(t, "case127.tsfile")
		defer func() { _ = w.Close() }()
		wantIs(t, w.WriteTableTablet(nil), tsfile.ErrInvalidArgument)
	})

	R(t, "Writer_关闭后调用", func(t *testing.T) { // Writer 关闭后调用
		w := newWriter(t, "case128.tsfile")
		wantNil(t, w.Close())
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: "value", DataType: tsfile.DataTypeDouble},
		}, 1)
		wantNil(t, err)
		defer func() { _ = tab.Close() }()
		wantNil(t, tab.AddTimestamp(0, 10))
		wantNil(t, tab.SetString(0, 0, "d1"))
		wantNil(t, tab.SetFloat64(0, 1, 1.0))
		wantIs(t, w.WriteTableTablet(tab), tsfile.ErrClosed)
	})
}

func TestWriteArrowBatchInputs(t *testing.T) {
	R(t, "data_nil_Record", func(t *testing.T) { // data=nil Record
		w, err := tsfile.NewWriter(tmpPath(t, "case130.tsfile"), metricsSchema())
		wantNil(t, err)
		defer func() { _ = w.Close() }()
		var rec arrow.Record
		wantIs(t, w.WriteArrowBatch(rec), tsfile.ErrInvalidArgument)
	})

	R(t, "data_nil_Table", func(t *testing.T) { // data=nil Table
		w, err := tsfile.NewWriter(tmpPath(t, "case131.tsfile"), metricsSchema())
		wantNil(t, err)
		defer func() { _ = w.Close() }()
		var tbl arrow.Table
		wantIs(t, w.WriteArrowBatch(tbl), tsfile.ErrInvalidArgument)
	})

	R(t, "data_非_Arrow_类型_int", func(t *testing.T) { // data=非 Arrow 类型（int）
		w, err := tsfile.NewWriter(tmpPath(t, "case132.tsfile"), metricsSchema())
		wantNil(t, err)
		defer func() { _ = w.Close() }()
		wantIs(t, w.WriteArrowBatch(42), tsfile.ErrInvalidArgument)
	})

	R(t, "Writer_关闭后调用", func(t *testing.T) { // Writer 关闭后调用
		w, err := tsfile.NewWriter(tmpPath(t, "case133.tsfile"), metricsSchema())
		wantNil(t, err)
		wantNil(t, w.Close())
		rec := buildRecord(t, arrow.NewSchema([]arrow.Field{
			{Name: "time", Type: arrow.PrimitiveTypes.Int64},
			{Name: "device", Type: arrow.BinaryTypes.String},
			{Name: "value", Type: arrow.PrimitiveTypes.Float64},
		}, nil), [][]any{{int64(10)}, {"d1"}, {1.0}})
		defer rec.Release()
		wantIs(t, w.WriteArrowBatch(rec), tsfile.ErrClosed)
	})
}

func TestWriteArrowBatchSchemaChecks(t *testing.T) {
	R(t, "schema_含重复列名", func(t *testing.T) { // schema 含重复列名
		w, err := tsfile.NewWriter(tmpPath(t, "case138.tsfile"), metricsSchema())
		wantNil(t, err)
		defer func() { _ = w.Close() }()
		rec := buildRecord(t, arrow.NewSchema([]arrow.Field{
			{Name: "time", Type: arrow.PrimitiveTypes.Int64},
			{Name: "device", Type: arrow.BinaryTypes.String},
			{Name: "device", Type: arrow.BinaryTypes.String},
		}, nil), [][]any{{int64(10)}, {"d1"}, {"d1"}})
		defer rec.Release()
		wantIs(t, w.WriteArrowBatch(rec), tsfile.ErrInvalidSchema)
	})

	R(t, "schema_含表结构外的列", func(t *testing.T) { // schema 含表结构外的列
		w, err := tsfile.NewWriter(tmpPath(t, "case139.tsfile"), metricsSchema())
		wantNil(t, err)
		defer func() { _ = w.Close() }()
		rec := buildRecord(t, arrow.NewSchema([]arrow.Field{
			{Name: "time", Type: arrow.PrimitiveTypes.Int64},
			{Name: "device", Type: arrow.BinaryTypes.String},
			{Name: "unknown", Type: arrow.PrimitiveTypes.Float64},
		}, nil), [][]any{{int64(10)}, {"d1"}, {1.0}})
		defer rec.Release()
		wantIs(t, w.WriteArrowBatch(rec), tsfile.ErrColumnNotExist)
	})

	R(t, "列类型与表结构不符", func(t *testing.T) { // 列类型与表结构不符
		w, err := tsfile.NewWriter(tmpPath(t, "case140.tsfile"), metricsSchema())
		wantNil(t, err)
		defer func() { _ = w.Close() }()
		rec := buildRecord(t, arrow.NewSchema([]arrow.Field{
			{Name: "time", Type: arrow.PrimitiveTypes.Int64},
			{Name: "device", Type: arrow.BinaryTypes.String},
			{Name: "value", Type: arrow.PrimitiveTypes.Int64}, // 表结构是 DOUBLE
		}, nil), [][]any{{int64(10)}, {"d1"}, {int64(1)}})
		defer rec.Release()
		wantIs(t, w.WriteArrowBatch(rec), tsfile.ErrTypeMismatch)
	})
}

func TestWriteArrowTableAndOwnership(t *testing.T) {
	R(t, "arrow_Table_写入等价性", func(t *testing.T) { // arrow.Table 写入等价性
		path := tmpPath(t, "case142.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		rec := buildRecordWithNull(t, arrow.NewSchema([]arrow.Field{
			{Name: "time", Type: arrow.PrimitiveTypes.Int64},
			{Name: "device", Type: arrow.BinaryTypes.String},
			{Name: "value", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
		}, nil), [][]any{{int64(10), int64(20)}, {"d1", "d1"}, {1.0, nil}})
		tbl := newTableFromRecord(t, rec)
		rec.Release()
		wantNil(t, w.WriteArrowBatch(tbl))
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rows := collectRowsWithTag(t, r)
		if len(rows) != 2 || rows[0].value != 1.0 || !rows[1].null || rows[0].device != "d1" || rows[1].device != "d1" {
			t.Fatalf("rows = %+v; 期望与 Record 写入等价（1.0 与 NULL）", rows)
		}
	})

	R(t, "输入对象所有权_写入后调用方_Release", func(t *testing.T) { // 输入对象所有权：写入后调用方 Release
		path := tmpPath(t, "case144.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		rows := bulkSample(t)
		times := make([]any, len(rows))
		devices := make([]any, len(rows))
		values := make([]any, len(rows))
		for i, row := range rows {
			times[i] = row.ts
			devices[i] = row.device
			if !row.isNull {
				values[i] = row.value
			}
		}
		rec := buildRecordWithNull(t, arrow.NewSchema([]arrow.Field{
			{Name: "time", Type: arrow.PrimitiveTypes.Int64},
			{Name: "device", Type: arrow.BinaryTypes.String},
			{Name: "value", Type: arrow.PrimitiveTypes.Float64},
		}, nil), [][]any{times, devices, values})
		wantNil(t, w.WriteArrowBatch(rec))
		tbl := newTableFromRecord(t, rec)
		rec.Release() // 调用方释放输入对象
		tbl.Release()
		wantNil(t, w.Close())
		wantBulkRowsReadBack(t, path)
	})
}

func TestWriteAddPropertyExtra(t *testing.T) {
	R(t, "value_含内嵌_NUL_按字节写入", func(t *testing.T) { // value=含内嵌 NUL（按字节写入）
		path := tmpPath(t, "case152.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		wantNil(t, w.AddProperty("k", []byte{0, 1, 2}))
		writeBulkRows(t, w)
		wantNil(t, w.Close())
		wantBulkRowsReadBack(t, path)
	})
}

func TestWriterFlush(t *testing.T) {
	R(t, "写入后_Flush", func(t *testing.T) { // 写入后 Flush
		path := tmpPath(t, "case155.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		writeBulkRows(t, w)
		wantNil(t, w.Flush())
		wantNil(t, w.Close())
		wantBulkRowsReadBack(t, path)
	})

	R(t, "重复_Flush", func(t *testing.T) { // 重复 Flush
		path := tmpPath(t, "case156.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		writeBulkRows(t, w)
		wantNil(t, w.Flush())
		wantNil(t, w.Flush())
		wantNil(t, w.Close())
		wantBulkRowsReadBack(t, path)
	})
}

func TestWriterCloseState(t *testing.T) {
	R(t, "重复_Close", func(t *testing.T) { // 重复 Close
		path := tmpPath(t, "case160.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		writeBulkRows(t, w)
		wantNil(t, w.Close())
		wantNil(t, w.Close())
		wantBulkRowsReadBack(t, path)
	})

	R(t, "关闭后_WriteTableTablet", func(t *testing.T) { // 关闭后 WriteTableTablet
		w, err := tsfile.NewWriter(tmpPath(t, "case161.tsfile"), metricsSchema())
		wantNil(t, err)
		wantNil(t, w.Close())
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: "value", DataType: tsfile.DataTypeDouble},
		}, 1)
		wantNil(t, err)
		defer func() { _ = tab.Close() }()
		wantIs(t, w.WriteTableTablet(tab), tsfile.ErrClosed)
	})

	R(t, "关闭后_AddProperty", func(t *testing.T) { // 关闭后 AddProperty
		w, err := tsfile.NewWriter(tmpPath(t, "case162.tsfile"), metricsSchema())
		wantNil(t, err)
		wantNil(t, w.Close())
		wantIs(t, w.AddProperty("k", []byte("v")), tsfile.ErrClosed)
	})
}
