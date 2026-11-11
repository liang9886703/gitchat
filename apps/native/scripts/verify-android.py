"""Reject APK native libraries that cannot load on Android 16 KiB page devices."""
import json
import struct
import sys
import zipfile
from pathlib import Path


def verify(path):
    libraries = []
    with path.open("rb") as raw, zipfile.ZipFile(path) as apk:
        for entry in apk.infolist():
            if not (entry.filename.startswith("lib/") and entry.filename.endswith(".so")):
                continue
            data = apk.read(entry)
            if data[:4] != b"\x7fELF" or data[5] != 1:
                raise ValueError(f"Unexpected ELF format: {entry.filename}")
            if data[4] == 2:
                phoff = struct.unpack_from("<Q", data, 32)[0]
                stride, count = struct.unpack_from("<HH", data, 54)
                offset_at, address_at, align_at, integer = 8, 16, 48, "<Q"
            elif data[4] == 1:
                phoff = struct.unpack_from("<I", data, 28)[0]
                stride, count = struct.unpack_from("<HH", data, 42)
                offset_at, address_at, align_at, integer = 4, 8, 28, "<I"
            else:
                raise ValueError(f"Unknown ELF class: {entry.filename}")
            alignments = []
            for index in range(count):
                header = phoff + index * stride
                if struct.unpack_from("<I", data, header)[0] != 1:
                    continue
                offset = struct.unpack_from(integer, data, header + offset_at)[0]
                address = struct.unpack_from(integer, data, header + address_at)[0]
                alignment = struct.unpack_from(integer, data, header + align_at)[0]
                if alignment < 16384 or alignment & (alignment - 1) or offset % alignment != address % alignment:
                    raise ValueError(f"16 KiB ELF alignment failed: {entry.filename}, LOAD {index}, alignment {alignment}")
                alignments.append(alignment)
            if not alignments:
                raise ValueError(f"ELF has no loadable segments: {entry.filename}")
            if entry.compress_type == zipfile.ZIP_STORED:
                raw.seek(entry.header_offset + 26)
                name_length, extra_length = struct.unpack("<HH", raw.read(4))
                data_offset = entry.header_offset + 30 + name_length + extra_length
                if data_offset % 16384:
                    raise ValueError(f"16 KiB ZIP alignment failed: {entry.filename}")
            libraries.append({"name": entry.filename, "bytes": len(data), "minimum_load_alignment": min(alignments)})
    if not libraries:
        raise ValueError("APK contains no native libraries")
    return {"apk": path.name, "bytes": path.stat().st_size, "libraries": libraries, "page_alignment": "passed"}


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("Usage: python3 scripts/verify-android.py path/to/app.apk")
    print(json.dumps(verify(Path(sys.argv[1])), indent=2))
