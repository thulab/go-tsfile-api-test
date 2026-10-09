package tsuite

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	tsfile "github.com/apache/tsfile/go/tsfile"
)

// 批次 5-B：值边界与大数据（RN-145 台账 112/113/116/119/143/145/181）

func singleFieldSchema(table, field string, dt tsfile.DataType) tsfile.TableSchema {
	return tsfile.TableSchema{Table: table, Columns: []tsfile.ColumnSchema{
		{Name: "device", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag},
		{Name: field, DataType: dt, Category: tsfile.ColumnCategoryField},
	}}
}

func writeOneFieldValue(t *testing.T, path string, schema tsfile.TableSchema, setValue func(*tsfile.Tablet)) {
	t.Helper()
	w, err := tsfile.NewWriter(path, schema)
	wantNil(t, err)
	tablet, err := tsfile.NewTablet([]tsfile.TabletColumn{
		{Name: "device", DataType: tsfile.DataTypeString},
		{Name: schema.Columns[1].Name, DataType: schema.Columns[1].DataType},
	}, 1)
	wantNil(t, err)
	defer func() { _ = tablet.Close() }()
	wantNil(t, tablet.AddTimestamp(0, 10))
	wantNil(t, tablet.SetString(0, 0, "d1"))
	setValue(tablet)
	wantNil(t, w.WriteTableTablet(tablet))
	wantNil(t, w.Close())
}

func TestTabletValueBoundsExtra(t *testing.T) {
	R(t, "SetFloat32_正负_Inf", func(t *testing.T) {
		schema := singleFieldSchema("fmetrics", "f", tsfile.DataTypeFloat)
		path := tmpPath(t, "float_inf.tsfile")
		w, err := tsfile.NewWriter(path, schema)
		wantNil(t, err)
		tablet, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: "f", DataType: tsfile.DataTypeFloat},
		}, 2)
		wantNil(t, err)
		defer func() { _ = tablet.Close() }()
		wantNil(t, tablet.AddTimestamp(0, 10))
		wantNil(t, tablet.SetString(0, 0, "d1"))
		wantNil(t, tablet.SetFloat32(0, 1, float32(math.Inf(1))))
		wantNil(t, tablet.AddTimestamp(1, 20))
		wantNil(t, tablet.SetString(1, 0, "d2"))
		wantNil(t, tablet.SetFloat32(1, 1, float32(math.Inf(-1))))
		wantNil(t, w.WriteTableTablet(tablet))
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "fmetrics", []string{"f"})
		defer func() { _ = rs.Close() }()
		if ok, err := rs.Next(); err != nil || !ok {
			t.Fatalf("Next = (%v,%v); want (true,nil)", ok, err)
		}
		v, err := rs.Float32(2)
		wantNil(t, err)
		if !math.IsInf(float64(v), 1) {
			t.Fatalf("第 1 行 = %v; want +Inf", v)
		}
		if ok, err := rs.Next(); err != nil || !ok {
			t.Fatalf("Next = (%v,%v); want (true,nil)", ok, err)
		}
		v, err = rs.Float32(2)
		wantNil(t, err)
		if !math.IsInf(float64(v), -1) {
			t.Fatalf("第 2 行 = %v; want -Inf", v)
		}
	})

	R(t, "SetFloat64_NaN", func(t *testing.T) {
		schema := singleFieldSchema("dmetrics", "v", tsfile.DataTypeDouble)
		path := tmpPath(t, "double_nan.tsfile")
		writeOneFieldValue(t, path, schema, func(tab *tsfile.Tablet) {
			wantNil(t, tab.SetFloat64(0, 1, math.NaN()))
		})
		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "dmetrics", []string{"v"})
		defer func() { _ = rs.Close() }()
		if ok, err := rs.Next(); err != nil || !ok {
			t.Fatalf("Next = (%v,%v); want (true,nil)", ok, err)
		}
		if null, err := rs.IsNull(2); err != nil || null {
			t.Fatalf("IsNull = (%v,%v); NaN 不是 NULL", null, err)
		}
		v, err := rs.Float64(2)
		wantNil(t, err)
		if !math.IsNaN(v) {
			t.Fatalf("重读值 = %v; want NaN", v)
		}
	})

	R(t, "SetString_超长_1_MiB", func(t *testing.T) {
		schema := singleFieldSchema("smetrics", "s", tsfile.DataTypeString)
		path := tmpPath(t, "string_1mib.tsfile")
		long := strings.Repeat("S", 1<<20)
		writeOneFieldValue(t, path, schema, func(tab *tsfile.Tablet) {
			wantNil(t, tab.SetString(0, 1, long))
		})
		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "smetrics", []string{"s"})
		defer func() { _ = rs.Close() }()
		if ok, err := rs.Next(); err != nil || !ok {
			t.Fatalf("Next = (%v,%v); want (true,nil)", ok, err)
		}
		got, err := rs.String(2)
		wantNil(t, err)
		if len(got) != len(long) || got != long {
			t.Fatalf("1MiB 字符串往返不一致：len=%d（want %d）、内容相等=%v", len(got), len(long), got == long)
		}
	})

	R(t, "SetBytes_超_MaxInt32_长度", func(t *testing.T) {
		tablet, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "b", DataType: tsfile.DataTypeBlob},
		}, 1)
		wantNil(t, err)
		defer func() { _ = tablet.Close() }()
		// 只构造超长 slice header（不做分配、不解引用）：Go 侧在拷贝前先做长度校验，
		// 因此这里验证的是 ABI 上界 [0, MaxInt32] 的拒绝路径。
		var dummy byte
		huge := unsafe.Slice(&dummy, math.MaxInt32+1)
		wantIs(t, tablet.SetBytes(0, 0, huge), tsfile.ErrOverflow)
	})
}

func TestWriteArrowBatchExtra(t *testing.T) {
	arrowSchema := func() *arrow.Schema {
		return arrow.NewSchema([]arrow.Field{
			{Name: "time", Type: arrow.PrimitiveTypes.Int64},
			{Name: "device", Type: arrow.BinaryTypes.String},
			{Name: "value", Type: arrow.PrimitiveTypes.Float64},
		}, nil)
	}

	R(t, "多记录_行数一致", func(t *testing.T) {
		rec1 := buildRecord(t, arrowSchema(), [][]any{{int64(10)}, {"d1"}, {1.0}})
		defer rec1.Release()
		rec2 := buildRecord(t, arrowSchema(), [][]any{{int64(20), int64(30)}, {"d1", "d2"}, {2.0, 3.0}})
		defer rec2.Release()
		tbl := array.NewTableFromRecords(arrowSchema(), []arrow.Record{rec1, rec2})
		defer tbl.Release()

		path := tmpPath(t, "arrow_multi_record.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		wantNil(t, w.WriteArrowBatch(tbl))
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		rows := collectRows(t, rs)
		if len(rows) != 3 || rows[0].ts != 10 || rows[1].ts != 20 || rows[2].ts != 30 {
			t.Fatalf("多记录写入后读回 = %+v; want ts=10,20,30", rows)
		}
	})

	R(t, "输入对象_Release_后数据已落盘", func(t *testing.T) {
		rec := buildRecord(t, arrowSchema(), [][]any{{int64(10)}, {"d1"}, {1.0}})
		path := tmpPath(t, "arrow_release.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		wantNil(t, w.WriteArrowBatch(rec))
		rec.Release() // 写入后由调用方释放输入对象
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		rows := collectRows(t, rs)
		if len(rows) != 1 || rows[0].ts != 10 || rows[0].value != 1.0 {
			t.Fatalf("输入对象 Release 后读回 = %+v; want 单行 ts=10 value=1.0", rows)
		}
	})
}

// 列数超 int32 上限：受环境限制无法构造（需 >2^31 个列名），本轮记为无法验证；
// 同时验证「列数很大但合法」的路径不崩溃（1000 列 → 列不存在错误）。
func TestQueryColumnsCountBound(t *testing.T) {
	R(t, "列数超_int32_上限", func(t *testing.T) {
		t.Logf("无法验证：构造 >2^31 个列名需要约 48GiB 指针数组 + 字符串，74 环境不具备；" +
			"静态判据：Go 侧把 len(columnNames) 直接转 uint32 交给 C ABI，未在 Go 侧做 int32 上界校验")
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		cols := make([]string, 1000)
		for i := range cols {
			cols[i] = fmt.Sprintf("c%04d", i)
		}
		if _, err := r.Query("metrics", cols); err == nil {
			t.Fatalf("1000 列（表内不存在）应返回错误")
		} else {
			t.Logf("1000 列实测错误：%v", err)
		}
	})
}
