package tsuite

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	tsfile "github.com/apache/tsfile/go/tsfile"
)

// 大数据量用例（口径 A：只验"写入成功 + 读回一致 + 不溢出"，耗时与文件大小仅记录，不判定）。
//
// 规模由环境变量控制（默认值用于常规回归，保持套件轻量）：
//   LARGE_ROWS           行数（默认 2000）
//   LARGE_PAYLOAD_BYTES  每行 BLOB 字节数（默认 1024）
//   LARGE_DATA_DIR       产物目录（默认 t.TempDir()；正式跑建议显式指到 /data 下）
// 正式口径：LARGE_ROWS=10000、LARGE_PAYLOAD_BYTES=102400（≈1.0 GB 原始数据）。

func largeEnvInt(t *testing.T, key string, def int) int {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n := 0
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
		t.Fatalf("%s=%q 不是正整数", key, v)
	}
	return n
}

func largeScale(t *testing.T) (rows, payload int, dir string) {
	t.Helper()
	rows = largeEnvInt(t, "LARGE_ROWS", 2000)
	payload = largeEnvInt(t, "LARGE_PAYLOAD_BYTES", 1024)
	dir = os.Getenv("LARGE_DATA_DIR")
	if dir == "" {
		dir = t.TempDir()
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建 %s 失败: %v", dir, err)
	}
	return
}

func largeSchema() tsfile.TableSchema {
	return tsfile.TableSchema{Table: "metrics", Columns: []tsfile.ColumnSchema{
		{Name: "device", DataType: tsfile.DataTypeString, Category: tsfile.ColumnCategoryTag},
		{Name: "payload", DataType: tsfile.DataTypeBlob, Category: tsfile.ColumnCategoryField},
	}}
}

// payloadFor 生成确定性伪随机 payload（同一行号永远得到同一字节序列，可复现比对）。
func payloadFor(row, n int) []byte {
	src := rand.New(rand.NewSource(int64(row) + 1))
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = byte(src.Intn(256))
	}
	return buf
}

func deviceOf(row int) string { return fmt.Sprintf("d%05d", row) }

// writeLarge 分批写入 rows 行（每批 1000 行，避免一次性占用过多内存）。
func writeLarge(t *testing.T, path string, rows, payload int) time.Duration {
	t.Helper()
	const batch = 1000
	start := time.Now()
	w, err := tsfile.NewWriter(path, largeSchema())
	wantNil(t, err)
	for from := 0; from < rows; from += batch {
		n := batch
		if rows-from < batch {
			n = rows - from
		}
		tab, err := tsfile.NewTablet([]tsfile.TabletColumn{
			{Name: "device", DataType: tsfile.DataTypeString},
			{Name: "payload", DataType: tsfile.DataTypeBlob},
		}, n)
		wantNil(t, err)
		for r := 0; r < n; r++ {
			row := from + r
			wantNil(t, tab.AddTimestamp(r, int64(row)))
			wantNil(t, tab.SetString(r, 0, deviceOf(row)))
			wantNil(t, tab.SetBytes(r, 1, payloadFor(row, payload)))
		}
		wantNil(t, w.WriteTableTablet(tab))
		_ = tab.Close()
	}
	wantNil(t, w.Close())
	return time.Since(start)
}

// 353：万行级写入与文件规模（口径 A：只记录耗时与文件大小，不做判定阈值）
func TestLargeDataWrite(t *testing.T) {
	R(t, "万行级写入_文件规模记录", func(t *testing.T) {
		rows, payload, dir := largeScale(t)
		path := filepath.Join(dir, "large_data.tsfile")
		elapsed := writeLarge(t, path, rows, payload)
		st, err := os.Stat(path)
		wantNil(t, err)
		raw := int64(rows) * int64(payload)
		t.Logf("规模：rows=%d payload=%dB（原始 %.1f MiB）；文件 %.1f MiB；写入耗时 %s（口径 A：仅记录）",
			rows, payload, float64(raw)/(1<<20), float64(st.Size())/(1<<20), elapsed)
		if st.Size() <= 0 {
			t.Fatalf("文件大小异常：%d", st.Size())
		}
	})
}

// 354：读回一致性（行数 + 抽样逐字节 + 批次模式批数与 EOF）
func TestLargeDataReadBack(t *testing.T) {
	R(t, "万行级读回一致性", func(t *testing.T) {
		rows, payload, dir := largeScale(t)
		path := filepath.Join(dir, "large_data.tsfile")
		writeLarge(t, path, rows, payload)

		// 行模式：计数 + 抽样逐字节比对
		samples := map[int]bool{0: true, rows / 2: true, rows - 1: true, 17: true, rows / 3: true}
		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"device", "payload"})
		count := 0
		for {
			ok, err := rs.Next()
			wantNil(t, err)
			if !ok {
				break
			}
			row := count
			ts, err := rs.Int64(1)
			wantNil(t, err)
			if ts != int64(row) {
				t.Fatalf("第 %d 行 ts=%d; want %d", row, ts, row)
			}
			if samples[row] {
				dev, err := rs.String(2)
				wantNil(t, err)
				if dev != deviceOf(row) {
					t.Fatalf("第 %d 行 device=%q; want %q", row, dev, deviceOf(row))
				}
				got, err := rs.Bytes(3)
				wantNil(t, err)
				if !bytes.Equal(got, payloadFor(row, payload)) {
					t.Fatalf("第 %d 行 payload 不一致（len got=%d want=%d）", row, len(got), payload)
				}
			}
			count++
		}
		if count != rows {
			t.Fatalf("行模式读回行数 = %d; want %d", count, rows)
		}
		_ = rs.Close()

		// 批次模式：批数/总行数/EOF
		rs2 := mustQuery(t, r, "metrics", []string{"payload"}, tsfile.WithBatchSize(1024))
		defer func() { _ = rs2.Close() }()
		batches, total := 0, 0
		for {
			rec, err := rs2.ReadArrowRecordBatch()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				t.Fatalf("ReadArrowRecordBatch: %v", err)
			}
			batches++
			n := int(rec.NumRows())
			if n > 1024 {
				t.Fatalf("第 %d 批行数 %d 超过 batchSize=1024", batches, n)
			}
			total += n
			rec.Release()
		}
		if total != rows || batches < 1 {
			t.Fatalf("批次模式总行数=%d 批数=%d; want %d 行", total, batches, rows)
		}
		t.Logf("读回：行模式 %d 行（抽样 %d 行逐字节一致）；批次模式 %d 批 / %d 行 / EOF=io.EOF",
			count, len(samples), batches, total)
	})
}

// 355：大数据量下的 Tag 过滤正确性（只验命中行数与抽样值，不测耗时）
func TestLargeDataTagFilter(t *testing.T) {
	R(t, "万行级_Tag_过滤", func(t *testing.T) {
		rows, payload, dir := largeScale(t)
		path := filepath.Join(dir, "large_data.tsfile")
		writeLarge(t, path, rows, payload)

		r := openReader(t, path)
		defer func() { _ = r.Close() }()
		target := rows / 2
		rs := mustQuery(t, r, "metrics", []string{"device", "payload"}, tsfile.WithTagFilter(tsfile.Eq("device", deviceOf(target))))
		defer func() { _ = rs.Close() }()
		matched := 0
		for {
			ok, err := rs.Next()
			wantNil(t, err)
			if !ok {
				break
			}
			ts, err := rs.Int64(1)
			wantNil(t, err)
			dev, err := rs.String(2)
			wantNil(t, err)
			got, err := rs.Bytes(3)
			wantNil(t, err)
			if int(ts) != target || dev != deviceOf(target) || !bytes.Equal(got, payloadFor(target, payload)) {
				t.Fatalf("Tag 过滤命中行 = ts:%d dev:%q payloadLen:%d; want ts=%d dev=%s", ts, dev, len(got), target, deviceOf(target))
			}
			matched++
		}
		if matched != 1 {
			t.Fatalf("Tag 过滤命中 %d 行; want 1 行", matched)
		}
		// 不存在的 Tag：0 行
		rs2 := mustQuery(t, r, "metrics", []string{"payload"}, tsfile.WithTagFilter(tsfile.Eq("device", "d99999")))
		defer func() { _ = rs2.Close() }()
		n := 0
		for {
			ok, err := rs2.Next()
			wantNil(t, err)
			if !ok {
				break
			}
			n++
		}
		if n != 0 {
			t.Fatalf("不存在的 Tag 命中 %d 行; want 0", n)
		}
	})
}

var _ = array.NewInt64Builder
var _ = arrow.PrimitiveTypes
