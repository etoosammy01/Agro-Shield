# AGRO-014 template performance

## Finding and implementation

`ChatHandler.renderTemplate` and `NotificationHandler.NotificationsHandler` parsed their page files per request. `render.TMPL` already parses all pages once during package initialization, with shared functions and `html/template` contextual escaping. The handlers now call `render.RenderTemplates`; no new mutable cache or public interface was introduced. The existing renderer has no development reload switch; restart the app after editing templates, consistently with other pages. Parse errors retain the renderer's existing fail-fast startup behavior. Execution errors remain handled by the handlers; the old notification parse-failure message is replaced by startup validation.

`backend/render/cache_test.go` compares the old `ParseFiles().Execute()` path byte-for-byte with cached rendering for chat, thread and notifications, populated/empty/invalid/missing data, HTML metacharacters and malicious script text. It also tests missing names and concurrent rendering. Shared templates are never parsed or modified after initialization by this change.

## Measurements

Measured on 2026-11-03, linux/amd64, Intel Celeron N4020, Go 1.25.0 (the module's declared toolchain), default benchmark concurrency suffix 2. Three repetitions, identical deterministic fixtures, same process/environment, output to `io.Discard`. Other verification jobs had finished before this final measurement. An exploratory run under concurrent compilation was excluded because CPU contention inflated timings. Cached rendering is warmed before timing to exclude one-time contextual escaping; the baseline intentionally includes file reads, parsing and first contextual escaping on every render as the old request path did. Reported values are medians; raw measurements follow.

```sh
cd backend
go test ./render -run '^$' -bench '^BenchmarkTemplateRendering$' -benchmem -count=3
```

| Template | Repeated parse ns/op | Cached ns/op | Ratio | B/op before → after | Allocs/op before → after |
|---|---:|---:|---:|---:|---:|
| `chat.html` | 696,952 | 114,751 | 6.07× | 82,074 → 5,016 | 1,021 → 216 |
| `chat_thread.html` | 634,274 | 95,201 | 6.66× | 85,589 → 4,704 | 1,109 → 190 |
| `notification.html` | 280,623 | 44,638 | 6.29× | 52,716 → 1,880 | 401 → 80 |

These measurements support faster warm template rendering and fewer allocations for these fixtures. They are not HTTP throughput or production latency measurements. Filesystem cache, CPU scheduling, shared-machine load and fixture size influence timing; no statistical significance or end-to-end speedup is claimed.

## Raw output

```text
goos: linux
goarch: amd64
pkg: backend/render
cpu: Intel(R) Celeron(R) N4020 CPU @ 1.10GHz
BenchmarkTemplateRendering/chat.html/RepeatedParse-2         	    1868	    774971 ns/op	   82074 B/op	    1021 allocs/op
BenchmarkTemplateRendering/chat.html/RepeatedParse-2         	    1923	    658778 ns/op	   82076 B/op	    1021 allocs/op
BenchmarkTemplateRendering/chat.html/RepeatedParse-2         	    1629	    696952 ns/op	   82074 B/op	    1021 allocs/op
BenchmarkTemplateRendering/chat.html/Cached-2                	   10000	    124628 ns/op	    5016 B/op	     216 allocs/op
BenchmarkTemplateRendering/chat.html/Cached-2                	   10174	    114751 ns/op	    5016 B/op	     216 allocs/op
BenchmarkTemplateRendering/chat.html/Cached-2                	   10000	    109230 ns/op	    5016 B/op	     216 allocs/op
BenchmarkTemplateRendering/chat_thread.html/RepeatedParse-2  	    2072	    593724 ns/op	   85767 B/op	    1109 allocs/op
BenchmarkTemplateRendering/chat_thread.html/RepeatedParse-2  	    1782	    725358 ns/op	   85546 B/op	    1109 allocs/op
BenchmarkTemplateRendering/chat_thread.html/RepeatedParse-2  	    1922	    634274 ns/op	   85589 B/op	    1109 allocs/op
BenchmarkTemplateRendering/chat_thread.html/Cached-2         	   12385	    112307 ns/op	    4704 B/op	     190 allocs/op
BenchmarkTemplateRendering/chat_thread.html/Cached-2         	   13046	     95201 ns/op	    4704 B/op	     190 allocs/op
BenchmarkTemplateRendering/chat_thread.html/Cached-2         	   13845	     87332 ns/op	    4704 B/op	     190 allocs/op
BenchmarkTemplateRendering/notification.html/RepeatedParse-2 	    3721	    280623 ns/op	   52716 B/op	     401 allocs/op
BenchmarkTemplateRendering/notification.html/RepeatedParse-2 	    4345	    282987 ns/op	   52716 B/op	     401 allocs/op
BenchmarkTemplateRendering/notification.html/RepeatedParse-2 	    4236	    273867 ns/op	   52716 B/op	     401 allocs/op
BenchmarkTemplateRendering/notification.html/Cached-2        	   30295	     50392 ns/op	    1880 B/op	      80 allocs/op
BenchmarkTemplateRendering/notification.html/Cached-2        	   23374	     43552 ns/op	    1880 B/op	      80 allocs/op
BenchmarkTemplateRendering/notification.html/Cached-2        	   27585	     44638 ns/op	    1880 B/op	      80 allocs/op
PASS
ok  	backend/render	29.511s
```

Final regression and race execution outcomes are recorded in `DB_INTEGRATION_TEST_REPORT.md`.
