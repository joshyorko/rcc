# Data Model: OCI Image Builder

Since RCC is a CLI tool, this data model describes the internal structures used to manage the build process and the resulting artifacts.

## Core Structures

### 1. OCIBuildPlan
Represents the configuration for a single image build operation.

```go
type OCIBuildPlan struct {
    // Input
    RobotYAML     string   // Path to robot.yaml
    RobotDir      string   // Root directory of the robot (context)
    BaseImage     string   // e.g., "debian:slim"
    Tags          []string // e.g., ["mybot:v1", "mybot:latest"]
    
    // Environment
    HolotreeSpace string   // Hash or path to the resolved environment to package
    RCCBinary     string   // Path to the linux-amd64 rcc binary to embed
    
    // Output
    TarballPath   string   // Where to save the resulting image tarball
    GenerateOnly  bool     // If true, only generate Dockerfile (User Story 5)
}
```

### 2. BuildManifest
A report generated after a successful build.

```go
type BuildManifest struct {
    ImageDigest   string    // sha256:...
    Tags          []string
    TotalSize     int64     // Bytes
    Layers        []LayerInfo
    GeneratedAt   time.Time
}

type LayerInfo struct {
    Digest    string
    Size      int64
    MediaType string
    CreatedBy string // Description (e.g., "RCC Binary", "Holotree Env")
}
```

## File System Artifacts

### Image Structure (Inside the Container)

The standard layout for an RCC-built image:

```text
/
├── bin/
│   └── rcc              # The embedded static binary
├── home/
│   └── rcc/
│       └── .rcc/        # Holotree location
├── app/                 # Robot code
│   ├── robot.yaml
│   ├── conda.yaml
│   └── ...
└── ... (Base Image OS)
```

### Env Vars (Inside the Container)

```env
PATH=/bin:$PATH
RCC_EXE=/bin/rcc
ROBOT_ROOT=/app
```
