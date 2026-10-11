"""Small path helpers for collecting the platform-specific RCC package data."""


def is_rcc_data(data: tuple[str, str]) -> bool:
    """Recognize the pinned RCC package data across POSIX and Windows paths."""
    source, destination = data
    source_name = source.replace("\\", "/").rsplit("/", 1)[-1]
    destination_name = destination.replace("\\", "/")
    return destination_name == "actions/server/bin" and source_name.startswith("rcc-")
