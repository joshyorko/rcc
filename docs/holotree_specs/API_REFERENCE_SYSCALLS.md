# Platform-Specific File Copy API Reference

Quick reference for syscalls and APIs used in file copy optimizations.

---

## LINUX

### 1. copy_file_range() - Kernel-Space Copy

**Availability**: Linux kernel 4.5+ (2016)
**Go Package**: `golang.org/x/sys/unix`

```go
func unix.CopyFileRange(
    rfd int,          // source file descriptor
    roff *int64,      // source offset (updated on return)
    wfd int,          // destination file descriptor
    woff *int64,      // destination offset (nil = auto-increment)
    len int,          // bytes to copy
    flags int,        // flags (usually 0)
) (n int, err error)
```

**Example**:
```go
var offset int64 = 0
n, err := unix.CopyFileRange(
    int(src.Fd()), &offset,
    int(dst.Fd()), nil,
    int(fileSize),
    0,
)
```

**Returns**: Number of bytes copied
**Errors**:
- `ENOSYS` - syscall not supported (old kernel)
- `EXDEV` - files on different filesystems
- `EINVAL` - invalid parameters

**Notes**:
- Can copy up to 2GB per call
- May internally use splice() or read/write
- Works across filesystems but faster on same FS

---

### 2. FICLONE ioctl - Reflink Clone

**Availability**: Btrfs, XFS 4.16+, OCFS2
**Go Package**: `golang.org/x/sys/unix`

```go
const FICLONE = 0x40049409

_, _, errno := unix.Syscall(
    unix.SYS_IOCTL,
    uintptr(dstFd),    // destination file descriptor
    FICLONE,           // ioctl command
    uintptr(srcFd),    // source file descriptor
)
```

**Example**:
```go
const FICLONE = 0x40049409
_, _, errno := unix.Syscall(
    unix.SYS_IOCTL,
    dst.Fd(),
    FICLONE,
    src.Fd(),
)
if errno != 0 {
    return errno
}
```

**Returns**: 0 on success
**Errors**:
- `EOPNOTSUPP` - filesystem doesn't support reflinks
- `EXDEV` - files on different filesystems
- `EINVAL` - invalid file descriptors

**Notes**:
- Instant copy via copy-on-write
- No disk space used initially
- Must be same filesystem
- Destination file must be empty/newly created

---

### 3. fallocate() - Pre-allocate Space

**Availability**: Linux 2.6.23+
**Go Package**: `golang.org/x/sys/unix`

```go
func unix.Fallocate(
    fd int,       // file descriptor
    mode uint32,  // allocation mode (0 for default)
    off int64,    // offset
    len int64,    // length to allocate
) error
```

**Example**:
```go
err := unix.Fallocate(int(dst.Fd()), 0, 0, fileSize)
if err != nil && err != unix.EOPNOTSUPP {
    return err
}
```

**Returns**: nil on success
**Errors**:
- `EOPNOTSUPP` - filesystem doesn't support fallocate
- `ENOSPC` - no space left on device

**Notes**:
- Reduces fragmentation
- Faster writes after allocation
- Not supported on all filesystems (e.g., tmpfs)

---

### 4. O_DIRECT - Direct I/O

**Availability**: Linux 2.4+
**Go Package**: `golang.org/x/sys/unix`

```go
fd, err := unix.Open(
    path,
    unix.O_RDONLY|unix.O_DIRECT,  // flags
    0,                              // mode
)
```

**Example**:
```go
src, err := unix.Open(source, unix.O_RDONLY|unix.O_DIRECT, 0)
dst, err := unix.Open(target,
    unix.O_WRONLY|unix.O_CREATE|unix.O_TRUNC|unix.O_DIRECT,
    0644)
```

**Returns**: File descriptor
**Errors**:
- `EINVAL` - O_DIRECT not supported on filesystem

**Notes**:
- Bypasses page cache
- Requires aligned buffers (typically 512B or 4KB)
- Best for large sequential I/O
- Complex buffer management

**Buffer Alignment**:
```go
const alignment = 4096
buffer := make([]byte, bufSize+alignment)
alignedBuf := buffer
if offset := int(uintptr(unsafe.Pointer(&buffer[0]))) % alignment; offset != 0 {
    alignedBuf = buffer[alignment-offset:]
}
```

---

## MACOS

### 1. clonefile() - APFS Clone

**Availability**: macOS 10.12+, APFS only
**Syscall Number**: 462

```go
const SYS_CLONEFILE = 462

_, _, errno := syscall.Syscall(
    SYS_CLONEFILE,
    uintptr(unsafe.Pointer(srcPtr)),  // source path (C string)
    uintptr(unsafe.Pointer(dstPtr)),  // dest path (C string)
    0,                                 // flags
)
```

**Example**:
```go
const SYS_CLONEFILE = 462

srcPtr, _ := syscall.BytePtrFromString(source)
dstPtr, _ := syscall.BytePtrFromString(target)

_, _, errno := syscall.Syscall(
    SYS_CLONEFILE,
    uintptr(unsafe.Pointer(srcPtr)),
    uintptr(unsafe.Pointer(dstPtr)),
    0,
)
if errno != 0 {
    return errno
}
```

**Returns**: 0 on success
**Errors**:
- `ENOTSUP` - not on APFS filesystem
- `EXDEV` - files on different volumes
- `EEXIST` - destination already exists

**Flags** (optional):
```go
const (
    CLONE_NOFOLLOW = 0x0001    // Don't follow symlinks
    CLONE_NOOWNERCOPY = 0x0002 // Don't copy ownership
)
```

**Notes**:
- Instant copy via CoW
- APFS only (not HFS+)
- Same volume only
- Preserves metadata, xattrs, ACLs

---

### 2. copyfile() - Smart Copy with Clone

**Availability**: macOS 10.5+
**Via CGO**: Requires C binding

```c
#include <copyfile.h>

int copyfile(
    const char *from,
    const char *to,
    copyfile_state_t state,
    copyfile_flags_t flags
);
```

**Flags**:
```c
COPYFILE_ALL   // Copy everything
COPYFILE_CLONE // Try CoW, fallback to copy
```

**Go Example** (requires CGO):
```go
/*
#include <copyfile.h>
int copy_with_clone(const char *src, const char *dst) {
    copyfile_state_t state = copyfile_state_alloc();
    int ret = copyfile(src, dst, state, COPYFILE_ALL | COPYFILE_CLONE);
    copyfile_state_free(state);
    return ret;
}
*/
import "C"

func CopyFileMacOS(source, target string) error {
    srcC := C.CString(source)
    defer C.free(unsafe.Pointer(srcC))

    dstC := C.CString(target)
    defer C.free(unsafe.Pointer(dstC))

    ret := C.copy_with_clone(srcC, dstC)
    if ret != 0 {
        return fmt.Errorf("copyfile failed: %d", ret)
    }
    return nil
}
```

**Notes**:
- Automatic fallback if CoW not available
- Copies metadata, xattrs, resource forks
- Works on HFS+ and APFS
- Requires CGO (or exec `/bin/cp -c`)

---

### 3. F_NOCACHE - Direct I/O

**Availability**: All macOS versions
**Go Package**: `golang.org/x/sys/unix`

```go
const F_NOCACHE = 48

err := unix.FcntlInt(
    uintptr(fd),   // file descriptor
    unix.F_NOCACHE,
    1,             // enable (1) or disable (0)
)
```

**Example**:
```go
const F_NOCACHE = 48

src, _ := os.Open(source)
dst, _ := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)

// Enable F_NOCACHE on both files
unix.FcntlInt(src.Fd(), unix.F_NOCACHE, 1)
unix.FcntlInt(dst.Fd(), unix.F_NOCACHE, 1)

// Now copy
buffer := make([]byte, 1024*1024)
io.CopyBuffer(dst, src, buffer)
```

**Returns**: 0 on success
**Errors**: Rarely fails

**Notes**:
- Bypasses page cache
- Good for large files
- Use larger buffers (1MB+)
- No alignment requirements (unlike Linux O_DIRECT)

---

## WINDOWS

### 1. CopyFileEx - Optimized Copy

**Availability**: Windows XP+
**DLL**: kernel32.dll

```go
var (
    kernel32       = syscall.NewLazyDLL("kernel32.dll")
    procCopyFileEx = kernel32.NewProc("CopyFileExW")
)

ret, _, err := procCopyFileEx.Call(
    uintptr(unsafe.Pointer(srcPtr)),     // source path (UTF-16)
    uintptr(unsafe.Pointer(dstPtr)),     // dest path (UTF-16)
    0,                                    // progress callback
    0,                                    // callback data
    0,                                    // cancel flag
    uintptr(flags),                      // copy flags
)
```

**Flags**:
```go
const (
    COPY_FILE_FAIL_IF_EXISTS = 0x00000001
    COPY_FILE_RESTARTABLE    = 0x00000002
    COPY_FILE_COPY_SYMLINK   = 0x00000800
    COPY_FILE_NO_BUFFERING   = 0x00001000
)
```

**Example**:
```go
var (
    kernel32       = syscall.NewLazyDLL("kernel32.dll")
    procCopyFileEx = kernel32.NewProc("CopyFileExW")
)

func CopyFileWindows(source, target string, overwrite bool) error {
    srcPtr, _ := syscall.UTF16PtrFromString(source)
    dstPtr, _ := syscall.UTF16PtrFromString(target)

    flags := uint32(COPY_FILE_COPY_SYMLINK)
    if !overwrite {
        flags |= COPY_FILE_FAIL_IF_EXISTS
    }

    ret, _, err := procCopyFileEx.Call(
        uintptr(unsafe.Pointer(srcPtr)),
        uintptr(unsafe.Pointer(dstPtr)),
        0, 0, 0,
        uintptr(flags),
    )

    if ret == 0 {
        return err
    }
    return nil
}
```

**Returns**: Non-zero on success
**Errors**: GetLastError() for details

**Notes**:
- Kernel-optimized copy
- Preserves attributes and security
- Supports progress callbacks
- Works on all filesystems

---

### 2. FSCTL_DUPLICATE_EXTENTS_TO_FILE - ReFS Clone

**Availability**: Windows Server 2016+, Windows 10 1709+, ReFS only
**Go Package**: `golang.org/x/sys/windows`

```go
const FSCTL_DUPLICATE_EXTENTS_TO_FILE = 0x00098344

type DUPLICATE_EXTENTS_DATA struct {
    FileHandle       windows.Handle
    SourceFileOffset int64
    TargetFileOffset int64
    ByteCount        int64
}

err := windows.DeviceIoControl(
    dstHandle,                              // destination file
    FSCTL_DUPLICATE_EXTENTS_TO_FILE,        // ioctl code
    (*byte)(unsafe.Pointer(&dupData)),      // input buffer
    uint32(unsafe.Sizeof(dupData)),         // input size
    nil,                                     // output buffer
    0,                                       // output size
    &bytesReturned,                         // bytes returned
    nil,                                     // overlapped
)
```

**Example**:
```go
import "golang.org/x/sys/windows"

const FSCTL_DUPLICATE_EXTENTS_TO_FILE = 0x00098344

type DUPLICATE_EXTENTS_DATA struct {
    FileHandle       windows.Handle
    SourceFileOffset int64
    TargetFileOffset int64
    ByteCount        int64
}

func CloneFileReFS(source, target string) error {
    // Open source
    srcHandle, _ := windows.CreateFile(
        syscall.StringToUTF16Ptr(source),
        windows.GENERIC_READ,
        windows.FILE_SHARE_READ,
        nil,
        windows.OPEN_EXISTING,
        windows.FILE_ATTRIBUTE_NORMAL,
        0,
    )
    defer windows.CloseHandle(srcHandle)

    // Get file size
    var fileInfo windows.ByHandleFileInformation
    windows.GetFileInformationByHandle(srcHandle, &fileInfo)
    fileSize := int64(fileInfo.FileSizeHigh)<<32 | int64(fileInfo.FileSizeLow)

    // Create destination
    dstHandle, _ := windows.CreateFile(
        syscall.StringToUTF16Ptr(target),
        windows.GENERIC_WRITE,
        0,
        nil,
        windows.CREATE_ALWAYS,
        windows.FILE_ATTRIBUTE_NORMAL,
        0,
    )
    defer windows.CloseHandle(dstHandle)

    // Clone
    dupData := DUPLICATE_EXTENTS_DATA{
        FileHandle:       srcHandle,
        SourceFileOffset: 0,
        TargetFileOffset: 0,
        ByteCount:        fileSize,
    }

    var bytesReturned uint32
    err := windows.DeviceIoControl(
        dstHandle,
        FSCTL_DUPLICATE_EXTENTS_TO_FILE,
        (*byte)(unsafe.Pointer(&dupData)),
        uint32(unsafe.Sizeof(dupData)),
        nil, 0, &bytesReturned, nil,
    )

    return err
}
```

**Returns**: nil on success
**Errors**:
- `ERROR_INVALID_FUNCTION` - not on ReFS
- `ERROR_BLOCK_TOO_MANY_REFERENCES` - too many clones

**Notes**:
- Instant copy via CoW
- ReFS only (not NTFS)
- Same volume only
- Maximum 4095 clones per block

---

## Summary Table

| Platform | Method | Syscall/API | Speed | Availability |
|----------|--------|-------------|-------|--------------|
| **Linux** | copy_file_range | syscall 326 | 2-3x | Kernel 4.5+ |
| **Linux** | FICLONE | ioctl 0x40049409 | 100x+ | Btrfs/XFS/OCFS2 |
| **Linux** | O_DIRECT | open flag | 1.5x | All (complex) |
| **macOS** | clonefile | syscall 462 | 100x+ | APFS only |
| **macOS** | copyfile | libc function | 1.5x | All (CGO) |
| **macOS** | F_NOCACHE | fcntl flag | 1.5x | All |
| **Windows** | CopyFileEx | kernel32.dll | 1.5-2x | All |
| **Windows** | ReFS Clone | DeviceIoControl | 100x+ | ReFS only |

---

## Quick Decision Tree

```
Is same filesystem AND CoW supported?
  ├─ Yes → Use FICLONE/clonefile/ReFS clone (100x+ faster)
  └─ No  ↓

Is Linux?
  ├─ Yes → Use copy_file_range (2-3x faster)
  └─ No  ↓

Is Windows?
  ├─ Yes → Use CopyFileEx (1.5-2x faster)
  └─ No  ↓

Is macOS?
  ├─ Yes → Use F_NOCACHE for large files (1.5x faster)
  └─ No  ↓

Fallback to io.Copy (baseline)
```

---

## Error Handling Pattern

```go
func platformCopy(source, target string) error {
    // Try fastest (CoW)
    if err := tryReflink(source, target); err == nil {
        return nil
    }

    // Try fast (kernel-space)
    if err := tryKernelCopy(source, target); err == nil {
        return nil
    }

    // Return error to trigger fallback in caller
    return fmt.Errorf("platform optimizations not available")
}

// Caller handles fallback
func FastCopy(source, target string) error {
    if err := platformCopy(source, target); err != nil {
        return copyFileStandard(source, target) // io.Copy
    }
    return nil
}
```

---

## Testing Syscall Availability

```go
// Linux
func supportsCopyFileRange() bool {
    _, err := unix.CopyFileRange(0, nil, 0, nil, 0, 0)
    return err != unix.ENOSYS
}

// macOS
func supportsClonefile() bool {
    // Create temp files and try
    src, _ := os.CreateTemp("", "test")
    defer os.Remove(src.Name())

    dst := src.Name() + ".clone"
    defer os.Remove(dst)

    err := cloneFile(src.Name(), dst)
    return err != syscall.ENOTSUP
}

// Windows
func supportsReFS(path string) bool {
    // Check filesystem type
    var volumeNameBuffer [256]uint16
    var fsNameBuffer [256]uint16

    err := windows.GetVolumeInformation(
        syscall.StringToUTF16Ptr(path),
        &volumeNameBuffer[0], 256,
        nil, nil, nil,
        &fsNameBuffer[0], 256,
    )

    if err != nil {
        return false
    }

    fsName := syscall.UTF16ToString(fsNameBuffer[:])
    return fsName == "ReFS"
}
```

---

## Performance Measurement

```go
import "time"

func measureCopy(source, target string, method func(string, string) error) time.Duration {
    start := time.Now()
    err := method(source, target)
    duration := time.Since(start)

    if err != nil {
        return -1
    }

    return duration
}

// Usage
iotime := measureCopy(src, dst1, func(s, t string) error {
    return copyWithIoCopy(s, t)
})

platform := measureCopy(src, dst2, func(s, t string) error {
    return platformCopy(s, t)
})

fmt.Printf("io.Copy: %v\n", iotime)
fmt.Printf("Platform: %v (%.1fx faster)\n",
    platform,
    float64(iotime)/float64(platform))
```

---

## References

- [Linux man pages](https://man7.org/linux/man-pages/)
- [macOS Darwin kernel](https://opensource.apple.com/source/xnu/)
- [Windows API documentation](https://docs.microsoft.com/en-us/windows/win32/api/)
- [golang.org/x/sys](https://pkg.go.dev/golang.org/x/sys)
