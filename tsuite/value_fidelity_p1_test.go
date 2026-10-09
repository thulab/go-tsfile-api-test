package tsuite

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	tsfile "github.com/apache/tsfile/go/tsfile"
)

// 值保真（原样往返）用例：写入什么，查询就必须原样返回什么。
// 覆盖用户点名场景：写 10 读 10、写 AbC 读 AbC（大小写不丢）、符号全集、全字节。
// 行数统一 caseRows（默认 1200，CASE_ROWS 可覆盖）。

const fidelityBatch = 500

var (
	asciiSymbols  = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"
	controlChars  = "\t\n\r"
	fullwidthSym  = "，。！？：；（）【】《》“”‘’"
	quoteBackslah = "\"'\\"
	emojiText     = "😀🚀"
)

// symbolValue 每行都包含全部符号族 + 行号（保证不重复、可人工核对）。
func symbolValue(row int) string {
	return fmt.Sprintf("#%d|%s|%s|%s|%s|%s|中文值", row, asciiSymbols, controlChars, fullwidthSym, quoteBackslah, emojiText)
}

// fullBytePayload 返回 256 字节（0x00..0xFF 各一次，按 row 旋转），确定性可复核。
func fullBytePayload(row int) []byte {
	buf := make([]byte, 256)
	for i := range buf {
		buf[i] = byte((i + row) % 256)
	}
	return buf
}

// 字符串原样往返：TAG（device）与 FIELD（STRING、TEXT）写大小写混合/空格/中文值
func TestValueFidelityString(t *testing.T) {
	R(t, "字符串值_大小写与空格中文原样往返", func(t *testing.T) {
		rows := caseRows(t)
		schema := tsfile.TableSchema{Table: "metrics", Columns: []tsfile.ColumnSchema{
			{Name: "device", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag},
			{Name: "s", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryField},
			{Name: "t", DataType: tsfile.DataTypeText, Category: tsfile.ColumnCategoryField},
		}}
		tagVals := []string{"AbC", "d-01", "中文设备", "Sp ace"}
		fldVals := []string{"AbC", "AbC-中文-1.5", " 前后空格 ", ""}
		path := tmpPath(t, "fidelity_string.tsfile")
		w, err := tsfile.NewWriter(path, schema)
		wantNil(t, err)
		for from := 0; from < rows; from += fidelityBatch {
			n := min(fidelityBatch, rows-from)
			tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
				{Name: "device", DataType: tsfile.DataTypeString},
				{Name: "s", DataType: tsfile.DataTypeString},
				{Name: "t", DataType: tsfile.DataTypeText},
			}, n)
			wantNil(t, err)
			for r := 0; r < n; r++ {
				row := from + r
				wantNil(t, tab.AddTimestamp(r, int64(row)))
				wantNil(t, tab.SetString(r, 0, tagVals[row%len(tagVals)]))
				wantNil(t, tab.SetString(r, 1, fldVals[row%len(fldVals)]))
				wantNil(t, tab.SetString(r, 2, fldVals[(row+1)%len(fldVals)]))
			}
			wantNil(t, w.WriteTableTablet(tab))
			_ = tab.Close()
		}
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"device", "s", "t"})
		defer func() { _ = rs.Close() }()
		// 读回顺序按 TAG 分组、不等于写入行序 → 按时间戳建表比对（顺序无关）
		got := map[int64][3]string{}
		for {
			ok, err := rs.Next()
			wantNil(t, err)
			if !ok {
				break
			}
			ts, err := rs.Int64(1)
			wantNil(t, err)
			tag, err := rs.String(2)
			wantNil(t, err)
			sv, err := rs.String(3)
			wantNil(t, err)
			tv, err := rs.String(4)
			wantNil(t, err)
			got[ts] = [3]string{tag, sv, tv}
		}
		if len(got) != rows {
			t.Fatalf("读回 %d 行; want %d", len(got), rows)
		}
		for row := 0; row < rows; row++ {
			v, ok := got[int64(row)]
			if !ok {
				t.Fatalf("缺少 ts=%d 的行", row)
			}
			wantTag, wantS, wantT := tagVals[row%len(tagVals)], fldVals[row%len(fldVals)], fldVals[(row+1)%len(fldVals)]
			if v[0] != wantTag {
				t.Fatalf("ts=%d TAG = %q; want %q（值必须原样，不得大小写归一）", row, v[0], wantTag)
			}
			if v[1] != wantS {
				t.Fatalf("ts=%d STRING = %q; want %q", row, v[1], wantS)
			}
			if v[2] != wantT {
				t.Fatalf("ts=%d TEXT = %q; want %q", row, v[2], wantT)
			}
		}
		t.Logf("字符串原样往返：%d 行（含 AbC / 中文设备 / 前后空格 / 空串）逐值一致", rows)
	})
}

// 数值原样往返：10 / -10 / 0 / 2.5 / -0.0 / MinMax
func TestValueFidelityNumeric(t *testing.T) {
	R(t, "数值_原样往返_含负零与极值", func(t *testing.T) {
		rows := caseRows(t)
		schema := tsfile.TableSchema{Table: "metrics", Columns: []tsfile.ColumnSchema{
			{Name: "device", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag},
			{Name: "n", DataType: tsfile.DataTypeInt64, Category: tsfile.ColumnCategoryField},
			{Name: "d", DataType: tsfile.DataTypeDouble, Category: tsfile.ColumnCategoryField},
		}}
		intVals := []int64{10, -10, 0, math.MinInt64, math.MaxInt64}
		dblVals := []float64{10, -10, 2.5, math.Copysign(0, -1), 0}
		path := tmpPath(t, "fidelity_numeric.tsfile")
		w, err := tsfile.NewWriter(path, schema)
		wantNil(t, err)
		for from := 0; from < rows; from += fidelityBatch {
			n := min(fidelityBatch, rows-from)
			tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
				{Name: "device", DataType: tsfile.DataTypeString},
				{Name: "n", DataType: tsfile.DataTypeInt64},
				{Name: "d", DataType: tsfile.DataTypeDouble},
			}, n)
			wantNil(t, err)
			for r := 0; r < n; r++ {
				row := from + r
				wantNil(t, tab.AddTimestamp(r, int64(row)))
				wantNil(t, tab.SetString(r, 0, "AbC"))
				wantNil(t, tab.SetInt64(r, 1, intVals[row%len(intVals)]))
				wantNil(t, tab.SetFloat64(r, 2, dblVals[row%len(dblVals)]))
			}
			wantNil(t, w.WriteTableTablet(tab))
			_ = tab.Close()
		}
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"n", "d"})
		defer func() { _ = rs.Close() }()
		count := 0
		for {
			ok, err := rs.Next()
			wantNil(t, err)
			if !ok {
				break
			}
			row := count
			gotN, err := rs.Int64(2)
			wantNil(t, err)
			if gotN != intVals[row%len(intVals)] {
				t.Fatalf("第 %d 行 INT64 = %d; want %d", row, gotN, intVals[row%len(intVals)])
			}
			gotD, err := rs.Float64(3)
			wantNil(t, err)
			want := dblVals[row%len(dblVals)]
			if gotD != want || math.Signbit(gotD) != math.Signbit(want) {
				t.Fatalf("第 %d 行 DOUBLE = %v(signbit=%v); want %v(signbit=%v)", row, gotD, math.Signbit(gotD), want, math.Signbit(want))
			}
			count++
		}
		if count != rows {
			t.Fatalf("读回 %d 行; want %d", count, rows)
		}
		t.Logf("数值原样往返：%d 行（10/-10/0/2.5/-0.0/MinMax）逐值一致（含 -0.0 符号位）", rows)
	})
}

// BLOB 原样往返：含 0x00 / 0xFF
func TestValueFidelityBlob(t *testing.T) {
	R(t, "BLOB_含_NUL_与_0xFF_原样往返", func(t *testing.T) {
		rows := caseRows(t)
		schema := tsfile.TableSchema{Table: "metrics", Columns: []tsfile.ColumnSchema{
			{Name: "device", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag},
			{Name: "b", DataType: tsfile.DataTypeBlob, Category: tsfile.ColumnCategoryField},
		}}
		payloads := [][]byte{{0x00}, {0xFF}, {0x00, 0x41, 0xFF}, {0x00, 0x00, 0xFF, 0xFF, 0x7F, 0x80}}
		path := tmpPath(t, "fidelity_blob.tsfile")
		w, err := tsfile.NewWriter(path, schema)
		wantNil(t, err)
		for from := 0; from < rows; from += fidelityBatch {
			n := min(fidelityBatch, rows-from)
			tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
				{Name: "device", DataType: tsfile.DataTypeString},
				{Name: "b", DataType: tsfile.DataTypeBlob},
			}, n)
			wantNil(t, err)
			for r := 0; r < n; r++ {
				row := from + r
				wantNil(t, tab.AddTimestamp(r, int64(row)))
				wantNil(t, tab.SetString(r, 0, "AbC"))
				wantNil(t, tab.SetBytes(r, 1, payloads[row%len(payloads)]))
			}
			wantNil(t, w.WriteTableTablet(tab))
			_ = tab.Close()
		}
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"b"})
		defer func() { _ = rs.Close() }()
		count := 0
		for {
			ok, err := rs.Next()
			wantNil(t, err)
			if !ok {
				break
			}
			got, err := rs.Bytes(2)
			wantNil(t, err)
			want := payloads[count%len(payloads)]
			if !bytes.Equal(got, want) {
				t.Fatalf("第 %d 行 BLOB = % x; want % x", count, got, want)
			}
			count++
		}
		if count != rows {
			t.Fatalf("读回 %d 行; want %d", count, rows)
		}
		t.Logf("BLOB 原样往返：%d 行（含 0x00/0xFF/0x7F/0x80）逐字节一致", rows)
	})
}

// 边界对照：列名小写归一生效、值大小写不生效
func TestValueFidelityNameVsValueCase(t *testing.T) {
	R(t, "列名归一_但值保持原样", func(t *testing.T) {
		rows := caseRows(t)
		schema := tsfile.TableSchema{Table: "Metrics", Columns: []tsfile.ColumnSchema{
			{Name: "Device", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag},
			{Name: "Value", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryField},
		}}
		path := tmpPath(t, "fidelity_name_vs_value.tsfile")
		w, err := tsfile.NewWriter(path, schema)
		wantNil(t, err)
		for from := 0; from < rows; from += fidelityBatch {
			n := min(fidelityBatch, rows-from)
			tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
				{Name: "device", DataType: tsfile.DataTypeString},
				{Name: "value", DataType: tsfile.DataTypeString},
			}, n)
			wantNil(t, err)
			for r := 0; r < n; r++ {
				row := from + r
				wantNil(t, tab.AddTimestamp(r, int64(row)))
				wantNil(t, tab.SetString(r, 0, "AbC"))
				wantNil(t, tab.SetString(r, 1, fmt.Sprintf("AbC-%d", row)))
			}
			wantNil(t, w.WriteTableTablet(tab))
			_ = tab.Close()
		}
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		s, err := r.GetTableSchema("metrics")
		wantNil(t, err)
		if s.Table != "metrics" || len(s.Columns) != 2 || s.Columns[0].Name != "device" || s.Columns[1].Name != "value" {
			t.Fatalf("schema = %+v; want 表名/列名归一小写（metrics/device/value）", s)
		}
		rs := mustQuery(t, r, "metrics", []string{"device", "value"})
		defer func() { _ = rs.Close() }()
		count := 0
		for {
			ok, err := rs.Next()
			wantNil(t, err)
			if !ok {
				break
			}
			gotTag, err := rs.String(2)
			wantNil(t, err)
			gotVal, err := rs.String(3)
			wantNil(t, err)
			if gotTag != "AbC" || gotVal != fmt.Sprintf("AbC-%d", count) {
				t.Fatalf("第 %d 行 = (%q, %q); want (AbC, AbC-%d) —— 列名归一但值必须原样", count, gotTag, gotVal, count)
			}
			count++
		}
		if count != rows {
			t.Fatalf("读回 %d 行; want %d", count, rows)
		}
		t.Logf("列名归一 vs 值原样：%d 行，列名小写、值 AbC/AbC-n 原样", rows)
	})
}

// 符号全集：ASCII 可打印符号 + 制表/换行/回车 + 单双引号/反斜杠 + 全角符号 + emoji + 中文
func TestValueFidelitySymbols(t *testing.T) {
	R(t, "字符串值_符号全集原样往返", func(t *testing.T) {
		rows := caseRows(t)
		schema := tsfile.TableSchema{Table: "metrics", Columns: []tsfile.ColumnSchema{
			{Name: "device", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag},
			{Name: "s", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryField},
			{Name: "t", DataType: tsfile.DataTypeText, Category: tsfile.ColumnCategoryField},
		}}
		path := tmpPath(t, "fidelity_symbols.tsfile")
		w, err := tsfile.NewWriter(path, schema)
		wantNil(t, err)
		for from := 0; from < rows; from += fidelityBatch {
			n := min(fidelityBatch, rows-from)
			tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
				{Name: "device", DataType: tsfile.DataTypeString},
				{Name: "s", DataType: tsfile.DataTypeString},
				{Name: "t", DataType: tsfile.DataTypeText},
			}, n)
			wantNil(t, err)
			for r := 0; r < n; r++ {
				row := from + r
				wantNil(t, tab.AddTimestamp(r, int64(row)))
				wantNil(t, tab.SetString(r, 0, symbolValue(row)))
				wantNil(t, tab.SetString(r, 1, symbolValue(row)))
				wantNil(t, tab.SetString(r, 2, symbolValue(row)))
			}
			wantNil(t, w.WriteTableTablet(tab))
			_ = tab.Close()
		}
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"device", "s", "t"})
		defer func() { _ = rs.Close() }()
		// 读回顺序按 TAG 分组 → 按时间戳建表比对（顺序无关）
		got := map[int64][3]string{}
		for {
			ok, err := rs.Next()
			wantNil(t, err)
			if !ok {
				break
			}
			ts, err := rs.Int64(1)
			wantNil(t, err)
			tag, err := rs.String(2)
			wantNil(t, err)
			sv, err := rs.String(3)
			wantNil(t, err)
			tv, err := rs.String(4)
			wantNil(t, err)
			got[ts] = [3]string{tag, sv, tv}
		}
		if len(got) != rows {
			t.Fatalf("读回 %d 行; want %d", len(got), rows)
		}
		for row := 0; row < rows; row++ {
			v, ok := got[int64(row)]
			if !ok {
				t.Fatalf("缺少 ts=%d 的行", row)
			}
			want := symbolValue(row)
			for i, name := range []string{"TAG", "STRING", "TEXT"} {
				if v[i] != want || len(v[i]) != len(want) {
					t.Fatalf("ts=%d %s 长度=%d 内容=%q；want 长度=%d 内容=%q\n gotHex=% x\nwantHex=% x",
						row, name, len(v[i]), v[i], len(want), want, []byte(v[i]), []byte(want))
				}
			}
		}
		t.Logf("符号全集原样往返：%d 行 × 3 列（ASCII 符号 %d 个 + 制表/换行/回车 + 引号反斜杠 + 全角符号 + emoji + 中文）逐字符一致",
			rows, len(asciiSymbols))
	})
}

// 全字节：0x00..0xFF 每行 256 字节（按行旋转）
func TestValueFidelityAllBytes(t *testing.T) {
	R(t, "BLOB_全字节_0x00_0xFF_原样往返", func(t *testing.T) {
		rows := caseRows(t)
		schema := tsfile.TableSchema{Table: "metrics", Columns: []tsfile.ColumnSchema{
			{Name: "device", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag},
			{Name: "b", DataType: tsfile.DataTypeBlob, Category: tsfile.ColumnCategoryField},
		}}
		path := tmpPath(t, "fidelity_allbytes.tsfile")
		w, err := tsfile.NewWriter(path, schema)
		wantNil(t, err)
		for from := 0; from < rows; from += fidelityBatch {
			n := min(fidelityBatch, rows-from)
			tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
				{Name: "device", DataType: tsfile.DataTypeString},
				{Name: "b", DataType: tsfile.DataTypeBlob},
			}, n)
			wantNil(t, err)
			for r := 0; r < n; r++ {
				row := from + r
				wantNil(t, tab.AddTimestamp(r, int64(row)))
				wantNil(t, tab.SetString(r, 0, "AbC"))
				wantNil(t, tab.SetBytes(r, 1, fullBytePayload(row)))
			}
			wantNil(t, w.WriteTableTablet(tab))
			_ = tab.Close()
		}
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"b"})
		defer func() { _ = rs.Close() }()
		count := 0
		for {
			ok, err := rs.Next()
			wantNil(t, err)
			if !ok {
				break
			}
			got, err := rs.Bytes(2)
			wantNil(t, err)
			if !bytes.Equal(got, fullBytePayload(count)) {
				t.Fatalf("第 %d 行 BLOB 不一致（len=%d）", count, len(got))
			}
			count++
		}
		if count != rows {
			t.Fatalf("读回 %d 行; want %d", count, rows)
		}
		t.Logf("全字节原样往返：%d 行 × 256 字节（0x00..0xFF 旋转）逐字节一致", rows)
	})
}
