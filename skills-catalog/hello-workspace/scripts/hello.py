#!/usr/bin/env python3
import json
import os
import sys

def main():
    raw = os.environ.get("YZJ_SKILL_ARGS", "{}")
    try:
        args = json.loads(raw) if raw else {}
    except json.JSONDecodeError:
        args = {"raw": raw}
    text = args.get("text", "")
    print("hello-workspace ok")
    print("tool=", os.environ.get("YZJ_SKILL_TOOL", ""))
    print("workspace=", os.environ.get("YZJ_WORKSPACE", ""))
    print("text=", text)
    return 0

if __name__ == "__main__":
    sys.exit(main())
