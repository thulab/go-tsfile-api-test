#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""跨语言互读样本工具（Python 侧）。

标准样本：表 metrics（device STRING/TAG + value DOUBLE/FIELD），默认 1200 行，
第 i 行：ts = 10*(i+1)、device = d1,d1,d2,d3 循环、value = i+1、i%4==2 时 value 为 NULL
（与 Go 侧 tsuite/harness.go 的 bulkSample 完全一致）。行数可用环境变量 CASE_ROWS 覆盖。
metrics2 写的是"形状相同、TAG 前缀换成 e（e1/e2/e3）"的样本，用于区分两张表的数据。

用法：
  python3 interop/py_sample.py write <path>        # 写标准单表样本
  python3 interop/py_sample.py multi <path>        # 写两张表（metrics + metrics2）
  python3 interop/py_sample.py read  <path>        # 读标准样本，输出 ts|device|value（NULL 输出 NULL）
  python3 interop/py_sample.py read-multi <path>   # 输出全部表名与列名
"""
import os
import sys

from tsfile import (ColumnCategory, ColumnSchema, TableSchema, Tablet,
                    TSDataType, TsFileReader, TsFileTableWriter)

INT64_MIN = -(2 ** 63)
INT64_MAX = 2 ** 63 - 1


def sample_rows(prefix="d"):
    """生成标准样本：# (timestamp, device, value, is_null)。"""
    n = int(os.environ.get("CASE_ROWS", "1200"))
    rows = []
    for i in range(n):
        mod = i % 4
        device = "%s1" % prefix if mod in (0, 1) else ("%s2" % prefix if mod == 2 else "%s3" % prefix)
        is_null = mod == 2
        rows.append((10 * (i + 1), device, None if is_null else float(i + 1), is_null))
    return rows


ROWS = sample_rows()
ROWS2 = sample_rows("e")


def table_schema(name):
    return TableSchema(name, [
        ColumnSchema("device", TSDataType.STRING, ColumnCategory.TAG),
        ColumnSchema("value", TSDataType.DOUBLE, ColumnCategory.FIELD),
    ])


def write_table(path, table, rows):
    writer = TsFileTableWriter(path, table_schema(table))
    tablet = Tablet(["device", "value"], [TSDataType.STRING, TSDataType.DOUBLE], len(rows))
    for row, (ts, device, value, is_null) in enumerate(rows):
        tablet.add_timestamp(row, ts)
        tablet.add_value_by_index(0, row, device)
        if not is_null:
            tablet.add_value_by_index(1, row, value)
    writer.write_table(tablet)
    writer.close()


def cmd_write(path):
    write_table(path, "metrics", ROWS)
    print("wrote metrics rows=%d -> %s" % (len(ROWS), path))


def cmd_multi(path):
    write_table(path, "metrics", ROWS)
    write_table(path, "metrics2", ROWS2)
    print("wrote metrics rows=%d + metrics2 rows=%d -> %s" % (len(ROWS), len(ROWS2), path))


def cmd_read(path):
    with TsFileReader(path) as reader:
        with reader.query_table("metrics", ["device", "value"], INT64_MIN, INT64_MAX) as rs:
            # 结果集列索引从 1 开始：1=time、2=device、3=value
            while rs.next():
                ts = rs.get_value_by_index(1)
                device = rs.get_value_by_index(2)
                value = "NULL" if rs.is_null_by_index(3) == 1 else repr(rs.get_value_by_index(3))
                print("%s|%s|%s" % (ts, device, value))


def cmd_read_multi(path):
    with TsFileReader(path) as reader:
        for name in reader.get_all_table_schemas():
            schema = reader.get_table_schema(name)
            cols = ",".join("%s:%s" % (c.column_name, c.category) for c in schema.columns)
            print("%s|%s" % (name, cols))


def main():
    if len(sys.argv) < 3:
        print(__doc__)
        return 2
    cmd, path = sys.argv[1], sys.argv[2]
    {"write": cmd_write, "multi": cmd_multi, "read": cmd_read, "read-multi": cmd_read_multi}[cmd](path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
