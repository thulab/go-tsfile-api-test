// 跨语言互读探针（原生侧，使用 libtsfile C API / 低层 C++ API）。
//
// 用法：
//   cpp_probe write <path>   写标准样本：表 metrics（device STRING/TAG + value DOUBLE/FIELD），
//                            默认 1200 行：第 i 行 ts=10*(i+1)、device=d1/d1/d2/d3 循环、
//                            value=i+1、i%4==2 时 value 为 NULL（与 Go 侧 bulkSample 一致）；
//                            行数可用环境变量 CASE_ROWS 覆盖（Go 测试进程会继承给本进程）
//   cpp_probe read  <path>   读标准样本并输出 ts|device|value（空值输出 NULL）
//   cpp_probe multi <path>   写两张表到同一文件：metrics（TAG=d1/d2/d3）+ metrics2（TAG=e1/e2/e3）
//
// 编译：interop/build.sh（g++ -I$SDK/include -L$SDK/lib -ltsfile -Wl,-rpath,$SDK/lib）
#include <algorithm>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <string>
#include <vector>

#include "common/schema.h"
#include "common/tablet.h"
#include "cwrapper/errno_define_c.h"
#include "cwrapper/tsfile_cwrapper.h"
#include "file/write_file.h"
#include "file/restorable_tsfile_io_writer.h"
#include "writer/tsfile_table_writer.h"
#include "writer/tsfile_writer.h"

// 与 C 头文件 TSDataType / ColumnCategory 取值一致（见 cwrapper/tsfile_cwrapper.h）
static const int kTypeDouble = 4;
static const int kTypeString = 11;
static const int kCatTag = 0;
static const int kCatField = 1;

// 样本行：与 Go 侧 tsuite/harness.go 的 bulkSample 完全一致（前 4 行即 10/d1/1.0、20/d1/2.0、
// 30/d2/NULL、40/d3/4.0）。metrics2 用 prefix='e' 生成同形状但 TAG 不同的样本。
struct SampleRow {
    int64_t ts;
    std::string device;
    double value;
    bool is_null;
};

static int sample_rows() {
    const char* env = std::getenv("CASE_ROWS");
    int n = env == nullptr ? 1200 : std::atoi(env);
    return n > 0 ? n : 1200;
}

// 单次写入的行数上限（与 Go 侧 writeSample 的 batch=500 对齐，避免依赖单 tablet 行数上限）。
static const size_t kWriteBatch = 500;

static std::vector<SampleRow> make_rows(char prefix, int n) {
    std::vector<SampleRow> rows;
    rows.reserve(static_cast<size_t>(n));
    for (int i = 0; i < n; i++) {
        SampleRow row;
        row.ts = 10LL * (i + 1);
        int mod = i % 4;
        row.device = std::string(1, prefix);
        row.device += (mod == 0 || mod == 1) ? '1' : (mod == 2 ? '2' : '3');
        row.is_null = (mod == 2);
        row.value = row.is_null ? 0.0 : static_cast<double>(i + 1);
        rows.push_back(row);
    }
    return rows;
}

static void die(const char* what, int code) {
    std::fprintf(stderr, "%s failed: code=%d\n", what, code);
    std::exit(1);
}

static TableSchema* make_schema(const char* table) {
    TableSchema* schema = static_cast<TableSchema*>(std::calloc(1, sizeof(TableSchema)));
    ColumnSchema* cols = static_cast<ColumnSchema*>(std::calloc(2, sizeof(ColumnSchema)));
    schema->table_name = ::strdup(table);
    schema->column_schemas = cols;
    schema->column_num = 2;
    cols[0].column_name = ::strdup("device");
    cols[0].data_type = static_cast<TSDataType>(kTypeString);
    cols[0].column_category = static_cast<ColumnCategory>(kCatTag);
    cols[1].column_name = ::strdup("value");
    cols[1].data_type = static_cast<TSDataType>(kTypeDouble);
    cols[1].column_category = static_cast<ColumnCategory>(kCatField);
    return schema;
}

static Tablet make_tablet(uint32_t rows) {
    char* names[2];
    names[0] = ::strdup("device");
    names[1] = ::strdup("value");
    TSDataType types[2] = {static_cast<TSDataType>(kTypeString), static_cast<TSDataType>(kTypeDouble)};
    ERRNO err = 0;
    Tablet tablet = tablet_new(names, types, 2, rows, &err);
    if (tablet == nullptr) die("tablet_new", err);
    return tablet;
}

// 用 C API 写单表样本
static void cmd_write(const char* path) {
    ERRNO err = 0;
    const std::vector<SampleRow> rows = make_rows('d', sample_rows());
    WriteFile file = write_file_new(path, &err);
    if (file == nullptr) die("write_file_new", err);
    TableSchema* schema = make_schema("metrics");
    TsFileWriter writer = tsfile_writer_new_with_memory_threshold(file, schema, 128u * 1024 * 1024, &err);
    if (writer == nullptr) die("tsfile_writer_new", err);
    for (size_t from = 0; from < rows.size(); from += kWriteBatch) {
        const size_t n = std::min(kWriteBatch, rows.size() - from);
        Tablet tablet = make_tablet(static_cast<uint32_t>(n));
        for (size_t row = 0; row < n; row++) {
            const SampleRow& sample = rows[from + row];
            int rc = tablet_add_timestamp(tablet, static_cast<uint32_t>(row), sample.ts);
            if (rc != 0) die("tablet_add_timestamp", rc);
            rc = tablet_add_value_by_index_string_with_len(tablet, static_cast<uint32_t>(row), 0,
                                                           sample.device.c_str(),
                                                           static_cast<int>(sample.device.size()));
            if (rc != 0) die("tablet_add_value(device)", rc);
            if (!sample.is_null) {
                rc = tablet_add_value_by_index_double(tablet, static_cast<uint32_t>(row), 1, sample.value);
                if (rc != 0) die("tablet_add_value(value)", rc);
            }
        }
        if (tsfile_writer_write(writer, tablet) != 0) die("tsfile_writer_write", 1);
    }
    if (tsfile_writer_flush(writer) != 0) die("tsfile_writer_flush", 1);
    if (tsfile_writer_close(writer) != 0) die("tsfile_writer_close", 1);
    std::printf("wrote metrics rows=%zu -> %s\n", rows.size(), path);
}

// 用 C API 读单表样本
static void cmd_read(const char* path) {
    ERRNO err = 0;
    TsFileReader reader = tsfile_reader_new(path, &err);
    if (reader == nullptr) die("tsfile_reader_new", err);
    char* columns[2];
    columns[0] = ::strdup("device");
    columns[1] = ::strdup("value");
    ResultSet rs = tsfile_reader_query_table(reader, "metrics", columns, 2,
                                             static_cast<Timestamp>(INT64_MIN),
                                             static_cast<Timestamp>(INT64_MAX), 0, -1, nullptr, 0, &err);
    if (rs == nullptr) die("tsfile_reader_query_table", err);
    int count = 0;
    while (true) {
        bool ok = tsfile_result_set_next(rs, &err);
        if (err != 0) die("tsfile_result_set_next", err);
        if (!ok) break;
        count++;
        // 结果集列索引从 1 开始：1=time、2=device、3=value
        int64_t ts = tsfile_result_set_get_value_by_index_int64_t(rs, 1);
        const char* device = tsfile_result_set_get_value_by_index_string(rs, 2);
        if (tsfile_result_set_is_null_by_index(rs, 3)) {
            std::printf("%lld|%s|NULL\n", static_cast<long long>(ts), device);
        } else {
            std::printf("%lld|%s|%.1f\n", static_cast<long long>(ts), device,
                        tsfile_result_set_get_value_by_index_double(rs, 3));
        }
    }
    tsfile_reader_close(reader);
    if (count != sample_rows()) {
        std::fprintf(stderr, "row count = %d; want %d\n", count, sample_rows());
        std::exit(1);
    }
}

// 写两张表到同一文件：同一 TableWriter 注册两张表后分别写入
static void cmd_multi(const char* path) {
    storage::libtsfile_init();
    const int n = sample_rows();
    const std::vector<SampleRow> rowsA = make_rows('d', n);
    const std::vector<SampleRow> rowsB = make_rows('e', n);

    std::vector<common::ColumnSchema> cols;
    cols.push_back(common::ColumnSchema("device", common::TSDataType::STRING, common::ColumnCategory::TAG));
    cols.push_back(common::ColumnSchema("value", common::TSDataType::DOUBLE, common::ColumnCategory::FIELD));
    std::vector<std::string> names = {"device", "value"};
    std::vector<common::TSDataType> types = {common::TSDataType::STRING, common::TSDataType::DOUBLE};

    storage::WriteFile file;
    if (file.create(path, O_WRONLY | O_CREAT | O_TRUNC, 0666) != 0) die("WriteFile::create", 1);
    storage::TableSchema schemaA("metrics", cols);
    storage::TableSchema schemaB("metrics2", cols);
    storage::TsFileTableWriter writer(&file, &schemaA);
    int rc = writer.register_table(std::make_shared<storage::TableSchema>(schemaB));
    if (rc != 0) die("register_table(metrics2)", rc);

    const char* table_names[2] = {"metrics", "metrics2"};
    const std::vector<SampleRow>* samples[2] = {&rowsA, &rowsB};
    for (int t = 0; t < 2; t++) {
        const std::vector<SampleRow>& rows = *samples[t];
        for (size_t from = 0; from < rows.size(); from += kWriteBatch) {
            const size_t batch = std::min(kWriteBatch, rows.size() - from);
            storage::Tablet tablet(names, types, static_cast<uint32_t>(batch));
            tablet.set_table_name(table_names[t]);
            for (size_t row = 0; row < batch; row++) {
                const SampleRow& sample = rows[from + row];
                tablet.add_timestamp(static_cast<int>(row), sample.ts);
                tablet.add_value(static_cast<int>(row), "device", sample.device);
                if (!sample.is_null) tablet.add_value(static_cast<int>(row), "value", sample.value);
            }
            rc = writer.write_table(tablet);
            if (rc != 0) die("write_table", rc);
        }
    }
    rc = writer.flush();
    if (rc != 0) die("flush", rc);
    rc = writer.close();
    if (rc != 0) die("close", rc);
    file.close();
    std::printf("wrote metrics(%d rows) + metrics2(%d rows) -> %s\n", n, n, path);
}

int main(int argc, char** argv) {
    if (argc < 3) {
        std::fprintf(stderr, "usage: cpp_probe {write|read|multi} <path>\n");
        return 2;
    }
    const std::string cmd = argv[1];
    if (cmd == "write") {
        cmd_write(argv[2]);
    } else if (cmd == "read") {
        cmd_read(argv[2]);
    } else if (cmd == "multi") {
        cmd_multi(argv[2]);
    } else {
        std::fprintf(stderr, "unknown command: %s\n", cmd.c_str());
        return 2;
    }
    return 0;
}
