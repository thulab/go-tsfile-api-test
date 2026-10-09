#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""go test -json → JUnit XML（用例名=子测试名=CASE_ID；case_id 属性可指定为 PLM CASE_ID）。"""
import json, sys
from xml.etree import ElementTree as ET


def main():
    if len(sys.argv) < 3:
        print("usage: xml_from_gojson.py <in.jsonl> <out.xml> [case_id]", file=sys.stderr)
        return 2
    inp, outp = sys.argv[1], sys.argv[2]
    wanted = (sys.argv[3] if len(sys.argv) > 3 else "").strip()

    results, order = {}, []
    with open(inp, encoding="utf-8", errors="replace") as fh:
        for line in fh:
            line = line.strip()
            if not line.startswith("{"):
                continue
            try:
                ev = json.loads(line)
            except json.JSONDecodeError:
                continue
            test = ev.get("Test")
            if not test:
                continue
            entry = results.setdefault(test, {"output": [], "elapsed": 0.0})
            if test not in order:
                order.append(test)
            action = ev.get("Action")
            if action == "output":
                entry["output"].append(ev.get("Output", ""))
            elif action in ("pass", "fail", "skip"):
                entry["action"] = action
                entry["elapsed"] = ev.get("Elapsed", entry["elapsed"])

    leaves = [n for n in order if "/" in n] or order
    root = ET.Element("testsuites", name="go-tsfile-api-test")
    suite = ET.SubElement(root, "testsuite", name="tsuite")
    n_pass = n_fail = n_skip = 0
    for name in leaves:
        entry = results[name]
        action = entry.get("action")
        if action not in ("pass", "fail", "skip"):
            continue
        parent, _, case = name.rpartition("/")
        case = case or name
        cid = wanted if (wanted and case.upper() == wanted.upper()) else case
        tc = ET.SubElement(suite, "testcase", classname=parent, name=case, time=f"{entry['elapsed']:.3f}")
        if action == "fail":
            n_fail += 1
            ET.SubElement(tc, "failure", message="failed").text = "".join(entry["output"])[-4000:]
        elif action == "skip":
            n_skip += 1
            ET.SubElement(tc, "skipped", message=("".join(entry["output"]).strip()[:400] or "skipped"))
        else:
            n_pass += 1
        props = ET.SubElement(tc, "properties")
        ET.SubElement(props, "property", {"name": "case_id", "value": cid})
    suite.set("tests", str(n_pass + n_fail + n_skip))
    suite.set("failures", str(n_fail))
    suite.set("skipped", str(n_skip))
    ET.ElementTree(root).write(outp, encoding="utf-8", xml_declaration=True)
    print(f"[xml] {outp}: pass={n_pass} fail={n_fail} skip={n_skip}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
