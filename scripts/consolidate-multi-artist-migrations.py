from pathlib import Path
import subprocess


def main():
    root = Path(__file__).resolve().parents[1]
    directory = root / "plugins/database/sqlite/migrations"
    previous = directory / "030_batch_011_030.sql"
    standalone = directory / "031_recording_covers.sql"
    target = directory / "031_batch_011_031.sql"
    old = previous.read_bytes()
    step = standalone.read_bytes()
    if not old.endswith(b"\n"):
        raise ValueError("Historical migration must end with a newline")
    marker = b"-- migration: " + standalone.name.encode("ascii") + b"\n"
    subprocess.run(["git", "mv", str(previous), str(target)], cwd=root, check=True)
    target.write_bytes(old + marker + step)
    subprocess.run(["git", "rm", str(standalone)], cwd=root, check=True)
    print("Consolidated SQLite migration 031 while preserving historical SQL")


if __name__ == "__main__":
    main()
