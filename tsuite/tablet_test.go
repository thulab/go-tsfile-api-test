package tsuite

import (
	"testing"

	tsfile "github.com/apache/tsfile/go/tsfile"
)

func TestNewTablet(t *testing.T) {
	valid := []tsfile.TabletColumn{
		{Name: "device", DataType: tsfile.DataTypeString},
		{Name: "value", DataType: tsfile.DataTypeDouble},
	}

	R(t, "columns_空数组", func(t *testing.T) { // columns=空数组
		tab, err := tsfile.NewTablet(nil, 1)
		if tab != nil {
			_ = tab.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})

	R(t, "某列名为空串", func(t *testing.T) { // 某列名为空串
		cols := []tsfile.TabletColumn{{Name: "", DataType: tsfile.DataTypeString}}
		tab, err := tsfile.NewTablet(cols, 1)
		if tab != nil {
			_ = tab.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})

	R(t, "列名含_NUL", func(t *testing.T) { // 列名含 NUL
		cols := []tsfile.TabletColumn{{Name: "dev\x00ice", DataType: tsfile.DataTypeString}}
		tab, err := tsfile.NewTablet(cols, 1)
		if tab != nil {
			_ = tab.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})

	R(t, "maxRows_-1", func(t *testing.T) { // maxRows=-1
		tab, err := tsfile.NewTablet(valid, -1)
		if tab != nil {
			_ = tab.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})

	R(t, "maxRows_0", func(t *testing.T) { // maxRows=0
		tab, err := tsfile.NewTablet(valid, 0)
		if tab != nil {
			_ = tab.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})

	R(t, "maxRows_1_可写_1_行", func(t *testing.T) { // maxRows=1 可写 1 行
		path := tmpPath(t, "case49.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		tab, err := tsfile.NewTablet(valid, 1)
		wantNil(t, err)
		defer func() { _ = tab.Close() }()
		wantNil(t, tab.AddTimestamp(0, 10))
		wantNil(t, tab.SetString(0, 0, "d1"))
		wantNil(t, tab.SetFloat64(0, 1, 1.0))
		wantNil(t, w.WriteTableTablet(tab))
		wantNil(t, w.Close())
		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		if rows := collectRows(t, rs); len(rows) != 1 {
			t.Fatalf("rows = %d; want 1", len(rows))
		}
	})

	R(t, "maxRows_2_30_ErrOverflow", func(t *testing.T) { // maxRows=2^30 → ErrOverflow
		tab, err := tsfile.NewTablet(valid, 1<<30)
		if tab != nil {
			_ = tab.Close()
		}
		wantIs(t, err, tsfile.ErrOverflow)
	})
}

func TestTabletAddTimestamp(t *testing.T) {
	newTablet := func(t *testing.T, maxRows int) *tsfile.Tablet {
		t.Helper()
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: "value", DataType: tsfile.DataTypeDouble},
		}, maxRows)
		wantNil(t, err)
		return tab
	}

	R(t, "row_0", func(t *testing.T) { // row=0
		tab := newTablet(t, 3)
		defer func() { _ = tab.Close() }()
		wantNil(t, tab.AddTimestamp(0, 10))
	})

	R(t, "row_-1", func(t *testing.T) { // row=-1
		tab := newTablet(t, 3)
		defer func() { _ = tab.Close() }()
		wantIs(t, tab.AddTimestamp(-1, 10), tsfile.ErrOutOfRange)
	})

	R(t, "row_maxRows", func(t *testing.T) { // row=maxRows
		tab := newTablet(t, 3)
		defer func() { _ = tab.Close() }()
		wantIs(t, tab.AddTimestamp(3, 10), tsfile.ErrOutOfRange)
	})
}

func TestTabletValueBounds(t *testing.T) {
	R(t, "SetString_值含内嵌_NUL", func(t *testing.T) { // SetString 值含内嵌 NUL
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
		}, 1)
		wantNil(t, err)
		defer func() { _ = tab.Close() }()
		wantIs(t, tab.SetString(0, 0, "d\x001"), tsfile.ErrInvalidArgument)
	})
}
