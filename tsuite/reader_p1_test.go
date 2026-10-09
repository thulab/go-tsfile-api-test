package tsuite

import (
	"math"
	"path/filepath"
	"testing"

	tsfile "github.com/apache/tsfile/go/tsfile"
)

func TestNewReaderDir(t *testing.T) {
	R(t, "path_目录", func(t *testing.T) { // path=目录
		dir := t.TempDir()
		r, err := tsfile.NewReader(dir)
		if r != nil {
			_ = r.Close()
		}
		wantErr(t, err)
		t.Logf("note: 目录路径实际错误 = %v", err)
	})
}

func TestQueryTableCaseInsensitive(t *testing.T) {
	R(t, "表名大小写不同", func(t *testing.T) { // 表名大小写不同
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		for _, name := range []string{"metrics", "METRICS", "MeTrIcS"} {
			rs := mustQuery(t, r, name, []string{"value"})
			rows := collectRows(t, rs)
			_ = rs.Close()
			if len(rows) != bulkRows(t) {
				t.Fatalf("表名 %q 查询返回 %d 行; want %d", name, len(rows), bulkRows(t))
			}
		}
	})
}

func TestQueryColumnsExtra(t *testing.T) {
	R(t, "多列合法_顺序保持", func(t *testing.T) { // 多列合法（顺序保持）
		r := openReader(t, wideSampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "wide", []string{"c_double", "c_int64", "c_string"})
		defer func() { _ = rs.Close() }()
		md := rs.Metadata()
		if len(md) != 4 {
			t.Fatalf("metadata 列数 = %d; want 4（time + 3 列）", len(md))
		}
		want := []string{"time", "c_double", "c_int64", "c_string"}
		for i, name := range want {
			if md[i].Name != name {
				t.Fatalf("metadata[%d].Name = %q; want %q", i, md[i].Name, name)
			}
		}
	})

	R(t, "数组含重复列_待实测钉死", func(t *testing.T) { // 数组含重复列（待实测钉死）
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("metrics", []string{"value", "value"})
		if err != nil {
			t.Logf("note: 重复列实际返回错误 = %v（待研发确认口径）", err)
			return
		}
		defer func() { _ = rs.Close() }()
		t.Logf("note: 重复列被接受，metadata 列数 = %d（time + 2）", len(rs.Metadata()))
	})
}

func TestQueryOptionsOrderIndependent(t *testing.T) {
	R(t, "同一选项集不同顺序传入", func(t *testing.T) { // 同一选项集不同顺序传入
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs1 := mustQuery(t, r, "metrics", []string{"value"},
			tsfile.WithTimeRange(10, 40), tsfile.WithTagFilter(tsfile.Eq("device", "d1")),
			tsfile.WithOffset(0), tsfile.WithLimit(2))
		rows1 := collectRows(t, rs1)
		_ = rs1.Close()
		rs2 := mustQuery(t, r, "metrics", []string{"value"},
			tsfile.WithLimit(2), tsfile.WithOffset(0), tsfile.WithTagFilter(tsfile.Eq("device", "d1")),
			tsfile.WithTimeRange(10, 40))
		rows2 := collectRows(t, rs2)
		_ = rs2.Close()
		if len(rows1) != len(rows2) {
			t.Fatalf("两种顺序行数不同: %d vs %d", len(rows1), len(rows2))
		}
		for i := range rows1 {
			if rows1[i] != rows2[i] {
				t.Fatalf("第 %d 行不同: %+v vs %+v", i, rows1[i], rows2[i])
			}
		}
	})
}

func TestWithTimeRangeExtra(t *testing.T) {
	R(t, "start_end", func(t *testing.T) { // start=end
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithTimeRange(30, 30))
		defer func() { _ = rs.Close() }()
		rows := collectRows(t, rs)
		if len(rows) != 1 || rows[0].ts != 30 {
			t.Fatalf("rows = %+v; want single row ts=30", rows)
		}
	})

	R(t, "start_MinInt64_end_MaxInt64", func(t *testing.T) { // start=MinInt64,end=MaxInt64
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithTimeRange(math.MinInt64, math.MaxInt64))
		defer func() { _ = rs.Close() }()
		if rows := collectRows(t, rs); len(rows) != bulkRows(t) {
			t.Fatalf("rows = %d; want %d（等价全时段）", len(rows), bulkRows(t))
		}
	})

	R(t, "范围外_无命中", func(t *testing.T) { // 范围外（无命中）
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithTimeRange(20000, 30000))
		defer func() { _ = rs.Close() }()
		if rows := collectRows(t, rs); len(rows) != 0 {
			t.Fatalf("rows = %d; want 0", len(rows))
		}
	})

	R(t, "与_WithOffset_组合", func(t *testing.T) { // 与 WithOffset 组合
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"},
			tsfile.WithTimeRange(10, 30), tsfile.WithOffset(1))
		defer func() { _ = rs.Close() }()
		rows := collectRows(t, rs)
		if len(rows) != 2 || rows[0].ts != 20 || rows[1].ts != 30 {
			t.Fatalf("rows = %+v; want ts=20,30（先过滤再跳过）", rows)
		}
	})
}

func TestWithOffsetExtra(t *testing.T) {
	R(t, "offset_0", func(t *testing.T) { // offset=0
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithOffset(0))
		defer func() { _ = rs.Close() }()
		wantRowValues(t, collectRows(t, rs), bulkReadOrder(t))
	})

	R(t, "offset_行数", func(t *testing.T) { // offset>行数
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithOffset(bulkRows(t)))
		defer func() { _ = rs.Close() }()
		if rows := collectRows(t, rs); len(rows) != 0 {
			t.Fatalf("offset=%d（=总行数）时 rows = %d; want 0", bulkRows(t), len(rows))
		}
	})

	R(t, "offset_MaxInt32_1", func(t *testing.T) { // offset=MaxInt32+1
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("metrics", []string{"value"}, tsfile.WithOffset(math.MaxInt32+1))
		if rs != nil {
			_ = rs.Close()
		}
		wantIs(t, err, tsfile.ErrOverflow)
	})
}

func TestWithLimitExtra(t *testing.T) {
	R(t, "limit_-2_按不限处理", func(t *testing.T) { // limit=-2（按不限处理）
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithLimit(-2))
		defer func() { _ = rs.Close() }()
		if rows := collectRows(t, rs); len(rows) != bulkRows(t) {
			t.Fatalf("rows = %d; want %d", len(rows), bulkRows(t))
		}
	})

	R(t, "limit_行数", func(t *testing.T) { // limit>行数
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithLimit(bulkRows(t)))
		defer func() { _ = rs.Close() }()
		if rows := collectRows(t, rs); len(rows) != bulkRows(t) {
			t.Fatalf("limit=%d 时 rows = %d; want %d", bulkRows(t), len(rows), bulkRows(t))
		}
	})

	R(t, "limit_MaxInt32_1", func(t *testing.T) { // limit=MaxInt32+1
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("metrics", []string{"value"}, tsfile.WithLimit(math.MaxInt32+1))
		if rs != nil {
			_ = rs.Close()
		}
		wantIs(t, err, tsfile.ErrOverflow)
	})
}

func TestOffsetLimitExtra(t *testing.T) {
	R(t, "Offset_Limit_大于结果集", func(t *testing.T) { // Offset+Limit 大于结果集
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		// offset+limit = 总行数+8 > 结果集 → 只返回尾部 2 行且无错误
		rs := mustQuery(t, r, "metrics", []string{"value"},
			tsfile.WithOffset(bulkRows(t)-2), tsfile.WithLimit(10))
		defer func() { _ = rs.Close() }()
		wantRowValues(t, collectRows(t, rs), bulkReadOrder(t)[bulkRows(t)-2:])
	})
}

func TestWithBatchSizeExtra(t *testing.T) {
	R(t, "batchSize_-1_按行模式", func(t *testing.T) { // batchSize=-1（按行模式）
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(-1))
		defer func() { _ = rs.Close() }()
		if rows := collectRows(t, rs); len(rows) != bulkRows(t) {
			t.Fatalf("rows = %d; want %d（负数按行模式）", len(rows), bulkRows(t))
		}
	})

	R(t, "batchSize_1", func(t *testing.T) { // batchSize=1
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(1))
		defer func() { _ = rs.Close() }()
		total := 0
		for {
			rec, err := rs.ReadArrowRecordBatch()
			if err != nil {
				break
			}
			if rec.NumRows() > 1 {
				t.Fatalf("batch rows = %d; want <= 1", rec.NumRows())
			}
			total += int(rec.NumRows())
			rec.Release()
		}
		if total != bulkRows(t) {
			t.Fatalf("arrow rows = %d; want %d", total, bulkRows(t))
		}
	})

	R(t, "batchSize_行数_不足一批", func(t *testing.T) { // batchSize>行数（不足一批）
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(100))
		defer func() { _ = rs.Close() }()
		total, batches := 0, 0
		for {
			rec, err := rs.ReadArrowRecordBatch()
			if err != nil {
				break
			}
			batches++
			total += int(rec.NumRows())
			rec.Release()
		}
		if total != bulkRows(t) || batches == 0 {
			t.Fatalf("batches=%d rows=%d; want rows=4", batches, total)
		}
	})

	R(t, "batchSize_MaxInt32", func(t *testing.T) { // batchSize=MaxInt32
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(math.MaxInt32))
		defer func() { _ = rs.Close() }()
		total := 0
		for {
			rec, err := rs.ReadArrowRecordBatch()
			if err != nil {
				break
			}
			total += int(rec.NumRows())
			rec.Release()
		}
		if total != bulkRows(t) {
			t.Fatalf("arrow rows = %d; want %d", total, bulkRows(t))
		}
	})

	R(t, "batchSize_MaxInt32_1", func(t *testing.T) { // batchSize=MaxInt32+1
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("metrics", []string{"value"}, tsfile.WithBatchSize(math.MaxInt32+1))
		if rs != nil {
			_ = rs.Close()
		}
		wantIs(t, err, tsfile.ErrOverflow)
	})
	R(t, "尾批次行数", func(t *testing.T) {
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(3))
		defer func() { _ = rs.Close() }()
		counts := []int{}
		for {
			rec, err := rs.ReadArrowRecordBatch()
			if err != nil {
				break
			}
			counts = append(counts, int(rec.NumRows()))
			rec.Release()
		}
		total := 0
		for _, c := range counts {
			if c > 3 {
				t.Fatalf("单批行数 %d 超过 batchSize=3", c)
			}
			total += c
		}
		// 尾批 = 总行数 mod batchSize；整除时尾批正好是满批（如 1200 % 3 == 0 → 尾批 3）
		wantTail := bulkRows(t) % 3
		if wantTail == 0 {
			wantTail = 3
		}
		wantBatches := (bulkRows(t) + 2) / 3
		if total != bulkRows(t) || len(counts) != wantBatches || counts[len(counts)-1] != wantTail {
			t.Fatalf("总行数=%d 批次数=%d 各批=%v; 期望总 %d、批次数 %d、尾批 %d",
				total, len(counts), counts, bulkRows(t), wantBatches, wantTail)
		}
		t.Logf("各批行数=%v", counts)
	})

}

func TestTagFilterConstructExtra(t *testing.T) {
	query := func(t *testing.T, filter tsfile.TagFilter) error {
		t.Helper()
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("metrics", []string{"value"}, tsfile.WithTagFilter(filter))
		if rs != nil {
			_ = rs.Close()
		}
		return err
	}

	R(t, "Between_的_upper_含_NUL", func(t *testing.T) { // Between 的 upper 含 NUL
		wantIs(t, query(t, tsfile.Between("device", "d1", "d\x002")), tsfile.ErrInvalidArgument)
	})

	R(t, "Not_nil", func(t *testing.T) { // Not(nil)
		wantIs(t, query(t, tsfile.Not(nil)), tsfile.ErrInvalidArgument)
	})

	R(t, "And_合法_nil", func(t *testing.T) { // And(合法,nil)
		wantIs(t, query(t, tsfile.And(tsfile.Eq("device", "d1"), nil)), tsfile.ErrInvalidArgument)
	})
}

func TestTagFilterOperatorsExtra(t *testing.T) {
	rowsOf := func(t *testing.T, filter tsfile.TagFilter) []rowValue {
		t.Helper()
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithTagFilter(filter))
		defer func() { _ = rs.Close() }()
		return collectRows(t, rs)
	}

	R(t, "Lt", func(t *testing.T) { // Lt
		rows := rowsOf(t, tsfile.Lt("device", "d2"))
		if len(rows) != bulkRows(t)/2 || rows[0].ts != 10 || rows[1].ts != 20 {
			t.Fatalf("Lt(device,d2) = %d 行（首两行 %v）; want %d 行（d1）", len(rows), rows[:2], bulkRows(t)/2)
		}
	})

	R(t, "Lte", func(t *testing.T) { // Lte
		rows := rowsOf(t, tsfile.Lte("device", "d2"))
		if len(rows) != 3*bulkRows(t)/4 {
			t.Fatalf("Lte(device,d2) rows = %d; want %d (d1,d2)", len(rows), 3*bulkRows(t)/4)
		}
	})

	R(t, "Gt", func(t *testing.T) { // Gt
		rows := rowsOf(t, tsfile.Gt("device", "d2"))
		if len(rows) != bulkRows(t)/4 || rows[0].ts != 40 {
			t.Fatalf("Gt(device,d2) = %d 行（首行 %v）; want %d 行（d3，首行 ts=40）", len(rows), rows[:1], bulkRows(t)/4)
		}
	})

	R(t, "Gte", func(t *testing.T) { // Gte
		rows := rowsOf(t, tsfile.Gte("device", "d2"))
		if len(rows) != bulkRows(t)/2 || rows[0].ts != 30 || rows[1].ts != 70 {
			t.Fatalf("Gte(device,d2) = %d 行（首两行 %v）; want %d 行（d2,d3）", len(rows), rows[:2], bulkRows(t)/2)
		}
	})

	R(t, "Between", func(t *testing.T) { // Between
		rows := rowsOf(t, tsfile.Between("device", "d1", "d2"))
		if len(rows) != 3*bulkRows(t)/4 {
			t.Fatalf("Between(device,d1,d2) rows = %d; want %d", len(rows), 3*bulkRows(t)/4)
		}
	})

	R(t, "不存在的值", func(t *testing.T) { // 不存在的值
		rows := rowsOf(t, tsfile.Eq("device", "d404"))
		if len(rows) != 0 {
			t.Fatalf("Eq(device,d404) rows = %d; want 0", len(rows))
		}
	})
}

func TestTagFilterInvalidExtra(t *testing.T) {
	R(t, "值类型与列不匹配", func(t *testing.T) { // 值类型与列不匹配
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs, err := r.Query("metrics", []string{"value"}, tsfile.WithTagFilter(tsfile.Gt("device", "10")))
		if rs != nil {
			_ = rs.Close()
		}
		if err != nil {
			t.Logf("note: 值类型不匹配实际错误 = %v", err)
			return
		}
		t.Logf("note: 值类型不匹配被接受（字符串列按字典序比较），需与需求口径核对")
	})
}

func TestTagFilterCombine(t *testing.T) {
	rowsOf := func(t *testing.T, filter tsfile.TagFilter) []rowValue {
		t.Helper()
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithTagFilter(filter))
		defer func() { _ = rs.Close() }()
		return collectRows(t, rs)
	}

	R(t, "And", func(t *testing.T) { // And
		rows := rowsOf(t, tsfile.And(tsfile.Gte("device", "d1"), tsfile.Lt("device", "d2")))
		if len(rows) != bulkRows(t)/2 || rows[0].ts != 10 || rows[1].ts != 20 {
			t.Fatalf("And(Gte d1, Lt d2) = %d 行; want %d 行（d1）", len(rows), bulkRows(t)/2)
		}
	})

	R(t, "Or", func(t *testing.T) { // Or
		wantRowValues(t, rowsOf(t, tsfile.Or(tsfile.Eq("device", "d1"), tsfile.Eq("device", "d3"))),
			bulkRowsOfDevices(t, "d1", "d3"))
	})

	R(t, "Not", func(t *testing.T) { // Not
		rows := rowsOf(t, tsfile.Not(tsfile.Eq("device", "d1")))
		if len(rows) != bulkRows(t)/2 || rows[0].ts != 30 || rows[1].ts != 70 {
			t.Fatalf("Not(d1) = %d 行（首两行 %v）; want %d 行（d2,d3）", len(rows), rows[:2], bulkRows(t)/2)
		}
	})
}

func TestGetTableSchemaExtra(t *testing.T) {
	R(t, "表名大小写不同", func(t *testing.T) { // 表名大小写不同
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		s, err := r.GetTableSchema("METRICS")
		wantNil(t, err)
		if s.Table != "metrics" {
			t.Fatalf("Table = %q; want metrics", s.Table)
		}
	})

	R(t, "空字符串", func(t *testing.T) { // 空字符串
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		_, err := r.GetTableSchema("")
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})

	R(t, "含内嵌_NUL", func(t *testing.T) { // 含内嵌 NUL
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		_, err := r.GetTableSchema("met\x00rics")
		wantIs(t, err, tsfile.ErrInvalidArgument)
	})
}

func TestGetAllTableSchemasExtra(t *testing.T) {
	R(t, "单表文件", func(t *testing.T) { // 单表文件
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		schemas, err := r.GetAllTableSchemas()
		wantNil(t, err)
		if len(schemas) != 1 || schemas[0].Table != "metrics" {
			t.Fatalf("schemas = %+v; want 单表 metrics", schemas)
		}
	})

	R(t, "只建表未写数据_Go_写接口最小文件", func(t *testing.T) { // 只建表未写数据（Go 写接口最小文件）
		path := tmpPath(t, "empty.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		wantNil(t, w.Close()) // 不写任何数据
		r, err := tsfile.NewReader(path)
		if err != nil {
			t.Logf("note: 空文件打开失败（%v）——与需求「空文件枚举结果为空集合」的口径需与研发确认", err)
			return
		}
		defer func() { _ = r.Close() }()
		schemas, err := r.GetAllTableSchemas()
		wantNil(t, err)
		if len(schemas) != 1 || schemas[0].Table != "metrics" {
			t.Fatalf("只建表未写数据的文件枚举 = %+v; want 1 张表 metrics（表结构在写入前已注册）", schemas)
		}
		t.Logf("note: 真正的「0 表空文件」无法由 Go 写接口产生（NewWriter 即注册表结构），需批次 4 用 C++/Python 生成后补测该子场景")
	})

	R(t, "Reader_Close_后已返回_Schema_仍可用", func(t *testing.T) { // Reader.Close 后已返回 Schema 仍可用
		r := openReader(t, sampleFile(t))
		schemas, err := r.GetAllTableSchemas()
		wantNil(t, err)
		single, err := r.GetTableSchema("metrics")
		wantNil(t, err)
		wantNil(t, r.Close())
		if single.Table != "metrics" || len(single.Columns) != 2 {
			t.Fatalf("Close 后 GetTableSchema 返回值不可用: %+v", single)
		}
		if len(schemas) != 1 || schemas[0].Columns[0].Name != "device" {
			t.Fatalf("Close 后 GetAllTableSchemas 返回值不可用: %+v", schemas)
		}
	})
}

var _ = filepath.Join
