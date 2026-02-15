# Work Items & Custom Adapters

Scalable producer-consumer patterns with pluggable backends using `robocorp.workitems`.

## Work Items API Reference

### Input Work Item Properties & Methods

| Property/Method | Description |
|-----------------|-------------|
| `item.payload` | JSON payload data (dict, list, or scalar) |
| `item.files` | List of attached file names |
| `item.id` | Current work item ID |
| `item.state` | Current release state |
| `item.released` | Is the item released? |
| `item.saved` | Is the item saved? |
| `item.done()` | Mark as successfully processed and release |
| `item.fail(exception_type, code, message)` | Mark as failed and release |
| `item.get_file(name, path)` | Download attached file |
| `item.add_file(path, name)` | Attach file to work item |
| `item.create_output()` | Create child output work item |
| `item.email(html, encoding)` | Parse email attachment |

### Output Work Item Creation

```python
workitems.outputs.create(
    payload={"key": "value"},  # JSON serializable data
    files=["path/to/file.pdf"],  # Files to attach
    save=True  # Immediately save (default)
)
```

### Exception Types

| Type | When to Use |
|------|-------------|
| `"BUSINESS"` | Business logic error - don't retry (invalid data, rules violation) |
| `"APPLICATION"` | Technical error - may retry (timeout, connection, temp failure) |

## Work Items Basics

```python
from robocorp import workitems
from robocorp.tasks import task

@task
def producer():
    """Create work items for processing."""
    data_to_process = fetch_data_from_source()

    for record in data_to_process:
        workitems.outputs.create(
            payload={
                "id": record["id"],
                "data": record["content"],
                "metadata": {"source": "api", "timestamp": datetime.now().isoformat()}
            }
        )

    print(f"Created {len(data_to_process)} work items")

@task
def consumer():
    """Process work items with proper error handling."""
    for item in workitems.inputs:
        try:
            result = process_record(item.payload)

            # Pass results to next stage
            workitems.outputs.create(payload={
                "original_id": item.payload["id"],
                "result": result,
                "status": "success"
            })

            item.done()

        except BusinessException as e:
            # Business error - don't retry
            item.fail(exception_type="BUSINESS", code="INVALID_DATA", message=str(e))

        except Exception as e:
            # Application error - may retry
            item.fail(exception_type="APPLICATION", message=str(e))

@task
def reporter():
    """Generate final report from processed items."""
    results = []

    for item in workitems.inputs:
        results.append(item.payload)
        item.done()

    generate_report(results)
```

## Custom Work Item Adapters

Repository: https://github.com/joshyorko/robocorp_adapters_custom
PyPI: https://pypi.org/project/robocorp-adapters-custom/

### Supported Backends

| Backend | Best For | Use Case |
|---------|----------|----------|
| SQLite | Local dev, single-worker | Development, testing |
| Redis | High-throughput, multi-worker | Production, scaling |
| DocumentDB/MongoDB | AWS-native, distributed | Cloud deployments |

### Installation

```bash
pip install robocorp-adapters-custom
```

Current release (as of Feb 3, 2026): PyPI lists `0.1.3`. Some third-party trackers list `0.1.4`. Verify the latest on PyPI before pinning.

### Adapter Selection (Environment Variables)

Set `RC_WORKITEM_ADAPTER` to choose a backend:

- SQLite: `robocorp_adapters_custom._sqlite.SQLiteAdapter`
- Redis: `robocorp_adapters_custom._redis.RedisAdapter`
- DocumentDB/MongoDB: `robocorp_adapters_custom._docdb.DocumentDBAdapter`
- Yorko Control Room: `robocorp_adapters_custom._yorko_control_room.YorkoControlRoomAdapter`
Other required variables (per adapter):
- SQLite: `RC_WORKITEM_DB_PATH`
- DocumentDB: `DOCDB_HOSTNAME`, `DOCDB_PORT`, `DOCDB_USERNAME`, `DOCDB_PASSWORD`, `DOCDB_DATABASE`, optional `DOCDB_TLS_CERT`; or use `DOCDB_URI` instead of host/port/user/pass.
- Yorko Control Room: `YORKO_API_URL`, `YORKO_API_TOKEN`, `YORKO_WORKSPACE_ID`, `YORKO_WORKER_ID`

### SQLite Adapter

**Environment configuration (env-sqlite.json):**
```json
{
  "RC_WORKITEM_ADAPTER": "robocorp_adapters_custom._sqlite.SQLiteAdapter",
  "RC_WORKITEM_QUEUE_NAME": "my_queue",
  "RC_WORKITEM_DB_PATH": "./workitems.db"
}
```

**Run:**
```bash
# Producer
rcc run -t Producer -e devdata/env-sqlite.json

# Consumer (can run multiple)
rcc run -t Consumer -e devdata/env-sqlite.json
```

### Redis Adapter

**Environment configuration (env-redis.json):**
```json
{
  "RC_WORKITEM_ADAPTER": "robocorp_adapters_custom._redis.RedisAdapter",
  "RC_WORKITEM_QUEUE_NAME": "my_queue",
  "REDIS_HOST": "localhost",
  "REDIS_PORT": "6379",
  "REDIS_DB": "0"
}
```

**Redis with password:**
```json
{
  "RC_WORKITEM_ADAPTER": "robocorp_adapters_custom._redis.RedisAdapter",
  "RC_WORKITEM_QUEUE_NAME": "production_queue",
  "REDIS_URL": "redis://:password@redis.example.com:6379/0"
}
```

### DocumentDB/MongoDB Adapter

**Environment configuration (env-docdb.json):**
```json
{
  "RC_WORKITEM_ADAPTER": "robocorp_adapters_custom._docdb.DocumentDBAdapter",
  "RC_WORKITEM_QUEUE_NAME": "my_queue",
  "DOCDB_URI": "mongodb://user:pass@docdb.cluster.amazonaws.com:27017/?ssl=true",
  "DOCDB_DATABASE": "workitems",
  "DOCDB_TLS_CERT": "rds-combined-ca-bundle.pem"
}
```

## Multi-Stage Pipelines

Configure explicit output queues for chaining stages:

```
[Producer] → queue_raw → [Processor] → queue_processed → [Reporter]
```

**Producer config:**
```json
{
  "RC_WORKITEM_ADAPTER": "robocorp_adapters_custom._redis.RedisAdapter",
  "RC_WORKITEM_QUEUE_NAME": "queue_raw"
}
```

**Processor config:**
```json
{
  "RC_WORKITEM_ADAPTER": "robocorp_adapters_custom._redis.RedisAdapter",
  "RC_WORKITEM_QUEUE_NAME": "queue_raw",
  "RC_WORKITEM_OUTPUT_QUEUE_NAME": "queue_processed"
}
```

**Reporter config:**
```json
{
  "RC_WORKITEM_ADAPTER": "robocorp_adapters_custom._redis.RedisAdapter",
  "RC_WORKITEM_QUEUE_NAME": "queue_processed"
}
```

## Work Item Patterns

### Batch Processing

```python
@task
def batch_producer():
    """Create batched work items."""
    all_records = fetch_all_records()
    batch_size = 100

    for i in range(0, len(all_records), batch_size):
        batch = all_records[i:i + batch_size]
        workitems.outputs.create(payload={
            "batch_id": i // batch_size,
            "records": batch,
            "total_batches": (len(all_records) + batch_size - 1) // batch_size
        })

@task
def batch_consumer():
    """Process batches."""
    for item in workitems.inputs:
        batch = item.payload["records"]
        results = []

        for record in batch:
            result = process_record(record)
            results.append(result)

        workitems.outputs.create(payload={
            "batch_id": item.payload["batch_id"],
            "results": results
        })

        item.done()
```

### Error Recovery

```python
@task
def resilient_consumer():
    """Process with retry and dead-letter handling."""
    for item in workitems.inputs:
        retry_count = item.payload.get("_retry_count", 0)

        try:
            process_record(item.payload)
            item.done()

        except TransientError as e:
            if retry_count < 3:
                # Re-queue with incremented retry count
                workitems.outputs.create(payload={
                    **item.payload,
                    "_retry_count": retry_count + 1
                })
                item.done()
            else:
                # Max retries exceeded - send to dead letter
                item.fail(
                    exception_type="APPLICATION",
                    message=f"Max retries exceeded: {e}"
                )

        except PermanentError as e:
            item.fail(exception_type="BUSINESS", message=str(e))
```

### File Attachments

```python
@task
def producer_with_files():
    """Create work items with file attachments."""
    for file_path in get_files_to_process():
        with open(file_path, "rb") as f:
            workitems.outputs.create(
                payload={"filename": os.path.basename(file_path)},
                files=[f]
            )

@task
def consumer_with_files():
    """Process work items with files."""
    for item in workitems.inputs:
        # Files are automatically downloaded
        for file_path in item.files:
            process_file(file_path)

        item.done()
```

## Local Development

### Option 1: FileAdapter with Environment Variables

Set environment variables for local work item development:

```bash
# Enable FileAdapter for local development
export RC_WORKITEM_ADAPTER=FileAdapter
export RC_WORKITEM_INPUT_PATH=/path/to/your/input.json
export RC_WORKITEM_OUTPUT_PATH=/path/to/your/output.json
```

### Option 2: Using devdata folder

Create test work items in `devdata/work-items-in/`:

```
devdata/
├── work-items-in/
│   └── test-item-1/
│       ├── work-item.json
│       └── attachment.pdf
└── work-items-out/
```

**work-item.json:**
```json
{
  "payload": {
    "customer_id": "CUST001",
    "order_data": {"items": ["A", "B", "C"]}
  }
}
```

### robot.yaml for local testing

```yaml
tasks:
  Producer:
    shell: python -m robocorp.tasks run tasks.py -t producer

  Consumer:
    shell: python -m robocorp.tasks run tasks.py -t consumer

condaConfigFile: conda.yaml

# Local work items path
env:
  RC_WORKITEMS_PATH: devdata
```

### Simple Handle Pattern

```python
from robocorp import workitems
from robocorp.tasks import task

@task
def handle_item():
    """Handle single current work item."""
    item = workitems.inputs.current
    print("Received payload:", item.payload)
    workitems.outputs.create(payload={"key": "value"})

@task
def handle_all_items():
    """Iterate through all available work items."""
    for item in workitems.inputs:
        print("Received payload:", item.payload)
        workitems.outputs.create(payload={"key": "value"})
```

## Environment Variables Reference

| Variable | Purpose |
|----------|---------|
| `RC_WORKITEM_ADAPTER` | Custom adapter class path |
| `RC_WORKITEM_QUEUE_NAME` | Input queue name |
| `RC_WORKITEM_OUTPUT_QUEUE_NAME` | Output queue name (if different) |
| `RC_WORKITEMS_PATH` | Local work items directory |
| `SQLITE_DB_PATH` | SQLite database file path |
| `REDIS_URL` | Redis connection URL |
| `REDIS_HOST` | Redis host (if not using URL) |
| `REDIS_PORT` | Redis port |
| `MONGODB_URI` | MongoDB/DocumentDB connection string |
| `MONGODB_DATABASE` | Database name |

## Scaling Patterns

### Horizontal Scaling with Redis

```bash
# Start multiple consumers (different terminals/machines)
rcc run -t Consumer -e env-redis.json &
rcc run -t Consumer -e env-redis.json &
rcc run -t Consumer -e env-redis.json &

# Monitor queue
redis-cli LLEN queue_raw
```

### Docker Compose Scaling

```yaml
services:
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

  producer:
    build: .
    command: ["rcc", "run", "-t", "Producer"]
    environment:
      - RC_WORKITEM_ADAPTER=robocorp_adapters_custom._redis.RedisAdapter
      - REDIS_HOST=redis

  consumer:
    build: .
    command: ["rcc", "run", "-t", "Consumer"]
    environment:
      - RC_WORKITEM_ADAPTER=robocorp_adapters_custom._redis.RedisAdapter
      - REDIS_HOST=redis
    deploy:
      replicas: 5
    depends_on:
      - redis
      - producer
```

## Example Reference Implementation

Repository: https://github.com/joshyorko/fetch-repos-bot

User-provided reference repo. Verify the README for the current producer/consumer pattern, Assistant usage, and adapter wiring before reusing.
