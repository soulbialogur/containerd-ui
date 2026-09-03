# Core Concepts

This section explains the application's main architectural and behavioral principles. The other guides refer here instead of repeating the same information.

## 1. Two Infrastructure Access Layers

The application uses two access layers:

- containerd's gRPC API is the primary, fast path for reading state and starting or stopping containers;
- WSL + nerdctl is the fallback when the gRPC API is unavailable, temporarily fails, or stops responding.

This makes the application resilient in a Windows + WSL2 environment: some operations run directly in Linux, while others use the containerd API.

All WSL commands run inside the selected distribution, usually `Ubuntu-24.04`. The UI uses caching to stay responsive and rereads data only when necessary.

## 2. Caching and Invalidation

The application uses a centralized `CacheManager`.

Its responsibilities are to:

- cache data for containers, images, volumes, networks, and statistics;
- track state-change events;
- invalidate stale entries when state changes;
- collect basic metrics (`hits`, `misses`, `errors`, `hitRate`).

### Invalidation Algorithm

Invalidation is event-driven rather than timer-driven. When state changes, `GlobalCacheManager.Invalidate(eventType, reason)` is called and:

1. a `CacheEvent` is created with a type such as `CacheEventContainers`, `CacheEventImages`, `CacheEventVolumes`, `CacheEventStats`, or `CacheEventAll`;
2. `Publish()` records the event in the recent history and notifies subscribers;
3. the matching cache cleanup method is called, such as `CDInvalidateContainersCache()` or `CDInvalidateImagesCache()`;
4. the next UI refresh reads the current state.

Typical triggers include:

- creating or removing a container;
- starting, stopping, or restarting a container;
- updating an image;
- creating or removing a volume;
- refreshing resource statistics;
- manual cleanup or a bulk UI operation.

This prevents stale data in the UI and reduces repeated requests to WSL/containerd.

### How `CacheManager` Works

`CacheManager` stores:

- `events []CacheEvent` — recent invalidation events;
- `metrics map[string]*CacheMetrics` — counters for each cache (`hits`, `misses`, `errors`);
- `subscribers []func(CacheEvent)` — subscribers notified after an event.

Metrics are tracked by cache name, for example:

- containers;
- images;
- volumes;
- statistics;
- system resources.

After each event, `GetSummary()` returns a summary including `hitRate = hits / (hits + misses) * 100`.

### Cache Behavior to Keep in Mind

TTL and invalidation work together:

- TTL controls the natural expiration of an entry;
- invalidation events force cleanup immediately after a system state change.

As a result, the UI stays fast without getting stuck on stale data after bulk operations.

## 3. Resource-Saving Mode

The `economy_mode` setting disables background updates for inactive tabs. In this mode:

- the active tab continues to refresh;
- inactive tabs pause timers and background requests;
- CPU, WSL, and containerd usage is reduced.

This is especially useful when working for a long time with multiple tabs, many containers, or a slow WSL environment.

## 4. BuildKit and Concurrency

Builds and bulk operations use these settings:

- `max_parallelism` limits concurrent builds;
- `container_operation_concurrency` limits bulk container operations;
- `buildkit_cache_ttl` and `buildkit_max_size` control BuildKit cache cleanup by age and size.

These limits reduce system load and keep long-running development builds stable.

## 5. Operation Progress and Cancellation

Long-running tasks go through `OperationManager`, which:

- stores operation status;
- reports progress;
- marks completion;
- removes completed records after 30 seconds.

### Cooperative Cancellation

Cancellation does not interrupt a system call directly. When a long-running operation starts, it creates a `cancelCh` and passes it to the worker. The worker checks it at safe points, for example:

```go
select {
case <-cancelCh:
    return context.Canceled
default:
}
```

or a bulk-operation variant:

```go
for _, container := range containers {
    select {
    case <-cancelCh:
        close(jobs)
        workers.Wait()
        return context.Canceled
    default:
        jobs <- container
    }
}
```

The operation stops at a safe point instead of being cut off in the middle of a WSL or `nerdctl` call. This matters especially for builds, bulk start/stop operations, and environment cleanup.

### What `OperationManager` Does

`OperationManager` stores state by operation ID and updates the UI through the `onUpdate` callback. When an operation succeeds or fails:

- `Progress` is set to `1.0`;
- `Finished` becomes `true`;
- `FinishedAt` stores the completion time;
- the record is removed automatically after 30 seconds.

This gives users progress information without filling the UI with old operation history.

## 6. Developer Architecture

The code is organized around three main layers:

- `ui/` — the Fyne UI, tables, buttons, progress indicators, tabs, and dialogs;
- `wsl/` — the WSL, `containerd`, `nerdctl`, and `buildkitd` integration layer, including validation and caches;
- `main.go` and configuration — the entry point and application initialization.

Typical data flow:

1. the user clicks a UI button;
2. `ui/*` builds the context and calls a function in `wsl/*`;
3. `wsl` validates the environment, logs the operation, and calls `RunWSLWithCancel` or a gRPC method;
4. the result returns to the UI, while `CacheManager` or `OperationManager` updates the state.

General practices:

- state changes should publish a cache invalidation event;
- long-running actions should pass `cancelCh` and check it at safe points;
- the UI should read from the cache and update through `safeUI` or a callback rather than directly from background goroutines.

## 7. Further Reading

For practical setup and diagnostic examples, see:

- [quickstart.md](quickstart.md)
- [installation.md](installation.md)
- [configuration.md](configuration.md)
- [deployment.md](deployment.md)
- [diagnostics.md](diagnostics.md)
- [project-requirements.md](project-requirements.md)
