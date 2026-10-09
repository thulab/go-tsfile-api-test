package tsuite

import (
	"errors"
	"io"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	tsfile "github.com/apache/tsfile/go/tsfile"
)

func TestResultSetNext(t *testing.T) {
	R(t, "逐行读取顺序与行数", func(t *testing.T) { // 逐行读取顺序与行数
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"device", "value"})
		defer func() { _ = rs.Close() }()
		// 行数 = 样本行数；顺序 = 按 TAG 分块（d1→d2→d3）、块内 ts 升序（实测行为，见 sampleReadOrder）
		wantRowsWithTagOrdered(t, collectRowsFromResultWithTag(t, rs), bulkReadOrder(t))
	})

	R(t, "读到末尾返回_false_nil", func(t *testing.T) { // 读到末尾返回 (false, nil)
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		for i := 0; i < bulkRows(t); i++ {
			if ok, err := rs.Next(); err != nil || !ok {
				t.Fatalf("Next #%d = (%v,%v); want (true,nil)", i+1, ok, err)
			}
		}
		ok, err := rs.Next()
		if ok || err != nil {
			t.Fatalf("Next at EOF = (%v,%v); want (false,nil)", ok, err)
		}
	})

	R(t, "批次模式结果集调用_Next", func(t *testing.T) { // 批次模式结果集调用 Next
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(2))
		defer func() { _ = rs.Close() }()
		ok, err := rs.Next()
		if err == nil {
			t.Fatalf("Next on batch-mode result = (%v,nil); want ErrWrongResultMode", ok)
		}
		wantIs(t, err, tsfile.ErrWrongResultMode)
	})
}

func TestResultSetIsNull(t *testing.T) {
	R(t, "列_0", func(t *testing.T) { // 列 0
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		if ok, err := rs.Next(); err != nil || !ok {
			t.Fatalf("Next = (%v,%v); want (true,nil)", ok, err)
		}
		_, err := rs.IsNull(0)
		wantIs(t, err, tsfile.ErrOutOfRange)
	})

	R(t, "列_n_1_结果列为_time_value_n_2", func(t *testing.T) { // 列 n+1（结果列为 time+value，n=2）
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		if _, err := rs.Next(); err != nil {
			t.Fatalf("Next: %v", err)
		}
		_, err := rs.IsNull(3)
		wantIs(t, err, tsfile.ErrOutOfRange)
	})

	R(t, "未_Next_即调用", func(t *testing.T) { // 未 Next 即调用
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		_, err := rs.IsNull(2)
		wantIs(t, err, tsfile.ErrNoCurrentRow)
	})
}

func TestResultSetGetter(t *testing.T) {
	positioned := func(t *testing.T) (*tsfile.Reader, *tsfile.ResultSet) {
		t.Helper()
		r := openReader(t, sampleFile(t))
		rs := mustQuery(t, r, "metrics", []string{"value"})
		if ok, err := rs.Next(); err != nil || !ok {
			t.Fatalf("Next = (%v,%v); want (true,nil)", ok, err)
		}
		return r, rs
	}

	R(t, "用错误_getter_读列_ErrTypeMismatch", func(t *testing.T) { // 用错误 getter 读列 → ErrTypeMismatch
		r, rs := positioned(t)
		defer func() { _ = rs.Close(); _ = r.Close() }()
		checks := []struct {
			name string
			call func() error
		}{
			{"Bool(2)", func() error { _, err := rs.Bool(2); return err }},
			{"Int32(2)", func() error { _, err := rs.Int32(2); return err }},
			{"Int64(2)", func() error { _, err := rs.Int64(2); return err }},
			{"Float32(2)", func() error { _, err := rs.Float32(2); return err }},
			{"String(2)", func() error { _, err := rs.String(2); return err }},
			{"Bytes(2)", func() error { _, err := rs.Bytes(2); return err }},
			{"Float64(1)", func() error { _, err := rs.Float64(1); return err }},
			{"String(1)", func() error { _, err := rs.String(1); return err }},
		}
		for _, c := range checks {
			if err := c.call(); !errors.Is(err, tsfile.ErrTypeMismatch) {
				t.Fatalf("%s error = %v; want ErrTypeMismatch", c.name, err)
			}
		}
	})

	R(t, "读空值_ErrNullValue", func(t *testing.T) { // 读空值 → ErrNullValue
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithTimeRange(30, 30))
		defer func() { _ = rs.Close() }()
		if ok, err := rs.Next(); err != nil || !ok {
			t.Fatalf("Next = (%v,%v); want (true,nil)", ok, err)
		}
		null, err := rs.IsNull(2)
		wantNil(t, err)
		if !null {
			t.Fatalf("IsNull(2) = false; want true for ts=30")
		}
		_, err = rs.Float64(2)
		wantIs(t, err, tsfile.ErrNullValue)
	})

	R(t, "列序号_0_-1_n_1_ErrOutOfRange", func(t *testing.T) { // 列序号 0/-1/n+1 → ErrOutOfRange
		r, rs := positioned(t)
		defer func() { _ = rs.Close(); _ = r.Close() }()
		for _, col := range []int{0, -1, 3, 99} {
			if _, err := rs.IsNull(col); !errors.Is(err, tsfile.ErrOutOfRange) {
				t.Fatalf("IsNull(%d) error = %v; want ErrOutOfRange", col, err)
			}
			if _, err := rs.Float64(col); !errors.Is(err, tsfile.ErrOutOfRange) {
				t.Fatalf("Float64(%d) error = %v; want ErrOutOfRange", col, err)
			}
		}
	})

	R(t, "未_Next_即取值_ErrNoCurrentRow", func(t *testing.T) { // 未 Next 即取值 → ErrNoCurrentRow
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		_, err := rs.Float64(2)
		wantIs(t, err, tsfile.ErrNoCurrentRow)
	})
}

func TestNullSemantics(t *testing.T) {
	R(t, "NULL_与_0_区分", func(t *testing.T) { // NULL 与 0 区分
		path := tmpPath(t, "case276.tsfile")
		w, err := tsfile.NewWriter(path, metricsSchema())
		wantNil(t, err)
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: "value", DataType: tsfile.DataTypeDouble},
		}, 2)
		wantNil(t, err)
		defer func() { _ = tab.Close() }()
		wantNil(t, tab.AddTimestamp(0, 10))
		wantNil(t, tab.SetString(0, 0, "d1"))
		wantNil(t, tab.SetFloat64(0, 1, 0)) // 显式 0
		wantNil(t, tab.AddTimestamp(1, 20))
		wantNil(t, tab.SetString(1, 0, "d1"))
		// row1 的 value 不设置 → NULL
		wantNil(t, w.WriteTableTablet(tab))
		wantNil(t, w.Close())

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		if !mustNext(t, rs) {
			t.Fatalf("Next #1 = false")
		}
		null, err := rs.IsNull(2)
		wantNil(t, err)
		if null {
			t.Fatalf("IsNull = true for explicit 0; want false")
		}
		if v, err := rs.Float64(2); err != nil || v != 0 {
			t.Fatalf("Float64 = (%v,%v); want (0,nil)", v, err)
		}
		if !mustNext(t, rs) {
			t.Fatalf("Next #2 = false")
		}
		null, err = rs.IsNull(2)
		wantNil(t, err)
		if !null {
			t.Fatalf("IsNull = false for NULL row; want true")
		}
		if _, err := rs.Float64(2); !errors.Is(err, tsfile.ErrNullValue) {
			t.Fatalf("Float64(NULL) error = %v; want ErrNullValue", err)
		}
	})

	R(t, "Arrow_空值与标量判空一致", func(t *testing.T) { // Arrow 空值与标量判空一致
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(4))
		defer func() { _ = rs.Close() }()
		nulls, nullAt := 0, []int64{}
		for {
			rec, err := rs.ReadArrowRecordBatch()
			if errors.Is(err, io.EOF) {
				break
			}
			wantNil(t, err)
			tsIdx, valIdx := -1, -1
			for i, f := range rec.Schema().Fields() {
				switch f.Name {
				case "time":
					tsIdx = i
				case "value":
					valIdx = i
				}
			}
			if tsIdx < 0 || valIdx < 0 {
				t.Fatalf("batch schema = %v; want time+value", rec.Schema())
			}
			tsCol := rec.Column(tsIdx).(*array.Int64)
			valCol := rec.Column(valIdx).(*array.Float64)
			nulls += valCol.NullN()
			for i := 0; i < valCol.Len(); i++ {
				if valCol.IsNull(i) {
					nullAt = append(nullAt, tsCol.Value(i))
				}
			}
			rec.Release()
		}
		wantNullRows := bulkNullRows(t)
		wantNullAt := make([]int64, 0, len(wantNullRows))
		for _, w := range wantNullRows {
			wantNullAt = append(wantNullAt, w.ts)
		}
		if nulls != len(wantNullAt) || len(nullAt) != len(wantNullAt) {
			t.Fatalf("Arrow nulls = %d（位置 %v）; want %d 个（ts=%v）",
				nulls, nullAt, len(wantNullAt), wantNullAt)
		}
		for i := range wantNullAt {
			if nullAt[i] != wantNullAt[i] {
				t.Fatalf("Arrow 第 %d 个空值 ts = %d; want %d（完整位置 %v）",
					i+1, nullAt[i], wantNullAt[i], nullAt)
			}
		}
		scalar := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithTimeRange(30, 30))
		defer func() { _ = scalar.Close() }()
		if !mustNext(t, scalar) {
			t.Fatalf("scalar Next = false")
		}
		null, err := scalar.IsNull(2)
		wantNil(t, err)
		if !null {
			t.Fatalf("scalar IsNull = false for the same NULL row; want true")
		}
	})
}

func TestConsumeMode(t *testing.T) {
	R(t, "行模式与批次模式结果一致", func(t *testing.T) { // 行模式与批次模式结果一致
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rowsRS := mustQuery(t, r, "metrics", []string{"value"})
		rows := collectRows(t, rowsRS)
		_ = rowsRS.Close()

		batchRS := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(2))
		defer func() { _ = batchRS.Close() }()
		var batchRows []rowValue
		for {
			rec, err := batchRS.ReadArrowRecordBatch()
			if errors.Is(err, io.EOF) {
				break
			}
			wantNil(t, err)
			tsIdx, valIdx := -1, -1
			for i, f := range rec.Schema().Fields() {
				switch f.Name {
				case "time":
					tsIdx = i
				case "value":
					valIdx = i
				}
			}
			if tsIdx < 0 || valIdx < 0 {
				t.Fatalf("batch schema = %v; want time+value", rec.Schema())
			}
			tsCol, ok := rec.Column(tsIdx).(*array.Int64)
			if !ok {
				t.Fatalf("time column type = %T; want *array.Int64", rec.Column(tsIdx))
			}
			valCol, ok := rec.Column(valIdx).(*array.Float64)
			if !ok {
				t.Fatalf("value column type = %T; want *array.Float64", rec.Column(valIdx))
			}
			for i := 0; i < int(rec.NumRows()); i++ {
				rv := rowValue{ts: tsCol.Value(i)}
				if valCol.IsNull(i) {
					rv.null = true
				} else {
					rv.value = valCol.Value(i)
				}
				batchRows = append(batchRows, rv)
			}
			rec.Release()
		}
		if len(rows) != bulkRows(t) {
			t.Fatalf("row-mode rows = %d; want %d", len(rows), bulkRows(t))
		}
		if len(batchRows) != len(rows) {
			t.Fatalf("batch-mode rows = %d; want %d", len(batchRows), len(rows))
		}
		for i := range rows {
			if rows[i] != batchRows[i] {
				t.Fatalf("row %d: row-mode %+v != batch-mode %+v", i, rows[i], batchRows[i])
			}
		}
	})

	R(t, "行模式结果集调用_ReadArrowRecordBatch", func(t *testing.T) { // 行模式结果集调用 ReadArrowRecordBatch
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		if _, err := rs.ReadArrowRecordBatch(); !errors.Is(err, tsfile.ErrWrongResultMode) {
			t.Fatalf("ReadArrowRecordBatch on row-mode result = %v; want ErrWrongResultMode", err)
		}
	})

	R(t, "批次模式结果集调用_getter", func(t *testing.T) { // 批次模式结果集调用 getter
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(2))
		defer func() { _ = rs.Close() }()
		if _, err := rs.Float64(2); !errors.Is(err, tsfile.ErrWrongResultMode) {
			t.Fatalf("Float64 on batch-mode result = %v; want ErrWrongResultMode", err)
		}
	})

	R(t, "批次模式_EOF_io_EOF", func(t *testing.T) { // 批次模式 EOF = io.EOF
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(2))
		defer func() { _ = rs.Close() }()
		total := 0
		for {
			rec, err := rs.ReadArrowRecordBatch()
			if errors.Is(err, io.EOF) {
				break
			}
			wantNil(t, err)
			total += int(rec.NumRows())
			rec.Release()
		}
		if total != bulkRows(t) {
			t.Fatalf("arrow rows = %d; want %d", total, bulkRows(t))
		}
	})
}

func TestQueryCombos(t *testing.T) {
	R(t, "TimeRange_TagFilter", func(t *testing.T) { // TimeRange × TagFilter
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"},
			tsfile.WithTimeRange(20, 40), tsfile.WithTagFilter(tsfile.Eq("device", "d1")))
		defer func() { _ = rs.Close() }()
		rows := collectRows(t, rs)
		if len(rows) != 1 || rows[0].ts != 20 {
			t.Fatalf("rows = %+v; want single row ts=20", rows)
		}
	})

	R(t, "TimeRange_TagFilter_Offset_Limit", func(t *testing.T) { // TimeRange × TagFilter × Offset × Limit
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"},
			tsfile.WithTimeRange(10, 40), tsfile.WithTagFilter(tsfile.Eq("device", "d1")),
			tsfile.WithOffset(1), tsfile.WithLimit(1))
		defer func() { _ = rs.Close() }()
		rows := collectRows(t, rs)
		if len(rows) != 1 || rows[0].ts != 20 {
			t.Fatalf("rows = %+v; want single row ts=20", rows)
		}
	})

	R(t, "TimeRange_TagFilter_Offset_Limit_BatchSize", func(t *testing.T) { // TimeRange × TagFilter × Offset × Limit × BatchSize
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"},
			tsfile.WithTimeRange(10, 40), tsfile.WithTagFilter(tsfile.Eq("device", "d1")),
			tsfile.WithOffset(0), tsfile.WithLimit(2), tsfile.WithBatchSize(1))
		defer func() { _ = rs.Close() }()
		batches, total := 0, 0
		for {
			rec, err := rs.ReadArrowRecordBatch()
			if errors.Is(err, io.EOF) {
				break
			}
			wantNil(t, err)
			batches++
			total += int(rec.NumRows())
			rec.Release()
		}
		if total != 2 || batches != 2 {
			t.Fatalf("batches=%d rows=%d; want 2 batches / 2 rows（跨批次连续计数）", batches, total)
		}
	})
}

func TestCloseSemantics(t *testing.T) {
	R(t, "Reader_Close_使所属结果集失效", func(t *testing.T) { // Reader.Close 使所属结果集失效
		r := openReader(t, sampleFile(t))
		rs := mustQuery(t, r, "metrics", []string{"value"})
		wantNil(t, r.Close())
		_, err := rs.Next()
		wantIs(t, err, tsfile.ErrClosed)
		_ = rs.Close()
	})
}

func TestErrorModel(t *testing.T) {
	R(t, "errors_Is_命中哨兵", func(t *testing.T) { // errors.Is 命中哨兵
		_, err := tsfile.NewReader("")
		wantErr(t, err)
		if !errors.Is(err, tsfile.ErrInvalidArgument) {
			t.Fatalf("errors.Is(err, ErrInvalidArgument) = false; err = %v", err)
		}
		if errors.Is(err, tsfile.ErrTableNotExist) {
			t.Fatalf("errors.Is(err, ErrTableNotExist) = true; want false for a different sentinel")
		}
	})

	R(t, "errors_As_取_Error_Code_Op", func(t *testing.T) { // errors.As 取 *Error（Code/Op）
		_, err := tsfile.NewReader(tmpPath(t, "nope.tsfile"))
		wantErr(t, err)
		var te *tsfile.Error
		if !errors.As(err, &te) {
			t.Fatalf("errors.As(err, *tsfile.Error) = false; err = %v", err)
		}
		if te.Code == 0 {
			t.Fatalf("Error.Code = 0; want non-zero code")
		}
		if te.Op == "" {
			t.Fatalf("Error.Op = empty; want non-empty op")
		}
	})

	R(t, "ErrNoCurrentRow_命中", func(t *testing.T) { // ErrNoCurrentRow 命中
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		_, err := rs.Float64(2)
		if !errors.Is(err, tsfile.ErrNoCurrentRow) {
			t.Fatalf("errors.Is(err, ErrNoCurrentRow) = false; err = %v", err)
		}
	})

	R(t, "ErrWrongResultMode_命中", func(t *testing.T) { // ErrWrongResultMode 命中
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithBatchSize(2))
		defer func() { _ = rs.Close() }()
		_, err := rs.Next()
		if !errors.Is(err, tsfile.ErrWrongResultMode) {
			t.Fatalf("errors.Is(err, ErrWrongResultMode) = false; err = %v", err)
		}
	})
}

func mustNext(t *testing.T, rs *tsfile.ResultSet) bool {
	t.Helper()
	ok, err := rs.Next()
	wantNil(t, err)
	return ok
}
