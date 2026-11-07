"""TQ 가격 마스터(data/master.parquet)를 myhome `invest import-tq`가 읽는 CSV로 뽑는다.

1회용 마이그레이션 도구다. pandas·pyarrow가 있는 TQ 가상환경으로 돌린다.

    /path/to/TQ/.venv/bin/python -I scripts/tq_export.py /path/to/TQ/data/master.parquet master.csv

값은 repr로 써서 float64가 비트 단위로 그대로 옮겨진다. 결측은 빈 칸이다.
"""

import math
import sys

import pandas as pd

COLS = ["SPX", "NDX", "QQQ", "QQQM", "QLD", "TQQQ", "USDKRW", "USDKRW_MKT"]


def main(src: str, dst: str) -> None:
    df = pd.read_parquet(src).sort_values("date")
    missing = [c for c in COLS if c not in df.columns]
    if missing:
        raise SystemExit(f"마스터에 열이 없다: {missing}")
    with open(dst, "w", encoding="utf-8") as f:
        f.write("date," + ",".join(COLS) + "\n")
        for row in df.itertuples(index=False):
            vals = []
            for c in COLS:
                v = getattr(row, c)
                vals.append("" if v is None or (isinstance(v, float) and math.isnan(v)) else repr(float(v)))
            f.write(f"{row.date:%Y-%m-%d}," + ",".join(vals) + "\n")
    print(f"{len(df)}행 → {dst} ({df['date'].min():%Y-%m-%d} ~ {df['date'].max():%Y-%m-%d})")


if __name__ == "__main__":
    if len(sys.argv) != 3:
        raise SystemExit(__doc__)
    main(sys.argv[1], sys.argv[2])
