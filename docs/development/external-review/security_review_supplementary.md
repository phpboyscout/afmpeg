# Supplementary Security Review: Deep Dive

This document details highly specific security vulnerabilities and stability risks discovered in the `ffmpeg-wasi` and Go-wrapper codebase.

## 1. Missing Wazero Memory Limits (OOM Risk)
**Location:** `afmpeg/pkg/afmpeg/runtime.go`, `New()` function.

**Analysis:**
The `wazero` runtime is initialized as follows:
```go
rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
    WithCoreFeatures(runtimeCoreFeatures).
    WithCloseOnContextDone(true))
```
Wazero allows you to enforce strict upper limits on the amount of memory a WASM guest can allocate using `.WithMemoryLimitPages()`. Because this is omitted, the `ffmpeg-wasi` guest can continue allocating memory up to the maximum permitted by the WASM spec (usually 4GB for 32-bit WASM).
If an attacker provides a "zip-bomb" video (a tiny file that expands to massive dimensions, e.g., 65535x65535 pixels), `libavcodec` will attempt to allocate Gigabytes of RAM for the uncompressed frames. This will consume the host Go process's memory and likely trigger an OS-level OOM kill, taking down the entire application.

**Recommendation:**
Enforce a strict memory limit on the `RuntimeConfig` (e.g., `WithMemoryLimitPages(wazero.MemoryLimitPages(512 * 1024 * 1024 / 65536))`).

## 2. Unbounded Context/Deadlock Risks
**Location:** `afmpeg/pkg/afmpeg/runtime.go`

**Analysis:**
The code relies heavily on `WithCloseOnContextDone(true)` to abort hanging jobs. This is generally safe, but heavily relies on the consumer passing a `context.Context` that actually has a timeout. If a user passes `context.Background()`, a maliciously crafted stream could theoretically cause an infinite loop within an older or edge-case `libavcodec` decoder, permanently hanging the worker thread and locking the `r.mu` Mutex in `Run()`.

**Recommendation:**
The `RunJob` wrapper should enforce a hard, non-negotiable maximum timeout internally (e.g., 1 hour), or return an error if a deadline is not present on the provided context.

## 3. cJSON Edge Cases
**Location:** `process.c`, `parse_output()`

**Analysis:**
The JSON parsing relies on `cJSON_GetStringValue`.
```c
o->path = cJSON_GetStringValue(cJSON_GetObjectItemCaseSensitive(spec, "path"));
```
If the `"path"` key exists but the value is, for example, a JSON Object `{}` instead of a string, `cJSON_GetStringValue` returns `NULL`. The code handles this via `if (!o->path)`, which is safe.
However, in the options mapping loop:
```c
const cJSON *opts = cJSON_GetObjectItemCaseSensitive(spec, "options"), *kv = NULL;
cJSON_ArrayForEach(kv, opts) {
    if (cJSON_IsString(kv)) av_dict_set(&o->enc_opts, kv->string, kv->valuestring, 0);
}
```
If `opts` is accidentally passed as a string instead of a JSON object/array, `cJSON_ArrayForEach` might fail or behave unpredictably. 

**Recommendation:**
Explicitly check the types of nested JSON items before iterating over them (e.g., `if (cJSON_IsObject(opts))`).
