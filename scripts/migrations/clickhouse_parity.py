import glob, os, re, sys

SETS = {
    "legacy": ("migrations/clickhouse", False),
    "versioned": ("migrations/versioned/clickhouse", True),
    "replicated": ("migrations/versioned/clickhouse/replicated", True),
}


def strip_comments(s):
    return re.sub(r"--[^\n]*", "", s)


def up_section(s, dbmate):
    if not dbmate:
        return s
    m = re.search(r"-- migrate:up(.*?)(?:-- migrate:down|\Z)", s, re.S)
    return m.group(1) if m else ""


def split_top(s, sep=","):
    parts, depth, cur, quote = [], 0, "", None
    for ch in s:
        if quote:
            cur += ch
            if ch == quote:
                quote = None
            continue
        if ch in "'\"`":
            quote = ch
        elif ch == "(":
            depth += 1
        elif ch == ")":
            depth -= 1
        elif ch == sep and depth == 0:
            parts.append(cur)
            cur = ""
            continue
        cur += ch
    if cur.strip():
        parts.append(cur)
    return parts


def norm(s):
    return re.sub(r"\s+", " ", s.replace("`", "")).strip()


def balanced(s, start):
    depth = 0
    for i in range(start, len(s)):
        if s[i] == "(":
            depth += 1
        elif s[i] == ")":
            depth -= 1
            if depth == 0:
                return i
    raise ValueError("unbalanced parens")


def parse_index(item):
    m = re.match(r"INDEX\s+(?:IF NOT EXISTS\s+)?(\w+)\s+(.+?)\s+TYPE\s+(.+?)\s+GRANULARITY\s+(\d+)$", norm(item), re.I | re.S)
    if not m:
        raise ValueError("unparsable index: " + norm(item))
    return m.group(1), (norm(m.group(2)), norm(m.group(3)), int(m.group(4)))


def new_table():
    return {"cols": set(), "idx": {}, "proj": set()}


def apply_item(t, item):
    it = norm(item)
    up = it.upper()
    if up.startswith("INDEX") or up.startswith("ADD INDEX"):
        name, spec = parse_index(re.sub(r"^ADD\s+", "", it, flags=re.I))
        t["idx"][name] = spec
    elif up.startswith("PROJECTION") or up.startswith("ADD PROJECTION"):
        m = re.match(r"(?:ADD\s+)?PROJECTION\s+(?:IF NOT EXISTS\s+)?(\w+)", it, re.I)
        t["proj"].add(m.group(1))
    elif up.startswith("ADD COLUMN"):
        m = re.match(r"ADD COLUMN\s+(?:IF NOT EXISTS\s+)?(\w+)", it, re.I)
        t["cols"].add(m.group(1))
    elif up.startswith("DROP INDEX"):
        t["idx"].pop(re.match(r"DROP INDEX\s+(?:IF EXISTS\s+)?(\w+)", it, re.I).group(1), None)
    elif up.startswith("DROP PROJECTION"):
        t["proj"].discard(re.match(r"DROP PROJECTION\s+(?:IF EXISTS\s+)?(\w+)", it, re.I).group(1))
    elif up.startswith("DROP COLUMN"):
        t["cols"].discard(re.match(r"DROP COLUMN\s+(?:IF EXISTS\s+)?(\w+)", it, re.I).group(1))
    elif up.startswith("CONSTRAINT") or up.startswith("MODIFY") or up.startswith("MATERIALIZE"):
        pass
    else:
        t["cols"].add(it.split(" ")[0])


def load(path, dbmate):
    tables = {}
    for f in sorted(glob.glob(os.path.join(path, "*.sql"))):
        body = strip_comments(up_section(open(f).read(), dbmate))
        for stmt in body.split(";"):
            stmt = stmt.strip()
            if not stmt:
                continue
            m = re.match(r"CREATE TABLE\s+(?:IF NOT EXISTS\s+)?flexprice\.(\w+)", stmt, re.I)
            if m:
                tbl = tables.setdefault(m.group(1), new_table())
                s = stmt.index("(", m.end())
                e = balanced(stmt, s)
                for item in split_top(stmt[s + 1:e]):
                    apply_item(tbl, item)
                continue
            m = re.match(r"ALTER TABLE\s+flexprice\.(\w+)(?:\s+ON CLUSTER\s+'[^']*')?\s+(.*)$", stmt, re.I | re.S)
            if not m:
                sys.exit("unrecognised statement in %s: %s" % (f, norm(stmt)[:80]))
            if m.group(1) not in tables:
                sys.exit("ALTER before CREATE for %s in %s" % (m.group(1), f))
            for item in split_top(m.group(2)):
                apply_item(tables[m.group(1)], item)
    return tables


def main():
    sets = {k: load(p, d) for k, (p, d) in SETS.items()}
    base = sets["replicated"]
    problems = []
    for name, tabs in sets.items():
        if name == "replicated":
            continue
        for t in sorted(set(base) | set(tabs)):
            if t not in tabs:
                problems.append("%s: table %s missing (present in replicated)" % (name, t))
                continue
            if t not in base:
                problems.append("%s: table %s extra (absent in replicated)" % (name, t))
                continue
            a, b = base[t], tabs[t]
            for label, x, y in (("columns", a["cols"], b["cols"]), ("projections", a["proj"], b["proj"])):
                if x != y:
                    problems.append("%s.%s %s: only in replicated %s; only in %s %s" % (
                        name, t, label, sorted(x - y), name, sorted(y - x)))
            for i in sorted(set(a["idx"]) | set(b["idx"])):
                if a["idx"].get(i) != b["idx"].get(i):
                    problems.append("%s.%s index %s: replicated=%s %s=%s" % (
                        name, t, i, a["idx"].get(i), name, b["idx"].get(i)))
    if problems:
        print("FAIL: ClickHouse sets disagree:", file=sys.stderr)
        for p in problems:
            print("  " + p, file=sys.stderr)
        sys.exit(1)
    print("clickhouse parity: %d tables identical across %d sets" % (len(base), len(sets)))


main()
