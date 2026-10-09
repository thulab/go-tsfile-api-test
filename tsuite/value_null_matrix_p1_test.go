package tsuite

import (
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	tsfile "github.com/apache/tsfile/go/tsfile"
)

// 空值矩阵 / 值边界 / 宽表（用户 2026-09-23 要求：数据行数不得过少，统一 ≥1200 行，
// 可用 CASE_ROWS 覆盖）。读回按时间戳建表比对，不依赖行序（NULL TAG 行可能单独分组）。

const nullEvery = 3 // 每 3 行 1 行为 NULL（TAG 部分空值场景）

// ---------- 空值矩阵（Tablet） ----------

func writeNullMatrixN(t *testing.T, path string, rows int, tagNull, valNull func(row int) bool) {
	t.Helper()
	const batch = 1000
	w, err := tsfile.NewWriter(path, metricsSchema())
	wantNil(t, err)
	for from := 0; from < rows; from += batch {
		n := min(batch, rows-from)
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: "value", DataType: tsfile.DataTypeDouble},
		}, n)
		wantNil(t, err)
		for r := 0; r < n; r++ {
			row := from + r
			wantNil(t, tab.AddTimestamp(r, int64(row)))
			if !tagNull(row) {
				wantNil(t, tab.SetString(r, 0, deviceOf(row)))
			}
			if !valNull(row) {
				wantNil(t, tab.SetFloat64(r, 1, float64(row)))
			}
		}
		wantNil(t, w.WriteTableTablet(tab))
		_ = tab.Close()
	}
	wantNil(t, w.Close())
}

type nullStat struct {
	rows      int
	tagNulls  int
	valNulls  int
	firstTag  string
	lastValue float64
}

func readNullMatrixN(t *testing.T, path string, rows int) nullStat {
	t.Helper()
	r := openReader(t, path)
	defer func() { _ = r.Close() }()
	rs := mustQuery(t, r, "metrics", []string{"device", "value"})
	defer func() { _ = rs.Close() }()
	st := nullStat{}
	seen := map[int64]bool{}
	for {
		ok, err := rs.Next()
		wantNil(t, err)
		if !ok {
			break
		}
		ts, err := rs.Int64(1)
		wantNil(t, err)
		if seen[ts] {
			t.Fatalf("时间戳重复：%d", ts)
		}
		seen[ts] = true
		tagNull, err := rs.IsNull(2)
		wantNil(t, err)
		if tagNull {
			st.tagNulls++
		} else if ts == 0 {
			st.firstTag, err = rs.String(2)
			wantNil(t, err)
		}
		valNull, err := rs.IsNull(3)
		wantNil(t, err)
		if valNull {
			st.valNulls++
		} else if ts == int64(rows-1) {
			st.lastValue, err = rs.Float64(3)
			wantNil(t, err)
		}
		st.rows++
	}
	return st
}

func TestNullMatrixTablet(t *testing.T) {
	R(t, "TAG_列部分空值", func(t *testing.T) {
		rows := caseRows(t)
		path := tmpPath(t, "tag_partial_null.tsfile")
		writeNullMatrixN(t, path, rows, func(r int) bool { return r%nullEvery == 1 }, func(int) bool { return false })
		st := readNullMatrixN(t, path, rows)
		want := rows / nullEvery
		if st.rows != rows || st.tagNulls != want {
			t.Fatalf("rows=%d tagNulls=%d; want rows=%d tagNulls=%d", st.rows, st.tagNulls, rows, want)
		}
		if st.firstTag != deviceOf(0) {
			t.Fatalf("ts=0 的 TAG = %q; want %q", st.firstTag, deviceOf(0))
		}
		if st.lastValue != float64(rows-1) {
			t.Fatalf("ts=%d 的 value = %v; want %d", rows-1, st.lastValue, rows-1)
		}
	})

	R(t, "TAG_列全空值", func(t *testing.T) {
		rows := caseRows(t)
		path := tmpPath(t, "tag_all_null.tsfile")
		writeNullMatrixN(t, path, rows, func(int) bool { return true }, func(int) bool { return false })
		st := readNullMatrixN(t, path, rows)
		if st.rows != rows || st.tagNulls != rows {
			t.Fatalf("rows=%d tagNulls=%d; want 全 NULL（%d）", st.rows, st.tagNulls, rows)
		}
		if st.valNulls != 0 || st.lastValue != float64(rows-1) {
			t.Fatalf("TAG 全空时 FIELD 应全有值：valNulls=%d lastValue=%v", st.valNulls, st.lastValue)
		}
	})

	R(t, "FIELD_列全空值", func(t *testing.T) {
		rows := caseRows(t)
		path := tmpPath(t, "field_all_null.tsfile")
		writeNullMatrixN(t, path, rows, func(int) bool { return false }, func(int) bool { return true })
		st := readNullMatrixN(t, path, rows)
		if st.rows != rows || st.valNulls != rows {
			t.Fatalf("rows=%d valNulls=%d; want 全 NULL（%d）", st.rows, st.valNulls, rows)
		}
		if st.tagNulls != 0 || st.firstTag != deviceOf(0) {
			t.Fatalf("FIELD 全空时 TAG 应全有值：tagNulls=%d firstTag=%q", st.tagNulls, st.firstTag)
		}
	})

	R(t, "全部列全空值", func(t *testing.T) {
		rows := caseRows(t)
		path := tmpPath(t, "all_columns_null.tsfile")
		writeNullMatrixN(t, path, rows, func(int) bool { return true }, func(int) bool { return true })
		st := readNullMatrixN(t, path, rows)
		if st.rows != rows || st.tagNulls != rows || st.valNulls != rows {
			t.Fatalf("rows=%d tagNulls=%d valNulls=%d; want 全列 NULL 且行不被丢弃（%d）", st.rows, st.tagNulls, st.valNulls, rows)
		}
	})
}

// ---------- 空值矩阵（Arrow） ----------

func TestNullMatrixArrow(t *testing.T) {
	R(t, "Arrow_空值矩阵_TAG_部分空_FIELD_全空", func(t *testing.T) {
		rows := caseRows(t)
		path := tmpPath(t, "arrow_null_matrix.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		const batch = 1000
		for from := 0; from < rows; from += batch {
			n := min(batch, rows-from)
			times := make([]int64, n)
			devices := make([]any, n)
			values := make([]any, n)
			for i := 0; i < n; i++ {
				row := from + i
				times[i] = int64(row)
				if row%nullEvery == 1 {
					devices[i] = nil
				} else {
					devices[i] = deviceOf(row)
				}
				values[i] = nil // FIELD 全 NULL
			}
			rec := buildRecordWithNull(t, arrow.NewSchema([]arrow.Field{
				{Name: "time", Type: arrow.PrimitiveTypes.Int64},
				{Name: "device", Type: arrow.BinaryTypes.String, Nullable: true},
				{Name: "value", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
			}, nil), [][]any{toAnySlice(times), devices, values})
			wantNil(t, w.WriteArrowBatch(rec))
			rec.Release()
		}
		wantNil(t, w.Close())

		st := readNullMatrixN(t, path, rows)
		if st.rows != rows || st.tagNulls != rows/nullEvery || st.valNulls != rows {
			t.Fatalf("Arrow 空值矩阵：rows=%d tagNulls=%d valNulls=%d; want rows=%d tagNulls=%d valNulls=%d",
				st.rows, st.tagNulls, st.valNulls, rows, rows/nullEvery, rows)
		}
	})
}

func toAnySlice(ts []int64) []any {
	out := make([]any, len(ts))
	for i, v := range ts {
		out[i] = v
	}
	return out
}

// ---------- 十类字段逐类型 NULL（≥1200 行） ----------

var tenTypeCols = []struct {
	name string
	dt   tsfile.DataType
}{
	{"c_bool", tsfile.DataTypeBoolean}, {"c_int32", tsfile.DataTypeInt32},
	{"c_int64", tsfile.DataTypeInt64}, {"c_float", tsfile.DataTypeFloat},
	{"c_double", tsfile.DataTypeDouble}, {"c_text", tsfile.DataTypeText},
	{"c_string", tsfile.DataTypeString}, {"c_blob", tsfile.DataTypeBlob},
	{"c_date", tsfile.DataTypeDate}, {"c_ts", tsfile.DataTypeTimestamp},
}

func TestNullMatrixTenTypes(t *testing.T) {
	R(t, "十类字段逐类型_NULL", func(t *testing.T) {
		rows := caseRows(t)
		cols := []tsfile.ColumnSchema{{Name: "device", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag}}
		tcols := []tsfile.TabletColumn{{Name: "device", DataType: tsfile.DataTypeString}}
		names := make([]string, 0, len(tenTypeCols))
		for _, c := range tenTypeCols {
			cols = append(cols, tsfile.ColumnSchema{Name: c.name, DataType: c.dt, Category: tsfile.ColumnCategoryField})
			tcols = append(tcols, tsfile.TabletColumn{Name: c.name, DataType: c.dt})
			names = append(names, c.name)
		}
		path := tmpPath(t, "ten_types_null.tsfile")
		w, err := tsfile.NewWriter(path, tsfile.TableSchema{Table: "metrics", Columns: cols})
		wantNil(t, err)
		const batch = 1000
		for from := 0; from < rows; from += batch {
			n := min(batch, rows-from)
			tab, err := tsfile.NewTablet(tcols, n)
			wantNil(t, err)
			for r := 0; r < n; r++ {
				row := from + r
				wantNil(t, tab.AddTimestamp(r, int64(row)))
				wantNil(t, tab.SetString(r, 0, deviceOf(row)))
				// 十类字段全部不设置 → NULL
			}
			wantNil(t, w.WriteTableTablet(tab))
			_ = tab.Close()
		}
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", names)
		defer func() { _ = rs.Close() }()
		count := 0
		nullCounts := make([]int, len(names))
		for {
			ok, err := rs.Next()
			wantNil(t, err)
			if !ok {
				break
			}
			for i := range names {
				null, err := rs.IsNull(i + 2)
				wantNil(t, err)
				if null {
					nullCounts[i]++
				}
			}
			count++
		}
		if count != rows {
			t.Fatalf("读回行数 = %d; want %d", count, rows)
		}
		for i, c := range tenTypeCols {
			if nullCounts[i] != rows {
				t.Fatalf("%s: NULL 行数 = %d; want %d（整列 NULL）", c.name, nullCounts[i], rows)
			}
		}
		// 抽样校验：重新查询首行，确认 getter 返回 ErrNullValue
		rs2 := mustQuery(t, r, "metrics", names)
		defer func() { _ = rs2.Close() }()
		if ok, err := rs2.Next(); err != nil || !ok {
			t.Fatalf("Next = (%v,%v); want (true,nil)", ok, err)
		}
		for i, c := range tenTypeCols {
			idx := i + 2
			var gerr error
			switch c.name {
			case "c_bool":
				_, gerr = rs2.Bool(idx)
			case "c_int32", "c_date":
				_, gerr = rs2.Int32(idx)
			case "c_int64", "c_ts":
				_, gerr = rs2.Int64(idx)
			case "c_float":
				_, gerr = rs2.Float32(idx)
			case "c_double":
				_, gerr = rs2.Float64(idx)
			case "c_text", "c_string":
				_, gerr = rs2.String(idx)
			case "c_blob":
				_, gerr = rs2.Bytes(idx)
			}
			if !errors.Is(gerr, tsfile.ErrNullValue) {
				t.Fatalf("%s: getter err = %v; want ErrNullValue", c.name, gerr)
			}
		}
		t.Logf("十类字段逐类型 NULL：%d 行、每列 NULL 行数 = %d，getter 均返回 ErrNullValue", rows, rows)
	})
}

// ---------- 浮点有限极值（≥1200 行） ----------

func writeFloatColumnN(t *testing.T, path string, rows int, dt tsfile.DataType, field string, set func(tab *tsfile.Tablet, row int, v float64)) {
	t.Helper()
	schema := tsfile.TableSchema{Table: "metrics", Columns: []tsfile.ColumnSchema{
		{Name: "device", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag},
		{Name: field, DataType: dt, Category: tsfile.ColumnCategoryField},
	}}
	w, err := tsfile.NewWriter(path, schema)
	wantNil(t, err)
	const batch = 1000
	for from := 0; from < rows; from += batch {
		n := min(batch, rows-from)
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: field, DataType: dt},
		}, n)
		wantNil(t, err)
		for r := 0; r < n; r++ {
			row := from + r
			wantNil(t, tab.AddTimestamp(r, int64(row)))
			wantNil(t, tab.SetString(r, 0, deviceOf(row)))
			set(tab, r, float64(row))
		}
		wantNil(t, w.WriteTableTablet(tab))
		_ = tab.Close()
	}
	wantNil(t, w.Close())
}

func TestFloatLimitsRoundTrip(t *testing.T) {
	R(t, "FLOAT32_有限极值", func(t *testing.T) {
		rows := caseRows(t)
		path := tmpPath(t, "float32_limits.tsfile")
		want := func(row int) float32 {
			switch row % 3 {
			case 0:
				return math.MaxFloat32
			case 1:
				return math.SmallestNonzeroFloat32
			default:
				return -math.MaxFloat32
			}
		}
		writeFloatColumnN(t, path, rows, tsfile.DataTypeFloat, "f", func(tab *tsfile.Tablet, r int, v float64) {
			wantNil(t, tab.SetFloat32(r, 1, want(int(v))))
		})
		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"f"})
		defer func() { _ = rs.Close() }()
		i := 0
		for {
			ok, err := rs.Next()
			wantNil(t, err)
			if !ok {
				break
			}
			got, err := rs.Float32(2)
			wantNil(t, err)
			if got != want(i) {
				t.Fatalf("第 %d 行 = %v; want %v", i, got, want(i))
			}
			i++
		}
		if i != rows {
			t.Fatalf("FLOAT32 读回 %d 行; want %d", i, rows)
		}
	})

	R(t, "FLOAT64_有限极值", func(t *testing.T) {
		rows := caseRows(t)
		path := tmpPath(t, "float64_limits.tsfile")
		want := func(row int) float64 {
			switch row % 3 {
			case 0:
				return math.MaxFloat64
			case 1:
				return math.SmallestNonzeroFloat64
			default:
				return -math.MaxFloat64
			}
		}
		writeFloatColumnN(t, path, rows, tsfile.DataTypeDouble, "value", func(tab *tsfile.Tablet, r int, v float64) {
			wantNil(t, tab.SetFloat64(r, 1, want(int(v))))
		})
		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		i := 0
		for {
			ok, err := rs.Next()
			wantNil(t, err)
			if !ok {
				break
			}
			got, err := rs.Float64(2)
			wantNil(t, err)
			if got != want(i) {
				t.Fatalf("第 %d 行 = %v; want %v", i, got, want(i))
			}
			i++
		}
		if i != rows {
			t.Fatalf("FLOAT64 读回 %d 行; want %d", i, rows)
		}
	})
}

// ---------- TEXT 类型（≥1200 行） ----------

func TestTextTypeRoundTrip(t *testing.T) {
	R(t, "TEXT_类型读写往返", func(t *testing.T) {
		rows := caseRows(t)
		schema := tsfile.TableSchema{Table: "metrics", Columns: []tsfile.ColumnSchema{
			{Name: "device", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag},
			{Name: "t", DataType: tsfile.DataTypeText, Category: tsfile.ColumnCategoryField},
		}}
		path := tmpPath(t, "text_type.tsfile")
		w, err := tsfile.NewWriter(path, schema)
		wantNil(t, err)
		const batch = 1000
		want := func(row int) string {
			if row%2 == 0 {
				return fmt.Sprintf("text-%05d", row)
			}
			return ""
		}
		for from := 0; from < rows; from += batch {
			n := min(batch, rows-from)
			tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
				{Name: "device", DataType: tsfile.DataTypeString},
				{Name: "t", DataType: tsfile.DataTypeText},
			}, n)
			wantNil(t, err)
			for r := 0; r < n; r++ {
				row := from + r
				wantNil(t, tab.AddTimestamp(r, int64(row)))
				wantNil(t, tab.SetString(r, 0, deviceOf(row)))
				wantNil(t, tab.SetString(r, 1, want(row)))
			}
			wantNil(t, w.WriteTableTablet(tab))
			_ = tab.Close()
		}
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"t"})
		defer func() { _ = rs.Close() }()
		i := 0
		for {
			ok, err := rs.Next()
			wantNil(t, err)
			if !ok {
				break
			}
			got, err := rs.String(2)
			wantNil(t, err)
			if got != want(i) {
				t.Fatalf("第 %d 行 TEXT = %q; want %q", i, got, want(i))
			}
			i++
		}
		if i != rows {
			t.Fatalf("TEXT 读回 %d 行; want %d", i, rows)
		}
	})
}

// ---------- 宽表（≥1200 行） ----------

func wideSchemaFor(tagN, fieldN int) (tsfile.TableSchema, []tsfile.TabletColumn) {
	cols := make([]tsfile.ColumnSchema, 0, tagN+fieldN)
	tcols := make([]tsfile.TabletColumn, 0, tagN+fieldN)
	for i := 0; i < tagN; i++ {
		name := fmt.Sprintf("t%03d", i)
		cols = append(cols, tsfile.ColumnSchema{Name: name, DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag})
		tcols = append(tcols, tsfile.TabletColumn{Name: name, DataType: tsfile.DataTypeString})
	}
	for i := 0; i < fieldN; i++ {
		name := fmt.Sprintf("f%03d", i)
		cols = append(cols, tsfile.ColumnSchema{Name: name, DataType: tsfile.DataTypeDouble, Category: tsfile.ColumnCategoryField})
		tcols = append(tcols, tsfile.TabletColumn{Name: name, DataType: tsfile.DataTypeDouble})
	}
	return tsfile.TableSchema{Table: "metrics", Columns: cols}, tcols
}

func TestWideColumns(t *testing.T) {
	R(t, "255_个_FIELD_列", func(t *testing.T) {
		rows := caseRows(t)
		path := tmpPath(t, "wide_field_255.tsfile")
		schema, tcols := wideSchemaFor(1, 255)
		w, err := tsfile.NewWriter(path, schema)
		wantNil(t, err)
		const batch = 200
		for from := 0; from < rows; from += batch {
			n := min(batch, rows-from)
			tab, err := tsfile.NewTablet(tcols, n)
			wantNil(t, err)
			for r := 0; r < n; r++ {
				row := from + r
				wantNil(t, tab.AddTimestamp(r, int64(row)))
				wantNil(t, tab.SetString(r, 0, deviceOf(row)))
				for i := 0; i < 255; i++ {
					wantNil(t, tab.SetFloat64(r, i+1, float64(row*255+i)))
				}
			}
			wantNil(t, w.WriteTableTablet(tab))
			_ = tab.Close()
		}
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		s, err := r.GetTableSchema("metrics")
		wantNil(t, err)
		if len(s.Columns) != 256 {
			t.Fatalf("schema 列数 = %d; want 256", len(s.Columns))
		}
		rs := mustQuery(t, r, "metrics", []string{"f000", "f254"})
		defer func() { _ = rs.Close() }()
		count := 0
		for {
			ok, err := rs.Next()
			wantNil(t, err)
			if !ok {
				break
			}
			a, err := rs.Float64(2)
			wantNil(t, err)
			b, err := rs.Float64(3)
			wantNil(t, err)
			if a != float64(count*255) || b != float64(count*255+254) {
				t.Fatalf("第 %d 行 f000=%v f254=%v; want %d/%d", count, a, b, count*255, count*255+254)
			}
			count++
		}
		if count != rows {
			t.Fatalf("读回 %d 行; want %d", count, rows)
		}
	})

	R(t, "255_个_TAG_列", func(t *testing.T) {
		rows := caseRows(t)
		path := tmpPath(t, "wide_tag_255.tsfile")
		schema, tcols := wideSchemaFor(255, 1)
		w, err := tsfile.NewWriter(path, schema)
		wantNil(t, err)
		const batch = 200
		for from := 0; from < rows; from += batch {
			n := min(batch, rows-from)
			tab, err := tsfile.NewTablet(tcols, n)
			wantNil(t, err)
			for r := 0; r < n; r++ {
				row := from + r
				wantNil(t, tab.AddTimestamp(r, int64(row)))
				for i := 0; i < 255; i++ {
					wantNil(t, tab.SetString(r, i, fmt.Sprintf("v%05d_%03d", row, i)))
				}
				wantNil(t, tab.SetFloat64(r, 255, float64(row)))
			}
			wantNil(t, w.WriteTableTablet(tab))
			_ = tab.Close()
		}
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		s, err := r.GetTableSchema("metrics")
		wantNil(t, err)
		if len(s.Columns) != 256 {
			t.Fatalf("schema 列数 = %d; want 256", len(s.Columns))
		}
		rs := mustQuery(t, r, "metrics", []string{"t000", "t254", "f000"})
		defer func() { _ = rs.Close() }()
		count := 0
		for {
			ok, err := rs.Next()
			wantNil(t, err)
			if !ok {
				break
			}
			a, err := rs.String(2)
			wantNil(t, err)
			b, err := rs.String(3)
			wantNil(t, err)
			v, err := rs.Float64(4)
			wantNil(t, err)
			if a != fmt.Sprintf("v%05d_000", count) || b != fmt.Sprintf("v%05d_254", count) || v != float64(count) {
				t.Fatalf("第 %d 行 t000=%q t254=%q f000=%v; want v%05d_000/v%05d_254/%d", count, a, b, v, count, count, count)
			}
			count++
		}
		if count != rows {
			t.Fatalf("读回 %d 行; want %d", count, rows)
		}
	})

	R(t, "1000_列_记录型", func(t *testing.T) {
		rows := caseRows(t)
		path := tmpPath(t, "wide_field_1000.tsfile")
		schema, tcols := wideSchemaFor(1, 1000)
		w, err := tsfile.NewWriter(path, schema)
		if err != nil {
			t.Logf("记录型：1 TAG + 1000 FIELD 构造失败 → %v（列数规模上限待产品确认）", err)
			return
		}
		const batch = 100
		for from := 0; from < rows; from += batch {
			n := min(batch, rows-from)
			tab, err := tsfile.NewTablet(tcols, n)
			wantNil(t, err)
			for r := 0; r < n; r++ {
				row := from + r
				wantNil(t, tab.AddTimestamp(r, int64(row)))
				wantNil(t, tab.SetString(r, 0, deviceOf(row)))
				for i := 0; i < 1000; i++ {
					wantNil(t, tab.SetFloat64(r, i+1, float64(row*1000+i)))
				}
			}
			if err := w.WriteTableTablet(tab); err != nil {
				t.Logf("记录型：1000 FIELD 写入失败（from=%d）→ %v", from, err)
				_ = tab.Close()
				_ = w.Close()
				return
			}
			_ = tab.Close()
		}
		wantNil(t, w.Close())
		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		s, err := r.GetTableSchema("metrics")
		wantNil(t, err)
		t.Logf("记录型：1 TAG + 1000 FIELD × %d 行 构造/写入/读回成功，schema 列数 = %d", rows, len(s.Columns))
	})
}

// 空值与零值区分 × ≥1200 行（0 / false / 空串 / 空字节串 与 NULL 逐行交错）
func TestNullVsZeroScale(t *testing.T) {
	R(t, "空值与零值区分_大批量", func(t *testing.T) {
		rows := caseRows(t)
		schema := tsfile.TableSchema{Table: "metrics", Columns: []tsfile.ColumnSchema{
			{Name: "device", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag},
			{Name: "b", DataType: tsfile.DataTypeBoolean, Category: tsfile.ColumnCategoryField},
			{Name: "i", DataType: tsfile.DataTypeInt32, Category: tsfile.ColumnCategoryField},
			{Name: "s", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryField},
			{Name: "bl", DataType: tsfile.DataTypeBlob, Category: tsfile.ColumnCategoryField},
		}}
		path := tmpPath(t, "null_vs_zero.tsfile")
		w, err := tsfile.NewWriter(path, schema)
		wantNil(t, err)
		const batch = 500
		for from := 0; from < rows; from += batch {
			n := min(batch, rows-from)
			tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
				{Name: "device", DataType: tsfile.DataTypeString},
				{Name: "b", DataType: tsfile.DataTypeBoolean},
				{Name: "i", DataType: tsfile.DataTypeInt32},
				{Name: "s", DataType: tsfile.DataTypeString},
				{Name: "bl", DataType: tsfile.DataTypeBlob},
			}, n)
			wantNil(t, err)
			for r := 0; r < n; r++ {
				row := from + r
				wantNil(t, tab.AddTimestamp(r, int64(row)))
				wantNil(t, tab.SetString(r, 0, deviceOf(row)))
				if row%2 == 0 { // 零值行
					wantNil(t, tab.SetBool(r, 1, false))
					wantNil(t, tab.SetInt32(r, 2, 0))
					wantNil(t, tab.SetString(r, 3, ""))
					wantNil(t, tab.SetBytes(r, 4, []byte{}))
				} // 奇数行：四列都不设 → NULL
			}
			wantNil(t, w.WriteTableTablet(tab))
			_ = tab.Close()
		}
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"b", "i", "s", "bl"})
		defer func() { _ = rs.Close() }()
		count, nullRows, zeroRows := 0, 0, 0
		for {
			ok, err := rs.Next()
			wantNil(t, err)
			if !ok {
				break
			}
			nulls := 0
			for col := 2; col <= 5; col++ {
				null, err := rs.IsNull(col)
				wantNil(t, err)
				if null {
					nulls++
				}
			}
			if nulls == 4 {
				nullRows++
			} else if nulls == 0 {
				zeroRows++
				b, err := rs.Bool(2)
				wantNil(t, err)
				i, err := rs.Int32(3)
				wantNil(t, err)
				s, err := rs.String(4)
				wantNil(t, err)
				bl, err := rs.Bytes(5)
				wantNil(t, err)
				if b || i != 0 || s != "" || len(bl) != 0 {
					t.Fatalf("第 %d 行零值读取不一致：b=%v i=%d s=%q blobLen=%d", count, b, i, s, len(bl))
				}
			} else {
				t.Fatalf("第 %d 行出现部分 NULL（nulls=%d）；零值行应四列均有值、NULL 行应四列均 NULL", count, nulls)
			}
			count++
		}
		if count != rows || nullRows+zeroRows != rows {
			t.Fatalf("rows=%d nullRows=%d zeroRows=%d; want %d", count, nullRows, zeroRows, rows)
		}
		t.Logf("空值与零值区分：%d 行（零值行 %d / NULL 行 %d），四类型逐行一致", rows, zeroRows, nullRows)
	})
}
