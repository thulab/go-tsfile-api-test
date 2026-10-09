package tsuite

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tsfile "github.com/apache/tsfile/go/tsfile"
)

// 批次 5-A：写入侧边界与关闭语义（RN-145 台账 4/9/31/34/50/56/70/76/82/88/94/100/106/121/129/149/153/154/157/158/163）

// 超长路径（单段 > 255 字节）→ 返回可识别错误，且不产生半成品文件
func TestWriterPathBounds(t *testing.T) {
	R(t, "超长路径_单段_255_字节以上", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, strings.Repeat("x", 300)+".tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		if err == nil {
			_ = w.Close()
			t.Fatalf("超长单段路径应返回错误，实际构造成功: %s", path)
		}
		if _, statErr := os.Stat(path); statErr == nil {
			t.Fatalf("构造失败后不应留下文件: %s", path)
		}
	})

	R(t, "不可写目录", func(t *testing.T) {
		// 74 以 root 运行，权限位对 root 无效 → 用 chattr +i 模拟真正不可写的目录。
		dir := t.TempDir()
		if out, err := exec.Command("chattr", "+i", dir).CombinedOutput(); err != nil {
			t.Fatalf("chattr +i 失败（需要 e2fsprogs 与特权）: %v %s", err, out)
		}
		defer func() { _ = exec.Command("chattr", "-i", dir).Run() }()

		w, err := tsfile.NewWriter(filepath.Join(dir, "x.tsfile"), metricsSchema())
		if err == nil {
			_ = w.Close()
			t.Fatalf("不可写目录应返回错误，实际构造成功")
		}
		t.Logf("不可写目录实测错误：%v", err)
	})
}

// 内存阈值边界与多选项叠加
func TestWriterMemoryThresholdBounds(t *testing.T) {
	R(t, "WithMemoryThreshold_MaxUint64", func(t *testing.T) {
		path := tmpPath(t, "threshold_max.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema(), tsfile.WithMemoryThreshold(math.MaxUint64))
		wantNil(t, err)
		writeSampleRows(t, w)
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		wantSampleRows(t, collectRows(t, rs))
	})

	R(t, "多个阈值选项叠加", func(t *testing.T) {
		path := tmpPath(t, "threshold_multi.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema(),
			tsfile.WithMemoryThreshold(1), tsfile.WithMemoryThreshold(128<<20))
		wantNil(t, err)
		writeSampleRows(t, w)
		wantNil(t, w.Close())
		t.Logf("多个阈值选项叠加：构造成功且读写正常（生效值为最后一个 128MiB，无法从外部直接观测）")

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		wantSampleRows(t, collectRows(t, rs))
	})
}

// writeSampleRows 用既有 Writer 写入标准样本 4 行。
func writeSampleRows(t *testing.T, w *tsfile.Writer) {
	t.Helper()
	tablet, err := tsfile.NewTablet([]tsfile.TabletColumn{
		{Name: "device", DataType: tsfile.DataTypeString},
		{Name: "value", DataType: tsfile.DataTypeDouble},
	}, bulkRows(t))
	wantNil(t, err)
	defer func() { _ = tablet.Close() }()
	for row, r := range bulkSample(t) {
		wantNil(t, tablet.AddTimestamp(row, r.ts))
		wantNil(t, tablet.SetString(row, 0, r.device))
		if !r.isNull {
			wantNil(t, tablet.SetFloat64(row, 1, r.value))
		}
	}
	wantNil(t, w.WriteTableTablet(tablet))
}

// NewTablet 的小 maxRows 与关闭后 Rows()
func TestNewTabletBounds(t *testing.T) {
	R(t, "maxRows_2", func(t *testing.T) {
		tablet, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: "value", DataType: tsfile.DataTypeDouble},
		}, 2)
		wantNil(t, err)
		defer func() { _ = tablet.Close() }()
		for row := 0; row < 2; row++ {
			wantNil(t, tablet.AddTimestamp(row, int64(row+1)))
			wantNil(t, tablet.SetString(row, 0, "d1"))
			wantNil(t, tablet.SetFloat64(row, 1, float64(row+1)))
		}
		path := tmpPath(t, "maxrows2.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		wantNil(t, w.WriteTableTablet(tablet))
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		rows := collectRows(t, rs)
		if len(rows) != 2 || rows[0].ts != 1 || rows[1].ts != 2 {
			t.Fatalf("maxRows=2 写入 2 行后读回 = %+v; want ts=1,2", rows)
		}
	})

	R(t, "Rows_关闭后", func(t *testing.T) {
		tablet, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "value", DataType: tsfile.DataTypeDouble},
		}, 1)
		wantNil(t, err)
		wantNil(t, tablet.Close())
		if got := tablet.Rows(); got != 0 {
			t.Fatalf("关闭后 Rows() = %d; Go API 文档口径为 0", got)
		}
		t.Logf("关闭后 Rows() = 0（与 Go API 文档一致；台账原预期 ErrClosed 与实现不符，按文档口径记录）")
	})
}

// 各类 Setter 在 Tablet 关闭后再调用 → ErrClosed
func TestTabletSettersClosed(t *testing.T) {
	closedTablet := func(t *testing.T) *tsfile.Tablet {
		t.Helper()
		tablet, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "c_bool", DataType: tsfile.DataTypeBoolean},
			{Name: "c_int32", DataType: tsfile.DataTypeInt32},
			{Name: "c_int64", DataType: tsfile.DataTypeInt64},
			{Name: "c_float", DataType: tsfile.DataTypeFloat},
			{Name: "c_double", DataType: tsfile.DataTypeDouble},
			{Name: "c_string", DataType: tsfile.DataTypeString},
			{Name: "c_blob", DataType: tsfile.DataTypeBlob},
		}, 1)
		wantNil(t, err)
		wantNil(t, tablet.Close())
		return tablet
	}

	cases := []struct {
		name string
		call func(tab *tsfile.Tablet) error
	}{
		{"SetBool_关闭后再调用", func(tab *tsfile.Tablet) error { return tab.SetBool(0, 0, true) }},
		{"SetInt32_关闭后再调用", func(tab *tsfile.Tablet) error { return tab.SetInt32(0, 1, 1) }},
		{"SetInt64_关闭后再调用", func(tab *tsfile.Tablet) error { return tab.SetInt64(0, 2, 1) }},
		{"SetFloat32_关闭后再调用", func(tab *tsfile.Tablet) error { return tab.SetFloat32(0, 3, 1) }},
		{"SetFloat64_关闭后再调用", func(tab *tsfile.Tablet) error { return tab.SetFloat64(0, 4, 1) }},
		{"SetString_关闭后再调用", func(tab *tsfile.Tablet) error { return tab.SetString(0, 5, "x") }},
		{"SetBytes_关闭后再调用", func(tab *tsfile.Tablet) error { return tab.SetBytes(0, 6, []byte("x")) }},
	}
	for _, c := range cases {
		c := c
		R(t, c.name, func(t *testing.T) {
			tab := closedTablet(t)
			wantIs(t, c.call(tab), tsfile.ErrClosed)
		})
	}
}

// Tablet 关闭后的其它入口 + Tablet 复用
func TestTabletClosedAndReuse(t *testing.T) {
	R(t, "关闭后_AddTimestamp", func(t *testing.T) {
		tablet, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "value", DataType: tsfile.DataTypeDouble},
		}, 1)
		wantNil(t, err)
		wantNil(t, tablet.Close())
		wantIs(t, tablet.AddTimestamp(0, 1), tsfile.ErrClosed)
	})

	R(t, "同一_Tablet_复用到两个_Writer", func(t *testing.T) {
		tablet, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: "value", DataType: tsfile.DataTypeDouble},
		}, bulkRows(t))
		wantNil(t, err)
		defer func() { _ = tablet.Close() }()
		for row, r := range bulkSample(t) {
			wantNil(t, tablet.AddTimestamp(row, r.ts))
			wantNil(t, tablet.SetString(row, 0, r.device))
			if !r.isNull {
				wantNil(t, tablet.SetFloat64(row, 1, r.value))
			}
		}

		paths := []string{tmpPath(t, "reuse_1.tsfile"), tmpPath(t, "reuse_2.tsfile")}
		for _, p := range paths {
			w, err := tsfile.NewWriter(p, metricsSchema())
			wantNil(t, err)
			wantNil(t, w.WriteTableTablet(tablet))
			wantNil(t, w.Close())
		}
		for _, p := range paths {
			r := openReader(t, p)
			rs := mustQuery(t, r, "metrics", []string{"value"})
			wantSampleRows(t, collectRows(t, rs))
			_ = rs.Close()
			_ = r.Close()
		}
		if got := tablet.Rows(); got != bulkRows(t) {
			t.Fatalf("复用后 Rows() = %d; want %d（所有权未转移）", got, bulkRows(t))
		}
	})
}

// AddProperty 的超长键/值与关闭后调用
func TestWriterAddPropertyBounds(t *testing.T) {
	newWriter := func(t *testing.T, name string) (*tsfile.Writer, string) {
		t.Helper()
		path := tmpPath(t, name)
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		return w, path
	}

	R(t, "key_超长_1_MiB", func(t *testing.T) {
		w, _ := newWriter(t, "prop_long_key.tsfile")
		defer func() { _ = w.Close() }()
		err := w.AddProperty(strings.Repeat("k", 1<<20), []byte("v"))
		if err != nil {
			t.Logf("1MiB key 实测报错：%v", err)
		} else {
			t.Logf("1MiB key 实测写入成功")
		}
		wantNil(t, w.AddProperty("k", []byte("v")))
	})

	R(t, "value_超长_1_MiB", func(t *testing.T) {
		w, _ := newWriter(t, "prop_long_value.tsfile")
		defer func() { _ = w.Close() }()
		err := w.AddProperty("k", []byte(strings.Repeat("v", 1<<20)))
		if err != nil {
			t.Logf("1MiB value 实测报错：%v", err)
		} else {
			t.Logf("1MiB value 实测写入成功")
		}
		wantNil(t, w.AddProperty("k2", []byte("v")))
	})

	R(t, "关闭后调用", func(t *testing.T) {
		w, _ := newWriter(t, "prop_closed.tsfile")
		wantNil(t, w.Close())
		wantIs(t, w.AddProperty("k", []byte("v")), tsfile.ErrClosed)
	})
}

// Flush / Close 的边界语义
func TestWriterFlushCloseBounds(t *testing.T) {
	R(t, "未写入即_Flush", func(t *testing.T) {
		path := tmpPath(t, "flush_empty.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		wantNil(t, w.Flush())
		wantNil(t, w.Close())
	})

	R(t, "关闭后_Flush", func(t *testing.T) {
		path := tmpPath(t, "flush_closed.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		wantNil(t, w.Close())
		wantIs(t, w.Flush(), tsfile.ErrClosed)
	})
}

// Writer 关闭后的其它写入入口
func TestWriterCloseBounds(t *testing.T) {
	R(t, "关闭后_Flush", func(t *testing.T) {
		path := tmpPath(t, "close_flush.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		wantNil(t, w.Close())
		wantIs(t, w.Flush(), tsfile.ErrClosed)

		tablet, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: "value", DataType: tsfile.DataTypeDouble},
		}, 1)
		wantNil(t, err)
		defer func() { _ = tablet.Close() }()
		wantNil(t, tablet.AddTimestamp(0, 1))
		wantNil(t, tablet.SetString(0, 0, "d1"))
		wantNil(t, tablet.SetFloat64(0, 1, 1))
		wantIs(t, w.WriteTableTablet(tablet), tsfile.ErrClosed)
	})
}
