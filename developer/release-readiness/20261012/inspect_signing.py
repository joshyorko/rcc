"""Inspect existing signature structures without asserting publisher identity."""
import hashlib
import json
import pathlib
import struct
import sys


def inspect(path):
    data = path.read_bytes()
    result = {"asset": path.name, "sha256": hashlib.sha256(data).hexdigest()}
    if data[:4] == b"\xcf\xfa\xed\xfe":
        count = struct.unpack_from("<I", data, 16)[0]
        cursor = 32
        signature = None
        for _ in range(count):
            command, size = struct.unpack_from("<II", data, cursor)
            if command == 0x1D:
                signature = struct.unpack_from("<II", data, cursor + 8)
            cursor += size
        if signature is None:
            result["signature"] = "no Mach-O LC_CODE_SIGNATURE"
            return result
        offset, size = signature
        magic, length, count = struct.unpack_from(">III", data, offset)
        assert magic == 0xFADE0CC0 and length <= size
        slots = dict(struct.unpack_from(">II", data, offset + 12 + 8 * n) for n in range(count))
        directory = offset + slots[0]
        fields = struct.unpack_from(">9I4B", data, directory)
        magic, length, version, flags, hash_offset, ident_offset, special, pages, limit, hash_size, hash_type, platform, page_power = fields
        assert magic == 0xFADE0C02 and hash_type == 2 and hash_size == 32
        assert pages == (limit + (1 << page_power) - 1) >> page_power
        for page in range(pages):
            start = page << page_power
            digest = hashlib.sha256(data[start:min(start + (1 << page_power), limit)]).digest()
            expected = data[directory + hash_offset + page * hash_size:directory + hash_offset + (page + 1) * hash_size]
            assert digest == expected, (path.name, page)
        result.update(signature="Mach-O embedded CodeDirectory", adHoc=bool(flags & 2), flags=flags, codePagesVerified=pages, cmsSignatureSlot=0x10000 in slots)
    elif data[:2] == b"MZ":
        pe = struct.unpack_from("<I", data, 0x3C)[0]
        assert data[pe:pe + 4] == b"PE\0\0"
        optional = pe + 24
        magic = struct.unpack_from("<H", data, optional)[0]
        directory = optional + (112 if magic == 0x20B else 96)
        offset, size = struct.unpack_from("<II", data, directory + 8 * 4)
        result.update(signature="PE Authenticode directory", certificateOffset=offset, certificateSize=size)
    elif data[:4] == b"\x7fELF":
        result["signature"] = "ELF; no detached vendor signature in preparation asset set"
    return result


if __name__ == "__main__":
    root = pathlib.Path(sys.argv[1])
    print(json.dumps([inspect(path) for path in sorted(root.glob("rcc*"))], indent=2))
