package tsuite

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	tsfile "github.com/apache/tsfile/go/tsfile"
)

func TestNewWriter(t *testing.T) {
	R(t, "path_正常_ASCII_路径", func(t *testing.T) { // path=正常 ASCII 路径
		path := tmpPath(t, "ok.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		writeBulkRows(t, w)
		wantNil(t, w.Close())
		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		wantRowValues(t, collectRows(t, rs), bulkReadOrder(t))
	})

	R(t, "path_空字符串", func(t *testing.T) { // path=空字符串
		w, err := tsfile.NewWriter("", metricsSchema())
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})

	R(t, "path_含内嵌_NUL", func(t *testing.T) { // path=含内嵌 NUL
		w, err := tsfile.NewWriter("a\x00b.tsfile", metricsSchema())
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})

	R(t, "path_不存在的目录", func(t *testing.T) { // path=不存在的目录
		w, err := tsfile.NewWriter(tmpPath(t, "no-such-dir/deep/out.tsfile"), metricsSchema())
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidPath, tsfile.ErrFileOpen)
	})

	R(t, "schema_Table_合法名_大小写归一", func(t *testing.T) { // schema.Table=合法名（大小写归一）
		path := tmpPath(t, "case10.tsfile")
		s := metricsSchema()
		s.Table = "Metrics"
		w, err := tsfile.NewWriter(path, s)
		wantNil(t, err)
		writeBulkRows(t, w)
		wantNil(t, w.Close())
		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		for _, name := range []string{"metrics", "METRICS", "Metrics"} {
			if _, err := r.GetTableSchema(name); err != nil {
				t.Fatalf("GetTableSchema(%q) = %v; want nil", name, err)
			}
		}
		rs := mustQuery(t, r, "mEtRiCs", []string{"value"})
		defer func() { _ = rs.Close() }()
		if rows := collectRows(t, rs); len(rows) != bulkRows(t) {
			t.Fatalf("query by mixed-case table returned %d rows; want %d", len(rows), bulkRows(t))
		}
	})

	R(t, "schema_Table_空字符串", func(t *testing.T) { // schema.Table=空字符串
		s := metricsSchema()
		s.Table = ""
		w, err := tsfile.NewWriter(tmpPath(t, "case12.tsfile"), s)
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidSchema)
	})

	R(t, "schema_Table_含内嵌_NUL", func(t *testing.T) { // schema.Table=含内嵌 NUL
		s := metricsSchema()
		s.Table = "met\x00rics"
		w, err := tsfile.NewWriter(tmpPath(t, "case13.tsfile"), s)
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidSchema)
	})

	R(t, "schema_Columns_1_TAG_十类_FIELD", func(t *testing.T) { // schema.Columns=1 TAG + 十类 FIELD
		cols := []tsfile.ColumnSchema{
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
		w, err := tsfile.NewWriter(tmpPath(t, "case15.tsfile"), tsfile.TableSchema{Table: "wide", Columns: cols})
		wantNil(t, err)
		wantNil(t, w.Close())
	})

	R(t, "schema_Columns_空数组", func(t *testing.T) { // schema.Columns=空数组
		w, err := tsfile.NewWriter(tmpPath(t, "case16.tsfile"), tsfile.TableSchema{Table: "metrics"})
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidSchema)
	})

	R(t, "某列名为空串", func(t *testing.T) { // 某列名为空串
		s := metricsSchema()
		s.Columns[1].Name = ""
		w, err := tsfile.NewWriter(tmpPath(t, "case17.tsfile"), s)
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidSchema)
	})

	R(t, "列名含_NUL", func(t *testing.T) { // 列名含 NUL
		s := metricsSchema()
		s.Columns[1].Name = "va\x00lue"
		w, err := tsfile.NewWriter(tmpPath(t, "case18.tsfile"), s)
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidSchema)
	})

	R(t, "重复列名_同大小写", func(t *testing.T) { // 重复列名（同大小写）
		s := metricsSchema()
		s.Columns = append(s.Columns, tsfile.ColumnSchema{Name: "value", DataType: tsfile.DataTypeInt64, Category: tsfile.ColumnCategoryField})
		w, err := tsfile.NewWriter(tmpPath(t, "case19.tsfile"), s)
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidSchema)
	})

	R(t, "重复列名_大小写不同", func(t *testing.T) { // 重复列名（大小写不同）
		s := metricsSchema()
		s.Columns = append(s.Columns, tsfile.ColumnSchema{Name: "VALUE", DataType: tsfile.DataTypeInt64, Category: tsfile.ColumnCategoryField})
		w, err := tsfile.NewWriter(tmpPath(t, "case20.tsfile"), s)
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidSchema)
	})

	R(t, "DataType_VECTOR_6", func(t *testing.T) { // DataType=VECTOR(6)
		s := metricsSchema()
		s.Columns = append(s.Columns, tsfile.ColumnSchema{Name: "vec", DataType: tsfile.DataType(6), Category: tsfile.ColumnCategoryField})
		w, err := tsfile.NewWriter(tmpPath(t, "case21.tsfile"), s)
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidSchema)
	})

	R(t, "DataType_NULL_TYPE_7", func(t *testing.T) { // DataType=NULL_TYPE(7)
		s := metricsSchema()
		s.Columns = append(s.Columns, tsfile.ColumnSchema{Name: "nt", DataType: tsfile.DataType(7), Category: tsfile.ColumnCategoryField})
		w, err := tsfile.NewWriter(tmpPath(t, "case22.tsfile"), s)
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidSchema)
	})

	R(t, "TAG_列用_INT32", func(t *testing.T) { // TAG 列用 INT32
		s := metricsSchema()
		s.Columns[0] = tsfile.ColumnSchema{Name: "device", DataType: tsfile.DataTypeInt32, Category: tsfile.ColumnCategoryTag}
		w, err := tsfile.NewWriter(tmpPath(t, "case27.tsfile"), s)
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidSchema)
	})

	R(t, "options_不传_默认阈值", func(t *testing.T) { // options=不传（默认阈值）
		path := tmpPath(t, "case28.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		writeBulkRows(t, w)
		wantNil(t, w.Close())
		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		if rows := collectRows(t, rs); len(rows) != bulkRows(t) {
			t.Fatalf("rows = %d; want %d", len(rows), bulkRows(t))
		}
	})

	R(t, "WithMemoryThreshold_0", func(t *testing.T) { // WithMemoryThreshold(0)
		w, err := tsfile.NewWriter(tmpPath(t, "case32.tsfile"), metricsSchema(), tsfile.WithMemoryThreshold(0))
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})

	R(t, "options_含_nil", func(t *testing.T) { // options 含 nil
		w, err := tsfile.NewWriter(tmpPath(t, "case33.tsfile"), metricsSchema(), nil)
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})

	R(t, "构造后修改原_TableSchema_副本语义", func(t *testing.T) { // 构造后修改原 TableSchema（副本语义）
		path := tmpPath(t, "case35.tsfile")
		s := metricsSchema()
		w, err := tsfile.NewWriter(path, s)
		wantNil(t, err)
		s.Table = "hacked"
		s.Columns[1].Name = "renamed"
		s.Columns[1].DataType = tsfile.DataTypeInt64
		writeBulkRows(t, w)
		wantNil(t, w.Close())
		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		got, err := r.GetTableSchema("metrics")
		wantNil(t, err)
		if got.Table != "metrics" {
			t.Fatalf("table = %q; want metrics (writer must keep its own copy)", got.Table)
		}
		names := make([]string, 0, len(got.Columns))
		for _, c := range got.Columns {
			names = append(names, c.Name)
		}
		if len(names) != 2 || names[0] != "device" || names[1] != "value" {
			t.Fatalf("columns = %v; want [device value]", names)
		}
		if got.Columns[1].DataType != tsfile.DataTypeDouble {
			t.Fatalf("value type = %v; want DOUBLE", got.Columns[1].DataType)
		}
	})
}

func TestWriteTableTablet(t *testing.T) {
	R(t, "3_行含_null_往返", func(t *testing.T) { // 3 行含 null 往返
		path := tmpPath(t, "case122.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		tablet, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: "value", DataType: tsfile.DataTypeDouble},
		}, 3)
		wantNil(t, err)
		defer func() { _ = tablet.Close() }()
		wantNil(t, tablet.AddTimestamp(0, 10))
		wantNil(t, tablet.SetString(0, 0, "d1"))
		wantNil(t, tablet.SetFloat64(0, 1, 1.5))
		wantNil(t, tablet.AddTimestamp(1, 20))
		wantNil(t, tablet.SetString(1, 0, "d1"))
		wantNil(t, tablet.SetFloat64(1, 1, 2.5))
		wantNil(t, tablet.AddTimestamp(2, 30))
		wantNil(t, tablet.SetString(2, 0, "d2"))
		// row 2 的 value 故意不设置 → NULL
		wantNil(t, w.WriteTableTablet(tablet))
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rows := collectRowsWithTag(t, r)
		if len(rows) != 3 {
			t.Fatalf("rows = %d; want 3", len(rows))
		}
		if rows[0].ts != 10 || rows[0].device != "d1" || rows[0].value != 1.5 ||
			rows[1].ts != 20 || rows[1].device != "d1" || rows[1].value != 2.5 {
			t.Fatalf("rows[0:2] = %+v; want (10,d1,1.5) and (20,d1,2.5)", rows[0:2])
		}
		if rows[2].ts != 30 || rows[2].device != "d2" || !rows[2].null {
			t.Fatalf("rows[2] = %+v; want ts=30 device=d2 NULL", rows[2])
		}
	})
}

func TestWriteArrowBatch(t *testing.T) {
	R(t, "Arrow_列数_表列数_缺_time_列", func(t *testing.T) { // Arrow 列数=表列数（缺 time 列）
		w, err := tsfile.NewWriter(tmpPath(t, "case134.tsfile"), metricsSchema())
		wantNil(t, err)
		defer func() { _ = w.Close() }()
		rec := buildRecord(t, arrow.NewSchema([]arrow.Field{
			{Name: "device", Type: arrow.BinaryTypes.String},
			{Name: "value", Type: arrow.PrimitiveTypes.Float64},
		}, nil), [][]any{{"d1"}, {1.0}})
		defer rec.Release()
		wantIs(t, w.WriteArrowBatch(rec), tsfile.ErrInvalidSchema)
	})

	R(t, "Arrow_列数_表列数_1", func(t *testing.T) { // Arrow 列数 > 表列数+1
		w, err := tsfile.NewWriter(tmpPath(t, "case135.tsfile"), metricsSchema())
		wantNil(t, err)
		defer func() { _ = w.Close() }()
		rec := buildRecord(t, arrow.NewSchema([]arrow.Field{
			{Name: "time", Type: arrow.PrimitiveTypes.Int64},
			{Name: "device", Type: arrow.BinaryTypes.String},
			{Name: "value", Type: arrow.PrimitiveTypes.Float64},
			{Name: "extra", Type: arrow.PrimitiveTypes.Int64},
		}, nil), [][]any{{int64(10)}, {"d1"}, {1.0}, {int64(1)}})
		defer rec.Release()
		wantIs(t, w.WriteArrowBatch(rec), tsfile.ErrInvalidSchema)
	})

	R(t, "time_列类型为_string", func(t *testing.T) { // time 列类型为 string
		w, err := tsfile.NewWriter(tmpPath(t, "case136.tsfile"), metricsSchema())
		wantNil(t, err)
		defer func() { _ = w.Close() }()
		rec := buildRecord(t, arrow.NewSchema([]arrow.Field{
			{Name: "time", Type: arrow.BinaryTypes.String},
			{Name: "device", Type: arrow.BinaryTypes.String},
			{Name: "value", Type: arrow.PrimitiveTypes.Float64},
		}, nil), [][]any{{"10"}, {"d1"}, {1.0}})
		defer rec.Release()
		wantIs(t, w.WriteArrowBatch(rec), tsfile.ErrTypeMismatch)
	})

	R(t, "time_列含_null", func(t *testing.T) { // time 列含 null
		w, err := tsfile.NewWriter(tmpPath(t, "case137.tsfile"), metricsSchema())
		wantNil(t, err)
		defer func() { _ = w.Close() }()
		rec := buildRecordWithNull(t, arrow.NewSchema([]arrow.Field{
			{Name: "time", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "device", Type: arrow.BinaryTypes.String},
			{Name: "value", Type: arrow.PrimitiveTypes.Float64},
		}, nil), [][]any{{nil}, {"d1"}, {1.0}})
		defer rec.Release()
		wantIs(t, w.WriteArrowBatch(rec), tsfile.ErrInvalidArgument)
	})

	R(t, "Record_基本往返_含_1_个_null", func(t *testing.T) { // Record 基本往返（含 1 个 null）
		path := tmpPath(t, "case141.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		rec := buildRecordWithNull(t, arrow.NewSchema([]arrow.Field{
			{Name: "time", Type: arrow.PrimitiveTypes.Int64},
			{Name: "device", Type: arrow.BinaryTypes.String},
			{Name: "value", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
		}, nil), [][]any{{int64(10), int64(20)}, {"d1", "d1"}, {1.0, nil}})
		wantNil(t, w.WriteArrowBatch(rec))
		rec.Release()
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rows := collectRowsWithTag(t, r)
		if len(rows) != 2 || rows[0].value != 1.0 || !rows[1].null || rows[0].device != "d1" || rows[1].device != "d1" {
			t.Fatalf("rows = %+v; want [ts=10 value=1] and [ts=20 NULL]", rows)
		}
	})
}

func TestWriteAddProperty(t *testing.T) {
	R(t, "key_正常值", func(t *testing.T) { // key=正常值
		path := tmpPath(t, "case146.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		wantNil(t, w.AddProperty("owner", []byte("qa")))
		writeBulkRows(t, w)
		wantNil(t, w.Close())
		wantBulkRowsReadBack(t, path)
	})

	R(t, "key_空字符串", func(t *testing.T) { // key=空字符串
		w, err := tsfile.NewWriter(tmpPath(t, "case147.tsfile"), metricsSchema())
		wantNil(t, err)
		defer func() { _ = w.Close() }()
		wantIs(t, w.AddProperty("", []byte("v")), tsfile.ErrInvalidArgument)
	})

	R(t, "key_含内嵌_NUL_需求只约束非空_NUL_未约束_记录实测行为", func(t *testing.T) { // key=含内嵌 NUL（需求只约束"非空"，NUL 未约束 → 记录实测行为）
		path := tmpPath(t, "case148.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		err = w.AddProperty("k\x00ey", []byte("v"))
		t.Logf("note: 需求 §3.2 只要求 key/value 非空，未约束内嵌 NUL；实测 err=%v（nil=被接受，可能存在截断风险，待研发确认）", err)
		writeBulkRows(t, w)
		wantNil(t, w.Close())
		wantBulkRowsReadBack(t, path)
	})

	R(t, "value_正常值_SDK_无属性读取接口_以文件可正常打开验证", func(t *testing.T) { // value=正常值（SDK 无属性读取接口，以文件可正常打开验证）
		path := tmpPath(t, "case150.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		wantNil(t, w.AddProperty("k", []byte("v")))
		writeBulkRows(t, w)
		wantNil(t, w.Close())
		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		if _, err := r.GetTableSchema("metrics"); err != nil {
			t.Fatalf("file with property is not readable: %v", err)
		}
	})

	R(t, "value_空字节串", func(t *testing.T) { // value=空字节串
		w, err := tsfile.NewWriter(tmpPath(t, "case151.tsfile"), metricsSchema())
		wantNil(t, err)
		defer func() { _ = w.Close() }()
		wantIs(t, w.AddProperty("k", []byte{}), tsfile.ErrInvalidArgument)
	})
}

func TestWriterClose(t *testing.T) {
	R(t, "正常_Close_数据完整可读", func(t *testing.T) { // 正常 Close + 数据完整可读
		path := tmpPath(t, "case159.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		writeBulkRows(t, w)
		wantNil(t, w.Close())
		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		wantRowValues(t, collectRows(t, rs), bulkReadOrder(t))
	})
}

// buildRecord 用给定 schema 与列数据构造一个 Arrow RecordBatch（列数据等长）。
func buildRecord(t *testing.T, schema *arrow.Schema, cols [][]any) arrow.Record {
	t.Helper()
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	for col, values := range cols {
		for _, v := range values {
			appendValue(t, builder.Field(col), v)
		}
	}
	rec := builder.NewRecord()
	builder.Release()
	return rec
}

// buildRecordWithNull 与 buildRecord 相同，但 nil 值按空值写入。
func buildRecordWithNull(t *testing.T, schema *arrow.Schema, cols [][]any) arrow.Record {
	t.Helper()
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	for col, values := range cols {
		for _, v := range values {
			if v == nil {
				builder.Field(col).AppendNull()
				continue
			}
			appendValue(t, builder.Field(col), v)
		}
	}
	rec := builder.NewRecord()
	builder.Release()
	return rec
}

func appendValue(t *testing.T, b array.Builder, v any) {
	t.Helper()
	switch b := b.(type) {
	case *array.Int64Builder:
		b.Append(v.(int64))
	case *array.Float64Builder:
		b.Append(v.(float64))
	case *array.StringBuilder:
		b.Append(v.(string))
	default:
		t.Fatalf("unsupported arrow builder %T", b)
	}
}
