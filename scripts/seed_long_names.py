#!/usr/bin/env python3
import hashlib
import os
import sqlite3
import sys
import uuid
from datetime import datetime, timezone

db_path = sys.argv[1] if len(sys.argv) > 1 else os.environ.get("DB_DSN", "scratch/test.db")
con = sqlite3.connect(db_path)
cur = con.cursor()

now = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S")

long_torrents = [
    {
        "title": "ThisIsAnExtremelyLongTorrentTitleWithoutAnySpacesDesignedToTestTableOverflowAndColumnWrappingBehaviorInBitmagnetWebUI1234567890abcdefghijklmnopqrstuvwxyz",
        "kind": "movie",
        "size": 14500000000,
        "files": [
            "ThisIsAnExtremelyLongFileNameWithoutAnySpacesDesignedToTestFileBrowserColumnEllipsisAndPreventWrappingBehavior1234567890abcdefghijklmnopqrstuvwxyz.mkv",
            "Sample_VeryLongSampleVideoFileWithoutAnySpacesToEnsureBothFilesAndTorrentsAreProperlyElidedInTheWebUI.mkv",
        ],
    },
    {
        "title": "Ubuntu.24.04.LTS.Desktop.amd64.Automated.Installation.Image.With.Custom.Packages.And.Kernel.Modules.Ultra.High.Performance.Edition.SuperLongNameWithoutSpaces.iso",
        "kind": "software",
        "size": 4500000000,
        "files": [
            "Ubuntu.24.04.LTS.Desktop.amd64.Automated.Installation.Image.With.Custom.Packages.And.Kernel.Modules.Ultra.High.Performance.Edition.SuperLongNameWithoutSpaces.iso",
            "Ubuntu.24.04.LTS.Desktop.amd64.Automated.Installation.Image.With.Custom.Packages.And.Kernel.Modules.Ultra.High.Performance.Edition.SuperLongNameWithoutSpaces.sha256sum",
        ],
    },
    {
        "title": "Short Torrent Title",
        "kind": "movie",
        "size": 3200000000,
        "files": [
            "ThisIsAnExtremelyLongFileNameWithoutAnySpacesInsideAShortNamedTorrentPayloadToDemonstrateTableColumnEllipsisAndEnsureThatLongFilenamesAreProperlyElidedWithEllipsisEvenOnVeryWideScreensWithoutAnyWrappingOrBreaking1234567890abcdefghijklmnopqrstuvwxyz.mkv",
            "short.nfo",
        ],
    },
    {
        "title": "AnotherSuperLongTorrentTitleWithoutAnySpacesAtAllThatExceedsTheColumnWidthAndPushesAllOtherColumnsOffTheScreenOrCausesWrapping1234567890",
        "kind": "tv_show",
        "size": 8900000000,
        "files": [
            "AnotherSuperLongFileNameWithoutAnySpacesInsideThisTorrentPayloadToDemonstrateTableColumnEllipsis1234567890abcdefghijklmnopqrstuvwxyz.mkv"
        ],
    },
]

for idx, item in enumerate(long_torrents):
    info_hash_bytes = hashlib.sha1(f"long-name-torrent-{idx}".encode("utf-8")).digest()
    files_count = len(item["files"])
    files_status = "multi" if files_count > 1 else "single"
    total_size = item["size"]

    cur.execute(
        """INSERT OR REPLACE INTO torrents (info_hash, name, size, private, files_status, files_count, created_at, updated_at)
           VALUES (?, ?, ?, 0, ?, ?, ?, ?)""",
        (info_hash_bytes, item["title"], total_size, files_status, files_count, now, now),
    )

    cur.execute(
        """INSERT OR REPLACE INTO torrent_contents (id, info_hash, content_type, languages, episodes, video_resolution, video_source, video_codec, seeders, leechers, published_at, size, files_count, created_at, updated_at)
           VALUES (?, ?, ?, '["en"]', '{}', 'V1080p', 'WEBDL', 'x265', 125, 12, ?, ?, ?, ?, ?)""",
        (str(uuid.uuid4()), info_hash_bytes, item["kind"], now, total_size, files_count, now, now),
    )

    cur.execute(
        """INSERT OR REPLACE INTO torrents_torrent_sources (source, info_hash, seeders, leechers, published_at, created_at, updated_at)
           VALUES ('dht', ?, 125, 12, ?, ?, ?)""",
        (info_hash_bytes, now, now, now),
    )

    cur.execute("DELETE FROM torrent_files WHERE info_hash = ?", (info_hash_bytes,))

    for f_idx, f_path in enumerate(item["files"]):
        ext = f_path.rsplit(".", 1)[-1] if "." in f_path else "bin"
        cur.execute(
            """INSERT OR REPLACE INTO torrent_files (info_hash, "index", path, extension, size, created_at, updated_at)
               VALUES (?, ?, ?, ?, ?, ?, ?)""",
            (info_hash_bytes, f_idx, f_path, ext, total_size // files_count, now, now),
        )

con.commit()
con.close()
print(f"Seeded {len(long_torrents)} dummy torrents with long names (no spaces) into {db_path}")
