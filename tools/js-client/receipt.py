"""Independent parser for the product's byte-framed receipts, including NUL."""


def parse(data, require_complete=True):
    frames = []
    position = 0
    while position < len(data):
        end = data.index(b"\n", position)
        kind, count = data[position:end].split(b" ")
        if kind not in (b"H", b"O", b"D", b"R", b"T", b"Z") or not count.isdigit():
            raise ValueError("InvalidReceiptFrame")
        position = end + 1
        length = int(count)
        if length > len(data) - position:
            raise ValueError("TruncatedReceipt")
        frames.append((kind.decode(), data[position:position + length]))
        position += length
    if not frames or frames[0] != ("H", b"QJS/1\n") or (require_complete and frames[-1][0] != "Z"):
        raise ValueError("IncompleteReceipt")
    return frames


def fields(row):
    return dict(item.split("=", 1) for item in row.decode().strip().split(" "))


def output(frames):
    return b"".join(data for kind, data in frames if kind == "O")


def results(frames):
    return [fields(data) for kind, data in frames if kind == "R"]
