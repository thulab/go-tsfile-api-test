#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""CASE_ID → go test 节点（ParentTest/CaseID）。用法：python3 map_lookup.py RN145-005"""
import json, sys


def main():
    if len(sys.argv) < 2:
        print("usage: map_lookup.py <CASE_ID>", file=sys.stderr)
        return 2
    cid = sys.argv[1].strip()
    m = json.load(open(".plm-case-id-map.json", encoding="utf-8"))
    nmap = m.get("nmap", m)
    aliases = m.get("aliases") or {}
    for table in (nmap, aliases):
        for key, nodes in table.items():
            if key.upper() != cid.upper():
                continue
            node = nodes[0] if isinstance(nodes, list) else nodes
            print(node)
            return 0
    return 1


if __name__ == "__main__":
    sys.exit(main())
