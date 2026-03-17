# Player Example

This example demonstrates `protoc-gen-fieldops` with a `PlayerService`.

## Setup

1. Generate the filtered reflection code:

```bash
buf generate
```

2. In your server setup, call `RegisterFilteredReflection` instead of `reflection.Register`:

```go
import playerv1 "github.com/lfreixial/proto-opscope/example/player/gen"

playerv1.RegisterFilteredReflection(grpcServer)
```

## What clients see

| RPC             | Visible fields                        |
|-----------------|---------------------------------------|
| `CreatePlayer`  | `name`, `email`, `team_id`            |
| `UpdatePlayer`  | `name`, `score`                       |
| `GetPlayer`     | `id`, `name`, `email`, `score`, `created_at` |

Clients using grpcurl or Postman will only see the fields relevant to each operation automatically.
