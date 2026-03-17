# proto-opscope

`protoc-gen-fieldops` is a protoc plugin that generates a filtered gRPC reflection server. By annotating your proto fields with `field_op` and your RPC methods with `rpc_op`, the reflection server automatically shows only the fields relevant to each operation (CREATE, READ, UPDATE, DELETE). This means tools like Postman, grpcurl, and Evans will surface the right schema per endpoint — no client changes required.

## Quick Start

### 1. Install the plugin

```bash
go install github.com/lfreixial/proto-opscope/cmd/protoc-gen-fieldops@latest
```

### 2. Annotate your proto

```protobuf
import "fieldops/v1/options.proto";

message Player {
  string id    = 1 [(field_op) = OPERATION_READ];
  string name  = 2 [(field_op) = OPERATION_CREATE, (field_op) = OPERATION_READ, (field_op) = OPERATION_UPDATE];
  string email = 3 [(field_op) = OPERATION_CREATE, (field_op) = OPERATION_READ];
}

service PlayerService {
  rpc CreatePlayer(Player) returns (Player) {
    option (rpc_op) = OPERATION_CREATE;
  }
  rpc GetPlayer(GetPlayerRequest) returns (Player) {
    option (rpc_op) = OPERATION_READ;
  }
}
```

### 3. Add to buf.gen.yaml

```yaml
version: v2
plugins:
  - local: protoc-gen-fieldops
    out: gen
    opt: paths=source_relative
```

### 4. Use RegisterFilteredReflection

In your server setup, replace `reflection.Register(s)` with:

```go
playerv1.RegisterFilteredReflection(grpcServer)
```

## Operation Values

| Value                | Meaning            |
|----------------------|--------------------|
| `OPERATION_CREATE`   | POST / Create      |
| `OPERATION_READ`     | GET / Read         |
| `OPERATION_UPDATE`   | PUT/PATCH / Update |
| `OPERATION_DELETE`   | DELETE / Delete    |

## Example: PlayerService

```protobuf
message Player {
  string id         = 1 [(field_op) = OPERATION_READ];
  string name       = 2 [(field_op) = OPERATION_CREATE, (field_op) = OPERATION_READ, (field_op) = OPERATION_UPDATE];
  string email      = 3 [(field_op) = OPERATION_CREATE, (field_op) = OPERATION_READ];
  string team_id    = 4 [(field_op) = OPERATION_CREATE];
  int32  score      = 5 [(field_op) = OPERATION_UPDATE, (field_op) = OPERATION_READ];
  string created_at = 6 [(field_op) = OPERATION_READ];
}
```

### What Postman / grpcurl sees per endpoint

| RPC             | Visible fields                               |
|-----------------|----------------------------------------------|
| `CreatePlayer`  | `name`, `email`, `team_id`                   |
| `UpdatePlayer`  | `name`, `score`                              |
| `GetPlayer`     | `id`, `name`, `email`, `score`, `created_at` |

## Note for API consumers

API consumers using reflection-based tools need to do nothing — the filtered reflection is fully transparent. The server automatically serves the correct schema for each RPC.