# checks if files are identical, if not, checks if tails are same with appended content or actually different
# run chmod +x ./diff.sh first

if [ "$#" -ne 2 ]; then
    echo "usage: $0 <file1> <file2>"
    exit 1
fi

if [ ! -f "$1" ]; then
    echo "error: '$1' is not a file"
    exit 1
fi

if [ ! -f "$2" ]; then
    echo "error: '$2' is not a file"
    exit 1
fi

python3 - "$@" <<PY
from pathlib import Path
import sys

a = Path(sys.argv[1]).read_bytes()
b = Path(sys.argv[2]).read_bytes()

if a == b:
    print("Files are identical YAY.")
    sys.exit(0)

print("len(a) =", len(a))
print("len(b) =", len(b))
print("b starts with a:", b[:len(a)] == a)

if b[:len(a)] != a:
    for i, (x, y) in enumerate(zip(a, b)):
        if x != y:
            print("first mismatch at byte", i, "orig=", x, "recv=", y)
            break

positions = [i for i in range(len(b)-1) if b[i] == 0xff and b[i+1] == 0xd9]
print("first few ffd9 positions in received:", positions[:10])
print("received tail:", b[-16:].hex())
PY