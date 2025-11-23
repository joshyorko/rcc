# CLI Contract: rcc robot unpack

## Command

`rcc robot unpack [flags]`

## Flags

| Flag | Short | Type | Required | Description |
| :--- | :--- | :--- | :--- | :--- |
| `--bundle` | `-b` | String | Yes | Path to the source bundle file (zip). |
| `--output` | `-o` | String | Yes | Path to the destination directory. |
| `--force` | `-f` | Bool | No | Force overwrite of existing files in the output directory. |

## Exit Codes

*   `0`: Success.
*   `1`: Invalid arguments (missing bundle/output) or output directory exists (without --force).
*   `2`: Bundle file access error (not found, permission denied).
*   `3`: Extraction error (invalid zip, write error).

## Output

*   **Success**:
    *   Stdout: "OK." (Standard RCC success message)
*   **Error**:
    *   Stderr: Error description (e.g., "Error: Output directory 'xxx' already exists.").
