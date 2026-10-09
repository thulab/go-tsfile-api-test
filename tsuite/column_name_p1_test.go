package tsuite

import (
	"strings"
	"testing"

	tsfile "github.com/apache/tsfile/go/tsfile"
)

// 列名形态用例（RN-145 补充）：需求 §3.2 规定「名称要求非空且不含 NUL」，
// 其余字符未作限制；列名在 Go 侧按小写归一后比较/去重（normalizeIdentifier）。

// TAG 列名为空串 → ErrInvalidSchema（与 FIELD 列为空同一约束，补齐 TAG 分支）
func TestColumnNameTagEmpty(t *testing.T) {
	R(t, "TAG_列名为空串", func(t *testing.T) {
		schema := tsfile.TableSchema{Table: "metrics", Columns: []tsfile.ColumnSchema{
			{Name: "", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag},
			{Name: "value", DataType: tsfile.DataTypeDouble, Category: tsfile.ColumnCategoryField},
		}}
		w, err := tsfile.NewWriter(tmpPath(t, "tag_empty_name.tsfile"), schema)
		if w != nil {
			_ = w.Close()
		}
		wantIs(t, err, tsfile.ErrInvalidSchema)
	})
}

// 纯数字列名：按文档「非空且不含 NUL」应被接受，写入后按该列名读回
func TestColumnNameNumeric(t *testing.T) {
	R(t, "纯数字列名_读写往返", func(t *testing.T) {
		const col = "123"
		schema := tsfile.TableSchema{Table: "metrics", Columns: []tsfile.ColumnSchema{
			{Name: "device", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag},
			{Name: col, DataType: tsfile.DataTypeDouble, Category: tsfile.ColumnCategoryField},
		}}
		path := tmpPath(t, "numeric_column.tsfile")
		rows := caseRows(t)
		writeRowsWithColumn(t, path, schema, col, rows)

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		s, err := r.GetTableSchema("metrics")
		wantNil(t, err)
		if len(s.Columns) != 2 || s.Columns[1].Name != col {
			t.Fatalf("schema = %+v; want 第二列名为 %q", s.Columns, col)
		}
		rs := mustQuery(t, r, "metrics", []string{col})
		defer func() { _ = rs.Close() }()
		got := collectRows(t, rs)
		if len(got) != rows {
			t.Fatalf("读回 %d 行; want %d", len(got), rows)
		}
		for i, row := range got {
			if row.ts != int64(i) || row.null || row.value != float64(i) {
				t.Fatalf("第 %d 行 = %+v; want ts=%d value=%d", i, row, i, i)
			}
		}
	})
}

// 含特殊字符与中文的列名：按文档应被接受，写入后按原名读回
func TestColumnNameSpecialChars(t *testing.T) {
	R(t, "含特殊字符与中文列名_读写往返", func(t *testing.T) {
		const col = "中文 名-1.2_x(测试)"
		schema := tsfile.TableSchema{Table: "metrics", Columns: []tsfile.ColumnSchema{
			{Name: "device", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag},
			{Name: col, DataType: tsfile.DataTypeDouble, Category: tsfile.ColumnCategoryField},
		}}
		path := tmpPath(t, "special_column.tsfile")
		rows := caseRows(t)
		writeRowsWithColumn(t, path, schema, col, rows)

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		s, err := r.GetTableSchema("metrics")
		wantNil(t, err)
		if len(s.Columns) != 2 || s.Columns[1].Name != col {
			t.Fatalf("schema = %+v; want 第二列名为 %q（原名保留）", s.Columns, col)
		}
		rs := mustQuery(t, r, "metrics", []string{col})
		defer func() { _ = rs.Close() }()
		got := collectRows(t, rs)
		if len(got) != rows {
			t.Fatalf("读回 %d 行; want %d", len(got), rows)
		}
		for i, row := range got {
			if row.ts != int64(i) || row.value != float64(i) {
				t.Fatalf("第 %d 行 = %+v; want ts=%d value=%d", i, row, i, i)
			}
		}
	})
}

// 大小写混合列名：写入用 Device/Value，schema 与查询按归一小写命中
func TestColumnNameCaseNormalization(t *testing.T) {
	R(t, "大小写混合列名_归一小写", func(t *testing.T) {
		schema := tsfile.TableSchema{Table: "Metrics", Columns: []tsfile.ColumnSchema{
			{Name: "Device", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag},
			{Name: "Value", DataType: tsfile.DataTypeDouble, Category: tsfile.ColumnCategoryField},
		}}
		path := tmpPath(t, "case_column.tsfile")
		rows := caseRows(t)
		writeRowsWithColumn(t, path, schema, "Value", rows)

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		s, err := r.GetTableSchema("metrics")
		wantNil(t, err)
		if s.Table != "metrics" || len(s.Columns) != 2 ||
			s.Columns[0].Name != "device" || s.Columns[1].Name != "value" {
			t.Fatalf("schema = %+v; want 表名与列名均为归一小写（metrics/device/value）", s)
		}
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		got := collectRows(t, rs)
		if len(got) != rows {
			t.Fatalf("读回 %d 行; want %d", len(got), rows)
		}
		for i, row := range got {
			if row.ts != int64(i) || row.value != float64(i) {
				t.Fatalf("第 %d 行 = %+v; want ts=%d value=%d", i, row, i, i)
			}
		}
	})
}

// 查询列名大小写：需求未约束，按实测记录口径（命中则校验结果，报错则记录为大小写敏感）
func TestQueryColumnCaseSensitivity(t *testing.T) {
	R(t, "查询列名大小写_大写传入", func(t *testing.T) {
		path := tmpPath(t, "case_query_upper.tsfile")
		writeSample(t, path)
		r := openReader(t, path)
		defer func() { _ = r.Close() }()

		rs, err := r.Query("metrics", []string{"VALUE"})
		if err != nil {
			t.Logf("实测：大写列名 VALUE 查询被拒（%v）→ 查询列名大小写敏感，需按 schema 归一后的小写名传入", err)
			return
		}
		defer func() { _ = rs.Close() }()
		rows := collectRows(t, rs)
		if len(rows) != bulkRows(t) {
			t.Fatalf("大写列名查询读回 %d 行; want %d", len(rows), bulkRows(t))
		}
		t.Logf("实测：大写列名 VALUE 命中，返回 %d 行（查询列名大小写不敏感）", len(rows))
	})
}

var _ = strings.ToLower

// writeRowsWithColumn 用指定列名的 schema 写 rows 行（device=d{i}、指定列=i），用于列名形态用例。
func writeRowsWithColumn(t *testing.T, path string, schema tsfile.TableSchema, field string, rows int) {
	t.Helper()
	w, err := tsfile.NewWriter(path, schema)
	wantNil(t, err)
	const batch = 500
	for from := 0; from < rows; from += batch {
		n := min(batch, rows-from)
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: field, DataType: tsfile.DataTypeDouble},
		}, n)
		wantNil(t, err)
		for r := 0; r < n; r++ {
			row := from + r
			wantNil(t, tab.AddTimestamp(r, int64(row)))
			wantNil(t, tab.SetString(r, 0, deviceOf(row)))
			wantNil(t, tab.SetFloat64(r, 1, float64(row)))
		}
		wantNil(t, w.WriteTableTablet(tab))
		_ = tab.Close()
	}
	wantNil(t, w.Close())
}
