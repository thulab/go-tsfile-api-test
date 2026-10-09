package tsuite

import (
	"bytes"
	"math"
	"os"
	"os/exec"
	"testing"

	tsfile "github.com/apache/tsfile/go/tsfile"
)

func TestNewTabletColumnsForms(t *testing.T) {
	R(t, "columns_单列合法", func(t *testing.T) { // columns=单列合法
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{{Name: "value", DataType: tsfile.DataTypeDouble}}, 1)
		wantNil(t, err)
		wantNil(t, tab.Close())
	})

	R(t, "columns_多列合法_列名小写归一", func(t *testing.T) { // columns=多列合法（列名小写归一）
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "Device", DataType: tsfile.DataTypeString},
			{Name: "Value", DataType: tsfile.DataTypeDouble},
		}, 1)
		wantNil(t, err)
		defer func() { _ = tab.Close() }()
		wantNil(t, tab.AddTimestamp(0, 10))
		wantNil(t, tab.SetString(0, 0, "d1"))
		wantNil(t, tab.SetFloat64(0, 1, 1.0))
	})

	R(t, "重复列名_同大小写", func(t *testing.T) { // 重复列名（同大小写）
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "value", DataType: tsfile.DataTypeDouble},
			{Name: "value", DataType: tsfile.DataTypeInt64},
		}, 1)
		if tab != nil {
			_ = tab.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidSchema)
	})

	R(t, "重复列名_大小写不同", func(t *testing.T) { // 重复列名（大小写不同）
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "value", DataType: tsfile.DataTypeDouble},
			{Name: "VALUE", DataType: tsfile.DataTypeInt64},
		}, 1)
		if tab != nil {
			_ = tab.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidSchema)
	})

	R(t, "columns_DataType_VECTOR_NULL_TYPE_合并断言三种非法类型", func(t *testing.T) { // columns DataType=VECTOR/NULL_TYPE（合并断言三种非法类型）
		for _, typ := range []tsfile.DataType{tsfile.DataType(6), tsfile.DataType(7), tsfile.DataType(99)} {
			tab, err := tsfile.NewTablet([]tsfile.TabletColumn{{Name: "bad", DataType: typ}}, 1)
			if tab != nil {
				_ = tab.Close()
			}
			if err == nil {
				t.Fatalf("DataType(%d) 未被拒绝", typ)
			}
		}
	})

	R(t, "maxRows_2_30_minus_1_超界非法使用_仅记录", func(t *testing.T) {
		const probeEnv = "RN145_MAXROWS_PROBE"
		if os.Getenv(probeEnv) == "1" {
			tab, err := tsfile.NewTablet([]tsfile.TabletColumn{{Name: "value", DataType: tsfile.DataTypeDouble}}, (1<<30)-1)
			if err == nil {
				_ = tab.Close()
			}
			t.Logf("probe: NewTablet(maxRows=2^30-1) err=%v", err)
			return
		}
		exe, err := os.Executable()
		wantNil(t, err)
		cmd := exec.Command(exe, "-test.run", "^TestNewTabletColumnsForms$/^maxRows_2_30_minus_1_超界非法使用_仅记录$", "-test.v")
		cmd.Env = append(os.Environ(), probeEnv+"=1")
		out, err := cmd.CombinedOutput()
		// 口径（用户 2026-09-22）：超出实际可分配范围属调用方非法使用，产品不做保证 → 仅记录实测，不判失败
		if err != nil {
			t.Logf("非法使用实测：子进程非正常结束 → %v\n%s", err, tailText(string(out), 300))
		} else {
			t.Logf("非法使用实测：子进程正常返回（%s）", tailText(string(out), 200))
		}
	})

	R(t, "maxRows_2_30_plus_1_ErrOverflow", func(t *testing.T) {
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{{Name: "value", DataType: tsfile.DataTypeDouble}}, (1<<30)+1)
		if tab != nil {
			_ = tab.Close()
		}
		wantIs(t, err, tsfile.ErrOverflow)
	})

	R(t, "Rows_初始值", func(t *testing.T) { // Rows() 初始值
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{{Name: "value", DataType: tsfile.DataTypeDouble}}, 4)
		wantNil(t, err)
		defer func() { _ = tab.Close() }()
		if n := tab.Rows(); n != 0 {
			t.Fatalf("Rows() = %d; want 0", n)
		}
	})

	R(t, "Rows_逐行写入后", func(t *testing.T) { // Rows() 逐行写入后
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{{Name: "value", DataType: tsfile.DataTypeDouble}}, 4)
		wantNil(t, err)
		defer func() { _ = tab.Close() }()
		for row := 0; row < 3; row++ {
			wantNil(t, tab.AddTimestamp(row, int64(10*(row+1))))
			wantNil(t, tab.SetFloat64(row, 0, float64(row)))
			if n := tab.Rows(); n != row+1 {
				t.Fatalf("Rows() = %d after %d rows; want %d", n, row+1, row+1)
			}
		}
	})
}

func TestTabletAddTimestampBounds(t *testing.T) {
	newTablet := func(t *testing.T, maxRows int) *tsfile.Tablet {
		t.Helper()
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{{Name: "value", DataType: tsfile.DataTypeDouble}}, maxRows)
		wantNil(t, err)
		return tab
	}

	// roundTripTimestamp 写入指定时间戳并读回校验（时间戳按 int64 传递，不自动换算）。
	roundTripTimestamp := func(t *testing.T, name string, ts int64) {
		t.Helper()
		path := tmpPath(t, name)
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: "value", DataType: tsfile.DataTypeDouble},
		}, 1)
		wantNil(t, err)
		defer func() { _ = tab.Close() }()
		wantNil(t, tab.AddTimestamp(0, ts))
		wantNil(t, tab.SetString(0, 0, "d1"))
		wantNil(t, tab.SetFloat64(0, 1, 1.0))
		wantNil(t, w.WriteTableTablet(tab))
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		rows := collectRows(t, rs)
		if len(rows) != 1 || rows[0].ts != ts {
			t.Fatalf("timestamp round trip = %+v; want single row ts=%d", rows, ts)
		}
	}

	R(t, "row_maxRows-1", func(t *testing.T) { // row=maxRows-1
		path := tmpPath(t, "row_maxrows_minus1.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: "value", DataType: tsfile.DataTypeDouble},
		}, 3)
		wantNil(t, err)
		defer func() { _ = tab.Close() }()
		for row, ts := range []int64{10, 20, 30} {
			wantNil(t, tab.AddTimestamp(row, ts))
			wantNil(t, tab.SetString(row, 0, "d1"))
			wantNil(t, tab.SetFloat64(row, 1, float64(row)))
		}
		wantNil(t, w.WriteTableTablet(tab))
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		rows := collectRows(t, rs)
		if len(rows) != 3 || rows[2].ts != 30 {
			t.Fatalf("row=maxRows-1 round trip = %+v; want 3 rows and last ts=30", rows)
		}
	})

	R(t, "ts_MinInt64", func(t *testing.T) { // ts=MinInt64
		roundTripTimestamp(t, "ts_min.tsfile", math.MinInt64)
	})

	R(t, "ts_0", func(t *testing.T) { // ts=0
		roundTripTimestamp(t, "ts_zero.tsfile", 0)
	})

	R(t, "ts_MaxInt64", func(t *testing.T) { // ts=MaxInt64
		roundTripTimestamp(t, "ts_max.tsfile", math.MaxInt64)
	})

	R(t, "关闭后再调用", func(t *testing.T) { // 关闭后再调用
		tab := newTablet(t, 1)
		wantNil(t, tab.Close())
		wantIs(t, tab.AddTimestamp(0, 10), tsfile.ErrClosed)
	})
}

func TestTabletSettersIndexAndType(t *testing.T) {
	type spec struct {
		name     string
		ids      [5]string // row=-1, row=maxRows, col=-1, col=列数, 类型不匹配
		col      tsfile.TabletColumn
		wrongCol tsfile.TabletColumn
		call     func(tab *tsfile.Tablet, row, col int) error
	}
	specs := []spec{
		{"SetBool", [5]string{"SetBool_row_neg1", "SetBool_row_maxRows", "SetBool_column_neg1", "SetBool_column_count", "SetBool_column_type_mismatch"},
			tsfile.TabletColumn{Name: "c", DataType: tsfile.DataTypeBoolean},
			tsfile.TabletColumn{Name: "w", DataType: tsfile.DataTypeInt32},
			func(tab *tsfile.Tablet, r, c int) error { return tab.SetBool(r, c, true) }},
		{"SetInt32", [5]string{"SetInt32_row_neg1", "SetInt32_row_maxRows", "SetInt32_column_neg1", "SetInt32_column_count", "SetInt32_column_type_mismatch"},
			tsfile.TabletColumn{Name: "c", DataType: tsfile.DataTypeInt32},
			tsfile.TabletColumn{Name: "w", DataType: tsfile.DataTypeBoolean},
			func(tab *tsfile.Tablet, r, c int) error { return tab.SetInt32(r, c, 1) }},
		{"SetInt64", [5]string{"SetInt64_row_neg1", "SetInt64_row_maxRows", "SetInt64_column_neg1", "SetInt64_column_count", "SetInt64_column_type_mismatch"},
			tsfile.TabletColumn{Name: "c", DataType: tsfile.DataTypeInt64},
			tsfile.TabletColumn{Name: "w", DataType: tsfile.DataTypeInt32},
			func(tab *tsfile.Tablet, r, c int) error { return tab.SetInt64(r, c, 1) }},
		{"SetFloat32", [5]string{"SetFloat32_row_neg1", "SetFloat32_row_maxRows", "SetFloat32_column_neg1", "SetFloat32_column_count", "SetFloat32_column_type_mismatch"},
			tsfile.TabletColumn{Name: "c", DataType: tsfile.DataTypeFloat},
			tsfile.TabletColumn{Name: "w", DataType: tsfile.DataTypeDouble},
			func(tab *tsfile.Tablet, r, c int) error { return tab.SetFloat32(r, c, 1) }},
		{"SetFloat64", [5]string{"SetFloat64_row_neg1", "SetFloat64_row_maxRows", "SetFloat64_column_neg1", "SetFloat64_column_count", "SetFloat64_column_type_mismatch"},
			tsfile.TabletColumn{Name: "c", DataType: tsfile.DataTypeDouble},
			tsfile.TabletColumn{Name: "w", DataType: tsfile.DataTypeFloat},
			func(tab *tsfile.Tablet, r, c int) error { return tab.SetFloat64(r, c, 1) }},
		{"SetString", [5]string{"SetString_row_neg1", "SetString_row_maxRows", "SetString_column_neg1", "SetString_column_count", "SetString_column_type_mismatch"},
			tsfile.TabletColumn{Name: "c", DataType: tsfile.DataTypeString},
			tsfile.TabletColumn{Name: "w", DataType: tsfile.DataTypeDouble},
			func(tab *tsfile.Tablet, r, c int) error { return tab.SetString(r, c, "x") }},
		{"SetBytes", [5]string{"SetBytes_row_neg1", "SetBytes_row_maxRows", "SetBytes_column_neg1", "SetBytes_column_count", "SetBytes_column_type_mismatch"},
			tsfile.TabletColumn{Name: "c", DataType: tsfile.DataTypeBlob},
			tsfile.TabletColumn{Name: "w", DataType: tsfile.DataTypeString},
			func(tab *tsfile.Tablet, r, c int) error { return tab.SetBytes(r, c, []byte{1}) }},
	}

	for _, s := range specs {
		s := s
		makeTablet := func(t *testing.T, cols []tsfile.TabletColumn, maxRows int) *tsfile.Tablet {
			t.Helper()
			tab, err := tsfile.NewTablet(cols, maxRows)
			wantNil(t, err)
			return tab
		}
		R(t, s.ids[0], func(t *testing.T) { // row=-1
			tab := makeTablet(t, []tsfile.TabletColumn{s.col}, 2)
			defer func() { _ = tab.Close() }()
			wantIs(t, s.call(tab, -1, 0), tsfile.ErrOutOfRange)
		})
		R(t, s.ids[1], func(t *testing.T) { // row=maxRows
			tab := makeTablet(t, []tsfile.TabletColumn{s.col}, 2)
			defer func() { _ = tab.Close() }()
			wantIs(t, s.call(tab, 2, 0), tsfile.ErrOutOfRange)
		})
		R(t, s.ids[2], func(t *testing.T) { // column=-1
			tab := makeTablet(t, []tsfile.TabletColumn{s.col}, 2)
			defer func() { _ = tab.Close() }()
			wantIs(t, s.call(tab, 0, -1), tsfile.ErrOutOfRange)
		})
		R(t, s.ids[3], func(t *testing.T) { // column=列数
			tab := makeTablet(t, []tsfile.TabletColumn{s.col}, 2)
			defer func() { _ = tab.Close() }()
			wantIs(t, s.call(tab, 0, 1), tsfile.ErrOutOfRange)
		})
		R(t, s.ids[4], func(t *testing.T) { // 目标列为其它类型
			tab := makeTablet(t, []tsfile.TabletColumn{s.col, s.wrongCol}, 2)
			defer func() { _ = tab.Close() }()
			wantIs(t, s.call(tab, 0, 1), tsfile.ErrTypeMismatch)
		})
	}
}

// roundTripSingle 写一行单列表并读回该列（getter 由调用方提供）。
func roundTripSingle(t *testing.T, colName string, colType tsfile.DataType,
	set func(tab *tsfile.Tablet) error, read func(rs *tsfile.ResultSet) (string, error)) string {
	t.Helper()
	path := tmpPath(t, colName+".tsfile")
	schema := tsfile.TableSchema{Table: "single", Columns: []tsfile.ColumnSchema{
		{Name: colName, DataType: colType, Category: tsfile.ColumnCategoryField},
	}}
	w, err := tsfile.NewWriter(path, schema)
	wantNil(t, err)
	tab, err := tsfile.NewTablet([]tsfile.TabletColumn{{Name: colName, DataType: colType}}, 1)
	wantNil(t, err)
	defer func() { _ = tab.Close() }()
	wantNil(t, tab.AddTimestamp(0, 10))
	wantNil(t, set(tab))
	wantNil(t, w.WriteTableTablet(tab))
	wantNil(t, w.Close())
	r := openReader(t, path)
	defer func() { _ = r.Close() }()
	rs := mustQuery(t, r, "single", []string{colName})
	defer func() { _ = rs.Close() }()
	if !mustNext(t, rs) {
		t.Fatalf("no row returned")
	}
	out, err := read(rs)
	wantNil(t, err)
	return out
}

func TestTabletValueBoundsSingle(t *testing.T) {
	R(t, "SetBool_true_false", func(t *testing.T) { // SetBool(true/false)
		for _, v := range []bool{true, false} {
			v := v
			got := roundTripSingle(t, "c", tsfile.DataTypeBoolean,
				func(tab *tsfile.Tablet) error { return tab.SetBool(0, 0, v) },
				func(rs *tsfile.ResultSet) (string, error) {
					b, err := rs.Bool(2)
					if err != nil {
						return "", err
					}
					if b {
						return "true", nil
					}
					return "false", nil
				})
			want := "false"
			if v {
				want = "true"
			}
			if got != want {
				t.Fatalf("Bool 往返 = %q; want %q", got, want)
			}
		}
	})

	R(t, "SetInt32_MinInt32", func(t *testing.T) { // SetInt32(MinInt32)
		got := roundTripSingle(t, "c", tsfile.DataTypeInt32,
			func(tab *tsfile.Tablet) error { return tab.SetInt32(0, 0, math.MinInt32) },
			func(rs *tsfile.ResultSet) (string, error) {
				v, err := rs.Int32(2)
				return itoa(int64(v)), err
			})
		if got != itoa(math.MinInt32) {
			t.Fatalf("Int32 往返 = %s; want %s", got, itoa(math.MinInt32))
		}
	})

	R(t, "SetInt32_MaxInt32", func(t *testing.T) { // SetInt32(MaxInt32)
		got := roundTripSingle(t, "c", tsfile.DataTypeInt32,
			func(tab *tsfile.Tablet) error { return tab.SetInt32(0, 0, math.MaxInt32) },
			func(rs *tsfile.ResultSet) (string, error) {
				v, err := rs.Int32(2)
				return itoa(int64(v)), err
			})
		if got != itoa(math.MaxInt32) {
			t.Fatalf("Int32 往返 = %s; want %s", got, itoa(math.MaxInt32))
		}
	})

	R(t, "SetInt64_MinInt64", func(t *testing.T) { // SetInt64(MinInt64)
		got := roundTripSingle(t, "c", tsfile.DataTypeInt64,
			func(tab *tsfile.Tablet) error { return tab.SetInt64(0, 0, math.MinInt64) },
			func(rs *tsfile.ResultSet) (string, error) {
				v, err := rs.Int64(2)
				return itoa(v), err
			})
		if got != itoa(math.MinInt64) {
			t.Fatalf("Int64 往返 = %s; want %s", got, itoa(math.MinInt64))
		}
	})

	R(t, "SetInt64_MaxInt64", func(t *testing.T) { // SetInt64(MaxInt64)
		got := roundTripSingle(t, "c", tsfile.DataTypeInt64,
			func(tab *tsfile.Tablet) error { return tab.SetInt64(0, 0, math.MaxInt64) },
			func(rs *tsfile.ResultSet) (string, error) {
				v, err := rs.Int64(2)
				return itoa(v), err
			})
		if got != itoa(math.MaxInt64) {
			t.Fatalf("Int64 往返 = %s; want %s", got, itoa(math.MaxInt64))
		}
	})

	R(t, "SetString_空串", func(t *testing.T) { // SetString(空串)
		got := roundTripSingle(t, "c", tsfile.DataTypeString,
			func(tab *tsfile.Tablet) error { return tab.SetString(0, 0, "") },
			func(rs *tsfile.ResultSet) (string, error) { return rs.String(2) })
		if got != "" {
			t.Fatalf("String 往返 = %q; want empty", got)
		}
	})

	R(t, "SetBytes_空字节串", func(t *testing.T) { // SetBytes(空字节串)
		got := roundTripSingle(t, "c", tsfile.DataTypeBlob,
			func(tab *tsfile.Tablet) error { return tab.SetBytes(0, 0, []byte{}) },
			func(rs *tsfile.ResultSet) (string, error) {
				b, err := rs.Bytes(2)
				return string(b), err
			})
		if got != "" {
			t.Fatalf("Bytes 往返长度 = %d; want 0", len(got))
		}
	})

	R(t, "SetBytes_内嵌_NUL", func(t *testing.T) { // SetBytes(内嵌 NUL)
		in := []byte{0, 1, 2, 0}
		got := roundTripSingle(t, "c", tsfile.DataTypeBlob,
			func(tab *tsfile.Tablet) error { return tab.SetBytes(0, 0, in) },
			func(rs *tsfile.ResultSet) (string, error) {
				b, err := rs.Bytes(2)
				return string(b), err
			})
		if !bytes.Equal([]byte(got), in) {
			t.Fatalf("Bytes 往返 = %v; want %v", []byte(got), in)
		}
	})
}

func TestTabletCloseExtra(t *testing.T) {
	R(t, "重复关闭", func(t *testing.T) { // 重复关闭
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{{Name: "value", DataType: tsfile.DataTypeDouble}}, 1)
		wantNil(t, err)
		wantNil(t, tab.Close())
		wantNil(t, tab.Close())
	})
}
