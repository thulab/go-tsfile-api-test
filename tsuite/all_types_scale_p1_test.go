package tsuite

import (
	"fmt"
	"math"
	"strings"
	"testing"

	tsfile "github.com/apache/tsfile/go/tsfile"
)

// 十类字段边界值 × ≥1200 行往返（用户 2026-09-23 要求：数据类型用例不得用太少的数据）
func TestAllTypesBoundaryScale(t *testing.T) {
	R(t, "十类字段_边界值_大批量往返", func(t *testing.T) {
		rows := caseRows(t)
		type spec struct {
			name string
			dt   tsfile.DataType
			set  func(tab *tsfile.Tablet, r, row int)
			want func(row int) string
		}
		specs := []spec{
			{"c_bool", tsfile.DataTypeBoolean, func(tab *tsfile.Tablet, r, row int) {
				wantNil(t, tab.SetBool(r, 1, row%2 == 0))
			}, func(row int) string { return fmt.Sprintf("%v", row%2 == 0) }},
			{"c_int32", tsfile.DataTypeInt32, func(tab *tsfile.Tablet, r, row int) {
				var v int32
				switch row % 3 {
				case 0:
					v = math.MinInt32
				case 1:
					v = math.MaxInt32
				default:
					v = int32(row)
				}
				wantNil(t, tab.SetInt32(r, 2, v))
			}, func(row int) string {
				switch row % 3 {
				case 0:
					return fmt.Sprintf("%d", int32(math.MinInt32))
				case 1:
					return fmt.Sprintf("%d", int32(math.MaxInt32))
				default:
					return fmt.Sprintf("%d", int32(row))
				}
			}},
			{"c_int64", tsfile.DataTypeInt64, func(tab *tsfile.Tablet, r, row int) {
				var v int64
				switch row % 3 {
				case 0:
					v = math.MinInt64
				case 1:
					v = math.MaxInt64
				default:
					v = int64(row)
				}
				wantNil(t, tab.SetInt64(r, 3, v))
			}, func(row int) string {
				switch row % 3 {
				case 0:
					return fmt.Sprintf("%d", int64(math.MinInt64))
				case 1:
					return fmt.Sprintf("%d", int64(math.MaxInt64))
				default:
					return fmt.Sprintf("%d", int64(row))
				}
			}},
			{"c_float", tsfile.DataTypeFloat, func(tab *tsfile.Tablet, r, row int) {
				var v float32
				switch row % 3 {
				case 0:
					v = math.MaxFloat32
				case 1:
					v = -math.MaxFloat32
				default:
					v = float32(row)
				}
				wantNil(t, tab.SetFloat32(r, 4, v))
			}, func(row int) string {
				switch row % 3 {
				case 0:
					return fmt.Sprintf("%v", float32(math.MaxFloat32))
				case 1:
					return fmt.Sprintf("%v", float32(-math.MaxFloat32))
				default:
					return fmt.Sprintf("%v", float32(row))
				}
			}},
			{"c_double", tsfile.DataTypeDouble, func(tab *tsfile.Tablet, r, row int) {
				var v float64
				switch row % 3 {
				case 0:
					v = math.MaxFloat64
				case 1:
					v = -math.MaxFloat64
				default:
					v = float64(row)
				}
				wantNil(t, tab.SetFloat64(r, 5, v))
			}, func(row int) string {
				switch row % 3 {
				case 0:
					return fmt.Sprintf("%v", math.MaxFloat64)
				case 1:
					return fmt.Sprintf("%v", -math.MaxFloat64)
				default:
					return fmt.Sprintf("%v", float64(row))
				}
			}},
			{"c_text", tsfile.DataTypeText, func(tab *tsfile.Tablet, r, row int) {
				wantNil(t, tab.SetString(r, 6, textValue(row)))
			}, textValue},
			{"c_string", tsfile.DataTypeString, func(tab *tsfile.Tablet, r, row int) {
				wantNil(t, tab.SetString(r, 7, textValue(row)))
			}, textValue},
			{"c_blob", tsfile.DataTypeBlob, func(tab *tsfile.Tablet, r, row int) {
				wantNil(t, tab.SetBytes(r, 8, []byte(fmt.Sprintf("blob-%05d-%s", row, strings.Repeat("x", row%17)))))
			}, func(row int) string { return fmt.Sprintf("blob-%05d-%s", row, strings.Repeat("x", row%17)) }},
			{"c_date", tsfile.DataTypeDate, func(tab *tsfile.Tablet, r, row int) {
				wantNil(t, tab.SetInt32(r, 9, int32(row)))
			}, func(row int) string { return fmt.Sprintf("%d", int32(row)) }},
			{"c_ts", tsfile.DataTypeTimestamp, func(tab *tsfile.Tablet, r, row int) {
				wantNil(t, tab.SetInt64(r, 10, int64(row)*1000))
			}, func(row int) string { return fmt.Sprintf("%d", int64(row)*1000) }},
		}

		cols := []tsfile.ColumnSchema{{Name: "device", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag}}
		tcols := []tsfile.TabletColumn{{Name: "device", DataType: tsfile.DataTypeString}}
		for _, sp := range specs {
			cols = append(cols, tsfile.ColumnSchema{Name: sp.name, DataType: sp.dt, Category: tsfile.ColumnCategoryField})
			tcols = append(tcols, tsfile.TabletColumn{Name: sp.name, DataType: sp.dt})
		}
		path := tmpPath(t, "all_types_boundary.tsfile")
		w, err := tsfile.NewWriter(path, tsfile.TableSchema{Table: "metrics", Columns: cols})
		wantNil(t, err)
		const batch = 500
		for from := 0; from < rows; from += batch {
			n := min(batch, rows-from)
			tab, err := tsfile.NewTablet(tcols, n)
			wantNil(t, err)
			for r := 0; r < n; r++ {
				row := from + r
				wantNil(t, tab.AddTimestamp(r, int64(row)))
				wantNil(t, tab.SetString(r, 0, deviceOf(row)))
				for _, sp := range specs {
					sp.set(tab, r, row)
				}
			}
			wantNil(t, w.WriteTableTablet(tab))
			_ = tab.Close()
		}
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		names := make([]string, 0, len(specs))
		for _, sp := range specs {
			names = append(names, sp.name)
		}
		rs := mustQuery(t, r, "metrics", names)
		defer func() { _ = rs.Close() }()
		count := 0
		for {
			ok, err := rs.Next()
			wantNil(t, err)
			if !ok {
				break
			}
			row := count
			for i, sp := range specs {
				idx := i + 2
				null, err := rs.IsNull(idx)
				wantNil(t, err)
				if null {
					t.Fatalf("第 %d 行 %s 不应为 NULL", row, sp.name)
				}
				var got string
				switch sp.name {
				case "c_bool":
					v, err := rs.Bool(idx)
					wantNil(t, err)
					got = fmt.Sprintf("%v", v)
				case "c_int32", "c_date":
					v, err := rs.Int32(idx)
					wantNil(t, err)
					got = fmt.Sprintf("%d", v)
				case "c_int64", "c_ts":
					v, err := rs.Int64(idx)
					wantNil(t, err)
					got = fmt.Sprintf("%d", v)
				case "c_float":
					v, err := rs.Float32(idx)
					wantNil(t, err)
					got = fmt.Sprintf("%v", v)
				case "c_double":
					v, err := rs.Float64(idx)
					wantNil(t, err)
					got = fmt.Sprintf("%v", v)
				case "c_text", "c_string":
					v, err := rs.String(idx)
					wantNil(t, err)
					got = v
				case "c_blob":
					v, err := rs.Bytes(idx)
					wantNil(t, err)
					got = string(v)
				}
				if want := sp.want(row); got != want {
					t.Fatalf("第 %d 行 %s = %q; want %q", row, sp.name, got, want)
				}
			}
			count++
		}
		if count != rows {
			t.Fatalf("读回行数 = %d; want %d", count, rows)
		}
		t.Logf("十类字段边界值往返：%d 行 × %d 列全部逐值一致", rows, len(specs))
	})
}

func textValue(row int) string {
	if row%3 == 0 {
		return ""
	}
	return fmt.Sprintf("text-%05d", row)
}
