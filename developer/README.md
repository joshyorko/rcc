# Developer setup helper

To give idea, what is needed to develop rcc. This is bootstrapping rcc
development with older version of rcc. So, you really need older rcc
installed somewhere available in PATH.

This developer toolkit uses both `tasks:` and `devTasks:` to enable tools.
Pay attention for `--dev` flag usage.

## One task to test the thing with robot

```
rcc run -r developer/toolkit.yaml -t robot
```

Then see `tmp/output/log.html` for possible failure details.

## Some developer tasks

### Unit tests

```
rcc run -r developer/toolkit.yaml --dev -t unitTests
```

You can also run tests running `invoke` directly from your CLI, or run `go test` - when running unit tests
outside of `invoke` however, make sure `GOARCH` env variable is set to `amd64`, as some tests may rely on it.

### Building the thing for local OS

```
rcc run -r developer/toolkit.yaml --dev -t local
```

### Building the thing (all OSes)

```
rcc run -r developer/toolkit.yaml --dev -t build
```

### Update documentation TOC

```
rcc run -r developer/toolkit.yaml --dev -t toc
```

### Show tools

```
rcc run -r developer/toolkit.yaml --dev -t tools
```

### Format and lint Go code

```
rcc run -r developer/toolkit.yaml --dev -t format
rcc run -r developer/toolkit.yaml --dev -t lint
rcc run -r developer/toolkit.yaml --dev -t lintAll
rcc run -r developer/toolkit.yaml --dev -t lintFix
```

`lint` and `lintFix` operate on changed code. `lintFix` applies only fixes
supported by golangci-lint, so review its diff before committing. `lintAll`
reports the repository-wide backlog and exits nonzero while findings remain.

## Dependencies

Needed dependencies are visible at `developer/setup.yaml` file.
