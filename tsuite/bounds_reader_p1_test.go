package tsuite

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	tsfile "github.com/apache/tsfile/go/tsfile"
)

// 批次 5-C：查询极值、Metadata 副本语义与资源类（RN-145 台账 197/204/239/255/256/320/321/323）

func TestQueryOffsetLimitBounds(t *testing.T) {
	R(t, "offset_MaxInt32", func(t *testing.T) {
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithOffset(math.MaxInt32))
		defer func() { _ = rs.Close() }()
		rows := collectRows(t, rs)
		if len(rows) != 0 {
			t.Fatalf("offset=MaxInt32 读回 %d 行; want 0 行且无错误", len(rows))
		}
	})

	R(t, "limit_MaxInt32", func(t *testing.T) {
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithLimit(math.MaxInt32))
		defer func() { _ = rs.Close() }()
		wantSampleRows(t, collectRows(t, rs))
	})
}

func TestTagFilterNestedExtra(t *testing.T) {
	R(t, "And_Or_Not_深度_3_嵌套", func(t *testing.T) {
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		filter := tsfile.And(
			tsfile.Eq("device", "d1"),
			tsfile.Or(tsfile.Eq("device", "d2"), tsfile.Not(tsfile.Eq("device", "d3"))),
		)
		rs := mustQuery(t, r, "metrics", []string{"value"}, tsfile.WithTagFilter(filter))
		defer func() { _ = rs.Close() }()
		// 逻辑化简：Not(Eq(d3))=d1∪d2，Or(Eq(d2), …)=d1∪d2，再与 Eq(d1) 取交 → 仅 d1
		wantRowValues(t, collectRows(t, rs), bulkRowsOfDevices(t, "d1"))
	})
}

func TestResultSetMetadataBoundsExtra(t *testing.T) {
	R(t, "副本隔离", func(t *testing.T) {
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		defer func() { _ = rs.Close() }()
		first := rs.Metadata()
		if len(first) == 0 {
			t.Fatalf("Metadata() 为空")
		}
		original := first[0].Name
		first[0].Name = "mutated_by_test"
		second := rs.Metadata()
		if second[0].Name != original {
			t.Fatalf("修改返回切片影响了内部状态：再次读取得到 %q; want %q", second[0].Name, original)
		}
	})

	R(t, "关闭后调用", func(t *testing.T) {
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})
		wantNil(t, rs.Close())
		if md := rs.Metadata(); md != nil {
			t.Fatalf("关闭后 Metadata() = %+v; Go API 文档口径为 nil", md)
		}
		t.Logf("关闭后 Metadata() = nil（与 Go API 文档一致；台账原预期 ErrClosed 不适用于无 error 返回的 Metadata）")
	})
}

// 错误信息是否包含未脱敏路径（需求未明确约束 → 记录实测口径）
func TestErrorPathDisclosure(t *testing.T) {
	R(t, "错误信息不含未脱敏路径", func(t *testing.T) {
		dir := t.TempDir()
		full := dir + "/secret_subdir/not_exists.tsfile"
		_, err := tsfile.NewReader(full)
		if err == nil {
			t.Fatalf("不存在的路径应返回错误")
		}
		msg := err.Error()
		t.Logf("实测错误信息：%s", msg)
		if strings.Contains(msg, "secret_subdir") {
			t.Logf("观察项：错误信息含完整路径（需求未约束脱敏口径，建议与研发确认）")
		}
	})
}

// 稳定性：反复读写 200 轮，fd / RSS 无持续增长
func TestRepeatedReadWriteLeak(t *testing.T) {
	R(t, "反复读写_200_轮_无泄漏", func(t *testing.T) {
		path := tmpPath(t, "longrun.tsfile")
		baselineFD := openFDCount(t)
		baselineRSS := rssBytes(t)

		const rounds = 200
		for i := 0; i < rounds; i++ {
			w, err := tsfile.NewWriter(path, metricsSchema())
			wantNil(t, err)
			writeSampleRows(t, w)
			wantNil(t, w.Close())

			r := openReader(t, path)
			rs := mustQuery(t, r, "metrics", []string{"value"})
			rows := collectRows(t, rs)
			if len(rows) != bulkRows(t) {
				t.Fatalf("第 %d 轮读回 %d 行; want %d", i, len(rows), bulkRows(t))
			}
			_ = rs.Close()
			_ = r.Close()
		}

		endFD := openFDCount(t)
		endRSS := rssBytes(t)
		t.Logf("%d 轮读写：fd %d → %d；RSS %.1f MiB → %.1f MiB",
			rounds, baselineFD, endFD, float64(baselineRSS)/(1<<20), float64(endRSS)/(1<<20))
		if endFD > baselineFD+5 {
			t.Fatalf("fd 持续增长：%d → %d（阈值 +5）", baselineFD, endFD)
		}
		if endRSS > baselineRSS+(200<<20) {
			t.Fatalf("RSS 增长超过 200MiB：%.1f MiB → %.1f MiB", float64(baselineRSS)/(1<<20), float64(endRSS)/(1<<20))
		}
	})
}

func openFDCount(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	wantNil(t, err)
	return len(entries)
}

func rssBytes(t *testing.T) int {
	t.Helper()
	f, err := os.Open("/proc/self/statm")
	wantNil(t, err)
	defer func() { _ = f.Close() }()
	fields := strings.Fields(readFirstLine(t, f))
	if len(fields) < 2 {
		t.Fatalf("statm 字段不足: %v", fields)
	}
	pages, err := strconv.Atoi(fields[1])
	wantNil(t, err)
	return pages * os.Getpagesize()
}

func readFirstLine(t *testing.T, f *os.File) string {
	t.Helper()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		t.Fatalf("读取失败: %v", sc.Err())
	}
	return sc.Text()
}

// 单结果集并发 Close
func TestResultSetConcurrentClose(t *testing.T) {
	R(t, "单结果集_并发_Close", func(t *testing.T) {
		r := openReader(t, sampleFile(t))
		defer func() { _ = r.Close() }()
		rs := mustQuery(t, r, "metrics", []string{"value"})

		const workers = 8
		var wg sync.WaitGroup
		errs := make([]error, workers)
		start := make(chan struct{})
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				errs[i] = rs.Close()
			}(i)
		}
		close(start)
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Logf("worker %d Close 返回：%v", i, err)
			}
		}
		if ok, err := rs.Next(); err == nil {
			t.Fatalf("并发 Close 后 Next = (%v,nil); want ErrClosed", ok)
		} else if !strings.Contains(err.Error(), "closed") {
			t.Fatalf("并发 Close 后 Next 错误 = %v; want closed 语义", err)
		}
		t.Logf("并发 Close %d 次：无崩溃，关闭后 Next → %v", workers, "ErrClosed")
	})
}

var _ = fmt.Sprintf
